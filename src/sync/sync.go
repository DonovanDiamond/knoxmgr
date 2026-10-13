// Package sync copies the Knox tenant (orgs, users, USER groups, memberships, devices) into the
// local mirror on a schedule and on demand. It never runs two syncs at once.
package sync

import (
	"context"
	"errors"
	"fmt"
	stdsync "sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/DonovanDiamond/knoxmgr/src/db"
	"github.com/DonovanDiamond/knoxmgr/src/knox"
)

const (
	pageSize        = 1000
	maxPages        = 20 // bounds a runaway Knox pager
	defaultInterval = 15 * time.Minute
)

// ErrNotConfigured means there is no Knox connection to sync from.
var ErrNotConfigured = errors.New("knox is not configured")

// Scheduler owns the sync loop and the single-flight run.
type Scheduler struct {
	mirror  *db.Mirror
	knox    func() *knox.Client // nil while Knox is not configured
	minutes func(context.Context) (int, error)
	log     zerolog.Logger
	synced  func(ctx context.Context, trigger string) // called after each successful run; must not block

	wake    chan struct{} // Knox settings saved
	resched chan struct{} // interval changed

	wg      stdsync.WaitGroup // in-flight run; lets tests wait for it
	mu      stdsync.Mutex
	base    context.Context // background runs outlive the request that triggered them
	running bool
	runID   int64
}

// New returns a Scheduler. minutes yields the configured sync interval in minutes.
func New(m *db.Mirror, k func() *knox.Client, minutes func(context.Context) (int, error), log zerolog.Logger) *Scheduler {
	return &Scheduler{mirror: m, knox: k, minutes: minutes, log: log,
		wake: make(chan struct{}, 1), resched: make(chan struct{}, 1), base: context.Background()}
}

// OnSynced sets a hook called after every successful run, before the next one can start. Call it before Start.
func (s *Scheduler) OnSynced(f func(ctx context.Context, trigger string)) { s.synced = f }

// Running returns the id of the run in progress, if any.
func (s *Scheduler) Running() (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runID, s.running
}

// SyncNow records a run and starts it in the background. If one is already running it returns
// that run's id with alreadyRunning set.
func (s *Scheduler) SyncNow(ctx context.Context) (id int64, alreadyRunning bool, err error) {
	return s.begin(ctx, "manual")
}

func (s *Scheduler) begin(ctx context.Context, trigger string) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return s.runID, true, nil
	}
	k := s.knox()
	if k == nil {
		return 0, false, ErrNotConfigured
	}
	id, err := s.mirror.StartRun(ctx, trigger)
	if err != nil {
		return 0, false, err
	}
	s.running, s.runID = true, id
	s.wg.Add(1)
	go s.run(k, id, trigger)
	return id, false, nil
}

// KnoxSaved asks for a sync as soon as possible; it never blocks.
func (s *Scheduler) KnoxSaved() { signal(s.wake) }

// Reschedule makes the loop re-read the interval; it never blocks.
func (s *Scheduler) Reschedule() { signal(s.resched) }

func signal(c chan struct{}) {
	select {
	case c <- struct{}{}:
	default:
	}
}

// Start syncs once if Knox is configured, then on every interval until ctx is done.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	s.base = ctx
	s.mu.Unlock()

	s.tick(ctx, "startup")
	timer := time.NewTimer(s.interval(ctx))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.tick(ctx, "scheduled")
		case <-s.wake:
			s.tick(ctx, "knox_saved")
		case <-s.resched:
		}
		// Every wake-up restarts the countdown, so a settings change takes effect now.
		timer.Reset(s.interval(ctx))
	}
}

func (s *Scheduler) tick(ctx context.Context, trigger string) {
	_, _, err := s.begin(ctx, trigger)
	switch {
	case errors.Is(err, ErrNotConfigured):
		s.log.Debug().Str("trigger", trigger).Msg("sync skipped: knox not configured")
	case err != nil:
		s.log.Error().Err(err).Str("trigger", trigger).Msg("could not start sync")
	}
}

func (s *Scheduler) interval(ctx context.Context) time.Duration {
	n, err := s.minutes(ctx)
	if err != nil || n <= 0 {
		return defaultInterval
	}
	return time.Duration(n) * time.Minute
}

func (s *Scheduler) run(k *knox.Client, id int64, trigger string) {
	defer s.wg.Done()
	s.mu.Lock()
	ctx := s.base
	s.mu.Unlock()

	snap, err := fetch(ctx, k)
	if err == nil {
		err = s.mirror.ReplaceAll(ctx, snap, time.Now())
	}
	status, msg := "ok", ""
	counts := db.SyncCounts{Orgs: len(snap.Orgs), Users: len(snap.Users), Groups: len(snap.Groups),
		Memberships: len(snap.Members), Devices: len(snap.Devices)}
	if err != nil {
		status, msg, counts = "failed", err.Error(), db.SyncCounts{}
		s.log.Error().Err(err).Int64("run", id).Msg("knox sync failed; mirror unchanged")
	}
	// Not ctx: a shutdown must still close the run row.
	if ferr := s.mirror.FinishRun(context.WithoutCancel(ctx), id, status, msg, counts); ferr != nil {
		s.log.Error().Err(ferr).Int64("run", id).Msg("could not record sync result")
	}
	if err == nil {
		s.log.Info().Int64("run", id).Int("users", counts.Users).Int("devices", counts.Devices).Msg("knox sync finished")
		if s.synced != nil {
			s.synced(ctx, trigger)
		}
	}
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
}

// collect drains a Knox pager. Hitting the page bound is an error: a truncated snapshot would
// delete mirrored rows.
func collect[T any](fetch func(start int) ([]T, int, error)) ([]T, error) {
	out := []T{}
	for range maxPages {
		items, total, err := fetch(len(out))
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if len(items) == 0 || len(out) >= total {
			return out, nil
		}
	}
	return nil, fmt.Errorf("more than %d records; refusing a partial sync", maxPages*pageSize)
}

func fetch(ctx context.Context, k *knox.Client) (db.Snapshot, error) {
	var snap db.Snapshot
	orgs, err := k.SelectOrgList(ctx, "")
	if err != nil {
		return snap, fmt.Errorf("orgs: %w", err)
	}
	users, err := collect(func(start int) ([]knox.User, int, error) {
		pg, err := k.SelectUsers(ctx, knox.UserQuery{Start: start, Limit: pageSize})
		return pg.Users, int(pg.Total), err
	})
	if err != nil {
		return snap, fmt.Errorf("users: %w", err)
	}
	groups, err := collect(func(start int) ([]knox.Group, int, error) {
		pg, err := k.SelectGroups(ctx, knox.GroupQuery{Start: start, Limit: pageSize, WithCounts: true})
		return pg.Groups, int(pg.Total), err
	})
	if err != nil {
		return snap, fmt.Errorf("groups: %w", err)
	}
	devices, err := collect(func(start int) ([]knox.Device, int, error) {
		pg, err := k.SelectDeviceList(ctx, knox.DeviceQuery{Start: start, Limit: pageSize})
		return pg.Devices, int(pg.Total), err
	})
	if err != nil {
		return snap, fmt.Errorf("devices: %w", err)
	}

	snap = db.Snapshot{}
	for _, o := range orgs {
		snap.Orgs = append(snap.Orgs, db.MirrorOrg(o))
	}
	owners := make(map[string]knox.User, len(users))
	for _, u := range users {
		owners[u.UserID] = u
		snap.Users = append(snap.Users, db.MirrorUser{
			UserID: u.UserID, UserName: u.UserName, Email: u.Email, FirstName: u.FirstName, LastName: u.LastName,
			MobilePhone: u.MobilePhone, Contact: u.Contact, EmpNo: u.EmpNo, OrgCode: u.OrgCode, OrgName: u.OrgName,
			Enabled: u.Enabled == "1", UserType: u.UserType, DeviceCount: int(u.DeviceCount),
		})
	}
	for _, g := range groups {
		if g.GroupType != "USER" {
			continue
		}
		snap.Groups = append(snap.Groups, db.MirrorGroup{GroupID: g.GroupID, GroupName: g.GroupName, GroupType: g.GroupType,
			AppCount: int(g.AssignAppCount), ProfileCount: int(g.AssignProfileCount)})
		members, err := collect(func(start int) ([]knox.User, int, error) {
			pg, err := k.SelectGroupUsers(ctx, g.GroupID, start, pageSize)
			return pg.Users, int(pg.Total), err
		})
		if err != nil {
			return db.Snapshot{}, fmt.Errorf("members of group %s: %w", g.GroupID, err)
		}
		for _, m := range members {
			if _, known := owners[m.UserID]; known {
				snap.Members = append(snap.Members, db.MirrorMember{GroupID: g.GroupID, UserID: m.UserID})
			}
		}
	}
	for _, d := range devices {
		org := d.OrgCode
		if o, ok := owners[d.UserID]; ok {
			org = o.OrgCode
		}
		snap.Devices = append(snap.Devices, db.MirrorDevice{
			DeviceID: d.DeviceID, DeviceModel: d.DeviceModel, DeviceModelKind: d.DeviceModelKind, DeviceStatus: d.DeviceStatus,
			DeviceVersionName: d.DeviceVersionName, Platform: d.Platform, UserID: d.UserID, UserName: d.UserName,
			OrgCode: org, LastConnection: d.LastConnection,
		})
	}
	return snap, nil
}

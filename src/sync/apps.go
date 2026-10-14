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
	inventoryWorkers = 4
	inventoryCheck   = time.Minute // how often Start re-reads the settings and the persisted timestamps
	reportEvery      = 24 * time.Hour
	androidPlatform  = "A"
)

// ErrInventoryRunning means a full inventory pull is already in progress.
var ErrInventoryRunning = errors.New("app inventory pull already running")

// Inventory keeps the mirrored per-device app lists fresh.
type Inventory struct {
	mirror *db.Mirror
	apps   *db.Apps
	audit  *db.Audit
	knox   func() *knox.Client // nil while Knox is not configured
	hours  func(context.Context) int
	daily  func(context.Context) bool
	log    zerolog.Logger

	pulling stdsync.Mutex // held for the whole of a full pull
	now     func() time.Time
}

func NewInventory(m *db.Mirror, a *db.Apps, au *db.Audit, k func() *knox.Client,
	hours func(context.Context) int, daily func(context.Context) bool, log zerolog.Logger) *Inventory {
	return &Inventory{mirror: m, apps: a, audit: au, knox: k, hours: hours, daily: daily, log: log, now: time.Now}
}

// PullDevice replaces one device's mirrored app list with Knox's current one.
func (i *Inventory) PullDevice(ctx context.Context, deviceID string) error {
	_, _, err := i.pullDevice(ctx, deviceID)
	return err
}

// pullDevice is PullDevice, also returning how many rows were stored and the app-list
// diagnostic when Knox sent no apps.
func (i *Inventory) pullDevice(ctx context.Context, deviceID string) (int, *knox.AppListDiagnostic, error) {
	k := i.knox()
	if k == nil {
		return 0, nil, ErrNotConfigured
	}
	list, diag, err := k.SelectDeviceAppList(ctx, deviceID)
	if err != nil {
		return 0, nil, err
	}
	rows := make([]db.DeviceAppRow, 0, len(list))
	for _, a := range list {
		r := db.DeviceAppRow{Package: a.PackageName, Name: a.AppName, Version: a.VersionName,
			System: a.System(), Managed: a.Managed(), Mandatory: a.Mandatory(), KnoxID: a.KnoxID}
		if a.Installed != "" {
			t := a.Installed
			r.InstalledAt = &t
		}
		rows = append(rows, r)
	}
	return len(rows), diag, i.apps.ReplaceDeviceApps(ctx, deviceID, rows, i.now())
}

// enrolledDevices lists the mirrored devices whose status says they are enrolled.
func (i *Inventory) enrolledDevices(ctx context.Context) ([]db.MirrorDevice, error) {
	all, err := i.mirror.Devices(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, d := range all {
		if knox.IsEnrolled(d.DeviceStatus) {
			out = append(out, d)
		}
	}
	return out, nil
}

// forEach runs fn over devices with a bounded pool and returns how many succeeded and failed.
func forEach(ctx context.Context, devices []db.MirrorDevice, fn func(db.MirrorDevice) error) (ok, failed int) {
	var mu stdsync.Mutex
	var wg stdsync.WaitGroup
	ch := make(chan db.MirrorDevice)
	for range inventoryWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range ch {
				err := fn(d)
				mu.Lock()
				if err != nil {
					failed++
				} else {
					ok++
				}
				mu.Unlock()
			}
		}()
	}
	for _, d := range devices {
		if ctx.Err() != nil {
			break
		}
		ch <- d
	}
	close(ch)
	wg.Wait()
	return ok, failed
}

// PullAll refreshes every enrolled device. A device that fails is logged and counted, never fatal.
func (i *Inventory) PullAll(ctx context.Context) error {
	if !i.pulling.TryLock() {
		return ErrInventoryRunning
	}
	defer i.pulling.Unlock()
	if i.knox() == nil {
		return ErrNotConfigured
	}
	devices, err := i.enrolledDevices(ctx)
	if err != nil {
		return fmt.Errorf("listing devices: %w", err)
	}
	var mu stdsync.Mutex
	stored, empty, warned := 0, 0, false
	ok, failed := forEach(ctx, devices, func(d db.MirrorDevice) error {
		n, diag, err := i.pullDevice(ctx, d.DeviceID)
		if err != nil {
			i.log.Warn().Err(err).Str("device", d.DeviceID).Msg("app inventory pull failed for device")
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		stored += n
		if diag != nil {
			empty++
			if !warned { // one sample per pull is enough to see the payload shape
				warned = true
				i.log.Warn().Str("device", d.DeviceID).Str("resultValueType", diag.ResultType).Interface("keys", diag.Keys).
					Str("total", diag.Total).Strs("otherAppKeys", diag.AppKeys).Msg("Knox returned no apps for device")
			}
		}
		return err
	})
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := i.apps.SetLastPull(ctx, i.now()); err != nil {
		return err
	}
	i.log.Info().Int("devices", ok).Int("failed", failed).Int("appRows", stored).Int("devicesWithoutApps", empty).Msg("app inventory pulled")
	return nil
}

// requestReports asks every enrolled Android device to report its apps again and records one audit row.
func (i *Inventory) requestReports(ctx context.Context) error {
	k := i.knox()
	if k == nil {
		return ErrNotConfigured
	}
	devices, err := i.enrolledDevices(ctx)
	if err != nil {
		return fmt.Errorf("listing devices: %w", err)
	}
	android := devices[:0]
	for _, d := range devices {
		if d.Platform == androidPlatform {
			android = append(android, d)
		}
	}
	sent, failed := forEach(ctx, android, func(d db.MirrorDevice) error {
		err := k.SyncInstalledAppList(ctx, d.DeviceID)
		if err != nil {
			i.log.Warn().Err(err).Str("device", d.DeviceID).Msg("app report request failed for device")
		}
		return err
	})
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := i.apps.SetLastReportRequest(ctx, i.now()); err != nil {
		return err
	}
	val := fmt.Sprintf("sent=%d failed=%d", sent, failed)
	return i.audit.Insert(ctx, &db.AuditEntry{ActorUsername: "system", Action: "device.apps.report_request", Target: "all-devices", NewValue: &val})
}

// AfterSync is the Scheduler hook: an admin's manual Sync now pulls every app list, other runs only when due.
func (i *Inventory) AfterSync(ctx context.Context, trigger string) {
	if trigger != "manual" {
		i.PullIfDue(ctx)
		return
	}
	if err := i.PullAll(ctx); err != nil && !errors.Is(err, ErrNotConfigured) && !errors.Is(err, ErrInventoryRunning) && ctx.Err() == nil {
		i.log.Error().Err(err).Msg("app inventory pull failed")
	}
}

// PullIfDue runs a full pull when the configured interval has passed since the persisted last pull.
func (i *Inventory) PullIfDue(ctx context.Context) {
	st, err := i.apps.Inventory(ctx)
	if err != nil {
		i.log.Error().Err(err).Msg("app inventory state unreadable")
		return
	}
	every := time.Duration(max(i.hours(ctx), 1)) * time.Hour
	if st.LastPullAt != nil && i.now().Sub(*st.LastPullAt) < every {
		return
	}
	if err := i.PullAll(ctx); err != nil && !errors.Is(err, ErrNotConfigured) && !errors.Is(err, ErrInventoryRunning) && ctx.Err() == nil {
		i.log.Error().Err(err).Msg("app inventory pull failed")
	}
}

func (i *Inventory) reportIfDue(ctx context.Context) {
	if !i.daily(ctx) || i.knox() == nil {
		return
	}
	st, err := i.apps.Inventory(ctx)
	if err != nil {
		i.log.Error().Err(err).Msg("app inventory state unreadable")
		return
	}
	if st.LastReportRequestAt != nil && i.now().Sub(*st.LastReportRequestAt) < reportEvery {
		return
	}
	if err := i.requestReports(ctx); err != nil && ctx.Err() == nil {
		i.log.Error().Err(err).Msg("app report request failed")
	}
}

// Start checks once a minute whether a pull or a report request is due, until ctx is done. Due-ness
// comes from persisted timestamps, so a restart does not repeat work early.
func (i *Inventory) Start(ctx context.Context) {
	t := time.NewTicker(inventoryCheck)
	defer t.Stop()
	for {
		i.reportIfDue(ctx)
		i.PullIfDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

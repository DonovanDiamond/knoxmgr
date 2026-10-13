package sync

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	stdsync "sync"
	"testing"

	"github.com/rs/zerolog"

	"github.com/DonovanDiamond/knoxmgr/src/config"
	"github.com/DonovanDiamond/knoxmgr/src/db"
	"github.com/DonovanDiamond/knoxmgr/src/knox"
	"github.com/DonovanDiamond/knoxmgr/src/knoxmock"
)

// newEnv returns a scheduler wired to a fresh mock and temp SQLite. If gate is non-nil, the first
// user listing blocks until it is closed.
func newEnv(t *testing.T, gate chan struct{}) (*Scheduler, *db.Mirror, *knox.Client) {
	t.Helper()
	var once stdsync.Once
	mock := knoxmock.New("cid", "secret", zerolog.Nop())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gate != nil && strings.HasSuffix(r.URL.Path, "/selectUsers") {
			once.Do(func() { <-gate })
		}
		mock.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	d, err := db.Open("sqlite", filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	k := knox.New(config.Knox{BaseURL: srv.URL, TokenURL: srv.URL + "/emm/oauth/token", ClientID: "cid", TenantID: "mock.local",
		ClientSecret: "secret", AllowInsecureHTTP: true}, nil)
	m := db.NewMirror(d)
	s := New(m, func() *knox.Client { return k }, func(context.Context) (int, error) { return 15, nil }, zerolog.Nop())
	return s, m, k
}

func counts(t *testing.T, m *db.Mirror) db.SyncCounts {
	t.Helper()
	ctx := context.Background()
	orgs, _ := m.Orgs(ctx)
	users, _ := m.Users(ctx, nil)
	groups, _ := m.Groups(ctx)
	devs, _ := m.Devices(ctx, nil)
	c := db.SyncCounts{Orgs: len(orgs), Users: len(users), Groups: len(groups), Devices: len(devs)}
	for _, g := range groups {
		c.Memberships += g.UserCount
	}
	return c
}

func TestSyncMirrorsKnox(t *testing.T) {
	ctx := context.Background()
	s, m, k := newEnv(t, nil)

	id, already, err := s.SyncNow(ctx)
	if err != nil || already {
		t.Fatalf("SyncNow: id=%d already=%v err=%v", id, already, err)
	}
	s.wg.Wait()

	want := db.SyncCounts{Orgs: 4, Users: 6, Groups: 2, Memberships: 5, Devices: 6} // knoxmock seed
	if got := counts(t, m); got != want {
		t.Fatalf("mirror counts = %+v, want %+v", got, want)
	}
	runs, _ := m.RecentRuns(ctx, 10)
	if len(runs) != 1 || runs[0].ID != id || runs[0].Status != "ok" || runs[0].SyncCounts != want || runs[0].FinishedAt == nil {
		t.Fatalf("runs = %+v", runs)
	}
	if u, err := m.User(ctx, "bob"); err != nil || !u.Enabled || u.OrgCode != "SALES" {
		t.Fatalf("bob = %+v, %v", u, err)
	}

	if err := k.PurgeUser(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SyncNow(ctx); err != nil {
		t.Fatal(err)
	}
	s.wg.Wait()
	if _, err := m.User(ctx, "alice"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("alice still mirrored: %v", err)
	}
	want.Users, want.Memberships = 5, 4
	if got := counts(t, m); got != want {
		t.Fatalf("after purge counts = %+v, want %+v", got, want)
	}
}

func TestSingleFlight(t *testing.T) {
	ctx := context.Background()
	gate := make(chan struct{})
	s, m, _ := newEnv(t, gate)

	id, already, err := s.SyncNow(ctx)
	if err != nil || already {
		t.Fatalf("first: id=%d already=%v err=%v", id, already, err)
	}
	if rid, ok := s.Running(); !ok || rid != id {
		t.Fatalf("Running = %d,%v", rid, ok)
	}
	id2, already, err := s.SyncNow(ctx)
	if err != nil || !already || id2 != id {
		t.Fatalf("second: id=%d already=%v err=%v, want %d,true", id2, already, err, id)
	}
	close(gate)
	s.wg.Wait()
	if _, ok := s.Running(); ok {
		t.Fatal("still running")
	}
	if runs, _ := m.RecentRuns(ctx, 10); len(runs) != 1 {
		t.Fatalf("want one run row, got %d", len(runs))
	}
}

func TestNotConfiguredAndFailure(t *testing.T) {
	ctx := context.Background()
	s, m, _ := newEnv(t, nil)
	s.wg.Wait()

	s.knox = func() *knox.Client { return nil }
	if _, _, err := s.SyncNow(ctx); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v", err)
	}
	s.tick(ctx, "scheduled")
	if runs, _ := m.RecentRuns(ctx, 10); len(runs) != 0 {
		t.Fatalf("unconfigured must not record runs: %+v", runs)
	}

	// Wrong credentials: the run fails and the mirror keeps its rows.
	s2, m2, _ := newEnv(t, nil)
	if _, _, err := s2.SyncNow(ctx); err != nil {
		t.Fatal(err)
	}
	s2.wg.Wait()
	before := counts(t, m2)
	bad := knox.New(config.Knox{BaseURL: "http://127.0.0.1:1", TokenURL: "http://127.0.0.1:1/t", ClientID: "x", TenantID: "t",
		ClientSecret: "y", AllowInsecureHTTP: true}, nil)
	s2.knox = func() *knox.Client { return bad }
	if _, _, err := s2.SyncNow(ctx); err != nil {
		t.Fatal(err)
	}
	s2.wg.Wait()
	runs, _ := m2.RecentRuns(ctx, 1)
	if runs[0].Status != "failed" || runs[0].Error == "" || counts(t, m2) != before {
		t.Fatalf("run = %+v, counts %+v want %+v", runs[0], counts(t, m2), before)
	}
}

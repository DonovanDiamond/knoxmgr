package api

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DonovanDiamond/knoxmgr/src/db"
	"github.com/DonovanDiamond/knoxmgr/src/knoxmock"
)

func TestUserGroups(t *testing.T) {
	tests := []struct {
		name, user, path, want string
		status                 int
	}{
		{"admin", "root", "/api/knox/users/bob/groups", knoxmock.GroupPilot, 200},
		{"admin, two groups none", "root", "/api/knox/users/frank/groups", "", 200},
		{"reader in scope", "reader", "/api/knox/users/dave/groups", knoxmock.GroupPilot, 200},
		{"reader out of scope", "reader", "/api/knox/users/alice/groups", "", 404},
		{"unknown", "root", "/api/knox/users/zed/groups", "", 404},
		{"bad id", "root", "/api/knox/users/a;b/groups", "", 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			resp := do(t, e.app, "GET", tt.path, "", e.token(t, tt.user))
			if resp.StatusCode != tt.status {
				t.Fatalf("status %d, want %d", resp.StatusCode, tt.status)
			}
			if tt.status != 200 {
				return
			}
			var body struct {
				Groups []struct {
					GroupID   string `json:"groupId"`
					UserCount int    `json:"userCount"`
				}
				Total int
			}
			decode(t, resp, &body)
			var ids []string
			for _, g := range body.Groups {
				ids = append(ids, g.GroupID)
			}
			if got := strings.Join(ids, " "); got != tt.want || body.Total != len(ids) {
				t.Fatalf("got %q total %d, want %q", got, body.Total, tt.want)
			}
		})
	}
}

// A mirror edit must be visible through the API without any Knox call: reads never touch Knox.
func TestReadsComeFromMirror(t *testing.T) {
	e := newEnv(t)
	if err := e.mirror.DeleteUser(context.Background(), "bob"); err != nil {
		t.Fatal(err)
	}
	if st := do(t, e.app, "GET", "/api/knox/users/bob", "", e.token(t, "root")).StatusCode; st != 404 {
		t.Fatalf("status %d, want 404", st)
	}
}

func TestWritesUpdateMirror(t *testing.T) {
	e := newEnv(t)
	tok := e.token(t, "root")
	ctx := context.Background()
	if r := do(t, e.app, "POST", "/api/knox/users", `{"userId":"zoe","userName":"Zoe","email":"z@example.org","orgCode":"SALES"}`, tok); r.StatusCode != 201 {
		t.Fatalf("create %d", r.StatusCode)
	}
	if u, err := e.mirror.User(ctx, "zoe"); err != nil || u.UserName != "Zoe" {
		t.Fatalf("created user not mirrored: %+v %v", u, err)
	}
	if r := do(t, e.app, "PUT", "/api/knox/users/zoe", `{"userName":"Zed"}`, tok); r.StatusCode != 200 {
		t.Fatalf("update %d", r.StatusCode)
	}
	if u, _ := e.mirror.User(ctx, "zoe"); u.UserName != "Zed" {
		t.Fatalf("update not mirrored: %+v", u)
	}
	path := "/api/knox/groups/" + knoxmock.GroupPilot + "/members"
	do(t, e.app, "POST", path, `{"userIds":["zoe"]}`, tok)
	if gs, _ := e.mirror.UserGroups(ctx, "zoe"); len(gs) != 1 || gs[0].UserCount != 4 {
		t.Fatalf("add not mirrored: %+v", gs)
	}
	do(t, e.app, "DELETE", path, `{"userIds":["zoe"]}`, tok)
	if gs, _ := e.mirror.UserGroups(ctx, "zoe"); len(gs) != 0 {
		t.Fatalf("remove not mirrored: %+v", gs)
	}
	do(t, e.app, "DELETE", "/api/knox/users/zoe", `{"confirmUserId":"zoe"}`, tok)
	if _, err := e.mirror.User(ctx, "zoe"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("purge not mirrored: %v", err)
	}
	var created struct{ GroupID string }
	decode(t, do(t, e.app, "POST", "/api/knox/groups", `{"name":"Fresh"}`, tok), &created)
	if _, err := e.mirror.Group(ctx, created.GroupID); err != nil {
		t.Fatalf("group create not mirrored: %v", err)
	}
	do(t, e.app, "DELETE", "/api/knox/groups/"+created.GroupID, "", tok)
	if _, err := e.mirror.Group(ctx, created.GroupID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("group delete not mirrored: %v", err)
	}
}

func TestAdminSyncStart(t *testing.T) {
	tests := []struct {
		name, user string
		fake       fakeSyncer
		status     int
		already    bool
	}{
		{"started", "root", fakeSyncer{id: 7}, 202, false},
		{"already running", "root", fakeSyncer{id: 7, already: true}, 202, true},
		{"sync error", "root", fakeSyncer{err: errors.New("boom")}, 500, false},
		{"non-admin", "writer", fakeSyncer{id: 7}, 403, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			*e.sync = tt.fake
			resp := do(t, e.app, "POST", "/api/admin/sync", "", e.token(t, tt.user))
			if resp.StatusCode != tt.status {
				t.Fatalf("status %d, want %d", resp.StatusCode, tt.status)
			}
			if tt.status != 202 {
				return
			}
			var body struct {
				ID             int64
				AlreadyRunning bool
			}
			decode(t, resp, &body)
			if body.ID != 7 || body.AlreadyRunning != tt.already {
				t.Fatalf("got %+v", body)
			}
			want := 1
			if tt.already {
				want = 0
			}
			var n int
			if err := e.db.Get(&n, `SELECT COUNT(*) FROM audit_log WHERE action = 'admin.sync.start'`); err != nil || n != want {
				t.Fatalf("audit rows %d, want %d, %v", n, want, err)
			}
		})
	}
}

func TestAdminSyncNeedsKnox(t *testing.T) {
	e := newEnvWith(t, func(d *Deps) { d.Knox = func() Knox { return nil } })
	tok := e.token(t, "root")
	resp := do(t, e.app, "POST", "/api/admin/sync", "", tok)
	var body errorBody
	decode(t, resp, &body)
	if resp.StatusCode != 503 || body.Code != "knox_not_configured" || e.sync.calls != 0 {
		t.Fatalf("status %d body %+v calls %d", resp.StatusCode, body, e.sync.calls)
	}
}

func TestAdminSyncStatus(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id1, _ := e.mirror.StartRun(ctx, "schedule")
	if err := e.mirror.FinishRun(ctx, id1, "failed", "knox down", db.SyncCounts{}); err != nil {
		t.Fatal(err)
	}
	id2, _ := e.mirror.StartRun(ctx, "manual")
	if err := e.mirror.FinishRun(ctx, id2, "ok", "", db.SyncCounts{Orgs: 1, Users: 2, Groups: 3, Memberships: 4, Devices: 5}); err != nil {
		t.Fatal(err)
	}
	id3, _ := e.mirror.StartRun(ctx, "manual")
	e.sync.id, e.sync.running = id3, true

	var body struct {
		Running bool
		RunID   int64
		Runs    []struct {
			ID         int64
			Trigger    string
			FinishedAt *string
			Status     string
			Error      string
			Counts     struct{ Orgs, Users, Groups, Memberships, Devices int }
		}
	}
	decode(t, do(t, e.app, "GET", "/api/admin/sync", "", e.token(t, "root")), &body)
	if !body.Running || body.RunID != id3 || len(body.Runs) != 3 {
		t.Fatalf("got %+v", body)
	}
	if r := body.Runs[0]; r.ID != id3 || r.FinishedAt != nil || r.Status != "running" {
		t.Fatalf("newest run %+v", r)
	}
	if r := body.Runs[1]; r.ID != id2 || r.FinishedAt == nil || r.Counts.Devices != 5 || r.Counts.Memberships != 4 {
		t.Fatalf("second run %+v", r)
	}
	if r := body.Runs[2]; r.Status != "failed" || r.Error != "knox down" {
		t.Fatalf("oldest run %+v", r)
	}

	e.sync.running = false
	var idle map[string]any
	decode(t, do(t, e.app, "GET", "/api/admin/sync", "", e.token(t, "root")), &idle)
	if _, has := idle["runId"]; idle["running"] != false || has {
		t.Fatalf("idle %+v", idle)
	}
	if st := do(t, e.app, "GET", "/api/admin/sync", "", e.token(t, "reader")).StatusCode; st != 403 {
		t.Fatalf("reader status %d", st)
	}
}

package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/DonovanDiamond/knoxmgr/src/db"
	"github.com/DonovanDiamond/knoxmgr/src/knox"
)

// appsEnv seeds two apps on carol's phone (org SALES, in scope for reader and writer) and the
// same user app on alice's phone (out of scope for them).
func appsEnv(t *testing.T) *env {
	t.Helper()
	// The mock only holds the apps its own catalog gives each device, so uninstalls are stubbed.
	e := newEnvWith(t, func(d *Deps) {
		real := d.Knox()
		d.Knox = func() Knox { return &countKnox{Knox: real} }
	})
	ctx := context.Background()
	user := db.DeviceAppRow{Package: "com.android.chrome", Name: "User App", Version: "3", KnoxID: "k1"}
	sys := db.DeviceAppRow{Package: "com.sys.app", Name: "Sys", System: true}
	for id, rows := range map[string][]db.DeviceAppRow{
		"dev-carol-phone": {user, sys}, "dev-alice-phone": {user}, "dev-spare": {user},
	} {
		if err := e.apps.ReplaceDeviceApps(ctx, id, rows, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

type resultsBody struct {
	Results []struct {
		DeviceID string
		OK       bool
		Error    string
	}
}

func TestAppsListAndDetailScope(t *testing.T) {
	e := appsEnv(t)
	tests := []struct {
		name, user, path string
		status           int
		apps, devices    int
	}{
		{"admin list", "root", "/api/knox/apps", 200, 1, -1},
		{"admin list with system", "root", "/api/knox/apps?system=true", 200, 2, -1},
		{"search miss", "root", "/api/knox/apps?search=nomatch", 200, 0, -1},
		{"reader list", "reader", "/api/knox/apps", 200, 1, -1},
		{"admin detail", "root", "/api/knox/apps/com.android.chrome", 200, -1, 3},
		{"reader detail scoped", "reader", "/api/knox/apps/com.android.chrome", 200, -1, 1},
		{"unknown app", "root", "/api/knox/apps/com.nope.app", 404, -1, -1},
		{"bad package", "root", "/api/knox/apps/bad", 400, -1, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := do(t, e.app, "GET", tt.path, "", e.token(t, tt.user))
			if r.StatusCode != tt.status {
				t.Fatalf("status %d, want %d", r.StatusCode, tt.status)
			}
			if tt.status != 200 {
				return
			}
			var got struct {
				Apps    []struct{ Package string }
				Devices []struct {
					DeviceID  string
					Android   bool
					Freshness string
				}
			}
			decode(t, r, &got)
			if tt.apps >= 0 && len(got.Apps) != tt.apps || tt.devices >= 0 && len(got.Devices) != tt.devices {
				t.Fatalf("got %+v", got)
			}
			for _, d := range got.Devices {
				if !d.Android || d.Freshness == "" {
					t.Errorf("device %+v", d)
				}
			}
		})
	}
}

func TestAppsUninstall(t *testing.T) {
	tests := []struct {
		name, user, pkg, body string
		status                int
		wantOK                map[string]bool
	}{
		{"read only", "reader", "com.android.chrome", `{"deviceIds":["dev-carol-phone"]}`, 403, nil},
		{"write in scope", "writer", "com.android.chrome", `{"deviceIds":["dev-carol-phone"]}`, 200, map[string]bool{"dev-carol-phone": true}},
		{"out of scope is per-device failure", "writer", "com.android.chrome", `{"deviceIds":["dev-alice-phone","dev-carol-phone"]}`, 200,
			map[string]bool{"dev-alice-phone": false, "dev-carol-phone": true}},
		{"unassigned device hidden from writer", "writer", "com.android.chrome", `{"deviceIds":["dev-spare"]}`, 200, map[string]bool{"dev-spare": false}},
		{"admin any device", "root", "com.android.chrome", `{"deviceIds":["dev-alice-phone","dev-spare","nope"]}`, 200,
			map[string]bool{"dev-alice-phone": true, "dev-spare": true, "nope": false}},
		{"app not on device", "root", "com.android.chrome", `{"deviceIds":["dev-bob-phone"]}`, 200, map[string]bool{"dev-bob-phone": false}},
		{"unknown app", "root", "com.nope.app", `{"deviceIds":["dev-carol-phone"]}`, 404, nil},
		{"bad package", "root", "bad", `{"deviceIds":["dev-carol-phone"]}`, 400, nil},
		{"empty list", "root", "com.android.chrome", `{"deviceIds":[]}`, 400, nil},
		{"bad device id", "root", "com.android.chrome", `{"deviceIds":["a b"]}`, 400, nil},
		{"bad body", "root", "com.android.chrome", `x`, 400, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := appsEnv(t)
			r := do(t, e.app, "POST", "/api/knox/apps/"+tt.pkg+"/uninstall", tt.body, e.token(t, tt.user))
			if r.StatusCode != tt.status {
				t.Fatalf("status %d, want %d", r.StatusCode, tt.status)
			}
			if tt.status != 200 {
				return
			}
			var got resultsBody
			decode(t, r, &got)
			okCount := 0
			for _, res := range got.Results {
				if res.OK != tt.wantOK[res.DeviceID] || res.OK == (res.Error != "") {
					t.Errorf("result %+v, want ok=%v", res, tt.wantOK[res.DeviceID])
				}
				if res.OK {
					okCount++
				}
			}
			if len(got.Results) != len(tt.wantOK) {
				t.Fatalf("results %+v", got.Results)
			}
			var audits int
			if err := e.db.Get(&audits, `SELECT COUNT(*) FROM audit_log WHERE action = 'device.app.uninstall' AND previous_value = '{"managed":false,"package":"com.android.chrome","version":"3"}'`); err != nil {
				t.Fatal(err)
			}
			if audits != okCount {
				t.Errorf("audit rows %d, want %d", audits, okCount)
			}
		})
	}
}

// failKnox refuses every uninstall to prove one failure does not stop the others.
type failKnox struct {
	Knox
	failFor string
}

func (k *failKnox) UninstallApp(ctx context.Context, id, pkg, knoxID string) error {
	if id == k.failFor {
		return &knox.Error{Code: "1", Message: "boom"}
	}
	return k.Knox.UninstallApp(ctx, id, pkg, knoxID)
}

func TestAppsUninstallPartialFailure(t *testing.T) {
	e := newEnvWith(t, func(d *Deps) {
		real := &countKnox{Knox: d.Knox()}
		d.Knox = func() Knox { return &failKnox{Knox: real, failFor: "dev-alice-phone"} }
	})
	for id := range map[string]bool{"dev-alice-phone": true, "dev-carol-phone": true} {
		if err := e.apps.ReplaceDeviceApps(context.Background(), id, []db.DeviceAppRow{{Package: "com.android.chrome", Name: "U"}}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	var got resultsBody
	decode(t, do(t, e.app, "POST", "/api/knox/apps/com.android.chrome/uninstall", `{"deviceIds":["dev-alice-phone","dev-carol-phone"]}`, e.token(t, "root")), &got)
	if len(got.Results) != 2 || got.Results[0].OK || got.Results[0].Error == "" || !got.Results[1].OK {
		t.Fatalf("results %+v", got.Results)
	}
	rows, _ := e.apps.DeviceApps(context.Background(), "dev-carol-phone")
	if len(rows) != 1 || rows[0].UninstallRequestedAt == nil {
		t.Errorf("carol not marked: %+v", rows)
	}
	rows, _ = e.apps.DeviceApps(context.Background(), "dev-alice-phone")
	if rows[0].UninstallRequestedAt != nil {
		t.Errorf("alice marked despite failure")
	}
}

func TestDeviceAppsFromMirror(t *testing.T) {
	tests := []struct {
		name, user, id string
		pullErr        error
		status         int
		stale          bool
	}{
		{"read", "reader", "dev-carol-phone", nil, 200, false},
		{"out of scope", "writer", "dev-alice-phone", nil, 404, false},
		{"knox down serves mirror", "root", "dev-carol-phone", fmt.Errorf("%w: down", knox.ErrUnreachable), 200, true},
		{"knox error serves mirror", "root", "dev-carol-phone", &knox.Error{Code: "1", Message: "bad"}, 200, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := appsEnv(t)
			e.inv.err = tt.pullErr
			r := do(t, e.app, "GET", "/api/knox/devices/"+tt.id+"/apps", "", e.token(t, tt.user))
			if r.StatusCode != tt.status {
				t.Fatalf("status %d, want %d", r.StatusCode, tt.status)
			}
			if tt.status != 200 {
				return
			}
			var got struct {
				Apps     []struct{ Package, Category string }
				SyncedAt *string
				Stale    bool
				Android  bool
			}
			decode(t, r, &got)
			if len(got.Apps) != 2 || got.Stale != tt.stale || !got.Android || (!tt.stale && got.SyncedAt == nil) {
				t.Fatalf("got %+v", got)
			}
			if got.Apps[1].Category != "System" && got.Apps[0].Category != "System" {
				t.Errorf("no System category: %+v", got.Apps)
			}
		})
	}
}

func TestDeviceAppsNotAndroid(t *testing.T) {
	e := appsEnv(t)
	if _, err := e.db.Exec(`UPDATE knox_devices SET platform = 'I' WHERE device_id = 'dev-carol-phone'`); err != nil {
		t.Fatal(err)
	}
	var got struct{ Android bool }
	decode(t, do(t, e.app, "GET", "/api/knox/devices/dev-carol-phone/apps", "", e.token(t, "root")), &got)
	if got.Android || e.inv.calls != 0 {
		t.Errorf("android=%v pulls=%d", got.Android, e.inv.calls)
	}
	if r := do(t, e.app, "POST", "/api/knox/devices/dev-carol-phone/apps/refresh", "", e.token(t, "root")); r.StatusCode != 400 {
		t.Errorf("refresh status %d", r.StatusCode)
	}
}

func TestDeviceAppsRefresh(t *testing.T) {
	tests := []struct {
		name, user, id string
		status         int
		audited        int
	}{
		{"write", "writer", "dev-carol-phone", 202, 1},
		{"admin", "root", "dev-spare", 202, 1},
		{"read only", "reader", "dev-carol-phone", 403, 0},
		{"out of scope", "writer", "dev-alice-phone", 404, 0},
		{"unknown", "root", "nope", 404, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := appsEnv(t)
			r := do(t, e.app, "POST", "/api/knox/devices/"+tt.id+"/apps/refresh", "", e.token(t, tt.user))
			if r.StatusCode != tt.status {
				t.Fatalf("status %d, want %d", r.StatusCode, tt.status)
			}
			var n int
			if err := e.db.Get(&n, `SELECT COUNT(*) FROM audit_log WHERE action = 'device.apps.refresh'`); err != nil || n != tt.audited {
				t.Errorf("audit rows %d (%v), want %d", n, err, tt.audited)
			}
		})
	}
}

func TestAppCategories(t *testing.T) {
	e := appsEnv(t)
	root, writer := e.token(t, "root"), e.token(t, "writer")
	counts := func(token string) (total int) {
		var got struct{ Categories []struct{ AppCount int } }
		if r := do(t, e.app, "GET", "/api/knox/app-categories", "", token); r.StatusCode != 200 {
			t.Fatalf("list status %d", r.StatusCode)
		} else {
			decode(t, r, &got)
		}
		for _, c := range got.Categories {
			total += c.AppCount
		}
		return total
	}
	if n := counts(root); n == 0 {
		t.Error("admin sees no category counts")
	}
	if n := counts(writer); n != 0 {
		t.Errorf("non-admin sees tenant-wide category counts: %d", n)
	}
	for _, m := range []string{"POST", "PUT", "DELETE"} {
		path := "/api/admin/app-categories"
		if m != "POST" {
			path += "/1"
		}
		if r := do(t, e.app, m, path, `{"name":"x"}`, writer); r.StatusCode != 403 {
			t.Errorf("%s as writer: %d", m, r.StatusCode)
		}
	}
	if r := do(t, e.app, "PUT", "/api/admin/apps/com.android.chrome/category", `{"categoryId":null}`, writer); r.StatusCode != 403 {
		t.Errorf("assign as writer: %d", r.StatusCode)
	}
	bad := []struct{ method, path, body string }{
		{"POST", "/api/admin/app-categories", `{"name":""}`},
		{"POST", "/api/admin/app-categories", `{"name":"System"}`},
		{"POST", "/api/admin/app-categories", `{"name":"` + string(make([]byte, 101)) + `"}`},
		{"POST", "/api/admin/app-categories", `{"name":"a\u0007b"}`},
		{"PUT", "/api/admin/app-categories/x", `{"name":"y"}`},
		{"PUT", "/api/admin/apps/bad/category", `{"categoryId":null}`},
		{"PUT", "/api/admin/apps/com.android.chrome/category", `{"categoryId":0}`},
	}
	for _, b := range bad {
		if r := do(t, e.app, b.method, b.path, b.body, root); r.StatusCode != 400 {
			t.Errorf("%s %s %s: %d, want 400", b.method, b.path, b.body, r.StatusCode)
		}
	}
	if r := do(t, e.app, "POST", "/api/admin/app-categories", `{"name":"Google"}`, root); r.StatusCode != 409 {
		t.Errorf("duplicate: %d", r.StatusCode)
	}

	var created struct {
		ID       int64
		Name     string
		AppCount int
	}
	r := do(t, e.app, "POST", "/api/admin/app-categories", `{"name":" Banking "}`, root)
	if r.StatusCode != 201 {
		t.Fatalf("create: %d", r.StatusCode)
	}
	decode(t, r, &created)
	if created.Name != "Banking" || created.ID == 0 {
		t.Fatalf("created %+v", created)
	}
	path := fmt.Sprintf("/api/admin/app-categories/%d", created.ID)
	if r := do(t, e.app, "PUT", path, `{"name":"Finance"}`, root); r.StatusCode != 200 {
		t.Fatalf("rename: %d", r.StatusCode)
	}
	if r := do(t, e.app, "PUT", "/api/admin/apps/com.android.chrome/category", fmt.Sprintf(`{"categoryId":%d}`, created.ID), root); r.StatusCode != 204 {
		t.Fatalf("assign: %d", r.StatusCode)
	}
	if r := do(t, e.app, "PUT", "/api/admin/apps/com.android.chrome/category", `{"categoryId":999999}`, root); r.StatusCode != 404 {
		t.Errorf("assign unknown category: %d", r.StatusCode)
	}
	var got struct{ App struct{ Category string } }
	decode(t, do(t, e.app, "GET", "/api/knox/apps/com.android.chrome", "", root), &got)
	if got.App.Category != "Finance" {
		t.Errorf("category %q", got.App.Category)
	}
	if r := do(t, e.app, "DELETE", path, "", root); r.StatusCode != 204 {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	if r := do(t, e.app, "DELETE", path, "", root); r.StatusCode != 404 {
		t.Errorf("delete again: %d", r.StatusCode)
	}
	var prev string
	if err := e.db.Get(&prev, `SELECT previous_value FROM audit_log WHERE action = 'app.category.rename'`); err != nil || prev != `{"name":"Banking"}` {
		t.Errorf("rename audit previous %q (%v)", prev, err)
	}
	var n int
	if err := e.db.Get(&n, `SELECT COUNT(*) FROM audit_log WHERE action LIKE 'app.category.%'`); err != nil || n != 4 {
		t.Errorf("category audit rows %d (%v), want 4", n, err)
	}
}

func TestProtectedReason(t *testing.T) {
	tests := []struct {
		name, pkg string
		ok        bool
	}{
		{name: "plain app", pkg: "com.android.chrome", ok: true},
		{name: "kmagent", pkg: "com.samsung.android.knox.kmagent"},
		{name: "containeragent", pkg: "com.samsung.android.knox.containeragent"},
		{name: "klmsagent", pkg: "com.samsung.klmsagent"},
		{name: "emm prefix", pkg: "com.sds.emm.client"},
		{name: "enterprise prefix", pkg: "com.sec.enterprise.foo"},
		{name: "knox prefix", pkg: "com.samsung.android.knox.other"},
		{name: "prefix needs the dot", pkg: "com.sds.emmx", ok: true},
		{name: "other samsung app", pkg: "com.samsung.klmsagentx", ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := protectedReason(tt.pkg)
			if (got == "") != tt.ok {
				t.Errorf("reason %q, want allowed=%v", got, tt.ok)
			}
		})
	}
}

// countKnox records uninstall calls so a refusal can be shown not to reach Knox.
type countKnox struct {
	Knox
	calls int
}

func (k *countKnox) UninstallApp(ctx context.Context, id, pkg, knoxID string) error {
	k.calls++
	return nil
}

func TestAppsUninstallProtected(t *testing.T) {
	tests := []struct {
		name, pkg string
		row       db.DeviceAppRow
		category  string
	}{
		{"kmagent", "com.samsung.android.knox.kmagent", db.DeviceAppRow{}, ""},
		{"knox prefix", "com.samsung.android.knox.other", db.DeviceAppRow{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &countKnox{}
			e := newEnvWith(t, func(d *Deps) {
				k.Knox = d.Knox()
				d.Knox = func() Knox { return k }
			})
			ctx := context.Background()
			tt.row.Package, tt.row.Name = tt.pkg, "P"
			if err := e.apps.ReplaceDeviceApps(ctx, "dev-carol-phone", []db.DeviceAppRow{tt.row}, time.Now()); err != nil {
				t.Fatal(err)
			}
			if tt.category != "" {
				cats, err := e.apps.Categories(ctx)
				if err != nil {
					t.Fatal(err)
				}
				var id int64
				for _, c := range cats {
					if c.Name == tt.category {
						id = c.ID
					}
				}
				if _, err := e.apps.SetAppCategory(ctx, tt.pkg, &id); err != nil {
					t.Fatal(err)
				}
			}
			r := do(t, e.app, "POST", "/api/knox/apps/"+tt.pkg+"/uninstall", `{"deviceIds":["dev-carol-phone"]}`, e.token(t, "root"))
			if r.StatusCode != 403 {
				t.Fatalf("status %d, want 403", r.StatusCode)
			}
			var got struct{ Code string }
			decode(t, r, &got)
			if got.Code != "app_protected" || k.calls != 0 {
				t.Errorf("code %q, knox calls %d", got.Code, k.calls)
			}
		})
	}
}

func TestBuiltInCategoryImmutable(t *testing.T) {
	e := appsEnv(t)
	root := e.token(t, "root")
	var list struct {
		Categories []struct {
			ID      int64
			Name    string
			BuiltIn bool
		}
	}
	decode(t, do(t, e.app, "GET", "/api/knox/app-categories", "", root), &list)
	var other int64
	for _, c := range list.Categories {
		if c.Name == "Other" && c.BuiltIn {
			other = c.ID
		}
	}
	if other == 0 {
		t.Fatalf("no built-in Other category in %+v", list.Categories)
	}
	path := fmt.Sprintf("/api/admin/app-categories/%d", other)
	for _, m := range []string{"PUT", "DELETE"} {
		r := do(t, e.app, m, path, `{"name":"Misc"}`, root)
		var got struct{ Code string }
		decode(t, r, &got)
		if r.StatusCode != 400 || got.Code != "category_builtin" {
			t.Errorf("%s: status %d code %q", m, r.StatusCode, got.Code)
		}
	}
}

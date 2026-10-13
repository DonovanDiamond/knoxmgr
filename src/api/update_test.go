package api

import (
	"context"
	"testing"
)

func TestUpdateEndpoints(t *testing.T) {
	e := newEnv(t)
	a := e.token(t, "root")

	var v map[string]any
	decode(t, do(t, e.app, "GET", "/api/admin/update", "", a), &v)
	if v["enabled"] != true || v["current"] != "v0.4.0" || v["updateAvailable"] != false || v["checkedAt"] != nil {
		t.Fatalf("before check = %v", v)
	}

	resp := do(t, e.app, "POST", "/api/admin/update/check", "", a)
	if resp.StatusCode != 200 {
		t.Fatalf("check: %d", resp.StatusCode)
	}
	decode(t, resp, &v)
	if v["latest"] != "v0.5.0" || v["updateAvailable"] != true || v["checkedAt"] == nil ||
		v["url"] != "https://github.com/DonovanDiamond/knoxmgr/releases/tag/v0.5.0" {
		t.Fatalf("after check = %v", v)
	}
	entries, err := e.audit.ListRecent(context.Background(), 10)
	if err != nil || len(entries) == 0 || entries[0].Action != "admin.update.check" {
		t.Fatalf("audit = %+v, %v", entries, err)
	}

	if resp := do(t, e.app, "PUT", "/api/admin/settings/general", `{"installationName":"x","syncIntervalMinutes":15,"checkUpdates":false}`, a); resp.StatusCode != 200 {
		t.Fatalf("PUT general: %d", resp.StatusCode)
	}
	if st := do(t, e.app, "POST", "/api/admin/update/check", "", a).StatusCode; st != 409 {
		t.Fatalf("check while disabled: %d", st)
	}
	decode(t, do(t, e.app, "GET", "/api/admin/update", "", a), &v)
	if v["enabled"] != false {
		t.Fatalf("disabled view = %v", v)
	}

	// Omitting checkUpdates must keep it disabled.
	if resp := do(t, e.app, "PUT", "/api/admin/settings/general", `{"installationName":"x","syncIntervalMinutes":20}`, a); resp.StatusCode != 200 {
		t.Fatalf("PUT general without checkUpdates: %d", resp.StatusCode)
	}
	decode(t, do(t, e.app, "GET", "/api/admin/update", "", a), &v)
	if v["enabled"] != false {
		t.Fatalf("absent checkUpdates re-enabled checks: %v", v)
	}
}

func TestUpdateAdminOnly(t *testing.T) {
	e := newEnv(t)
	for _, r := range [][2]string{{"GET", "/api/admin/update"}, {"POST", "/api/admin/update/check"}} {
		if st := do(t, e.app, r[0], r[1], "", e.token(t, "reader")).StatusCode; st != 403 {
			t.Errorf("%s %s as reader: %d", r[0], r[1], st)
		}
		if st := do(t, e.app, r[0], r[1], "", "").StatusCode; st != 401 {
			t.Errorf("%s %s anonymous: %d", r[0], r[1], st)
		}
	}
}

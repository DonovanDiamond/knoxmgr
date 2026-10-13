package api

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DonovanDiamond/knoxmgr/src/knox"
)

func TestFreshnessOf(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	tests := []struct {
		name     string
		enrolled int
		last     time.Time
		want     string
	}{
		{"no enrolled devices", 0, ago(time.Hour), freshNone},
		{"no enrolled, never connected", 0, time.Time{}, freshNone},
		{"just now", 1, now, freshFresh},
		{"in the future", 1, now.Add(time.Minute), freshFresh},
		{"exactly 24h", 1, ago(staleAfter), freshFresh},
		{"just over 24h", 1, ago(staleAfter + time.Second), freshStale},
		{"3 days", 2, ago(3 * 24 * time.Hour), freshStale},
		{"exactly 7d", 1, ago(oldAfter), freshStale},
		{"just over 7d", 1, ago(oldAfter + time.Second), freshOld},
		{"enrolled, never connected", 1, time.Time{}, freshOld},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := freshnessOf(tt.enrolled, tt.last, now); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeviceFreshness(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	tests := []struct {
		name     string
		enrolled bool
		last     time.Time
		want     string
	}{
		{"not enrolled, never connected", false, time.Time{}, freshNotEnrolled},
		{"not enrolled, connected", false, ago(time.Hour), freshNotEnrolled},
		{"enrolled, never connected", true, time.Time{}, freshNever},
		{"enrolled, just now", true, now, freshFresh},
		{"enrolled, 3 days", true, ago(3 * 24 * time.Hour), freshStale},
		{"enrolled, 8 days", true, ago(8 * 24 * time.Hour), freshOld},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deviceFreshness(tt.enrolled, tt.last, now); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSummarizeUsesStalestEnrolledDevice(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }
	devs := []knox.Device{
		{UserID: "a", DeviceStatus: "A", LastConnection: at(time.Hour)},
		{UserID: "a", DeviceStatus: "A", LastConnection: at(3 * 24 * time.Hour)},
		{UserID: "a", DeviceStatus: "P", LastConnection: at(30 * 24 * time.Hour)}, // not enrolled: ignored
		{UserID: "b", DeviceStatus: "A", LastConnection: at(time.Hour)},
		{UserID: "b", DeviceStatus: "B"}, // blocked is still enrolled; never connected
		{UserID: "", DeviceStatus: "A"},  // unassigned
		{UserID: "c", DeviceStatus: "I"},
	}
	got := summarize(devs, now)
	if a := got["a"]; a.Enrolled != 2 || a.Freshness != freshStale || a.LastConnection != at(3*24*time.Hour) {
		t.Errorf("a = %+v", a)
	}
	if b := got["b"]; b.Enrolled != 2 || b.Freshness != freshOld || b.LastConnection != "" {
		t.Errorf("b = %+v", b)
	}
	if _, ok := got["c"]; ok || len(got) != 2 {
		t.Errorf("got %+v", got)
	}
}

func deviceList(t *testing.T, e *env, path, tok string) string {
	t.Helper()
	ids := listIDs(t, e, path, tok, "devices", "deviceId")
	slices.Sort(ids) // the mirror orders by model, which the expectations do not encode
	return strings.Join(ids, " ")
}

func TestDeviceListScope(t *testing.T) {
	const all = "dev-alice-phone dev-alice-tab dev-bob-phone dev-carol-phone dev-frank-phone dev-spare"
	tests := []struct {
		name, user, path, want string
	}{
		{"admin sees all incl. unassigned", "root", "/api/knox/devices", all},
		{"writer sees subtree only", "writer", "/api/knox/devices", "dev-bob-phone dev-carol-phone"},
		{"reader sees subtree only", "reader", "/api/knox/devices", "dev-bob-phone dev-carol-phone"},
		{"search by model", "root", "/api/knox/devices?q=tab", "dev-alice-tab"},
		{"search by user name", "root", "/api/knox/devices?q=FRANK", "dev-frank-phone"},
		{"search respects scope", "writer", "/api/knox/devices?q=alice", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			if got := deviceList(t, e, tt.path, e.token(t, tt.user)); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeviceGet(t *testing.T) {
	tests := []struct {
		name, user, id string
		status         int
	}{
		{"admin in scope", "root", "dev-carol-phone", 200},
		{"admin unassigned", "root", "dev-spare", 200},
		{"reader in scope", "reader", "dev-carol-phone", 200},
		{"reader out of scope", "reader", "dev-alice-phone", 404},
		{"reader unassigned", "reader", "dev-spare", 404},
		{"unknown", "root", "nope", 404},
		{"bad id", "root", "a%20b", 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			r := do(t, e.app, "GET", "/api/knox/devices/"+tt.id, "", e.token(t, tt.user))
			if r.StatusCode != tt.status {
				t.Fatalf("status %d, want %d", r.StatusCode, tt.status)
			}
			if tt.status == 404 {
				var body struct{ Error string }
				decode(t, r, &body)
				if tt.id != "nope" && body.Error != "device not found" {
					t.Errorf("error %q", body.Error)
				}
			}
		})
	}
}

func TestDeviceJSONShape(t *testing.T) {
	e := newEnv(t)
	tok := e.token(t, "root")
	var d map[string]any
	decode(t, do(t, e.app, "GET", "/api/knox/devices/dev-carol-phone", "", tok), &d)
	for k, want := range map[string]any{
		"deviceId": "dev-carol-phone", "modelName": "Galaxy XCover6 Pro", "os": "Android", "status": "Activated",
		"enrolled": true, "freshness": "old", "userId": "carol", "userName": "Carol Clark", "orgCode": "SALES",
	} {
		if d[k] != want {
			t.Errorf("%s = %v, want %v", k, d[k], want)
		}
	}
	if _, err := time.Parse(time.RFC3339, d["lastConnection"].(string)); err != nil {
		t.Errorf("lastConnection: %v", err)
	}
	decode(t, do(t, e.app, "GET", "/api/knox/devices/dev-frank-phone", "", tok), &d)
	if d["enrolled"] != false || d["freshness"] != "not_enrolled" || d["status"] != "Provisioned" {
		t.Errorf("frank device = %v", d)
	}
}

func TestUserDevicesScope(t *testing.T) {
	tests := []struct {
		name, user, path, want string
		status                 int
	}{
		{"admin", "root", "/api/knox/users/alice/devices", "dev-alice-phone dev-alice-tab", 200},
		{"reader in scope", "reader", "/api/knox/users/carol/devices", "dev-carol-phone", 200},
		{"user without devices", "reader", "/api/knox/users/dave/devices", "", 200},
		{"reader out of scope", "reader", "/api/knox/users/alice/devices", "", 404},
		{"bad id", "root", "/api/knox/users/a%20b/devices", "", 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			tok := e.token(t, tt.user)
			if st := do(t, e.app, "GET", tt.path, "", tok).StatusCode; st != tt.status {
				t.Fatalf("status %d, want %d", st, tt.status)
			}
			if tt.status == 200 {
				if got := deviceList(t, e, tt.path, tok); got != tt.want {
					t.Fatalf("got %q, want %q", got, tt.want)
				}
			}
		})
	}
}

func TestDeviceSearchValidation(t *testing.T) {
	e := newEnv(t)
	tok := e.token(t, "root")
	for name, q := range map[string]string{"too long": strings.Repeat("a", 101), "control char": "a%00b"} {
		if st := do(t, e.app, "GET", "/api/knox/devices?q="+q, "", tok).StatusCode; st != 400 {
			t.Errorf("%s: status %d, want 400", name, st)
		}
	}
}

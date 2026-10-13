package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/DonovanDiamond/knoxmgr/src/knox"
)

// liveKnox overrides the device-detail calls of the real client with canned answers.
type liveKnox struct {
	Knox
	err   error
	calls int
}

func (k *liveKnox) SelectDeviceDetail(context.Context, string) (knox.DeviceDetail, error) {
	k.calls++
	return knox.DeviceDetail{DeviceModelKind: "Galaxy", IsRooting: "N", Profiles: []string{"p1"}, Created: "2024-12-03T22:30:01Z"}, k.err
}

func (k *liveKnox) SelectDeviceAppList(context.Context, string) ([]knox.DeviceApp, error) {
	k.calls++
	return []knox.DeviceApp{{AppName: "Mail", PackageName: "com.mail", VersionName: "1"}}, k.err
}

func (k *liveKnox) SelectDeviceLocation(context.Context, string) (knox.DeviceLocation, error) {
	k.calls++
	return knox.DeviceLocation{Latitude: "10.5", Longitude: "20.5", Altitude: "null", Updated: "2024-05-19T21:41:33Z"}, k.err
}

func (k *liveKnox) SelectDeviceLocationHistory(context.Context, string, time.Time, time.Time) ([]knox.DeviceLocation, error) {
	return []knox.DeviceLocation{
		{Latitude: "1", Longitude: "2"}, {Latitude: "91", Longitude: "2"}, {Latitude: "x", Longitude: "2"},
		{Latitude: "1", Longitude: "181"}, {Latitude: "NaN", Longitude: "2"},
	}, k.err
}

func liveEnv(t *testing.T, k *liveKnox) *env {
	t.Helper()
	return newEnvWith(t, func(d *Deps) {
		k.Knox = d.Knox()
		d.Knox = func() Knox { return k }
	})
}

func TestDeviceLiveScopeAndPermission(t *testing.T) {
	tests := []struct {
		name, user, id, suffix string
		status                 int
	}{
		{"admin detail", "root", "dev-carol-phone", "detail", 200},
		{"admin detail unassigned", "root", "dev-spare", "detail", 200},
		{"read detail", "reader", "dev-carol-phone", "detail", 200},
		{"read detail out of scope", "reader", "dev-alice-phone", "detail", 404},
		{"read detail unassigned", "reader", "dev-spare", "detail", 404},
		{"read apps", "reader", "dev-carol-phone", "apps", 200},
		{"write apps out of scope", "writer", "dev-alice-phone", "apps", 404},
		{"admin location", "root", "dev-carol-phone", "location", 200},
		{"admin location unassigned", "root", "dev-spare", "location", 200},
		{"write location", "writer", "dev-carol-phone", "location", 200},
		{"write location out of scope", "writer", "dev-alice-phone", "location", 404},
		{"write location unassigned", "writer", "dev-spare", "location", 404},
		{"read location", "reader", "dev-carol-phone", "location", 404},
		{"unknown device", "root", "nope", "location", 404},
		{"bad id", "root", "a%20b", "detail", 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := liveEnv(t, &liveKnox{})
			r := do(t, e.app, "GET", "/api/knox/devices/"+tt.id+"/"+tt.suffix, "", e.token(t, tt.user))
			if r.StatusCode != tt.status {
				t.Fatalf("status %d, want %d", r.StatusCode, tt.status)
			}
		})
	}
}

func TestDeviceDetailContract(t *testing.T) {
	for user, canView := range map[string]bool{"root": true, "writer": true, "reader": false} {
		e := liveEnv(t, &liveKnox{})
		var got struct {
			Info struct {
				ModelName string
				Rooted    *bool
				CreatedAt string
			}
			Profiles        []string
			Cached          bool
			FetchedAt       string
			CanViewLocation bool
		}
		decode(t, do(t, e.app, "GET", "/api/knox/devices/dev-carol-phone/detail", "", e.token(t, user)), &got)
		if got.Info.ModelName != "Galaxy" || got.Info.Rooted == nil || *got.Info.Rooted || got.Info.CreatedAt != "2024-12-03T22:30:01Z" ||
			len(got.Profiles) != 1 || got.Cached || got.FetchedAt == "" || got.CanViewLocation != canView {
			t.Errorf("%s: got %+v", user, got)
		}
	}
}

func TestDeviceCacheFallback(t *testing.T) {
	for _, suffix := range []string{"detail", "apps", "location"} {
		tests := []struct {
			name      string
			warm      bool
			err       error
			status    int
			cached    bool
			wantAudit bool
		}{
			{"live ok", false, nil, 200, false, suffix == "location"},
			{"unreachable with cache", true, fmt.Errorf("%w: down", knox.ErrUnreachable), 200, true, suffix == "location"},
			{"unreachable no cache", false, fmt.Errorf("%w: down", knox.ErrUnreachable), 502, false, false},
			{"knox error is not served from cache", true, &knox.Error{Code: "1", Message: "bad"}, 502, false, false},
		}
		for _, tt := range tests {
			t.Run(suffix+"/"+tt.name, func(t *testing.T) {
				k := &liveKnox{}
				e := liveEnv(t, k)
				tok := e.token(t, "root")
				path := "/api/knox/devices/dev-carol-phone/" + suffix
				if tt.warm {
					if r := do(t, e.app, "GET", path, "", tok); r.StatusCode != 200 {
						t.Fatalf("warm-up status %d", r.StatusCode)
					}
				}
				k.err = tt.err
				before := auditCount(t, e)
				r := do(t, e.app, "GET", path, "", tok)
				if r.StatusCode != tt.status {
					t.Fatalf("status %d, want %d", r.StatusCode, tt.status)
				}
				if tt.status == 200 {
					var got struct {
						Cached    bool
						FetchedAt string
					}
					decode(t, r, &got)
					if got.Cached != tt.cached || got.FetchedAt == "" {
						t.Errorf("got %+v, want cached=%v", got, tt.cached)
					}
				}
				if d := auditCount(t, e) - before; (d == 1) != tt.wantAudit || d > 1 {
					t.Errorf("audit rows added = %d, want audited=%v", d, tt.wantAudit)
				}
			})
		}
	}
}

func auditCount(t *testing.T, e *env) int {
	t.Helper()
	var n int
	if err := e.db.Get(&n, `SELECT COUNT(*) FROM audit_log WHERE action = 'device.location.view'`); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDeviceLocation(t *testing.T) {
	e := liveEnv(t, &liveKnox{})
	tok := e.token(t, "writer")
	for q, want := range map[string]int{"": 200, "?days=1": 200, "?days=30": 200, "?days=2": 400, "?days=": 400, "?days=x": 400} {
		if r := do(t, e.app, "GET", "/api/knox/devices/dev-carol-phone/location"+q, "", tok); r.StatusCode != want {
			t.Errorf("%q: status %d, want %d", q, r.StatusCode, want)
		}
	}
	var got struct {
		Current *struct {
			Latitude, Longitude float64
			Altitude            *float64
			Time                string
		}
		History []struct{ Latitude float64 }
		Days    int
	}
	decode(t, do(t, e.app, "GET", "/api/knox/devices/dev-carol-phone/location?days=30", "", tok), &got)
	if got.Current == nil || got.Current.Latitude != 10.5 || got.Current.Altitude != nil || got.Current.Time == "" ||
		len(got.History) != 1 || got.Days != 30 {
		t.Fatalf("got %+v", got)
	}
	var target, newVal string
	if err := e.db.QueryRow(`SELECT target, new_value FROM audit_log WHERE action = 'device.location.view' ORDER BY id DESC LIMIT 1`).Scan(&target, &newVal); err != nil {
		t.Fatal(err)
	}
	if target != "dev-carol-phone" || newVal != `{"cached":false,"days":30}` {
		t.Errorf("audit target %q value %q", target, newVal)
	}
}

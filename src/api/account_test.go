package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/DonovanDiamond/knoxmgr/src/auth"
)

func (e *env) adLogin(t *testing.T) authBody {
	t.Helper()
	e.dir.user = &auth.DirectoryUser{DisplayName: "Ann", Groups: []string{"Admins"}}
	resp := do(t, e.app, "POST", "/api/auth/login", `{"username":"ann","password":"x"}`, "")
	if resp.StatusCode != 200 {
		t.Fatalf("ad login: status %d", resp.StatusCode)
	}
	var b authBody
	decode(t, resp, &b)
	return b
}

func TestUpdateAccount(t *testing.T) {
	tests := []struct {
		name, body string
		status     int
		wantName   string
		audited    int
	}{
		{"changed", `{"displayName":"New Name"}`, 200, "New Name", 1},
		{"unchanged is a no-op", `{"displayName":"writer"}`, 200, "writer", 0},
		{"empty", `{"displayName":""}`, 400, "writer", 0},
		{"too long", `{"displayName":"` + strings.Repeat("a", maxTextLen+1) + `"}`, 400, "writer", 0},
		{"control char", `{"displayName":"a\u0007b"}`, 400, "writer", 0},
		{"not json", `nope`, 400, "writer", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			b, _ := e.login(t, "writer")
			resp := do(t, e.app, "PUT", "/api/auth/me", tt.body, b.AccessToken)
			if resp.StatusCode != tt.status {
				t.Fatalf("status %d, want %d", resp.StatusCode, tt.status)
			}
			if tt.status == 200 {
				var got userBody
				decode(t, resp, &got)
				if got.DisplayName != tt.wantName || got.Username != "writer" {
					t.Fatalf("got %+v", got)
				}
			}
			u, _ := e.users.GetByUsername(context.Background(), "writer")
			if u.DisplayName != tt.wantName {
				t.Fatalf("stored name %q, want %q", u.DisplayName, tt.wantName)
			}
			entries, _ := e.audit.ListRecent(context.Background(), 10)
			if len(entries) != tt.audited {
				t.Fatalf("%d audit rows, want %d", len(entries), tt.audited)
			}
			if tt.audited == 1 && (entries[0].Action != "account.update" || !strings.Contains(*entries[0].PreviousValue, "writer")) {
				t.Fatalf("audit %+v", entries[0])
			}
		})
	}
}

func TestAccountADRejected(t *testing.T) {
	for _, tt := range []struct{ name, method, path, body string }{
		{"profile", "PUT", "/api/auth/me", `{"displayName":"X"}`},
		{"password", "POST", "/api/auth/me/password", `{"currentPassword":"x","newPassword":"long-enough-1"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			b := e.adLogin(t)
			resp := do(t, e.app, tt.method, tt.path, tt.body, b.AccessToken)
			var eb errorBody
			decode(t, resp, &eb)
			if resp.StatusCode != 403 || eb.Code != "ad_managed" {
				t.Fatalf("status %d body %+v", resp.StatusCode, eb)
			}
		})
	}
}

func TestChangePassword(t *testing.T) {
	tests := []struct {
		name, body string
		status     int
		code       string
	}{
		{"wrong current", `{"currentPassword":"nope","newPassword":"brand-new-pass"}`, 403, "wrong_password"},
		{"short new", `{"currentPassword":"password1","newPassword":"short"}`, 400, "invalid_password"},
		{"missing current", `{"newPassword":"brand-new-pass"}`, 400, ""},
		{"ok", `{"currentPassword":"password1","newPassword":"brand-new-pass"}`, 204, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			b, _ := e.login(t, "writer")
			resp := do(t, e.app, "POST", "/api/auth/me/password", tt.body, b.AccessToken)
			if resp.StatusCode != tt.status {
				t.Fatalf("status %d, want %d", resp.StatusCode, tt.status)
			}
			if tt.code != "" {
				var eb errorBody
				decode(t, resp, &eb)
				if eb.Code != tt.code {
					t.Fatalf("code %q, want %q", eb.Code, tt.code)
				}
			}
			u, _ := e.users.GetByUsername(context.Background(), "writer")
			if changed := !auth.CheckPassword(u.PasswordHash, "password1"); changed != (tt.status == 204) {
				t.Fatalf("password changed=%v with status %d", changed, tt.status)
			}
		})
	}
}

func TestChangePasswordRevokesOtherSessions(t *testing.T) {
	e := newEnv(t)
	b, keep := e.login(t, "writer")
	_, other := e.login(t, "writer")
	resp := do(t, e.app, "POST", "/api/auth/me/password",
		`{"currentPassword":"password1","newPassword":"brand-new-pass"}`, b.AccessToken, keep)
	if resp.StatusCode != 204 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	// Checked first: replaying a revoked token ends the whole family.
	if r := do(t, e.app, "POST", "/api/auth/refresh", "", "", keep); r.StatusCode != 200 {
		t.Fatalf("current session refresh: %d, want 200", r.StatusCode)
	}
	if r := do(t, e.app, "POST", "/api/auth/refresh", "", "", other); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("other session refresh: %d, want 401", r.StatusCode)
	}
}

func TestChangePasswordRateLimited(t *testing.T) {
	e := newEnv(t)
	b, _ := e.login(t, "writer")
	body := `{"currentPassword":"nope","newPassword":"brand-new-pass"}`
	for i := 0; i < 5; i++ {
		if r := do(t, e.app, "POST", "/api/auth/me/password", body, b.AccessToken); r.StatusCode != 403 {
			t.Fatalf("attempt %d: %d, want 403", i, r.StatusCode)
		}
	}
	if r := do(t, e.app, "POST", "/api/auth/me/password", body, b.AccessToken); r.StatusCode != 429 {
		t.Fatalf("after limit: %d, want 429", r.StatusCode)
	}
}

func TestChangePasswordAuditHasNoSecrets(t *testing.T) {
	e := newEnv(t)
	b, _ := e.login(t, "writer")
	do(t, e.app, "POST", "/api/auth/me/password", `{"currentPassword":"password1","newPassword":"brand-new-pass"}`, b.AccessToken)
	entries, _ := e.audit.ListRecent(context.Background(), 10)
	if len(entries) != 1 || entries[0].Action != "account.password" {
		t.Fatalf("entries %+v", entries)
	}
	for _, v := range []*string{entries[0].PreviousValue, entries[0].NewValue} {
		if v != nil && (strings.Contains(*v, "password1") || strings.Contains(*v, "brand-new") || strings.Contains(*v, "$2")) {
			t.Fatalf("secret in audit: %s", *v)
		}
	}
}

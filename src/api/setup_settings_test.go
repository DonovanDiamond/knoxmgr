package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/DonovanDiamond/knoxmgr/src/auth"
)

// setupEnv is a fresh install: the setup code is pending and no account exists.
func setupEnv(t *testing.T) (*env, *auth.SetupCode) {
	t.Helper()
	code, err := auth.NewSetupCode()
	if err != nil {
		t.Fatal(err)
	}
	e := newEnvWith(t, func(d *Deps) { d.Setup = code })
	if _, err := e.db.Exec(`DELETE FROM app_users`); err != nil {
		t.Fatal(err)
	}
	return e, code
}

func bodyOf(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSetupGate(t *testing.T) {
	e, _ := setupEnv(t)
	tests := []struct {
		method, path string
		want         int
		wantCode     string
	}{
		{"GET", "/api/health", 200, ""},
		{"HEAD", "/api/health", 200, ""},
		{"POST", "/api/setup/verify", 400, ""}, // reaches the handler (empty body is bad JSON)
		{"POST", "/api/auth/login", 409, "setup_required"},
		{"GET", "/api/auth/me", 409, "setup_required"},
		{"GET", "/api/knox/orgs", 409, "setup_required"},
		{"GET", "/api/admin/settings/knox", 409, "setup_required"},
		{"GET", "/api/nonexistent", 409, "setup_required"},
		{"GET", "/", 200, ""},
		{"GET", "/_app/immutable/a.js", 200, ""},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			resp := do(t, e.app, tt.method, tt.path, "", "")
			if resp.StatusCode != tt.want {
				t.Fatalf("status %d, want %d", resp.StatusCode, tt.want)
			}
			if tt.wantCode != "" {
				var b errorBody
				decode(t, resp, &b)
				if b.Code != tt.wantCode || b.Error != "setup required" {
					t.Fatalf("body %+v", b)
				}
			}
		})
	}
	var h map[string]any
	decode(t, do(t, e.app, "GET", "/api/health", "", ""), &h)
	if h["setupRequired"] != true || h["knoxConfigured"] != true {
		t.Fatalf("health = %v", h)
	}
}

func TestSetupNotPending(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{"/api/setup/verify", "/api/setup/admin"} {
		resp := do(t, e.app, "POST", path, `{"code":"AAAA-AAAA-AAAA","username":"x","password":"password1"}`, "")
		var b errorBody
		if resp.StatusCode != 409 {
			t.Fatalf("%s: status %d", path, resp.StatusCode)
		}
		decode(t, resp, &b)
		if b.Code != "setup_done" {
			t.Fatalf("%s: body %+v", path, b)
		}
	}
}

func TestSetupVerify(t *testing.T) {
	e, code := setupEnv(t)
	tests := []struct {
		name, body string
		want       int
		wantCode   string
	}{
		{"wrong", `{"code":"AAAA-AAAA-AAAA"}`, 401, "invalid_setup_code"},
		{"empty", `{"code":""}`, 401, "invalid_setup_code"},
		{"huge", `{"code":"` + strings.Repeat("A", 5000) + `"}`, 401, "invalid_setup_code"},
		{"bad json", `nope`, 400, ""},
		{"right", `{"code":"` + code.String() + `"}`, 204, ""},
		{"right lowercase no dashes", `{"code":"` + strings.ToLower(strings.ReplaceAll(code.String(), "-", "")) + `"}`, 204, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := do(t, e.app, "POST", "/api/setup/verify", tt.body, "")
			if resp.StatusCode != tt.want {
				t.Fatalf("status %d, want %d", resp.StatusCode, tt.want)
			}
			if tt.wantCode != "" {
				var b errorBody
				decode(t, resp, &b)
				if b.Code != tt.wantCode {
					t.Fatalf("body %+v", b)
				}
			}
		})
	}
}

func TestSetupCodeRateLimit(t *testing.T) {
	e, code := setupEnv(t)
	for i := range 5 {
		if resp := do(t, e.app, "POST", "/api/setup/verify", `{"code":"AAAA-AAAA-AAAA"}`, ""); resp.StatusCode != 401 {
			t.Fatalf("guess %d: status %d, want 401", i, resp.StatusCode)
		}
	}
	// Once the budget is gone even the right code is refused, on both setup routes.
	right := `{"code":"` + code.String() + `","username":"boss","password":"password1"}`
	for _, path := range []string{"/api/setup/verify", "/api/setup/admin"} {
		if resp := do(t, e.app, "POST", path, right, ""); resp.StatusCode != 429 {
			t.Fatalf("%s after limit: status %d, want 429", path, resp.StatusCode)
		}
	}
	if !code.Required() {
		t.Fatal("a limited request must not complete setup")
	}
	if n, _ := e.users.CountAdmins(context.Background()); n != 0 {
		t.Fatal("admin created while limited")
	}
}

func TestSetupAdmin(t *testing.T) {
	e, code := setupEnv(t)
	ctx := context.Background()
	body := func(code, user, pw string) string {
		b, _ := json.Marshal(map[string]string{"code": code, "username": user, "password": pw})
		return string(b)
	}
	bad := []struct {
		name, body string
		want       int
	}{
		{"wrong code", body("AAAA-AAAA-AAAA", "boss", "password1"), 401},
		{"bad username", body(code.String(), "Bad Name!", "password1"), 400},
		{"short password", body(code.String(), "boss", "short"), 400},
		{"long password", body(code.String(), "boss", strings.Repeat("x", 73)), 400},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			if resp := do(t, e.app, "POST", "/api/setup/admin", tt.body, ""); resp.StatusCode != tt.want {
				t.Fatalf("status %d, want %d", resp.StatusCode, tt.want)
			}
			if n, _ := e.users.CountAdmins(ctx); n != 0 || !code.Required() {
				t.Fatal("rejected request changed state")
			}
		})
	}

	resp := do(t, e.app, "POST", "/api/setup/admin", body(strings.ToLower(code.String()), "Boss", "password1"), "")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, bodyOf(t, resp))
	}
	cookie := refreshCookieOf(resp)
	var got authBody
	decode(t, resp, &got)
	if cookie == nil || cookie.Value == "" || !cookie.HttpOnly || got.AccessToken == "" || got.User.Username != "boss" || got.User.Role != "admin" {
		t.Fatalf("session = %+v cookie = %+v", got, cookie)
	}
	if code.Required() {
		t.Fatal("code must be cleared once the admin exists")
	}
	entries, _ := e.audit.ListRecent(ctx, 5)
	if len(entries) != 1 || entries[0].Action != "setup.create_admin" || entries[0].ActorUsername != "boss" || entries[0].ActorUserID == nil {
		t.Fatalf("audit = %+v", entries)
	}
	for _, v := range []*string{entries[0].PreviousValue, entries[0].NewValue} {
		if v != nil && strings.Contains(*v, "password1") {
			t.Fatal("audit holds the password")
		}
	}

	// The API opens up, the new token works, and setup cannot run again (even with the old code).
	if resp := do(t, e.app, "GET", "/api/auth/me", "", got.AccessToken); resp.StatusCode != 200 {
		t.Fatalf("me status %d", resp.StatusCode)
	}
	if resp := do(t, e.app, "POST", "/api/setup/admin", body(code.String(), "second", "password1"), ""); resp.StatusCode != 409 {
		t.Fatalf("second setup status %d, want 409", resp.StatusCode)
	}
	if resp := do(t, e.app, "POST", "/api/auth/refresh", "", "", cookie); resp.StatusCode != 200 {
		t.Fatalf("refresh with the setup cookie: status %d", resp.StatusCode)
	}
	var h map[string]any
	decode(t, do(t, e.app, "GET", "/api/health", "", ""), &h)
	if h["setupRequired"] != false {
		t.Fatalf("health = %v", h)
	}
}

func TestKnoxNotConfigured(t *testing.T) {
	e := newEnvWith(t, func(d *Deps) { d.Knox = func() Knox { return nil } })
	tok, _ := e.login(t, "root")
	for _, path := range []string{"/api/knox/orgs", "/api/knox/users", "/api/knox/groups", "/api/knox/devices"} {
		resp := do(t, e.app, "GET", path, "", tok.AccessToken)
		var b errorBody
		if resp.StatusCode != 503 {
			t.Fatalf("%s: status %d", path, resp.StatusCode)
		}
		decode(t, resp, &b)
		if b.Code != "knox_not_configured" || b.Error != "Knox is not connected" {
			t.Fatalf("%s: body %+v", path, b)
		}
	}
	// Unauthenticated callers learn nothing about configuration.
	if resp := do(t, e.app, "GET", "/api/knox/orgs", "", ""); resp.StatusCode != 401 {
		t.Fatalf("unauthenticated status %d, want 401", resp.StatusCode)
	}
	// Admin settings keep working so the admin can fix it.
	if resp := do(t, e.app, "GET", "/api/admin/settings/knox", "", tok.AccessToken); resp.StatusCode != 200 {
		t.Fatalf("settings status %d", resp.StatusCode)
	}
	var h map[string]any
	decode(t, do(t, e.app, "GET", "/api/health", "", ""), &h)
	if h["knoxConfigured"] != false {
		t.Fatalf("health = %v", h)
	}
}

const (
	knoxPut = `{"region":"custom","baseURL":"%s","clientId":"cid","tenantId":"t","allowInsecureHTTP":true}`
	adPut   = `{"enabled":true,"url":"ldap://127.0.0.1:1","baseDN":"DC=x,DC=y","upnSuffix":"","bindUser":"svc","bindPassword":"bind-secret-pw","mappings":[{"group":"Admins","role":"admin","orgCode":"","access":""}]}`
)

func TestSettingsAdminOnly(t *testing.T) {
	e := newEnv(t)
	tokens := map[string]string{"": ""}
	for _, u := range []string{"root", "reader", "writer"} {
		b, _ := e.login(t, u)
		tokens[u] = b.AccessToken
	}
	routes := []struct{ method, path, body string }{
		{"GET", "/api/admin/settings/knox", ""},
		{"PUT", "/api/admin/settings/knox", `{}`},
		{"POST", "/api/admin/settings/knox/test", `{}`},
		{"GET", "/api/admin/settings/ad", ""},
		{"PUT", "/api/admin/settings/ad", `{}`},
		{"POST", "/api/admin/settings/ad/test", `{}`},
		{"GET", "/api/admin/settings/general", ""},
		{"PUT", "/api/admin/settings/general", `{}`},
	}
	for _, r := range routes {
		for user, want := range map[string]int{"": 401, "reader": 403, "writer": 403} {
			t.Run(r.method+" "+r.path+" as "+user, func(t *testing.T) {
				resp := do(t, e.app, r.method, r.path, r.body, tokens[user])
				if resp.StatusCode != want {
					t.Fatalf("status %d, want %d", resp.StatusCode, want)
				}
			})
		}
	}
	// Nothing a non-admin sent may have changed anything.
	if n, _ := e.audit.ListRecent(context.Background(), 5); len(n) != 0 {
		t.Fatalf("audit = %+v", n)
	}
}

func TestKnoxSettingsEndpoints(t *testing.T) {
	e := newEnv(t)
	tok, _ := e.login(t, "root")
	a := tok.AccessToken
	const secret = "api-secret-value-xyz"

	var got map[string]any
	decode(t, do(t, e.app, "GET", "/api/admin/settings/knox", "", a), &got)
	regions, _ := got["regions"].([]any)
	if got["secretSet"] != true || got["configured"] != true || len(regions) != 11 {
		t.Fatalf("GET = %v", got)
	}
	if _, has := got["clientSecret"]; has {
		t.Fatal("GET returned a clientSecret field")
	}

	bad := []struct{ name, body string }{
		{"bad json", `{`},
		{"unknown region", `{"region":"mars","clientId":"c","tenantId":"t"}`},
		{"http without allow", `{"region":"custom","baseURL":"http://x.example","clientId":"c","tenantId":"t"}`},
		{"missing client id", `{"region":"eu01","tenantId":"t"}`},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			if resp := do(t, e.app, "PUT", "/api/admin/settings/knox", tt.body, a); resp.StatusCode != 400 {
				t.Fatalf("status %d, want 400", resp.StatusCode)
			}
		})
	}

	// Live swap: a second mock with other credentials replaces the first without a restart.
	second := newEnv(t).mock
	if resp := do(t, e.app, "GET", "/api/knox/orgs", "", a); resp.StatusCode != 200 {
		t.Fatalf("orgs before swap: %d", resp.StatusCode)
	}
	e.mock.Close() // only the second mock can answer from here on
	put := `{"region":"custom","baseURL":"` + second.URL + `","clientId":"cid","tenantId":"t","clientSecret":"secret","allowInsecureHTTP":true}`
	resp := do(t, e.app, "PUT", "/api/admin/settings/knox", put, a)
	out := bodyOf(t, resp)
	if resp.StatusCode != 200 || strings.Contains(out, `"secret"`) || !strings.Contains(out, `"secretSet":true`) || !strings.Contains(out, second.URL) {
		t.Fatalf("PUT = %d %s", resp.StatusCode, out)
	}
	if resp := do(t, e.app, "GET", "/api/knox/orgs", "", a); resp.StatusCode != 200 {
		t.Fatalf("orgs after swap: %d (client not rebuilt)", resp.StatusCode)
	}

	// A secret never appears in any response or in the audit log.
	put = `{"region":"custom","baseURL":"` + second.URL + `","clientId":"cid","tenantId":"t","clientSecret":"` + secret + `","allowInsecureHTTP":true}`
	for _, r := range []*http.Response{
		do(t, e.app, "PUT", "/api/admin/settings/knox", put, a),
		do(t, e.app, "GET", "/api/admin/settings/knox", "", a),
		do(t, e.app, "POST", "/api/admin/settings/knox/test", put, a),
		do(t, e.app, "GET", "/api/admin/audit?limit=100", "", a),
	} {
		if s := bodyOf(t, r); strings.Contains(s, secret) {
			t.Fatalf("response leaks the secret: %s", s)
		}
	}
	// The wrong secret was saved just above, so the live client is now rejected by the mock.
	// Reads come from the mirror, so probe with a write, which goes to Knox first.
	if resp := do(t, e.app, "POST", "/api/knox/groups", `{"name":"probe"}`, a); resp.StatusCode == 201 {
		t.Fatal("wrong secret should fail against the mock")
	}
	var audited struct{ Entries []struct{ Action string } }
	decode(t, do(t, e.app, "GET", "/api/admin/audit?limit=100", "", a), &audited)
	n := 0
	for _, en := range audited.Entries {
		if en.Action == "settings.knox" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("settings.knox audit entries = %d, want 2", n)
	}
}

func TestKnoxTestEndpoint(t *testing.T) {
	e := newEnv(t)
	tok, _ := e.login(t, "root")
	base := `{"region":"custom","baseURL":"` + e.mock.URL + `","clientId":"cid","tenantId":"t","allowInsecureHTTP":true%s}`
	tests := []struct {
		name, extra string
		wantOK      bool
		wantStage   string
	}{
		{"stored secret", ``, true, ""},
		{"wrong secret", `,"clientSecret":"nope-nope"`, false, "token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := do(t, e.app, "POST", "/api/admin/settings/knox/test", strings.Replace(base, "%s", tt.extra, 1), tok.AccessToken)
			var got map[string]any
			if resp.StatusCode != 200 {
				t.Fatalf("status %d", resp.StatusCode)
			}
			decode(t, resp, &got)
			if got["ok"] != tt.wantOK || got["message"] == "" {
				t.Fatalf("got %v", got)
			}
			if stage, _ := got["stage"].(string); stage != tt.wantStage {
				t.Fatalf("stage %q, want %q", stage, tt.wantStage)
			}
		})
	}
	if n, _ := e.audit.ListRecent(context.Background(), 5); len(n) != 0 {
		t.Fatalf("a test must not audit: %+v", n)
	}
}

func TestADAndGeneralEndpoints(t *testing.T) {
	e := newEnv(t)
	tok, _ := e.login(t, "root")
	a := tok.AccessToken

	resp := do(t, e.app, "PUT", "/api/admin/settings/ad", adPut, a)
	out := bodyOf(t, resp)
	if resp.StatusCode != 200 || strings.Contains(out, "bind-secret-pw") || !strings.Contains(out, `"bindPasswordSet":true`) || !strings.Contains(out, `"group":"Admins"`) {
		t.Fatalf("PUT ad = %d %s", resp.StatusCode, out)
	}
	if s := bodyOf(t, do(t, e.app, "GET", "/api/admin/settings/ad", "", a)); strings.Contains(s, "bind-secret-pw") || !strings.Contains(s, `"bindPasswordSet":true`) {
		t.Fatalf("GET ad = %s", s)
	}
	// An absent password keeps the stored one.
	keep := strings.Replace(adPut, `"bindPassword":"bind-secret-pw",`, ``, 1)
	if s := bodyOf(t, do(t, e.app, "PUT", "/api/admin/settings/ad", keep, a)); !strings.Contains(s, `"bindPasswordSet":true`) {
		t.Fatalf("keep = %s", s)
	}
	if resp := do(t, e.app, "PUT", "/api/admin/settings/ad", `{"enabled":true,"url":"ldap://x","baseDN":"DC=x","mappings":[]}`, a); resp.StatusCode != 400 {
		t.Fatalf("enabled without mappings: %d", resp.StatusCode)
	}

	var res map[string]any
	decode(t, do(t, e.app, "POST", "/api/admin/settings/ad/test", `{"username":"ann","password":"pw"}`, a), &res)
	if res["ok"] != false || res["code"] != "ad_unreachable" {
		t.Fatalf("ad test = %v", res)
	}
	decode(t, do(t, e.app, "POST", "/api/admin/settings/ad/test", `{"username":"ann","password":"pw","settings":{"enabled":false}}`, a), &res)
	if res["ok"] != false || res["code"] != "ad_disabled" {
		t.Fatalf("ad test disabled = %v", res)
	}
	if resp := do(t, e.app, "POST", "/api/admin/settings/ad/test", `{"username":"","password":""}`, a); resp.StatusCode != 400 {
		t.Fatalf("empty credentials: %d", resp.StatusCode)
	}

	var gen map[string]any
	decode(t, do(t, e.app, "GET", "/api/admin/settings/general", "", a), &gen)
	if gen["installationName"] != "knoxmgr" || gen["syncIntervalMinutes"] != float64(15) || gen["checkUpdates"] != true || gen["showMap"] != true {
		t.Fatalf("general default = %v", gen)
	}
	for _, body := range []string{`{"installationName":"","syncIntervalMinutes":15}`, `{"installationName":"A","syncIntervalMinutes":0}`, `{"installationName":"A","syncIntervalMinutes":1441}`} {
		if resp := do(t, e.app, "PUT", "/api/admin/settings/general", body, a); resp.StatusCode != 400 {
			t.Fatalf("%s: status %d", body, resp.StatusCode)
		}
	}
	if resp := do(t, e.app, "PUT", "/api/admin/settings/general", `{"installationName":"Acme Corp","syncIntervalMinutes":30}`, a); resp.StatusCode != 200 {
		t.Fatalf("PUT general: %d", resp.StatusCode)
	}
	var h map[string]any
	decode(t, do(t, e.app, "GET", "/api/health", "", ""), &h)
	if h["name"] != "Acme Corp" {
		t.Fatalf("health name not live: %v", h)
	}
}

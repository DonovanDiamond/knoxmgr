package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jmoiron/sqlx"
	"github.com/rs/zerolog"

	"github.com/DonovanDiamond/knoxmgr/src/auth"
	"github.com/DonovanDiamond/knoxmgr/src/config"
	"github.com/DonovanDiamond/knoxmgr/src/db"
	"github.com/DonovanDiamond/knoxmgr/src/knox"
	"github.com/DonovanDiamond/knoxmgr/src/knoxmock"
	"github.com/DonovanDiamond/knoxmgr/src/secretbox"
	"github.com/DonovanDiamond/knoxmgr/src/settings"
	"github.com/DonovanDiamond/knoxmgr/src/update"
)

type fakeDir struct {
	user *auth.DirectoryUser
	err  error
}

func (f *fakeDir) Authenticate(context.Context, string, string, []string) (*auth.DirectoryUser, error) {
	return f.user, f.err
}

type fakeSyncer struct {
	id      int64
	already bool
	running bool
	err     error
	calls   int
}

func (f *fakeSyncer) SyncNow(context.Context) (int64, bool, error) {
	f.calls++
	return f.id, f.already, f.err
}

func (f *fakeSyncer) Running() (int64, bool) { return f.id, f.running }

// seedPage is larger than the mock tenant, so one page holds everything.
const seedPage = 1000

// seedMirror copies the whole Knox mock tenant into the mirror, as a sync would.
func seedMirror(t *testing.T, k *knox.Client, m *db.Mirror) {
	t.Helper()
	ctx := context.Background()
	var snap db.Snapshot
	orgs, err := k.Orgs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range orgs {
		snap.Orgs = append(snap.Orgs, db.MirrorOrg{OrgCode: o.OrgCode, OrgName: o.OrgName, ParentCode: o.ParentCode})
	}
	users, err := k.SelectUsers(ctx, knox.UserQuery{Limit: seedPage})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users.Users {
		snap.Users = append(snap.Users, mirrorOfUser(u))
	}
	groups, err := k.SelectGroups(ctx, knox.GroupQuery{Limit: seedPage})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups.Groups {
		snap.Groups = append(snap.Groups, db.MirrorGroup{GroupID: g.GroupID, GroupName: g.GroupName, GroupType: g.GroupType})
		members, err := k.SelectGroupUsers(ctx, g.GroupID, 0, seedPage)
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range members.Users {
			snap.Members = append(snap.Members, db.MirrorMember{GroupID: g.GroupID, UserID: u.UserID})
		}
	}
	devices, err := k.SelectDeviceList(ctx, knox.DeviceQuery{Limit: seedPage})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range devices.Devices {
		snap.Devices = append(snap.Devices, db.MirrorDevice{DeviceID: d.DeviceID, DeviceModel: d.DeviceModel, DeviceModelKind: d.DeviceModelKind,
			DeviceStatus: d.DeviceStatus, DeviceVersionName: d.DeviceVersionName, Platform: d.Platform, UserID: d.UserID,
			UserName: d.UserName, OrgCode: d.OrgCode, LastConnection: d.LastConnection})
	}
	if err := m.ReplaceAll(ctx, snap, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// fakeInventory stands in for the Knox inventory pull; it records calls and can fail.
type fakeInventory struct {
	err   error
	calls int
}

func (f *fakeInventory) PullDevice(context.Context, string) error { f.calls++; return f.err }

type env struct {
	app   *fiber.App
	svc   *auth.Service
	dir   *fakeDir
	users *db.Users
	audit *db.Audit
	mgr   *settings.Manager
	mock  *httptest.Server
	db    *sqlx.DB
	// mirror is seeded from the Knox mock; sync is the fake behind the admin sync endpoints.
	mirror *db.Mirror
	sync   *fakeSyncer
	apps   *db.Apps
	inv    *fakeInventory
}

func newEnv(t *testing.T) *env { return newEnvWith(t, nil) }

// newEnvWith lets a test adjust the Deps (log sink, trusted proxies) before the app is built.
func newEnvWith(t *testing.T, tweak func(*Deps)) *env {
	t.Helper()
	d, err := db.Open("sqlite", filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	users := db.NewUsers(d)
	dir := &fakeDir{}
	mappings := []config.ADGroup{{Group: "Admins", Role: config.RoleAdmin}}
	svc := auth.NewService(users, db.NewRefreshTokens(d), auth.NewTokens(priv, pub, 15*time.Minute), time.Hour, dir, mappings)
	for _, u := range []struct{ name, role, access string }{
		{"root", config.RoleAdmin, ""}, {"reader", config.RoleUser, config.AccessRead}, {"writer", config.RoleUser, config.AccessWrite},
	} {
		hash, _ := auth.HashPassword("password1")
		du := &db.User{Username: u.name, DisplayName: u.name, PasswordHash: &hash, Role: u.role, AuthSource: "local"}
		if u.role == config.RoleUser {
			org := "SALES"
			du.OrgCode, du.Access = &org, &u.access
		}
		if err := users.Create(context.Background(), du); err != nil {
			t.Fatal(err)
		}
	}
	static := fstest.MapFS{
		"index.html": {Data: []byte(`<html>spa<script>var a=1;</script><script src="/x.js"></script>` +
			`<script type="module">start()</script></html>`)},
		"_app/immutable/a.js":  {Data: []byte("console.log(1)")},
		"manifest.webmanifest": {Data: []byte(`{"name":"knoxmgr"}`)},
	}
	mock := httptest.NewServer(knoxmock.New("cid", "secret", zerolog.Nop()))
	t.Cleanup(mock.Close)
	box, err := secretbox.LoadOrCreate(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	mgr := settings.New(db.NewAppSettings(d), box, svc, zerolog.Nop())
	if err := mgr.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	root, err := users.GetByUsername(context.Background(), "root")
	if err != nil {
		t.Fatal(err)
	}
	// The live client is built by the same path an admin's save takes.
	if _, err := mgr.SaveKnox(context.Background(), settings.KnoxInput{Region: "custom", BaseURL: mock.URL, ClientID: "cid", TenantID: "t",
		ClientSecret: "secret", AllowInsecureHTTP: true}, &auth.Principal{UserID: root.ID, Username: "root"}); err != nil {
		t.Fatal(err)
	}
	// The tests count audit rows, so start from an empty log; and Load reset the AD swap-in that
	// the tests drive through fakeDir.
	if _, err := d.Exec(`DELETE FROM audit_log`); err != nil {
		t.Fatal(err)
	}
	svc.SetDirectory(dir, mappings)
	audit := db.NewAudit(d)
	mirror := db.NewMirror(d)
	seedMirror(t, mgr.Knox(), mirror)
	syncer := &fakeSyncer{}
	apps, inv := db.NewApps(d), &fakeInventory{}
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v0.5.0"}`))
	}))
	t.Cleanup(gh.Close)
	deps := Deps{Log: zerolog.Nop(), Version: "v0.4.0", Update: update.New("v0.4.0", gh.URL, mgr.CheckUpdates, zerolog.Nop()), Auth: svc, Users: users, Audit: audit, Settings: mgr, Mirror: mirror, Apps: apps, Inventory: inv, Sync: syncer,
		Knox: func() Knox {
			if c := mgr.Knox(); c != nil {
				return c
			}
			return nil
		},
		LoginLimiter: auth.NewLoginLimiter(), SetupLimiter: auth.NewLoginLimiter(), RefreshTTL: time.Hour, SecureCookies: true, Static: static}
	if tweak != nil {
		tweak(&deps)
	}
	app := New(deps)
	return &env{app: app, svc: svc, dir: dir, users: users, audit: audit, mgr: mgr, mock: mock, db: d, mirror: mirror, sync: syncer, apps: apps, inv: inv}
}

func do(t *testing.T, app *fiber.App, method, path, body, bearer string, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decode(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}

func refreshCookieOf(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == refreshCookie {
			return c
		}
	}
	return nil
}

func (e *env) login(t *testing.T, user string) (authBody, *http.Cookie) {
	t.Helper()
	resp := do(t, e.app, "POST", "/api/auth/login", `{"username":"`+user+`","password":"password1"}`, "")
	if resp.StatusCode != 200 {
		t.Fatalf("login %s: status %d", user, resp.StatusCode)
	}
	c := refreshCookieOf(resp)
	var b authBody
	decode(t, resp, &b)
	return b, c
}

func TestHealth(t *testing.T) {
	var got map[string]any
	decode(t, do(t, newEnv(t).app, "GET", "/api/health", "", ""), &got)
	if got["status"] != "ok" || got["name"] != "knoxmgr" || got["knoxConfigured"] != true || got["setupRequired"] != false || got["version"] != "v0.4.0" || got["secureCookies"] == nil {
		t.Fatalf("got %v", got)
	}
}

func TestHealthMethods(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		t.Run(method, func(t *testing.T) {
			resp := do(t, newEnv(t).app, method, "/api/health", "", "")
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatalf("status %d", resp.StatusCode)
			}
			if method == "HEAD" {
				if b, _ := io.ReadAll(resp.Body); len(b) != 0 {
					t.Fatalf("HEAD body %q", b)
				}
			}
		})
	}
}

func TestLogin(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		dir      *fakeDir
		status   int
		wantCode string
	}{
		{"local ok", `{"username":"root","password":"password1"}`, nil, 200, ""},
		{"wrong password", `{"username":"root","password":"nope"}`, nil, 401, ""},
		{"unknown user falls to AD, bad creds", `{"username":"ghost","password":"x"}`, &fakeDir{err: auth.ErrInvalidCredentials}, 401, ""},
		{"ad ok", `{"username":"ann","password":"x"}`, &fakeDir{user: &auth.DirectoryUser{DisplayName: "Ann", Groups: []string{"Admins"}}}, 200, ""},
		{"ad unreachable", `{"username":"ann","password":"x"}`, &fakeDir{err: auth.ErrDirectoryUnreachable}, 503, "ad_unreachable"},
		{"ad no mapped group", `{"username":"ann","password":"x"}`, &fakeDir{user: &auth.DirectoryUser{DisplayName: "Ann"}}, 403, ""},
		{"empty password", `{"username":"root","password":""}`, nil, 400, ""},
		{"not json", `nope`, nil, 400, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			if tt.dir != nil {
				*e.dir = *tt.dir
			}
			resp := do(t, e.app, "POST", "/api/auth/login", tt.body, "")
			if resp.StatusCode != tt.status {
				t.Fatalf("status %d, want %d", resp.StatusCode, tt.status)
			}
			c := refreshCookieOf(resp)
			if tt.status == 200 {
				if c == nil || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/api/auth" {
					t.Fatalf("bad cookie %+v", c)
				}
				var b authBody
				decode(t, resp, &b)
				if b.AccessToken == "" || b.ExpiresAt == "" || b.User.Username == "" {
					t.Fatalf("bad body %+v", b)
				}
				return
			}
			if c != nil {
				t.Fatal("cookie set on failure")
			}
			var eb errorBody
			decode(t, resp, &eb)
			if eb.Error == "" || eb.Code != tt.wantCode {
				t.Fatalf("body %+v", eb)
			}
		})
	}
}

func TestRefreshRotatesAndLogoutRevokes(t *testing.T) {
	e := newEnv(t)
	_, c1 := e.login(t, "root")

	resp := do(t, e.app, "POST", "/api/auth/refresh", "", "", c1)
	if resp.StatusCode != 200 {
		t.Fatalf("refresh status %d", resp.StatusCode)
	}
	c2 := refreshCookieOf(resp)
	if c2 == nil || c2.Value == c1.Value {
		t.Fatal("refresh token was not rotated")
	}
	if r := do(t, e.app, "POST", "/api/auth/refresh", "", "", c1); r.StatusCode != 401 {
		t.Fatalf("reused token status %d, want 401", r.StatusCode)
	}
	if r := do(t, e.app, "POST", "/api/auth/refresh", "", ""); r.StatusCode != 401 {
		t.Fatalf("no cookie status %d, want 401", r.StatusCode)
	}

	out := do(t, e.app, "POST", "/api/auth/logout", "", "", c2)
	if out.StatusCode != 204 {
		t.Fatalf("logout status %d", out.StatusCode)
	}
	if cc := refreshCookieOf(out); cc == nil || cc.MaxAge >= 0 {
		t.Fatalf("cookie not cleared: %+v", cc)
	}
	if r := do(t, e.app, "POST", "/api/auth/refresh", "", "", c2); r.StatusCode != 401 {
		t.Fatalf("after logout status %d, want 401", r.StatusCode)
	}
}

func TestMe(t *testing.T) {
	e := newEnv(t)
	b, _ := e.login(t, "writer")
	tests := []struct {
		name, token string
		status      int
	}{
		{"ok", b.AccessToken, 200},
		{"missing", "", 401},
		{"invalid", "garbage", 401},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := do(t, e.app, "GET", "/api/auth/me", "", tt.token)
			if resp.StatusCode != tt.status {
				t.Fatalf("status %d, want %d", resp.StatusCode, tt.status)
			}
			if tt.status == 200 {
				var u userBody
				decode(t, resp, &u)
				if u.Username != "writer" || u.Access == nil || *u.Access != "write" || u.OrgCode == nil || u.AuthSource != "local" {
					t.Fatalf("user %+v", u)
				}
			}
		})
	}
}

func TestRouteMiddleware(t *testing.T) {
	e := newEnv(t)
	app := fiber.New(fiber.Config{ErrorHandler: (&server{}).errorHandler})
	ok := func(c fiber.Ctx) error { return c.SendString("ok") }
	// Guards go first: handlers run in registration order.
	app.Use("/admin", Authenticate(e.svc, e.users), RequireAdmin())
	app.Use("/write", Authenticate(e.svc, e.users), RequireWrite())
	app.Get("/admin", ok)
	app.Get("/write", ok)

	tokens := map[string]string{}
	for _, u := range []string{"root", "reader", "writer"} {
		b, _ := e.login(t, u)
		tokens[u] = b.AccessToken
	}
	tests := []struct {
		name, path, token string
		status            int
	}{
		{"admin no token", "/admin", "", 401},
		{"admin bad token", "/admin", "garbage", 401},
		{"admin as user", "/admin", tokens["writer"], 403},
		{"admin as admin", "/admin", tokens["root"], 200},
		{"write as read", "/write", tokens["reader"], 403},
		{"write as write", "/write", tokens["writer"], 200},
		{"write as admin", "/write", tokens["root"], 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := do(t, app, "GET", tt.path, "", tt.token).StatusCode; got != tt.status {
				t.Fatalf("status %d, want %d", got, tt.status)
			}
		})
	}
}

func TestWebAndNotFound(t *testing.T) {
	app := newEnv(t).app
	tests := []struct {
		name, method, path string
		status             int
		contains           string
	}{
		{"root", "GET", "/", 200, "spa"},
		{"spa route falls back", "GET", "/login", 200, "spa"},
		{"nested spa route", "GET", "/users/12/edit", 200, "spa"},
		{"asset", "GET", "/_app/immutable/a.js", 200, "console.log"},
		{"missing asset", "GET", "/_app/missing.js", 404, ""},
		{"dotted spa deep link", "GET", "/apps/com.facebook.katana", 200, "spa"},
		{"manifest", "GET", "/manifest.webmanifest", 200, `"knoxmgr"`},
		{"missing service worker is not the SPA", "GET", "/service-worker.js", 404, ""},
		{"missing css", "GET", "/missing.css", 404, ""},
		{"missing upper-case png", "GET", "/logo.PNG", 404, ""},
		{"missing under _app without ext-match", "GET", "/_app/x.y", 404, ""},
		{"unknown api", "GET", "/api/nope", 404, `"error"`},
		{"unknown api nested", "POST", "/api/auth/nope/x", 404, `"error"`},
		{"traversal", "GET", "/../../etc/passwd", 200, "spa"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := do(t, app, tt.method, tt.path, "", "")
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tt.status || !strings.Contains(string(body), tt.contains) {
				t.Fatalf("status %d body %q", resp.StatusCode, body)
			}
			if tt.name == "manifest" && resp.Header.Get("Content-Type") != "application/manifest+json" {
				t.Fatalf("content type %q", resp.Header.Get("Content-Type"))
			}
			if strings.HasPrefix(tt.path, "/api") && !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
				t.Fatalf("content type %q", resp.Header.Get("Content-Type"))
			}
		})
	}
}

func TestBodyLimit(t *testing.T) {
	app := newEnv(t).app
	big := `{"username":"` + strings.Repeat("a", maxBody) + `","password":"x"}`
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(big))
	req.Header.Set("Content-Type", "application/json")
	// fasthttp aborts the connection once the limit is crossed, so Test reports an error, not a 413.
	if resp, err := app.Test(req); err == nil {
		t.Fatalf("oversized body accepted, status %d", resp.StatusCode)
	}
}

func TestKnoxUnreachableIs502(t *testing.T) {
	s := &server{Deps: Deps{Log: zerolog.Nop()}}
	app := fiber.New(fiber.Config{ErrorHandler: s.errorHandler})
	app.Get("/x", func(fiber.Ctx) error { return errors.Join(errors.New("request failed"), knox.ErrUnreachable) })
	resp := do(t, app, "GET", "/x", "", "")
	var body errorBody
	decode(t, resp, &body)
	if resp.StatusCode != 502 || body.Code != "knox_unreachable" || !strings.Contains(body.Error, "could not be reached") {
		t.Fatalf("got %d %+v", resp.StatusCode, body)
	}
}

func TestKnoxErrorIs502(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"result code", &knox.Error{Code: "1001", Message: "bad thing"}, "Knox rejected the request: bad thing"},
		{"http status", &knox.Error{Code: "http", Message: "forbidden", HTTPStatus: 403}, "Knox answered HTTP 403: forbidden"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &server{Deps: Deps{Log: zerolog.Nop()}}
			app := fiber.New(fiber.Config{ErrorHandler: s.errorHandler})
			app.Get("/x", func(c fiber.Ctx) error {
				c.Locals(principalKey, &auth.Principal{Role: config.RoleAdmin}) // admins see Knox's detail
				return fmt.Errorf("wrapped: %w", tt.err)
			})
			resp := do(t, app, "GET", "/x", "", "")
			var body errorBody
			decode(t, resp, &body)
			if resp.StatusCode != 502 || body.Code != "knox_error" || body.Error != tt.want {
				t.Fatalf("got %d %+v", resp.StatusCode, body)
			}
		})
	}
}

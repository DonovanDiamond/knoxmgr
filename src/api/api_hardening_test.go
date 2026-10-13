package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/DonovanDiamond/knoxmgr/src/auth"
	"github.com/DonovanDiamond/knoxmgr/src/knox"
	"github.com/DonovanDiamond/knoxmgr/web"
)

func hash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

func TestSecurityHeaders(t *testing.T) {
	app := newEnv(t).app
	api := do(t, app, "GET", "/api/health", "", "")
	if got := api.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("api Cache-Control %q", got)
	}
	for k, want := range map[string]string{
		"Strict-Transport-Security": "max-age=31536000",
		"Permissions-Policy":        "camera=(), microphone=(), geolocation=()",
	} {
		if got := api.Header.Get(k); got != want {
			t.Fatalf("%s = %q, want %q", k, got, want)
		}
	}
	if api.Header.Get("Content-Security-Policy") != "" {
		t.Fatal("CSP set on API response")
	}

	csp := do(t, app, "GET", "/login", "", "").Header.Get("Content-Security-Policy")
	want := "default-src 'self'; script-src 'self' " + hash("var a=1;") + " " + hash("start()") +
		"; img-src 'self' data: https://tile.openstreetmap.org; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'"
	if csp != want {
		t.Fatalf("csp\n got %q\nwant %q", csp, want)
	}
	if do(t, app, "GET", "/_app/immutable/a.js", "", "").Header.Get("Content-Security-Policy") != "" {
		t.Fatal("CSP set on a JS asset")
	}
}

func TestNoHSTSWithoutSecureCookies(t *testing.T) {
	e := newEnvWith(t, func(d *Deps) { d.SecureCookies = false })
	if got := do(t, e.app, "GET", "/api/health", "", "").Header.Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("HSTS %q", got)
	}
}

func TestContentSecurityPolicyBuiltIndex(t *testing.T) {
	// Every inline script of the real build must be hashed; a src script must not be.
	static, err := web.FS()
	if err != nil {
		t.Fatal(err)
	}
	csp := contentSecurityPolicy(static)
	if strings.Count(csp, "'sha256-") < 2 {
		t.Fatalf("expected hashes for the theme and bootstrap scripts: %s", csp)
	}
}

func TestClientIPFromTrustedProxy(t *testing.T) {
	// app.Test connects from 0.0.0.0, so that is the peer address the proxy list is matched against.
	tests := []struct {
		name    string
		proxies []string
		wantIP  string
	}{
		{"trusted peer uses XFF", []string{"0.0.0.0"}, "203.0.113.9"},
		{"untrusted peer ignores XFF", []string{"10.9.9.9"}, "0.0.0.0"},
		{"no proxies ignores XFF", nil, "0.0.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			e := newEnvWith(t, func(d *Deps) { d.Log = zerolog.New(&buf); d.TrustedProxies = tt.proxies })
			req := httptest.NewRequest("GET", "/api/health", nil)
			req.Header.Set("X-Forwarded-For", "203.0.113.9")
			if _, err := e.app.Test(req); err != nil {
				t.Fatal(err)
			}
			if want := `"ip":"` + tt.wantIP + `"`; !strings.Contains(buf.String(), want) {
				t.Fatalf("want %s in log %s", want, buf.String())
			}
		})
	}
}

func TestClientIPSpoofedChain(t *testing.T) {
	// The client prepends a fake hop; the proxy appends the real peer. The real client is the
	// rightmost hop that is not a trusted proxy, not the leftmost value.
	var buf bytes.Buffer
	e := newEnvWith(t, func(d *Deps) { d.Log = zerolog.New(&buf); d.TrustedProxies = []string{"0.0.0.0"} })
	req := httptest.NewRequest("GET", "/api/health", nil)
	req.Header.Set("X-Forwarded-For", "1.1.1.1, 203.0.113.9")
	if _, err := e.app.Test(req); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"ip":"203.0.113.9"`) {
		t.Fatalf("log %s", buf.String())
	}
}

func TestAuthPostsRejectCrossOrigin(t *testing.T) {
	e := newEnv(t)
	tests := []struct {
		name, path, origin string
		status             int
	}{
		{"refresh no origin", "/api/auth/refresh", "", 401},
		{"refresh same origin", "/api/auth/refresh", "http://example.com", 401},
		{"refresh other origin", "/api/auth/refresh", "https://evil.test", 403},
		{"refresh null origin", "/api/auth/refresh", "null", 403},
		{"logout no origin", "/api/auth/logout", "", 204},
		{"logout same origin", "/api/auth/logout", "https://example.com", 204},
		{"logout other origin", "/api/auth/logout", "https://example.com.evil.test", 403},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", tt.path, nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			resp, err := e.app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tt.status {
				t.Fatalf("status %d, want %d", resp.StatusCode, tt.status)
			}
		})
	}
}

func TestKnox4xxDetailOnlyForAdmins(t *testing.T) {
	var buf bytes.Buffer
	s := &server{Deps: Deps{Log: zerolog.New(&buf)}}
	app := fiber.New(fiber.Config{ErrorHandler: s.errorHandler})
	kerr := &knox.Error{Code: "http", Message: "tenant secret detail", HTTPStatus: 403}
	app.Get("/anon", func(fiber.Ctx) error { return kerr })
	app.Get("/user", func(c fiber.Ctx) error {
		c.Locals(principalKey, &auth.Principal{Role: "user"})
		return kerr
	})
	app.Get("/admin", func(c fiber.Ctx) error {
		c.Locals(principalKey, &auth.Principal{Role: "admin"})
		return kerr
	})
	app.Get("/user5xx", func(c fiber.Ctx) error {
		c.Locals(principalKey, &auth.Principal{Role: "user"})
		return &knox.Error{Code: "http", Message: "boom", HTTPStatus: 500}
	})
	tests := []struct{ path, want string }{
		{"/anon", "Knox rejected the request"},
		{"/user", "Knox rejected the request"},
		{"/admin", "Knox answered HTTP 403: tenant secret detail"},
		{"/user5xx", "Knox answered HTTP 500: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			var body errorBody
			resp := do(t, app, "GET", tt.path, "", "")
			decode(t, resp, &body)
			if resp.StatusCode != 502 || body.Error != tt.want {
				t.Fatalf("got %d %+v", resp.StatusCode, body)
			}
		})
	}
	if !strings.Contains(buf.String(), "tenant secret detail") {
		t.Fatalf("detail missing from log: %s", buf.String())
	}
}

func TestLoginFailureLogHidesUnknownUsernames(t *testing.T) {
	tests := []struct {
		name, user string
		contains   string
		absent     string
	}{
		{"known account", " Reader ", `"username":"reader"`, "userHash"},
		{"unknown account", "typo-password-1", `"userHash":"` + hash12("typo-password-1") + `"`, "typo-password-1\""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			e := newEnvWith(t, func(d *Deps) { d.Log = zerolog.New(&buf) })
			e.dir.err = auth.ErrInvalidCredentials
			resp := do(t, e.app, "POST", "/api/auth/login", `{"username":"`+tt.user+`","password":"wrong-password"}`, "")
			if resp.StatusCode != 401 {
				t.Fatalf("status %d", resp.StatusCode)
			}
			log := buf.String()
			if !strings.Contains(log, tt.contains) || strings.Contains(log, tt.absent) {
				t.Fatalf("log %s", log)
			}
		})
	}
}

func hash12(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}

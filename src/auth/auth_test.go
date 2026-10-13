package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DonovanDiamond/knoxmgr/src/config"
	"github.com/DonovanDiamond/knoxmgr/src/db"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

func init() { bcryptCost = bcrypt.MinCost }

func TestPassword(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		hash     *string
		password string
		want     bool
	}{
		{"correct", &hash, "correct horse", true},
		{"wrong", &hash, "correct horsf", false},
		{"empty", &hash, "", false},
		{"missing user", nil, "correct horse", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CheckPassword(tt.hash, tt.password); got != tt.want {
				t.Fatalf("CheckPassword = %v, want %v", got, tt.want)
			}
		})
	}
	for _, pw := range []string{"short", string(make([]byte, 73))} {
		if _, err := HashPassword(pw); err == nil {
			t.Fatalf("HashPassword(len %d) should fail", len(pw))
		}
	}
}

func newTokens(t *testing.T, ttl time.Duration) *Tokens {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return NewTokens(priv, pub, ttl)
}

func TestJWT(t *testing.T) {
	tk := newTokens(t, 15*time.Minute)
	base := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	tk.now = func() time.Time { return base }
	want := Principal{UserID: 7, Username: "alice", Role: config.RoleUser, OrgCode: "ORG1", Access: config.AccessWrite}
	good, exp, err := tk.Issue(want)
	if err != nil || !exp.Equal(base.Add(15*time.Minute)) {
		t.Fatalf("Issue = %v, %v", exp, err)
	}

	other := newTokens(t, time.Minute)
	other.now = tk.now
	wrongKey, _, _ := other.Issue(want)

	hs := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"uid": 7, "role": "admin", "iss": issuer, "exp": base.Add(time.Hour).Unix()})
	hsToken, _ := hs.SignedString([]byte("x"))
	none, _ := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{"uid": 7, "role": "admin", "iss": issuer, "exp": base.Add(time.Hour).Unix()}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	noExp := mustSign(t, tk, claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: issuer}, UID: 7, Role: config.RoleAdmin})
	badRole := mustSign(t, tk, claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: issuer, ExpiresAt: jwt.NewNumericDate(base.Add(time.Hour))}, UID: 7, Role: "root"})
	badIss := mustSign(t, tk, claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: "evil", ExpiresAt: jwt.NewNumericDate(base.Add(time.Hour))}, UID: 7, Role: config.RoleAdmin})

	tests := []struct {
		name  string
		token string
		at    time.Time
		ok    bool
	}{
		{"valid", good, base, true},
		{"valid just before expiry", good, base.Add(15*time.Minute - time.Second), true},
		{"expired", good, base.Add(15*time.Minute + time.Second), false},
		{"wrong key", wrongKey, base, false},
		{"hmac alg confusion", hsToken, base, false},
		{"alg none", none, base, false},
		{"no expiry", noExp, base, false},
		{"unknown role", badRole, base, false},
		{"wrong issuer", badIss, base, false},
		{"garbage", "not.a.jwt", base, false},
		{"empty", "", base, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tk.now = func() time.Time { return tt.at }
			p, err := tk.Verify(tt.token)
			if !tt.ok {
				if !errors.Is(err, ErrInvalidToken) {
					t.Fatalf("want ErrInvalidToken, got %v", err)
				}
				return
			}
			if err != nil || *p != want {
				t.Fatalf("Verify = %+v, %v; want %+v", p, err, want)
			}
		})
	}
}

func mustSign(t *testing.T, tk *Tokens, c claims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, c).SignedString(tk.priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestKeys(t *testing.T) {
	dir := t.TempDir()
	priv, pub := filepath.Join(dir, "keys", "jwt.key"), filepath.Join(dir, "keys", "jwt.pub")

	k1, p1, err := LoadOrGenerateKeys(priv, pub)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{priv, pub} {
		if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("%s: mode %v, err %v; want 0600", path, fi.Mode().Perm(), err)
		}
	}
	k2, p2, err := LoadOrGenerateKeys(priv, pub)
	if err != nil || !k1.Equal(k2) || !p1.Equal(p2) {
		t.Fatalf("reload should return the same pair: %v", err)
	}

	if err := os.Remove(pub); err != nil {
		t.Fatal(err)
	}
	if _, p3, err := LoadOrGenerateKeys(priv, pub); err != nil || !p1.Equal(p3) {
		t.Fatalf("missing public key should be re-derived: %v", err)
	}

	other := filepath.Join(dir, "other.pub")
	if _, _, err := LoadOrGenerateKeys(filepath.Join(dir, "other.key"), other); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(other)
	os.WriteFile(pub, b, 0o600)
	if _, _, err := LoadOrGenerateKeys(priv, pub); err == nil {
		t.Fatal("mismatched public key must be refused")
	}

	os.WriteFile(priv, []byte("junk"), 0o600)
	if _, _, err := LoadOrGenerateKeys(priv, pub); err == nil {
		t.Fatal("corrupt private key must be refused")
	}
}

func TestMapGroups(t *testing.T) {
	mappings := []config.ADGroup{
		{Group: "Readers", Role: config.RoleUser, OrgCode: "A", Access: config.AccessRead},
		{Group: "Writers", Role: config.RoleUser, OrgCode: "B", Access: config.AccessWrite},
		{Group: "Admins", Role: config.RoleAdmin},
	}
	tests := []struct {
		name   string
		member []string
		want   Grant
		ok     bool
	}{
		{"no groups", nil, Grant{}, false},
		{"unmapped group", []string{"Other"}, Grant{}, false},
		{"reader", []string{"Readers"}, Grant{config.RoleUser, "A", config.AccessRead}, true},
		{"writer", []string{"Writers"}, Grant{config.RoleUser, "B", config.AccessWrite}, true},
		{"admin", []string{"Admins"}, Grant{Role: config.RoleAdmin}, true},
		{"admin wins over user", []string{"Readers", "Admins"}, Grant{Role: config.RoleAdmin}, true},
		{"first user mapping wins", []string{"Writers", "Readers"}, Grant{config.RoleUser, "A", config.AccessRead}, true},
		{"case sensitive", []string{"admins"}, Grant{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := MapGroups(mappings, tt.member)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("MapGroups = %+v, %v; want %+v, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestAuthorization(t *testing.T) {
	admin := &Principal{Role: config.RoleAdmin}
	reader := &Principal{Role: config.RoleUser, OrgCode: "A", Access: config.AccessRead}
	writer := &Principal{Role: config.RoleUser, OrgCode: "A", Access: config.AccessWrite}
	tests := []struct {
		name      string
		p         *Principal
		wantAdmin bool
		wantWrite bool
	}{
		{"nil", nil, false, false},
		{"admin", admin, true, true},
		{"reader", reader, false, false},
		{"writer", writer, false, true},
		{"user with no access", &Principal{Role: config.RoleUser}, false, false},
		{"unknown role", &Principal{Role: "x", Access: config.AccessWrite}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RequireAdmin(tt.p) == nil; got != tt.wantAdmin {
				t.Fatalf("RequireAdmin allowed=%v, want %v", got, tt.wantAdmin)
			}
			if got := RequireWrite(tt.p) == nil; got != tt.wantWrite {
				t.Fatalf("RequireWrite allowed=%v, want %v", got, tt.wantWrite)
			}
			if err := RequireAdmin(tt.p); err != nil && !errors.Is(err, ErrForbidden) {
				t.Fatalf("want ErrForbidden, got %v", err)
			}
		})
	}
}

type fakeDir struct {
	user  *DirectoryUser
	err   error
	calls int
}

func (f *fakeDir) Authenticate(context.Context, string, string, []string) (*DirectoryUser, error) {
	f.calls++
	return f.user, f.err
}

type env struct {
	svc     *Service
	users   *db.Users
	audit   *db.Audit
	refresh *db.RefreshTokens
	dir     *fakeDir
	clock   *time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d, err := db.Open("sqlite", filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	e := &env{users: db.NewUsers(d), audit: db.NewAudit(d), refresh: db.NewRefreshTokens(d), dir: &fakeDir{}}
	mappings := []config.ADGroup{
		{Group: "Admins", Role: config.RoleAdmin},
		{Group: "OrgA", Role: config.RoleUser, OrgCode: "A", Access: config.AccessWrite},
	}
	tk := newTokens(t, 15*time.Minute)
	clock := time.Now()
	e.clock = &clock
	tk.now = func() time.Time { return *e.clock }
	e.svc = NewService(e.users, e.refresh, tk, time.Hour, e.dir, mappings)
	e.svc.now = tk.now
	return e
}

func (e *env) addLocal(t *testing.T, name, password string, disabled bool) {
	t.Helper()
	h, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.users.Create(context.Background(), &db.User{Username: name, PasswordHash: &h, Role: "admin", AuthSource: "local", Disabled: disabled}); err != nil {
		t.Fatal(err)
	}
}

func TestLogin(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		dir      *fakeDir
		noAD     bool
		wantErr  error
		wantRole string
		wantOrg  string
		wantAD   int // expected directory calls
	}{
		{"local ok", "Root", "rootpassword", &fakeDir{err: ErrDirectoryUnreachable}, false, nil, "admin", "", 0},
		{"local wrong password never tries AD", "root", "nope-nope", &fakeDir{}, false, ErrInvalidCredentials, "", "", 0},
		{"local works while AD is down", "root", "rootpassword", &fakeDir{err: ErrDirectoryUnreachable}, false, nil, "admin", "", 0},
		{"local disabled", "off", "rootpassword", &fakeDir{}, false, ErrAccountDisabled, "", "", 0},
		{"empty password", "root", "", &fakeDir{}, false, ErrInvalidCredentials, "", "", 0},
		{"unknown user, no AD", "ghost", "whatever1", nil, true, ErrInvalidCredentials, "", "", 0},
		{"ad bad password", "ann", "bad", &fakeDir{err: ErrInvalidCredentials}, false, ErrInvalidCredentials, "", "", 1},
		{"ad unreachable", "ann", "pw", &fakeDir{err: ErrDirectoryUnreachable}, false, ErrDirectoryUnreachable, "", "", 1},
		{"ad no mapped group", "ann", "pw", &fakeDir{user: &DirectoryUser{Groups: []string{"Nope"}}}, false, ErrNoMappedGroup, "", "", 1},
		{"ad admin", "ann", "pw", &fakeDir{user: &DirectoryUser{DisplayName: "Ann", Groups: []string{"OrgA", "Admins"}}}, false, nil, "admin", "", 1},
		{"ad user", "ann", "pw", &fakeDir{user: &DirectoryUser{DisplayName: "Ann", Groups: []string{"OrgA"}}}, false, nil, "user", "A", 1},
		{"ad name colliding with local account uses local", "root", "pw", &fakeDir{user: &DirectoryUser{Groups: []string{"Admins"}}}, false, ErrInvalidCredentials, "", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.addLocal(t, "root", "rootpassword", false)
			e.addLocal(t, "off", "rootpassword", true)
			if tt.noAD {
				e.svc.SetDirectory(nil, nil)
			} else {
				e.svc.SetDirectory(tt.dir, e.svc.ad.Load().mappings)
			}
			s, err := e.svc.Login(context.Background(), tt.username, tt.password)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.dir != nil && tt.dir.calls != tt.wantAD {
				t.Fatalf("directory calls = %d, want %d", tt.dir.calls, tt.wantAD)
			}
			if tt.wantErr != nil {
				if s != nil {
					t.Fatal("no session expected on failure")
				}
				return
			}
			p, err := e.svc.Verify(s.AccessToken)
			if err != nil || p.Role != tt.wantRole || p.OrgCode != tt.wantOrg {
				t.Fatalf("principal = %+v, %v", p, err)
			}
			if s.RefreshToken == "" {
				t.Fatal("refresh token missing")
			}
		})
	}
}

func TestRefreshRotation(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.addLocal(t, "root", "rootpassword", false)
	s1, err := e.svc.Login(ctx, "root", "rootpassword")
	if err != nil {
		t.Fatal(err)
	}

	if hashToken(s1.RefreshToken) == s1.RefreshToken {
		t.Fatal("refresh token must not be stored raw")
	}
	if _, err := e.refresh.GetValid(ctx, hashToken(s1.RefreshToken), *e.clock); err != nil {
		t.Fatalf("hash of token should be stored: %v ", err)
	}

	s2, err := e.svc.Refresh(ctx, s1.RefreshToken)
	if err != nil || s2.RefreshToken == s1.RefreshToken {
		t.Fatalf("Refresh = %+v, %v", s2, err)
	}
	if _, err := e.svc.Refresh(ctx, s1.RefreshToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("reusing a rotated token: want ErrInvalidToken, got %v", err)
	}
	if _, err := e.svc.Refresh(ctx, "garbage"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("unknown token: want ErrInvalidToken, got %v", err)
	}

	if err := e.svc.Logout(ctx, s2.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Logout(ctx, s2.RefreshToken); err != nil {
		t.Fatalf("second logout should be a no-op, got %v", err)
	}
	if _, err := e.svc.Refresh(ctx, s2.RefreshToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("after logout: want ErrInvalidToken, got %v", err)
	}

	s3, _ := e.svc.Login(ctx, "root", "rootpassword")
	if err := e.svc.LogoutAll(ctx, s3.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Refresh(ctx, s3.RefreshToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("after logout all: want ErrInvalidToken, got %v", err)
	}

	s4, _ := e.svc.Login(ctx, "root", "rootpassword")
	*e.clock = e.clock.Add(2 * time.Hour)
	if _, err := e.svc.Refresh(ctx, s4.RefreshToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expired refresh token: want ErrInvalidToken, got %v", err)
	}
}

func TestRefreshReplayRevokesAllSessions(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.addLocal(t, "root", "rootpassword", false)
	s1, _ := e.svc.Login(ctx, "root", "rootpassword")
	other, _ := e.svc.Login(ctx, "root", "rootpassword") // a second device
	s2, err := e.svc.Refresh(ctx, s1.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Refresh(ctx, s1.RefreshToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("replay: want ErrInvalidToken, got %v", err)
	}
	for name, tok := range map[string]string{"rotated successor": s2.RefreshToken, "other device": other.RefreshToken} {
		if _, err := e.svc.Refresh(ctx, tok); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("%s must be revoked after replay, got %v", name, err)
		}
	}
}

func TestRefreshDisabledUser(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.addLocal(t, "root", "rootpassword", false)
	s, _ := e.svc.Login(ctx, "root", "rootpassword")
	u := s.User
	u.Disabled = true
	if err := e.users.Update(ctx, u); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Refresh(ctx, s.RefreshToken); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("want ErrAccountDisabled, got %v", err)
	}
}

func TestNewADDirectory(t *testing.T) {
	tests := []struct {
		url     string
		wantErr bool
		port    int
	}{
		{"ldaps://dc.example.com", false, 636},
		{"ldap://dc.example.com", false, 389},
		{"ldaps://dc.example.com:3269", false, 3269},
		{"ldaps://dc.example.com:abc", true, 0},
		{"http://dc.example.com", true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			d, err := NewADDirectory(config.AD{URL: tt.url, BaseDN: "DC=example,DC=com"})
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v", err)
			}
			if err == nil && d.cfg.Port != tt.port {
				t.Fatalf("port = %d, want %d", d.cfg.Port, tt.port)
			}
		})
	}
}

func TestADDirectoryUnreachable(t *testing.T) {
	d, err := NewADDirectory(config.AD{URL: "ldap://127.0.0.1:1", BaseDN: "DC=example,DC=com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Authenticate(context.Background(), "ann", "pw", []string{"g"}); !errors.Is(err, ErrDirectoryUnreachable) {
		t.Fatalf("want ErrDirectoryUnreachable, got %v", err)
	}
}

func TestSetupCode(t *testing.T) {
	c, err := NewSetupCode()
	if err != nil {
		t.Fatal(err)
	}
	shown := c.String()
	if !regexp.MustCompile(`^[2-9A-HJ-NP-Z]{4}-[2-9A-HJ-NP-Z]{4}-[2-9A-HJ-NP-Z]{4}$`).MatchString(shown) {
		t.Fatalf("code %q has the wrong shape", shown)
	}
	other, _ := NewSetupCode()
	if other.String() == shown {
		t.Fatal("two codes equal")
	}
	tests := []struct {
		name    string
		attempt string
		want    bool
	}{
		{"exact", shown, true},
		{"lower case", strings.ToLower(shown), true},
		{"no dashes", strings.ReplaceAll(shown, "-", ""), true},
		{"spaces", strings.ReplaceAll(shown, "-", " "), true},
		{"wrong", "AAAA-AAAA-AAAA", false},
		{"truncated", shown[:len(shown)-1], false},
		{"extra", shown + "A", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.Verify(tt.attempt); got != tt.want {
				t.Fatalf("Verify(%q) = %v, want %v", tt.attempt, got, tt.want)
			}
		})
	}
	if !c.Required() {
		t.Fatal("fresh code must be required")
	}
	c.Clear()
	if c.Required() || c.Verify(shown) || c.Verify("") {
		t.Fatal("cleared code must not be required or verify (not even empty)")
	}
	var nilCode *SetupCode
	if nilCode.Required() || nilCode.Verify("x") {
		t.Fatal("nil code must never be required")
	}
}

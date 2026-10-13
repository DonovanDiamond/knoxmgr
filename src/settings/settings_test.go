package settings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/DonovanDiamond/knoxmgr/src/auth"
	"github.com/DonovanDiamond/knoxmgr/src/db"
	"github.com/DonovanDiamond/knoxmgr/src/knoxmock"
	"github.com/DonovanDiamond/knoxmgr/src/secretbox"
)

func newMock(t *testing.T, id, secret string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(knoxmock.New(id, secret, zerolog.Nop()))
	t.Cleanup(s.Close)
	return s
}

func newManager(t *testing.T) (*Manager, *db.AppSettings, *db.Audit, *auth.Service) {
	t.Helper()
	d, err := db.Open("sqlite", filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	box, err := secretbox.LoadOrCreate(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := auth.HashPassword("password1")
	root := &db.User{Username: "root", DisplayName: "root", PasswordHash: &hash, Role: "admin", AuthSource: "local"}
	if err := db.NewUsers(d).Create(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	actor = &auth.Principal{UserID: root.ID, Username: root.Username}
	svc := auth.NewService(db.NewUsers(d), db.NewRefreshTokens(d), auth.NewTokens(nil, nil, time.Minute), time.Hour, nil, nil)
	repo := db.NewAppSettings(d)
	m := New(repo, box, svc, zerolog.Nop())
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	return m, repo, db.NewAudit(d), svc
}

// actor is the admin created by the latest newManager; these tests do not run in parallel.
var actor *auth.Principal

func TestKnoxValidation(t *testing.T) {
	base := KnoxInput{Region: "custom", BaseURL: "https://knox.example.com", ClientID: "cid", TenantID: "tid", ClientSecret: "s"}
	with := func(f func(*KnoxInput)) KnoxInput { in := base; f(&in); return in }
	tests := []struct {
		name    string
		in      KnoxInput
		wantErr string
		want    string // expected BaseURL after normalising
	}{
		{"custom ok", base, "", "https://knox.example.com"},
		{"custom trailing slash", with(func(in *KnoxInput) { in.BaseURL += "/" }), "", "https://knox.example.com"},
		{"preset ignores sent url", with(func(in *KnoxInput) { in.Region, in.BaseURL = "eu03", "https://evil.example" }), "", "https://eu03.manage.samsungknox.com"},
		{"preset forces https", with(func(in *KnoxInput) { in.Region, in.AllowInsecureHTTP = "us01", true }), "", "https://us01.manage.samsungknox.com"},
		{"unknown region", with(func(in *KnoxInput) { in.Region = "eu99" }), "region", ""},
		{"empty region", with(func(in *KnoxInput) { in.Region = "" }), "region", ""},
		{"custom no url", with(func(in *KnoxInput) { in.BaseURL = "" }), "baseURL", ""},
		{"ftp", with(func(in *KnoxInput) { in.BaseURL = "ftp://x" }), "baseURL", ""},
		{"path", with(func(in *KnoxInput) { in.BaseURL += "/emm/oapi" }), "no path", ""},
		{"query", with(func(in *KnoxInput) { in.BaseURL += "?a=b" }), "baseURL", ""},
		{"userinfo", with(func(in *KnoxInput) { in.BaseURL = "https://u:p@knox.example.com" }), "baseURL", ""},
		{"http refused", with(func(in *KnoxInput) { in.BaseURL = "http://knox.example.com" }), "plain http", ""},
		{"http allowed", with(func(in *KnoxInput) { in.BaseURL, in.AllowInsecureHTTP = "http://127.0.0.1:9", true }), "", "http://127.0.0.1:9"},
		{"client id empty", with(func(in *KnoxInput) { in.ClientID = "" }), "clientId", ""},
		{"client id at sign", with(func(in *KnoxInput) { in.ClientID = "a@b" }), "clientId", ""},
		{"client id too long", with(func(in *KnoxInput) { in.ClientID = strings.Repeat("a", 101) }), "clientId", ""},
		{"tenant space", with(func(in *KnoxInput) { in.TenantID = "a b" }), "tenantId", ""},
		{"secret control char", with(func(in *KnoxInput) { in.ClientSecret = "a\nb" }), "clientSecret", ""},
		{"secret too long", with(func(in *KnoxInput) { in.ClientSecret = strings.Repeat("x", 1025) }), "clientSecret", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.in
			err := in.normalise()
			if tt.wantErr == "" {
				if err != nil || in.BaseURL != tt.want {
					t.Fatalf("err = %v, baseURL = %q, want %q", err, in.BaseURL, tt.want)
				}
				return
			}
			if _, ok := err.(*InvalidError); !ok || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want InvalidError containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestPresetRegions(t *testing.T) {
	want := "ap01 ap02 eu01 eu02 eu03 eu04 eu05 eu06 eu07 us01 us02"
	var ids []string
	for _, r := range Regions() {
		ids = append(ids, r.ID)
		if r.BaseURL != "https://"+r.ID+".manage.samsungknox.com" || r.Label != strings.ToUpper(r.ID) {
			t.Fatalf("region %+v", r)
		}
	}
	if strings.Join(ids, " ") != want {
		t.Fatalf("regions = %v", ids)
	}
}

func TestADValidation(t *testing.T) {
	base := ADInput{Enabled: true, URL: "ldaps://dc.example.com", BaseDN: "DC=example,DC=com", Mappings: []ADMapping{
		{Group: "Admins", Role: "admin"}, {Group: "OrgA", Role: "user", OrgCode: "A", Access: "read"}}}
	with := func(f func(*ADInput)) ADInput {
		in := base
		in.Mappings = append([]ADMapping(nil), base.Mappings...)
		f(&in)
		return in
	}
	tests := []struct {
		name    string
		in      ADInput
		wantErr string
	}{
		{"valid", base, ""},
		{"valid with suffix and port", with(func(in *ADInput) { in.UPNSuffix, in.URL = "corp.example.com", "ldap://dc:389" }), ""},
		{"disabled and empty", ADInput{}, ""},
		{"disabled with junk url", ADInput{URL: "http://x"}, "url"},
		{"disabled keeps mappings checked", ADInput{Mappings: []ADMapping{{Group: "g", Role: "root"}}}, "role"},
		{"bad scheme", with(func(in *ADInput) { in.URL = "http://dc" }), "url"},
		{"no host", with(func(in *ADInput) { in.URL = "ldaps://" }), "url"},
		{"bad port", with(func(in *ADInput) { in.URL = "ldaps://dc:99999" }), "port"},
		{"enabled no url", with(func(in *ADInput) { in.URL = "" }), "url"},
		{"no base dn", with(func(in *ADInput) { in.BaseDN = "" }), "baseDN"},
		{"suffix with at", with(func(in *ADInput) { in.UPNSuffix = "@corp" }), "upnSuffix"},
		{"enabled no mappings", with(func(in *ADInput) { in.Mappings = nil }), "at least one"},
		{"empty group", with(func(in *ADInput) { in.Mappings[0].Group = " " }), "group is required"},
		{"duplicate group", with(func(in *ADInput) { in.Mappings[1].Group = "Admins" }), "twice"},
		{"bad role", with(func(in *ADInput) { in.Mappings[0].Role = "root" }), "role"},
		{"user no org", with(func(in *ADInput) { in.Mappings[1].OrgCode = "" }), "orgCode"},
		{"user bad org", with(func(in *ADInput) { in.Mappings[1].OrgCode = "a b" }), "orgCode"},
		{"user bad access", with(func(in *ADInput) { in.Mappings[1].Access = "rw" }), "access"},
		{"admin with org", with(func(in *ADInput) { in.Mappings[0].OrgCode = "A" }), "admin mappings"},
		{"admin with access", with(func(in *ADInput) { in.Mappings[0].Access = "read" }), "admin mappings"},
		{"too many", with(func(in *ADInput) {
			in.Mappings = nil
			for i := range 201 {
				in.Mappings = append(in.Mappings, ADMapping{Group: strings.Repeat("g", 5) + string(rune('a'+i%26)) + strings.Repeat("h", i/26), Role: "admin"})
			}
		}), "too many"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.in
			err := in.normalise()
			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if tt.wantErr != "" {
				if _, ok := err.(*InvalidError); !ok || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want InvalidError containing %q", err, tt.wantErr)
				}
			}
		})
	}
}

func TestGeneralValidation(t *testing.T) {
	tests := []struct {
		name    string
		in      GeneralInput
		wantErr bool
	}{
		{"ok", GeneralInput{"Acme", 15, bp(true), nil}, false},
		{"min", GeneralInput{"A", 1, bp(true), nil}, false},
		{"max", GeneralInput{"A", 1440, bp(true), nil}, false},
		{"empty name", GeneralInput{" ", 15, bp(true), nil}, true},
		{"control in name", GeneralInput{"a\tb", 15, bp(true), nil}, true},
		{"long name", GeneralInput{strings.Repeat("a", 101), 15, bp(true), nil}, true},
		{"zero interval", GeneralInput{"A", 0, bp(true), nil}, true},
		{"huge interval", GeneralInput{"A", 1441, bp(true), nil}, true},
		{"negative interval", GeneralInput{"A", -5, bp(true), nil}, true},
		{"checkUpdates absent", GeneralInput{"A", 15, nil, nil}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.in
			if err := in.normalise(); (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSaveKnoxKeepsAndHidesSecret(t *testing.T) {
	ctx := context.Background()
	m, repo, audit, _ := newManager(t)
	mock := newMock(t, "cid", "topsecret-value")

	if m.Knox() != nil {
		t.Fatal("fresh install must have no Knox client")
	}
	if _, err := m.SaveKnox(ctx, KnoxInput{Region: "custom", BaseURL: mock.URL, ClientID: "cid", TenantID: "t", AllowInsecureHTTP: true}, actor); err == nil ||
		!strings.Contains(err.Error(), "clientSecret is required") {
		t.Fatalf("first save without secret: %v", err)
	}
	v, err := m.SaveKnox(ctx, KnoxInput{Region: "custom", BaseURL: mock.URL, ClientID: "cid", TenantID: "t", ClientSecret: "topsecret-value", AllowInsecureHTTP: true}, actor)
	if err != nil || !v.SecretSet || !v.Configured {
		t.Fatalf("save = %+v, %v", v, err)
	}
	first := m.Knox()
	if first == nil {
		t.Fatal("client not built on save")
	}
	if _, err := first.Orgs(ctx); err != nil {
		t.Fatalf("live client cannot reach the mock: %v", err)
	}

	stored, _ := repo.GetKnox(ctx)
	if stored.ClientSecretSeal == "" || strings.Contains(stored.ClientSecretSeal, "topsecret") {
		t.Fatalf("secret stored wrongly: %q", stored.ClientSecretSeal)
	}

	// Empty secret keeps the stored one and still rebuilds the client (new pointer, still working).
	v, err = m.SaveKnox(ctx, KnoxInput{Region: "custom", BaseURL: mock.URL, ClientID: "cid", TenantID: "t2", AllowInsecureHTTP: true}, actor)
	if err != nil || !v.SecretSet {
		t.Fatalf("keep-secret save = %+v, %v", v, err)
	}
	if again, _ := repo.GetKnox(ctx); again.ClientSecretSeal != stored.ClientSecretSeal || again.TenantID != "t2" {
		t.Fatalf("secret not kept: %+v", again)
	}
	if m.Knox() == first {
		t.Fatal("client not swapped on save")
	}
	if _, err := m.Knox().Orgs(ctx); err != nil {
		t.Fatalf("swapped client cannot reach the mock: %v", err)
	}

	for _, view := range []any{v, mustView(t, m)} {
		if b, _ := json.Marshal(view); strings.Contains(string(b), "topsecret") || strings.Contains(string(b), stored.ClientSecretSeal) {
			t.Fatalf("view leaks the secret: %s", b)
		}
	}
	entries, _ := audit.ListRecent(ctx, 10)
	if len(entries) != 2 {
		t.Fatalf("audit entries = %d, want 2 (failed save must not audit)", len(entries))
	}
	for _, e := range entries {
		if b, _ := json.Marshal(e); strings.Contains(string(b), "topsecret") || strings.Contains(string(b), stored.ClientSecretSeal) {
			t.Fatalf("audit leaks: %s", b)
		}
	}
}

func mustView(t *testing.T, m *Manager) KnoxView {
	t.Helper()
	v, err := m.GetKnox(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTamperedSecret(t *testing.T) {
	ctx := context.Background()
	m, repo, _, _ := newManager(t)
	mock := newMock(t, "cid", "s")
	in := KnoxInput{Region: "custom", BaseURL: mock.URL, ClientID: "cid", TenantID: "t", ClientSecret: "s", AllowInsecureHTTP: true}
	if _, err := m.SaveKnox(ctx, in, actor); err != nil {
		t.Fatal(err)
	}
	k, _ := repo.GetKnox(ctx)
	k.ClientSecretSeal = k.ClientSecretSeal[:len(k.ClientSecretSeal)-4] + "AAAA"
	if err := repo.SaveKnox(ctx, k, false, nil, "x"); err != nil {
		t.Fatal(err)
	}
	// Startup survives and leaves Knox disconnected rather than refusing to boot.
	if err := m.Load(ctx); err != nil {
		t.Fatalf("Load must tolerate an unreadable secret: %v", err)
	}
	if m.Knox() != nil {
		t.Fatal("client built from a tampered secret")
	}
	in.ClientSecret = ""
	if _, err := m.SaveKnox(ctx, in, actor); err == nil || !strings.Contains(err.Error(), "enter it again") {
		t.Fatalf("keep-secret over tampered ciphertext: %v", err)
	}
	in.ClientSecret = "s"
	if _, err := m.SaveKnox(ctx, in, actor); err != nil || m.Knox() == nil {
		t.Fatalf("re-entering the secret must recover: %v", err)
	}
}

func TestTestKnox(t *testing.T) {
	ctx := context.Background()
	m, _, audit, _ := newManager(t)
	mock := newMock(t, "cid", "right-secret")
	dead := httptest.NewServer(nil)
	deadURL := dead.URL
	dead.Close() // nothing listens here any more

	if _, err := m.SaveKnox(ctx, KnoxInput{Region: "custom", BaseURL: mock.URL, ClientID: "cid", TenantID: "t", ClientSecret: "right-secret", AllowInsecureHTTP: true}, actor); err != nil {
		t.Fatal(err)
	}
	before, _ := audit.ListRecent(ctx, 50)

	base := KnoxInput{Region: "custom", BaseURL: mock.URL, ClientID: "cid", TenantID: "t", AllowInsecureHTTP: true}
	with := func(f func(*KnoxInput)) KnoxInput { in := base; f(&in); return in }
	tests := []struct {
		name      string
		in        KnoxInput
		wantOK    bool
		wantStage string
		wantMsg   string
	}{
		{"stored secret used", base, true, "", "Connected"},
		{"explicit secret", with(func(in *KnoxInput) { in.ClientSecret = "right-secret" }), true, "", "Connected"},
		{"wrong secret", with(func(in *KnoxInput) { in.ClientSecret = "wrong-secret-xyz" }), false, "token", "rejected the credentials"},
		{"wrong client id", with(func(in *KnoxInput) { in.ClientID = "other" }), false, "token", "rejected the credentials"},
		{"connection refused", with(func(in *KnoxInput) { in.BaseURL = deadURL }), false, "token", "Could not reach Knox: connection failed"},
		{"http not allowed", with(func(in *KnoxInput) { in.AllowInsecureHTTP = false }), false, "token", "plain http"},
		{"invalid input", with(func(in *KnoxInput) { in.ClientID = "" }), false, "token", "clientId"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := m.TestKnox(ctx, tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if res.OK != tt.wantOK || res.Stage != tt.wantStage || !strings.Contains(res.Message, tt.wantMsg) {
				t.Fatalf("result = %+v, want ok=%v stage=%q msg~%q", res, tt.wantOK, tt.wantStage, tt.wantMsg)
			}
			b, _ := json.Marshal(res)
			for _, secret := range []string{"right-secret", "wrong-secret-xyz"} {
				if strings.Contains(string(b), secret) {
					t.Fatalf("result echoes the secret: %s", b)
				}
			}
		})
	}
	if after, _ := audit.ListRecent(ctx, 50); len(after) != len(before) {
		t.Fatal("a test must not save or audit")
	}
	// Testing a tenant with no stored secret and none supplied is a message, not a crash.
	m2, _, _, _ := newManager(t)
	if res, err := m2.TestKnox(ctx, base); err != nil || res.OK || !strings.Contains(res.Message, "clientSecret") {
		t.Fatalf("no secret anywhere: %+v %v", res, err)
	}
}

func TestADSaveAndLiveSwap(t *testing.T) {
	ctx := context.Background()
	m, repo, audit, svc := newManager(t)
	in := ADInput{Enabled: true, URL: "ldap://127.0.0.1:1", BaseDN: "DC=example,DC=com", BindUser: "svc", BindPassword: "bind-pw-xyz",
		Mappings: []ADMapping{{Group: "Admins", Role: "admin"}}}
	v, err := m.SaveAD(ctx, in, actor)
	if err != nil || !v.BindPasswordSet || len(v.Mappings) != 1 {
		t.Fatalf("save = %+v, %v", v, err)
	}
	if b, _ := json.Marshal(v); strings.Contains(string(b), "bind-pw-xyz") {
		t.Fatalf("view leaks the bind password: %s", b)
	}
	stored, _ := repo.GetAD(ctx)
	if stored.BindPasswordSeal == "" || strings.Contains(stored.BindPasswordSeal, "bind-pw") {
		t.Fatalf("bind password stored wrongly: %q", stored.BindPasswordSeal)
	}
	in.BindPassword = ""
	if v, err = m.SaveAD(ctx, in, actor); err != nil || !v.BindPasswordSet {
		t.Fatalf("keep save = %+v, %v", v, err)
	}
	if again, _ := repo.GetAD(ctx); again.BindPasswordSeal != stored.BindPasswordSeal {
		t.Fatal("bind password not kept")
	}
	// An unreachable directory proves AD logins are live; a nil directory would say invalid credentials.
	if _, err := svc.Login(ctx, "ann", "x"); !errors.Is(err, auth.ErrDirectoryUnreachable) {
		t.Fatalf("AD directory not swapped in: %v", err)
	}

	in.Enabled = false
	if _, err := m.SaveAD(ctx, in, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, "ann", "x"); err != auth.ErrInvalidCredentials {
		t.Fatalf("disabled AD must turn logins off, got %v", err)
	}
	entries, _ := audit.ListRecent(ctx, 10)
	for _, e := range entries {
		if b, _ := json.Marshal(e); strings.Contains(string(b), "bind-pw-xyz") || strings.Contains(string(b), stored.BindPasswordSeal) {
			t.Fatalf("audit leaks: %s", b)
		}
	}

	res, err := m.TestAD(ctx, "ann", "x", nil)
	if f, ok := res.(ADTestFailure); err != nil || !ok || f.Code != "ad_disabled" {
		t.Fatalf("test with AD off = %+v %v", res, err)
	}
	in.Enabled = true
	res, err = m.TestAD(ctx, "ann", "x", &in)
	if f, ok := res.(ADTestFailure); err != nil || !ok || f.Code != "ad_unreachable" {
		t.Fatalf("test unreachable = %+v %v", res, err)
	}
	if _, err := m.TestAD(ctx, "ann", "x", &ADInput{Enabled: true}); err == nil {
		t.Fatal("invalid test settings must be rejected")
	}
}

func bp(b bool) *bool { return &b }

func TestGeneralKeepsCheckUpdatesWhenAbsent(t *testing.T) {
	m, _, _, _ := newManager(t)
	ctx := context.Background()
	if v, err := m.SaveGeneral(ctx, GeneralInput{"Acme", 30, bp(false), nil}, actor); err != nil || v.CheckUpdates || m.CheckUpdates() {
		t.Fatalf("explicit false: %+v, %v", v, err)
	}
	if v, err := m.SaveGeneral(ctx, GeneralInput{"Acme", 45, nil, nil}, actor); err != nil || v.CheckUpdates || m.CheckUpdates() {
		t.Fatalf("absent must keep false: %+v, %v", v, err)
	}
}

func TestGeneralLive(t *testing.T) {
	m, _, audit, _ := newManager(t)
	if m.InstallationName() != "knoxmgr" {
		t.Fatalf("default name = %q", m.InstallationName())
	}
	if _, err := m.SaveGeneral(context.Background(), GeneralInput{"Acme", 30, bp(true), nil}, actor); err != nil || m.InstallationName() != "Acme" {
		t.Fatalf("name = %q, %v", m.InstallationName(), err)
	}
	if _, err := m.SaveGeneral(context.Background(), GeneralInput{"Acme", 0, bp(true), nil}, actor); err == nil || m.InstallationName() != "Acme" {
		t.Fatal("invalid save must change nothing")
	}
	if e, _ := audit.ListRecent(context.Background(), 5); len(e) != 1 || e[0].Action != "settings.general" {
		t.Fatalf("audit = %+v", e)
	}
}

func TestGeneralShowMap(t *testing.T) {
	m, _, _, _ := newManager(t)
	ctx := context.Background()
	if !m.ShowMap() {
		t.Fatal("show map must default to true")
	}
	for _, c := range []struct {
		name string
		in   *bool
		want bool
	}{{"off", bp(false), false}, {"absent keeps", nil, false}, {"on", bp(true), true}} {
		if v, err := m.SaveGeneral(ctx, GeneralInput{"Acme", 30, nil, c.in}, actor); err != nil || v.ShowMap != c.want || m.ShowMap() != c.want {
			t.Fatalf("%s: %+v, %v", c.name, v, err)
		}
	}
}

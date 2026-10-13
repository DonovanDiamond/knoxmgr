package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func valid() Config {
	return Config{
		ListenAddr: ":8080",
		Timezone:   "Africa/Johannesburg",
		Database:   Database{Driver: DriverSQLite, DSN: "knoxmgr.db"},
		JWT:        JWT{AccessTTL: time.Minute, RefreshTTL: time.Hour},
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string // empty means valid
	}{
		{"valid", func(*Config) {}, ""},
		{"valid postgres", func(c *Config) { c.Database.Driver = DriverPostgres }, ""},
		{"bad listen", func(c *Config) { c.ListenAddr = "8080" }, "listenAddr"},
		{"bad timezone", func(c *Config) { c.Timezone = "Mars/Base" }, "timezone"},
		{"empty timezone", func(c *Config) { c.Timezone = "" }, "timezone"},
		{"bad driver", func(c *Config) { c.Database.Driver = "mysql" }, "database.driver"},
		{"missing dsn", func(c *Config) { c.Database.DSN = "" }, "database.dsn"},
		{"zero access ttl", func(c *Config) { c.JWT.AccessTTL = 0 }, "jwt.accessTTL"},
		{"negative refresh ttl", func(c *Config) { c.JWT.RefreshTTL = -1 }, "jwt.refreshTTL"},
		{"trusted proxies ok", func(c *Config) { c.TrustedProxies = []string{"10.0.0.0/8", "192.168.1.5", "::1", "fd00::/8"} }, ""},
		{"trusted proxies empty ok", func(c *Config) { c.TrustedProxies = []string{} }, ""},
		{"trusted proxy hostname", func(c *Config) { c.TrustedProxies = []string{"10.0.0.1", "proxy.local"} }, "trustedProxies[1]"},
		{"trusted proxy bad mask", func(c *Config) { c.TrustedProxies = []string{"10.0.0.0/33"} }, "trustedProxies[0]"},
		{"trusted proxy empty entry", func(c *Config) { c.TrustedProxies = []string{""} }, "trustedProxies[0]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := valid()
			tt.mutate(&c)
			err := c.Validate()
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && err == nil:
				t.Fatalf("want error containing %q, got nil", tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Fatalf("want error containing %q, got %q", tt.wantErr, err)
			}
		})
	}
}

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{"empty file", "", ""},
		{"only comments", "# nothing\n", ""},
		{"full", "listenAddr: \":9090\"\ntimezone: UTC\nsecureCookies: false\ntrustedProxies: [10.0.0.0/8]\ndatabase: {driver: sqlite, dsn: x.db}\njwt: {accessTTL: 5m, refreshTTL: 24h}\n", ""},
		{"unknown key", "bogus: 1\n", "bogus"},
		{"invalid", "timezone: Nowhere/Land\n", "timezone"},
		{"not yaml", "a: [", "config"},
		{"installationName moved", "installationName: Acme\n", "installationName moved to admin settings"},
		{"knox moved", "knox: {clientId: c}\n", "knox moved to admin settings"},
		{"ad moved", "ad: {enabled: false}\n", "ad moved to admin settings"},
		{"bootstrap moved", "bootstrap: {adminUsername: root}\n", "bootstrap moved to admin settings"},
		{"private key removed", "jwt: {privateKeyPath: p.pem}\n", "jwt.privateKeyPath removed: keys live in the data dir"},
		{"public key removed", "jwt: {publicKeyPath: p.pem}\n", "jwt.publicKeyPath removed: keys live in the data dir"},
		{"several reported", "knox: {}\nad: {}\n", "ad moved"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(write(t, tt.yaml), true, t.TempDir())
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	c, err := Load(filepath.Join(dir, "absent.yaml"), false, dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenAddr != ":8080" || c.Timezone != "UTC" || c.Database.Driver != DriverSQLite ||
		c.Database.DSN.Reveal() != filepath.Join(dir, "knoxmgr.db") || c.DataDir != dir ||
		c.JWT.AccessTTL != 15*time.Minute || c.JWT.RefreshTTL != 720*time.Hour || !c.UseSecureCookies() {
		t.Fatalf("defaults wrong: %+v", c)
	}
	if c.Location().String() != "UTC" {
		t.Fatalf("location = %v", c.Location())
	}
}

func TestLoadMissingFile(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.yaml")
	if _, err := Load(missing, true, dir); err == nil {
		t.Fatal("explicit missing file must be an error")
	}
	if _, err := Load(missing, false, dir); err != nil {
		t.Fatalf("implicit missing file must be fine: %v", err)
	}
}

func TestSecureCookies(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want bool
	}{
		{"absent defaults to true", "", true},
		{"explicit false", "secureCookies: false\n", false},
		{"explicit true", "secureCookies: true\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Load(write(t, tt.yaml), true, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if got := c.UseSecureCookies(); got != tt.want {
				t.Fatalf("UseSecureCookies = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveDataDir(t *testing.T) {
	base := t.TempDir()
	t.Setenv("KNOXMGR_DATA", filepath.Join(base, "env"))
	tests := []struct{ name, flag, want string }{
		{"flag wins", filepath.Join(base, "flag"), filepath.Join(base, "flag")},
		{"env fallback", "", filepath.Join(base, "env")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveDataDir(tt.flag)
			if err != nil || got != tt.want {
				t.Fatalf("got %q, %v; want %q", got, err, tt.want)
			}
			if info, err := os.Stat(got); err != nil || info.Mode().Perm() != 0o700 {
				t.Fatalf("data dir not created 0700: %v %v", info, err)
			}
		})
	}
}

func TestSecretsNeverPrint(t *testing.T) {
	c := valid()
	c.Database.DSN = "postgres://u:dsnpw-xyz@h/db"
	k := Knox{ClientSecret: "s3cret"}
	a := AD{BindPassword: "bindpw"}
	for _, out := range []string{
		fmt.Sprintf("%v %v %v", c, k, a), fmt.Sprintf("%+v %+v %+v", c, k, a), fmt.Sprintf("%#v %#v %#v", c, k, a), fmt.Sprintf("%v", &c),
	} {
		for _, secret := range []string{"s3cret", "bindpw", "dsnpw-xyz"} {
			if strings.Contains(out, secret) {
				t.Fatalf("output leaks %q: %s", secret, out)
			}
		}
	}
}

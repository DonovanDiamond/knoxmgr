// Package config loads and validates config.yaml into a typed struct.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	RoleAdmin   = "admin"
	RoleUser    = "user"
	AccessRead  = "read"
	AccessWrite = "write"

	DriverSQLite   = "sqlite"
	DriverPostgres = "postgres"

	// bcrypt silently ignores bytes past 72, so longer passwords are refused.
	MinPasswordLen = 8
	MaxPasswordLen = 72
)

// Secret is a string that never prints itself; call Reveal to use it.
type Secret string

func (Secret) String() string               { return "[redacted]" }
func (Secret) GoString() string             { return "[redacted]" }
func (Secret) MarshalText() ([]byte, error) { return []byte("[redacted]"), nil }
func (s Secret) Reveal() string             { return string(s) }
func (s Secret) isSet() bool                { return s != "" }

// Config is the file-level configuration: only what is needed before the database is open.
// Knox, Active Directory and general settings live in the database (see package settings).
type Config struct {
	ListenAddr     string   `yaml:"listenAddr"`
	Timezone       string   `yaml:"timezone"`
	SecureCookies  *bool    `yaml:"secureCookies"`  // nil means true; set false only for plain-HTTP development
	TrustedProxies []string `yaml:"trustedProxies"` // CIDRs or IPs whose forwarding headers are believed
	Database       Database `yaml:"database"`
	JWT            JWT      `yaml:"jwt"`

	// DataDir holds the database (by default), the JWT key pair and the secrets key.
	DataDir string `yaml:"-"`
}

type Database struct {
	Driver string `yaml:"driver"`
	DSN    Secret `yaml:"dsn"` // a Postgres DSN can carry a password
}

type JWT struct {
	AccessTTL  time.Duration `yaml:"accessTTL"`
	RefreshTTL time.Duration `yaml:"refreshTTL"`
}

// AD is the runtime Active Directory configuration, built from the database settings.
type AD struct {
	Enabled      bool
	URL          string
	BaseDN       string
	UPNSuffix    string
	BindUser     string
	BindPassword Secret
	Groups       []ADGroup
}

type ADGroup struct {
	Group   string
	Role    string
	OrgCode string
	Access  string
}

// Knox is the runtime Knox connection, built from the database settings.
type Knox struct {
	BaseURL      string
	TokenURL     string
	ClientID     string
	TenantID     string
	ClientSecret Secret

	AllowInsecureHTTP bool // permits an http:// baseURL; for the mock only
}

// movedKeys were once config.yaml keys. Loading fails loudly rather than silently ignoring them.
var movedKeys = map[string]string{
	"installationName": "installationName moved to admin settings",
	"knox":             "knox moved to admin settings",
	"ad":               "ad moved to admin settings",
	"bootstrap":        "bootstrap moved to admin settings: the first admin is created on the setup page",
}

var movedJWTKeys = map[string]string{
	"privateKeyPath": "jwt.privateKeyPath removed: keys live in the data dir",
	"publicKeyPath":  "jwt.publicKeyPath removed: keys live in the data dir",
}

// ResolveDataDir picks the flag value, else $KNOXMGR_DATA, else ./data, and creates it (0700).
func ResolveDataDir(flagValue string) (string, error) {
	dir := flagValue
	if dir == "" {
		dir = os.Getenv("KNOXMGR_DATA")
	}
	if dir == "" {
		dir = "data"
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating data dir %q: %w", dir, err)
	}
	return dir, nil
}

// Load reads path and applies defaults. A missing file is an error only when required (the
// -config flag was set explicitly); otherwise the defaults are used. Unknown keys are rejected.
func Load(path string, required bool, dataDir string) (*Config, error) {
	cfg := Config{
		ListenAddr: ":8080", Timezone: "UTC", DataDir: dataDir,
		Database: Database{Driver: DriverSQLite, DSN: Secret(filepath.Join(dataDir, "knoxmgr.db"))},
		JWT:      JWT{AccessTTL: 15 * time.Minute, RefreshTTL: 720 * time.Hour},
	}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist) && !required:
		return &cfg, nil
	case err != nil:
		return nil, fmt.Errorf("reading config %q: %w", path, err)
	}
	if err := rejectMoved(raw); err != nil {
		return nil, fmt.Errorf("invalid config %q: %w", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) { // io.EOF is an empty file
		return nil, fmt.Errorf("parsing config %q: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config %q: %w", path, err)
	}
	return &cfg, nil
}

// rejectMoved reports every key that no longer belongs in config.yaml.
func rejectMoved(raw []byte) error {
	var top map[string]any
	if err := yaml.Unmarshal(raw, &top); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}
	var errs []error
	for _, k := range slices.Sorted(maps.Keys(movedKeys)) {
		if _, ok := top[k]; ok {
			errs = append(errs, errors.New(movedKeys[k]))
		}
	}
	if jwt, ok := top["jwt"].(map[string]any); ok {
		for _, k := range slices.Sorted(maps.Keys(movedJWTKeys)) {
			if _, ok := jwt[k]; ok {
				errs = append(errs, errors.New(movedJWTKeys[k]))
			}
		}
	}
	return errors.Join(errs...)
}

// UseSecureCookies reports whether auth cookies get the Secure flag; true unless explicitly disabled.
func (c *Config) UseSecureCookies() bool { return c.SecureCookies == nil || *c.SecureCookies }

// Location returns the configured timezone; valid once Validate has passed.
func (c *Config) Location() *time.Location {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// Validate reports every problem found, one per line.
func (c *Config) Validate() error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if _, _, err := net.SplitHostPort(c.ListenAddr); err != nil {
		bad("listenAddr %q must be host:port (for example :8080)", c.ListenAddr)
	}
	if _, err := time.LoadLocation(c.Timezone); c.Timezone == "" || err != nil {
		bad("timezone %q is not a known IANA timezone", c.Timezone)
	}

	switch c.Database.Driver {
	case DriverSQLite, DriverPostgres:
	default:
		bad("database.driver %q must be %q or %q", c.Database.Driver, DriverSQLite, DriverPostgres)
	}
	if !c.Database.DSN.isSet() {
		bad("database.dsn is required")
	}

	if c.JWT.AccessTTL <= 0 {
		bad("jwt.accessTTL must be positive")
	}
	if c.JWT.RefreshTTL <= 0 {
		bad("jwt.refreshTTL must be positive")
	}

	for i, p := range c.TrustedProxies {
		if _, err := netip.ParsePrefix(p); err != nil {
			if _, err := netip.ParseAddr(p); err != nil {
				bad("trustedProxies[%d] %q must be a CIDR or an IP address", i, p)
			}
		}
	}
	return errors.Join(errs...)
}

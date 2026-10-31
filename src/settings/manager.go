// Package settings owns the admin-editable settings (Knox connection, Active Directory, general):
// validation, encrypted secrets, and rebuilding the live Knox client and AD directory on save.
package settings

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/DonovanDiamond/knoxmgr/src/auth"
	"github.com/DonovanDiamond/knoxmgr/src/config"
	"github.com/DonovanDiamond/knoxmgr/src/db"
	"github.com/DonovanDiamond/knoxmgr/src/knox"
	"github.com/DonovanDiamond/knoxmgr/src/netproxy"
	"github.com/DonovanDiamond/knoxmgr/src/secretbox"
)

const (
	tokenPath   = "/emm/oauth/token"
	testTimeout = 20 * time.Second
	knoxTimeout = 30 * time.Second
)

// Manager holds the live Knox client, applies saved settings to it and to the auth service, and
// never lets a secret leave it in the clear.
type Manager struct {
	repo *db.AppSettings
	box  *secretbox.Box
	auth *auth.Service
	log  zerolog.Logger

	transport *netproxy.Transport // shared by Knox and update checks; Set on proxy save

	saveMu  sync.Mutex // serialises save-then-rebuild so the live state matches the last save
	knox    atomic.Pointer[knox.Client]
	general atomic.Pointer[db.GeneralSettings]

	onKnoxSaved, onGeneralSaved func() // set once before serving; must not block
}

func New(repo *db.AppSettings, box *secretbox.Box, authSvc *auth.Service, log zerolog.Logger) *Manager {
	return &Manager{repo: repo, box: box, auth: authSvc, log: log, transport: netproxy.NewTransport(netproxy.Config{})}
}

// Transport is the outbound round tripper that follows the saved proxy setting.
func (m *Manager) Transport() *netproxy.Transport { return m.transport }

func (m *Manager) knoxHTTP() *http.Client {
	return &http.Client{Timeout: knoxTimeout, Transport: m.transport}
}

// SetHooks registers callbacks run after a successful SaveKnox and SaveGeneral. Call before serving.
func (m *Manager) SetHooks(onKnoxSaved, onGeneralSaved func()) {
	m.onKnoxSaved, m.onGeneralSaved = onKnoxSaved, onGeneralSaved
}

// Load builds the live Knox client, AD directory and general settings from the database.
func (m *Manager) Load(ctx context.Context) error {
	g, err := m.repo.GetGeneral(ctx)
	if err != nil {
		return err
	}
	m.general.Store(&g)
	// The proxy goes first so the first Knox call already uses it.
	px, err := m.repo.GetProxy(ctx)
	if err != nil {
		return err
	}
	if err := m.applyProxy(px); err != nil {
		m.log.Error().Err(err).Msg("stored proxy settings unusable; falling back to the environment proxy until they are saved again")
	}
	k, err := m.repo.GetKnox(ctx)
	if err != nil {
		return err
	}
	if err := m.applyKnox(k); err != nil {
		// A key mismatch must not stop the app: the admin can re-enter the secret.
		m.log.Error().Err(err).Msg("stored Knox settings unusable; Knox stays disconnected until they are saved again")
	}
	a, err := m.repo.GetAD(ctx)
	if err != nil {
		return err
	}
	if err := m.applyAD(a); err != nil {
		m.log.Error().Err(err).Msg("stored AD settings unusable; AD logins stay off until they are saved again")
	}
	return nil
}

// Knox returns the live client, or nil while Knox is not configured.
func (m *Manager) Knox() *knox.Client { return m.knox.Load() }

// InstallationName is the current display name of this installation.
func (m *Manager) InstallationName() string { return m.general.Load().InstallationName }

// CheckUpdates reports whether the admin allows contacting GitHub for new releases.
func (m *Manager) CheckUpdates() bool { return m.general.Load().CheckUpdates }

// ShowMap reports whether the UI may draw device locations on a map (which loads OSM tiles).
func (m *Manager) ShowMap() bool { return m.general.Load().ShowMap }

// AppInventoryHours is how often the app inventory is pulled from Knox.
func (m *Manager) AppInventoryHours(_ context.Context) int { return m.general.Load().AppInventoryHours }

// AppReportDaily reports whether Knox is asked to generate a fresh app report once a day.
func (m *Manager) AppReportDaily(_ context.Context) bool { return m.general.Load().AppReportDaily }

func configured(k db.KnoxSettings) bool {
	return k.BaseURL != "" && k.ClientID != "" && k.TenantID != "" && k.ClientSecretSeal != ""
}

func (m *Manager) knoxConfig(k db.KnoxSettings, secret string) config.Knox {
	return config.Knox{BaseURL: k.BaseURL, TokenURL: k.BaseURL + tokenPath, ClientID: k.ClientID, TenantID: k.TenantID,
		ClientSecret: config.Secret(secret), AllowInsecureHTTP: k.AllowInsecureHTTP}
}

// applyKnox atomically swaps in a client for k, or none when k is incomplete.
func (m *Manager) applyKnox(k db.KnoxSettings) error {
	if !configured(k) {
		m.knox.Store(nil)
		return nil
	}
	secret, err := m.box.Open(k.ClientSecretSeal)
	if err != nil {
		m.knox.Store(nil)
		return fmt.Errorf("knox client secret: %w", err)
	}
	if k.AllowInsecureHTTP && strings.HasPrefix(k.BaseURL, "http:") {
		m.log.Warn().Msg("Knox connection uses plain HTTP: credentials may travel unencrypted; use only with the mock")
	}
	m.knox.Store(knox.New(m.knoxConfig(k, secret), m.knoxHTTP()))
	return nil
}

func (m *Manager) adConfig(a db.ADSettings, bindPassword string) config.AD {
	groups := make([]config.ADGroup, len(a.Mappings))
	for i, g := range a.Mappings {
		groups[i] = config.ADGroup(g)
	}
	return config.AD{Enabled: a.Enabled, URL: a.URL, BaseDN: a.BaseDN, UPNSuffix: a.UPNSuffix,
		BindUser: a.BindUser, BindPassword: config.Secret(bindPassword), Groups: groups}
}

func (m *Manager) applyAD(a db.ADSettings) error {
	if !a.Enabled {
		m.auth.SetDirectory(nil, nil)
		return nil
	}
	cfg := m.adConfig(a, "") // the directory authenticates as the user; the bind password is not needed
	dir, err := auth.NewADDirectory(cfg)
	if err != nil {
		m.auth.SetDirectory(nil, nil)
		return err
	}
	if strings.HasPrefix(a.URL, "ldap://") {
		m.log.Warn().Msg("AD url uses ldap://: AD passwords are sent unencrypted; use ldaps://")
	}
	m.auth.SetDirectory(dir, cfg.Groups)
	return nil
}

// applyProxy swaps the live transport to p. On an unreadable password it leaves the environment fallback.
func (m *Manager) applyProxy(p db.ProxySettings) error {
	c := netproxy.Config{URL: p.URL, Username: p.Username, Bypass: p.Bypass}
	if p.PasswordCipher != "" {
		pw, err := m.box.Open(p.PasswordCipher)
		if err != nil {
			m.transport.Set(netproxy.Config{})
			return fmt.Errorf("proxy password: %w", err)
		}
		c.Password = pw
	}
	m.transport.Set(c)
	return nil
}

// ProxyView is what the API returns for the proxy; the password is only ever "set or not".
type ProxyView struct {
	URL          string `json:"url"`
	Username     string `json:"username"`
	PasswordSet  bool   `json:"passwordSet"`
	Bypass       string `json:"bypass"`
	Source       string `json:"source"`
	EffectiveURL string `json:"effectiveUrl"`
}

func proxyView(p db.ProxySettings) ProxyView {
	src, eff := netproxy.Source(netproxy.Config{URL: p.URL})
	return ProxyView{URL: p.URL, Username: p.Username, PasswordSet: p.PasswordCipher != "", Bypass: p.Bypass, Source: src, EffectiveURL: eff}
}

func (m *Manager) GetProxy(ctx context.Context) (ProxyView, error) {
	p, err := m.repo.GetProxy(ctx)
	return proxyView(p), err
}

// SaveProxy validates, stores (with its audit entry) and applies the proxy without a restart.
func (m *Manager) SaveProxy(ctx context.Context, in ProxyInput, actor *auth.Principal) (ProxyView, error) {
	if err := in.normalise(); err != nil {
		return ProxyView{}, err
	}
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	next, err := m.repo.GetProxy(ctx)
	if err != nil {
		return ProxyView{}, err
	}
	next.URL, next.Username, next.Bypass = in.URL, in.Username, in.Bypass
	changed := false
	switch {
	case in.ClearPassword:
		next.PasswordCipher, changed = "", next.PasswordCipher != ""
	case in.Password != "":
		changed = true
		if next.PasswordCipher, err = m.box.Seal(in.Password); err != nil {
			return ProxyView{}, err
		}
	}
	if err := m.repo.SaveProxy(ctx, next, changed, &actor.UserID, actor.Username); err != nil {
		return ProxyView{}, err
	}
	if err := m.applyProxy(next); err != nil {
		return ProxyView{}, err
	}
	return proxyView(next), nil
}

// KnoxView is what the API returns for Knox settings; the secret is only ever "set or not".
type KnoxView struct {
	Region            string   `json:"region"`
	BaseURL           string   `json:"baseURL"`
	ClientID          string   `json:"clientId"`
	TenantID          string   `json:"tenantId"`
	SecretSet         bool     `json:"secretSet"`
	AllowInsecureHTTP bool     `json:"allowInsecureHTTP"`
	Configured        bool     `json:"configured"`
	Regions           []Region `json:"regions"`
}

func knoxView(k db.KnoxSettings) KnoxView {
	return KnoxView{Region: k.Region, BaseURL: k.BaseURL, ClientID: k.ClientID, TenantID: k.TenantID,
		SecretSet: k.ClientSecretSeal != "", AllowInsecureHTTP: k.AllowInsecureHTTP, Configured: configured(k), Regions: Regions()}
}

func (m *Manager) GetKnox(ctx context.Context) (KnoxView, error) {
	k, err := m.repo.GetKnox(ctx)
	return knoxView(k), err
}

// resolveKnox validates in and returns the settings it describes plus the plaintext secret in use
// (the new one, else the stored one). The seal is for storing; secretChanged says it is new.
func (m *Manager) resolveKnox(ctx context.Context, in KnoxInput) (next db.KnoxSettings, secret string, secretChanged bool, err error) {
	if err = in.normalise(); err != nil {
		return
	}
	stored, err := m.repo.GetKnox(ctx)
	if err != nil {
		return
	}
	next = db.KnoxSettings{Region: in.Region, BaseURL: in.BaseURL, ClientID: in.ClientID, TenantID: in.TenantID,
		ClientSecretSeal: stored.ClientSecretSeal, AllowInsecureHTTP: in.AllowInsecureHTTP}
	switch {
	case in.ClientSecret != "":
		secret, secretChanged = in.ClientSecret, true
		next.ClientSecretSeal, err = m.box.Seal(secret)
	case stored.ClientSecretSeal != "":
		if secret, err = m.box.Open(stored.ClientSecretSeal); err != nil {
			err = invalid("the stored client secret cannot be read; enter it again")
		}
	default:
		err = invalid("clientSecret is required")
	}
	return
}

// SaveKnox validates, stores (with its audit entry) and applies new Knox settings live.
func (m *Manager) SaveKnox(ctx context.Context, in KnoxInput, actor *auth.Principal) (KnoxView, error) {
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	next, _, changed, err := m.resolveKnox(ctx, in)
	if err != nil {
		return KnoxView{}, err
	}
	if err := m.repo.SaveKnox(ctx, next, changed, &actor.UserID, actor.Username); err != nil {
		return KnoxView{}, err
	}
	if err := m.applyKnox(next); err != nil {
		return KnoxView{}, err
	}
	if m.onKnoxSaved != nil {
		m.onKnoxSaved()
	}
	return knoxView(next), nil
}

// KnoxTestResult is the outcome of a connection test; Stage is "token" or "api" on failure.
type KnoxTestResult struct {
	OK      bool   `json:"ok"`
	Stage   string `json:"stage,omitempty"`
	Message string `json:"message"`
}

// TestKnox tries in against Knox without saving or touching the live client.
func (m *Manager) TestKnox(ctx context.Context, in KnoxInput) (KnoxTestResult, error) {
	next, secret, _, err := m.resolveKnox(ctx, in)
	var ie *InvalidError
	if errors.As(err, &ie) {
		return KnoxTestResult{Stage: "token", Message: ie.Msg}, nil
	}
	if err != nil {
		return KnoxTestResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	// A fresh client with its own token cache, so the check is of these credentials.
	err = knox.New(m.knoxConfig(next, secret), m.knoxHTTP()).Probe(ctx)
	var pe *knox.ProbeError
	switch {
	case errors.As(err, &pe):
		return KnoxTestResult{Stage: pe.Stage, Message: pe.Message}, nil
	case err != nil:
		return KnoxTestResult{}, err
	}
	return KnoxTestResult{OK: true, Message: "Connected to Knox and listed organizations."}, nil
}

// ADView is what the API returns for AD settings.
type ADView struct {
	Enabled         bool        `json:"enabled"`
	URL             string      `json:"url"`
	BaseDN          string      `json:"baseDN"`
	UPNSuffix       string      `json:"upnSuffix"`
	BindUser        string      `json:"bindUser"`
	BindPasswordSet bool        `json:"bindPasswordSet"`
	Mappings        []ADMapping `json:"mappings"`
}

func adView(a db.ADSettings) ADView {
	v := ADView{a.Enabled, a.URL, a.BaseDN, a.UPNSuffix, a.BindUser, a.BindPasswordSeal != "", make([]ADMapping, len(a.Mappings))}
	for i, g := range a.Mappings {
		v.Mappings[i] = ADMapping(g)
	}
	return v
}

func (m *Manager) GetAD(ctx context.Context) (ADView, error) {
	a, err := m.repo.GetAD(ctx)
	return adView(a), err
}

func (m *Manager) resolveAD(ctx context.Context, in ADInput) (next db.ADSettings, changed bool, err error) {
	if err = in.normalise(); err != nil {
		return
	}
	stored, err := m.repo.GetAD(ctx)
	if err != nil {
		return
	}
	next = db.ADSettings{Enabled: in.Enabled, URL: in.URL, BaseDN: in.BaseDN, UPNSuffix: in.UPNSuffix, BindUser: in.BindUser,
		BindPasswordSeal: stored.BindPasswordSeal, Mappings: make([]db.ADMapping, len(in.Mappings))}
	for i, g := range in.Mappings {
		next.Mappings[i] = db.ADMapping(g)
	}
	if in.BindPassword != "" {
		changed = true
		next.BindPasswordSeal, err = m.box.Seal(in.BindPassword)
	}
	return
}

// SaveAD validates, stores (with its audit entry) and applies new AD settings live.
func (m *Manager) SaveAD(ctx context.Context, in ADInput, actor *auth.Principal) (ADView, error) {
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	next, changed, err := m.resolveAD(ctx, in)
	if err != nil {
		return ADView{}, err
	}
	if err := m.repo.SaveAD(ctx, next, changed, &actor.UserID, actor.Username); err != nil {
		return ADView{}, err
	}
	if err := m.applyAD(next); err != nil {
		return ADView{}, err
	}
	return adView(next), nil
}

// ADTestSuccess and ADTestFailure are the two shapes of POST /ad/test.
type ADTestSuccess struct {
	OK          bool     `json:"ok"`
	DisplayName string   `json:"displayName"`
	Groups      []string `json:"groups"`
	Role        string   `json:"role"`
	OrgCode     string   `json:"orgCode"`
	Access      string   `json:"access"`
}

type ADTestFailure struct {
	OK      bool   `json:"ok"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func adFail(code, msg string) ADTestFailure { return ADTestFailure{Code: code, Message: msg} }

// TestAD signs username in against the given settings, or the stored ones when in is nil, and
// reports what role they would get. It saves nothing and never touches live logins.
func (m *Manager) TestAD(ctx context.Context, username, password string, in *ADInput) (any, error) {
	var a db.ADSettings
	var err error
	if in != nil {
		if a, _, err = m.resolveAD(ctx, *in); err != nil {
			return nil, err
		}
	} else if a, err = m.repo.GetAD(ctx); err != nil {
		return nil, err
	}
	if !a.Enabled {
		return adFail("ad_disabled", "Active Directory sign-in is not enabled."), nil
	}
	cfg := m.adConfig(a, "")
	dir, err := auth.NewADDirectory(cfg)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	names := make([]string, len(cfg.Groups))
	for i, g := range cfg.Groups {
		names[i] = g.Group
	}
	du, err := dir.Authenticate(ctx, username, password, names)
	switch {
	case errors.Is(err, auth.ErrDirectoryUnreachable):
		return adFail("ad_unreachable", "The directory server could not be reached."), nil
	case errors.Is(err, auth.ErrInvalidCredentials):
		return adFail("invalid_credentials", "The username or password was rejected."), nil
	case err != nil:
		return nil, err
	}
	grant, ok := auth.MapGroups(cfg.Groups, du.Groups)
	if !ok {
		return adFail("no_mapped_group", "The sign-in worked, but the user is in none of the mapped groups."), nil
	}
	groups := du.Groups
	if groups == nil {
		groups = []string{}
	}
	return ADTestSuccess{true, du.DisplayName, groups, grant.Role, grant.OrgCode, grant.Access}, nil
}

func (m *Manager) GetGeneral(ctx context.Context) (db.GeneralSettings, error) {
	return m.repo.GetGeneral(ctx)
}

func (m *Manager) SaveGeneral(ctx context.Context, in GeneralInput, actor *auth.Principal) (db.GeneralSettings, error) {
	if err := in.normalise(); err != nil {
		return db.GeneralSettings{}, err
	}
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	next := db.GeneralSettings{InstallationName: in.InstallationName, SyncIntervalMinutes: in.SyncIntervalMinutes,
		CheckUpdates: m.general.Load().CheckUpdates, ShowMap: m.general.Load().ShowMap,
		AppInventoryHours: m.general.Load().AppInventoryHours, AppReportDaily: m.general.Load().AppReportDaily}
	if in.AppInventoryHours != nil {
		next.AppInventoryHours = *in.AppInventoryHours
	}
	if in.AppReportDaily != nil {
		next.AppReportDaily = *in.AppReportDaily
	}
	if in.ShowMap != nil {
		next.ShowMap = *in.ShowMap
	}
	if in.CheckUpdates != nil {
		next.CheckUpdates = *in.CheckUpdates
	}
	if err := m.repo.SaveGeneral(ctx, next, &actor.UserID, actor.Username); err != nil {
		return db.GeneralSettings{}, err
	}
	m.general.Store(&next)
	if m.onGeneralSaved != nil {
		m.onGeneralSaved()
	}
	return next, nil
}

// Package api holds routing, handlers and middleware. Handlers parse and validate input, call
// auth, and format output; business rules live elsewhere.
package api

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/DonovanDiamond/knoxmgr/src/auth"
	"github.com/DonovanDiamond/knoxmgr/src/db"
	"github.com/DonovanDiamond/knoxmgr/src/knox"
	"github.com/DonovanDiamond/knoxmgr/src/settings"
	"github.com/DonovanDiamond/knoxmgr/src/update"
)

// maxBody bounds every request body; the largest legitimate payload is a login form.
const maxBody = 1 << 20

// Knox is the part of *knox.Client the handlers use; a small interface keeps the API testable.
type Knox interface {
	SelectUsers(ctx context.Context, q knox.UserQuery) (knox.UserPage, error)
	SelectUserWithID(ctx context.Context, userID string) (knox.User, error)
	CreateUser(ctx context.Context, u knox.UserInput) error
	UpdateUser(ctx context.Context, u knox.UserInput) error
	PurgeUser(ctx context.Context, userID string) error
	RequestEnrollment(ctx context.Context, userIDs []string) (knox.EnrollmentResult, error)
	SelectGroups(ctx context.Context, q knox.GroupQuery) (knox.GroupPage, error)
	InsertGroup(ctx context.Context, name string, userIDs []string) (knox.Group, error)
	DeleteGroup(ctx context.Context, groupID string) error
	SelectGroupUsers(ctx context.Context, groupID string, start, limit int) (knox.UserPage, error)
	InsertGroupUnits(ctx context.Context, groupID string, userIDs []string) error
	DeleteGroupUnits(ctx context.Context, groupID string, userIDs []string) error
	SelectDeviceList(ctx context.Context, q knox.DeviceQuery) (knox.DevicePage, error)
	SelectDeviceInfo(ctx context.Context, deviceID string) (knox.Device, error)
	SelectDevicesByUser(ctx context.Context, userID string) ([]knox.Device, error)
	SelectDeviceDetail(ctx context.Context, deviceID string) (knox.DeviceDetail, error)
	UninstallApp(ctx context.Context, deviceID, pkg, knoxID string) error
	SyncInstalledAppList(ctx context.Context, deviceID string) error
	SelectDeviceLocation(ctx context.Context, deviceID string) (knox.DeviceLocation, error)
	SelectDeviceLocationHistory(ctx context.Context, deviceID string, from, to time.Time) ([]knox.DeviceLocation, error)
}

// Syncer is the background mirror sync, as seen by the admin endpoints.
type Syncer interface {
	// SyncNow starts a sync and returns its run id; alreadyRunning is true when it joined one in progress.
	SyncNow(ctx context.Context) (id int64, alreadyRunning bool, err error)
	// Running reports the id of the sync in progress, if any.
	Running() (id int64, running bool)
}

// Inventory pulls one device's installed apps from Knox into the mirror.
type Inventory interface {
	PullDevice(ctx context.Context, deviceID string) error
}

type Deps struct {
	Log   zerolog.Logger
	Auth  *auth.Service
	Users *db.Users
	Audit *db.Audit
	// Mirror is the local copy of the Knox tenant that reads are served from.
	Mirror *db.Mirror
	// Apps is the mirrored app inventory and the package categories; Inventory refreshes it per device.
	Apps      *db.Apps
	Inventory Inventory
	// Sync runs and reports mirror syncs.
	Sync Syncer
	// Knox returns the live client, or nil while Knox is not configured (settings can swap it).
	Knox     func() Knox
	Settings *settings.Manager
	// Update is the GitHub release checker; Version is the running build, shown on /api/health.
	Update  *update.Checker
	Version string
	// Setup is the pending first-run code; nil or cleared means setup is not required.
	Setup        *auth.SetupCode
	LoginLimiter *auth.LoginLimiter
	SetupLimiter *auth.LoginLimiter // separate from LoginLimiter so setup guesses and logins do not share budgets
	RefreshTTL   time.Duration      // lifetime of the refresh cookie
	// SecureCookies sets the Secure flag on the refresh cookie (config.UseSecureCookies).
	SecureCookies bool
	Static        fs.FS // the built web UI
	// TrustedProxies are the peer IPs/CIDRs whose X-Forwarded-For is believed; empty trusts none.
	TrustedProxies []string
}

type server struct {
	Deps
	csp     string // Content-Security-Policy for SPA responses
	setupMu sync.Mutex
}

// New builds the application: JSON API under /api, the web UI for everything else.
func New(d Deps) *fiber.App {
	s := &server{Deps: d, csp: contentSecurityPolicy(d.Static)}
	cfg := fiber.Config{BodyLimit: maxBody, ErrorHandler: s.errorHandler}
	if len(d.TrustedProxies) > 0 {
		cfg.TrustProxy = true
		cfg.TrustProxyConfig = fiber.TrustProxyConfig{Proxies: d.TrustedProxies}
		cfg.ProxyHeader = fiber.HeaderXForwardedFor
		// Without validation c.IP() returns the raw header, spoofable and unbounded. With it fiber
		// walks the chain right to left and returns the first hop that is not a trusted proxy.
		cfg.EnableIPValidation = true
	}
	app := fiber.New(cfg)
	app.Use(s.requestLog, s.securityHeaders)

	api := app.Group("/api", noStore, s.setupGate)
	api.Get("/health", s.health)
	api.Head("/health", s.health) // proxy and container health checks probe with HEAD

	setup := api.Group("/setup")
	setup.Post("/verify", s.setupVerify)
	setup.Post("/admin", s.setupAdmin)

	a := api.Group("/auth")
	a.Post("/login", s.login)
	a.Post("/refresh", sameOrigin, s.refresh)
	a.Post("/logout", sameOrigin, s.logout)
	a.Get("/me", Authenticate(d.Auth, d.Users), s.me)
	a.Put("/me", Authenticate(d.Auth, d.Users), s.updateAccount)
	a.Post("/me/password", sameOrigin, Authenticate(d.Auth, d.Users), s.changePassword)

	knoxG := api.Group("/knox", Authenticate(d.Auth, d.Users), s.knoxConfigured)
	s.knoxRoutes(knoxG)
	s.appRoutes(knoxG)
	adminG := api.Group("/admin", Authenticate(d.Auth, d.Users), RequireAdmin())
	s.adminRoutes(adminG)
	s.appAdminRoutes(adminG)

	api.All("/*", func(fiber.Ctx) error { return fiber.NewError(fiber.StatusNotFound, "not found") })
	app.Use(s.serveWeb)
	return app
}

type errorBody struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// errorHandler turns fiber's own errors (404, 413, bad JSON...) into the API's error shape.
// Anything else is a 500 whose detail stays in the log.
func (s *server) errorHandler(c fiber.Ctx, err error) error {
	var fe *fiber.Error
	var ke *knox.Error
	var ie *settings.InvalidError
	var se *fiberError
	switch {
	case errors.As(err, &fe):
		return c.Status(fe.Code).JSON(errorBody{Error: fe.Message})
	case errors.As(err, &se):
		return c.Status(se.status).JSON(errorBody{Error: se.msg, Code: se.code})
	case errors.Is(err, db.ErrInvalidCategoryName):
		return c.Status(fiber.StatusBadRequest).JSON(errorBody{Error: err.Error()})
	case errors.As(err, &ie):
		return c.Status(fiber.StatusBadRequest).JSON(errorBody{Error: ie.Msg})
	case errors.Is(err, auth.ErrForbidden):
		return c.Status(fiber.StatusForbidden).JSON(errorBody{Error: "forbidden"})
	case errors.Is(err, db.ErrNotFound):
		return c.Status(fiber.StatusNotFound).JSON(errorBody{Error: "not found"})
	case errors.Is(err, db.ErrDuplicate), errors.Is(err, db.ErrSourceConflict):
		return c.Status(fiber.StatusConflict).JSON(errorBody{Error: "already exists"})
	case errors.Is(err, knox.ErrUnreachable):
		s.Log.Warn().Err(err).Str("path", c.Path()).Msg("knox unreachable")
		return c.Status(fiber.StatusBadGateway).JSON(errorBody{Error: "Knox could not be reached", Code: "knox_unreachable"})
	case errors.As(err, &ke):
		// Knox's message is its own business text; the URL, token and secret never reach an *Error.
		s.Log.Warn().Str("knoxCode", ke.Code).Int("httpStatus", ke.HTTPStatus).Str("path", c.Path()).Str("detail", ke.Message).Msg("knox rejected request")
		msg := "Knox rejected the request: " + ke.Message
		if ke.HTTPStatus != 0 {
			msg = fmt.Sprintf("Knox answered HTTP %d: %s", ke.HTTPStatus, ke.Message)
			// Knox's 4xx text can describe its tenant; only admins see it, everyone else gets the log.
			if ke.HTTPStatus >= 400 && ke.HTTPStatus < 500 && auth.RequireAdmin(principalFrom(c)) != nil {
				msg = "Knox rejected the request"
			}
		}
		return c.Status(fiber.StatusBadGateway).JSON(errorBody{Error: msg, Code: "knox_error"})
	}
	s.Log.Error().Err(err).Str("path", c.Path()).Msg("unhandled error")
	return c.Status(fiber.StatusInternalServerError).JSON(errorBody{Error: "internal error"})
}

func (s *server) health(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "ok", "name": s.Settings.InstallationName(),
		"knoxConfigured": s.Knox() != nil, "setupRequired": s.Setup.Required(), "version": s.Version,
		"secureCookies": s.SecureCookies})
}

// setupGate holds back the whole API until the first admin exists; only health and setup answer.
func (s *server) setupGate(c fiber.Ctx) error {
	if p := c.Path(); s.Setup.Required() && p != "/api/health" && !strings.HasPrefix(p, "/api/setup/") {
		return c.Status(fiber.StatusConflict).JSON(errorBody{Error: "setup required", Code: "setup_required"})
	}
	return c.Next()
}

// knoxConfigured answers Knox routes with 503 until the admin has connected a tenant.
func (s *server) knoxConfigured(c fiber.Ctx) error {
	if s.Knox() == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorBody{Error: "Knox is not connected", Code: "knox_not_configured"})
	}
	return c.Next()
}

func (s *server) securityHeaders(c fiber.Ctx) error {
	c.Set("X-Content-Type-Options", "nosniff")
	c.Set("X-Frame-Options", "DENY")
	c.Set("Referrer-Policy", "same-origin")
	c.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	if s.SecureCookies {
		c.Set("Strict-Transport-Security", "max-age=31536000")
	}
	return c.Next()
}

// noStore keeps tokens and Knox data out of browser and proxy caches.
func noStore(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	return c.Next()
}

// sameOrigin rejects cross-origin browser requests to the cookie-authenticated endpoints. A missing
// Origin (non-browser client) passes; SameSite=Strict already covers the cookie.
func sameOrigin(c fiber.Ctx) error {
	origin := c.Get(fiber.HeaderOrigin)
	if origin == "" {
		return c.Next()
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host != c.Host() {
		return fiber.NewError(fiber.StatusForbidden, "cross-origin request refused")
	}
	return c.Next()
}

// requestLog logs method, path (never the query string or body), status and caller. 5xx are errors.
func (s *server) requestLog(c fiber.Ctx) error {
	start := time.Now()
	err := c.Next()
	status := c.Response().StatusCode()
	if err != nil {
		var fe *fiber.Error
		status = fiber.StatusInternalServerError
		if errors.As(err, &fe) {
			status = fe.Code
		}
	}
	ev := s.Log.Info()
	if status >= 500 {
		ev = s.Log.Error()
	}
	if p := principalFrom(c); p != nil {
		ev = ev.Str("user", p.Username)
	}
	ev.Str("method", c.Method()).Str("path", c.Path()).Int("status", status).
		Dur("took", time.Since(start)).Str("ip", c.IP()).Msg("request")
	return err
}

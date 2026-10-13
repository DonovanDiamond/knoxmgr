// Command knoxmgr is the single-binary Knox Manage web UI. This file only wires packages together.
package main

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"golang.org/x/term"

	"github.com/DonovanDiamond/knoxmgr/src/api"
	"github.com/DonovanDiamond/knoxmgr/src/auth"
	"github.com/DonovanDiamond/knoxmgr/src/config"
	"github.com/DonovanDiamond/knoxmgr/src/db"
	"github.com/DonovanDiamond/knoxmgr/src/secretbox"
	"github.com/DonovanDiamond/knoxmgr/src/settings"
	"github.com/DonovanDiamond/knoxmgr/src/sync"
	"github.com/DonovanDiamond/knoxmgr/src/update"
	"github.com/DonovanDiamond/knoxmgr/web"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	if err := run(log); err != nil {
		log.Fatal().Err(err).Msg("knoxmgr failed")
	}
}

func run(log zerolog.Logger) error {
	configPath := flag.String("config", "config.yaml", "path to config.yaml (optional unless set explicitly)")
	dataFlag := flag.String("data", "", "data directory (default $KNOXMGR_DATA, else ./data)")
	listenFlag := flag.String("listen", "", "listen address host:port (default $KNOXMGR_LISTEN, else config listenAddr)")
	createAdmin := flag.String("create-admin", "", "create a local admin with this username, then exit")
	flag.Parse()
	explicit := false
	flag.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "config" })

	dataDir, err := config.ResolveDataDir(*dataFlag)
	if err != nil {
		return err
	}
	cfg, err := config.Load(*configPath, explicit, dataDir)
	if err != nil {
		return err
	}
	listen := cmp.Or(*listenFlag, os.Getenv("KNOXMGR_LISTEN"), cfg.ListenAddr)
	if _, _, err := net.SplitHostPort(listen); err != nil {
		return fmt.Errorf("listen address %q must be host:port (for example :8080)", listen)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	conn, err := db.Open(cfg.Database.Driver, cfg.Database.DSN.Reveal())
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := db.Migrate(ctx, conn); err != nil {
		return err
	}
	users, audit := db.NewUsers(conn), db.NewAudit(conn)

	if *createAdmin != "" {
		return createLocalAdmin(ctx, users, audit, *createAdmin)
	}

	priv, pub, err := auth.LoadOrGenerateKeys(filepath.Join(dataDir, "jwt-private.pem"), filepath.Join(dataDir, "jwt-public.pem"))
	if err != nil {
		return err
	}
	box, err := secretbox.LoadOrCreate(filepath.Join(dataDir, "secret.key"))
	if err != nil {
		return err
	}
	svc := auth.NewService(users, db.NewRefreshTokens(conn), auth.NewTokens(priv, pub, cfg.JWT.AccessTTL), cfg.JWT.RefreshTTL, nil, nil)
	mgr := settings.New(db.NewAppSettings(conn), box, svc, log)
	if err := mgr.Load(ctx); err != nil {
		return err
	}

	mirror := db.NewMirror(conn)
	syncer := sync.New(mirror, mgr.Knox, func(ctx context.Context) (int, error) {
		g, err := mgr.GetGeneral(ctx)
		return g.SyncIntervalMinutes, err
	}, log)
	// KNOXMGR_TEST_GITHUB_API points the checker at a fake GitHub for screenshots; unset in production.
	checker := update.New(version, os.Getenv("KNOXMGR_TEST_GITHUB_API"), mgr.CheckUpdates, log)
	mgr.SetHooks(syncer.KnoxSaved, func() { syncer.Reschedule(); checker.Wake() })
	go syncer.Start(ctx)
	go checker.Start(ctx)

	var setup *auth.SetupCode
	if n, err := users.CountAdmins(ctx); err != nil {
		return err
	} else if n == 0 {
		if setup, err = auth.NewSetupCode(); err != nil {
			return err
		}
		log.Warn().Str("setupCode", setup.String()).Msg("first-run setup code")
	}

	static, err := web.FS()
	if err != nil {
		return err
	}
	app := api.New(api.Deps{
		Log: log, Auth: svc, Users: users, Audit: audit, Settings: mgr, Setup: setup, Mirror: mirror, Sync: syncer, Update: checker, Version: version,
		Knox: func() api.Knox {
			if c := mgr.Knox(); c != nil { // a nil *Client must not become a non-nil interface
				return c
			}
			return nil
		},
		LoginLimiter: auth.NewLoginLimiter(), SetupLimiter: auth.NewLoginLimiter(),
		RefreshTTL: cfg.JWT.RefreshTTL, SecureCookies: cfg.UseSecureCookies(), Static: static, TrustedProxies: cfg.TrustedProxies,
	})
	log.Info().Str("addr", listen).Str("dataDir", dataDir).Msg("listening")
	return app.Listen(listen, fiber.ListenConfig{DisableStartupMessage: true, GracefulContext: ctx})
}

func createLocalAdmin(ctx context.Context, users *db.Users, audit *db.Audit, username string) error {
	password, err := adminPassword()
	if err != nil {
		return err
	}
	if len(password) < config.MinPasswordLen || len(password) > config.MaxPasswordLen {
		return fmt.Errorf("password must be %d-%d bytes", config.MinPasswordLen, config.MaxPasswordLen)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	username = strings.ToLower(strings.TrimSpace(username))
	u := &db.User{Username: username, DisplayName: username, PasswordHash: &hash, Role: config.RoleAdmin, AuthSource: "local"}
	if err := users.Create(ctx, u); err != nil {
		return err
	}
	if err := audit.Insert(ctx, &db.AuditEntry{ActorUsername: "system", Action: "user.create_admin_cli", Target: username}); err != nil {
		return err
	}
	fmt.Printf("admin %q created\n", username)
	return nil
}

// adminPassword reads KNOXMGR_ADMIN_PASSWORD, else prompts without echo (or reads a line when
// stdin is not a terminal, so scripts can pipe it).
func adminPassword() (string, error) {
	if p := os.Getenv("KNOXMGR_ADMIN_PASSWORD"); p != "" {
		return p, nil
	}
	fd := int(os.Stdin.Fd())
	fmt.Fprint(os.Stderr, "Password: ")
	defer fmt.Fprintln(os.Stderr)
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		return string(b), err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return "", fmt.Errorf("reading password: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

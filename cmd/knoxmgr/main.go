// Command knoxmgr is the single-binary Knox Manage web UI. This file only wires packages together.
package main

import (
	"cmp"
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"

	"github.com/DonovanDiamond/knoxmgr/src/config"
	"github.com/DonovanDiamond/knoxmgr/src/server"
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return server.Run(ctx, log, server.Options{
		ConfigPath: *configPath, ConfigRequired: explicit, DataDir: dataDir,
		Listen: cmp.Or(*listenFlag, os.Getenv("KNOXMGR_LISTEN")), Version: version, CreateAdmin: *createAdmin,
	})
}

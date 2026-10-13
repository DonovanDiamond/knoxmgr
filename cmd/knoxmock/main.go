// Command knoxmock runs the in-memory fake Knox Manage server for local development.
package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/DonovanDiamond/knoxmgr/src/knoxmock"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	log := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Str("svc", "knoxmock").Logger()

	addr := flag.String("addr", envOr("KNOXMOCK_ADDR", "127.0.0.1:9090"), "listen address")
	id := flag.String("client-id", os.Getenv("KNOXMOCK_CLIENT_ID"), "OAuth client id")
	secret := flag.String("client-secret", os.Getenv("KNOXMOCK_CLIENT_SECRET"), "OAuth client secret")
	flag.Parse()
	if *id == "" || *secret == "" {
		log.Fatal().Msg("client id and secret are required (-client-id/-client-secret or KNOXMOCK_CLIENT_ID/KNOXMOCK_CLIENT_SECRET)")
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           knoxmock.New(*id, *secret, log),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			log.Error().Err(err).Msg("shutdown")
		}
	}()

	log.Info().Str("addr", *addr).Msg("knoxmock listening")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal().Err(err).Msg("serve")
	}
	log.Info().Msg("stopped")
}

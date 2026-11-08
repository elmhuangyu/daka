// Command daka is the entry point of the Daka habit tracker backend.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata" // embed the IANA database so timezones work in minimal images

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/elmhuangyu/daka/backend/lib/api"
	"github.com/elmhuangyu/daka/backend/lib/auth"
	"github.com/elmhuangyu/daka/backend/lib/config"
	"github.com/elmhuangyu/daka/backend/lib/db"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "daka: %v\n", err)
		os.Exit(1)
	}
}

// run loads the configuration and serves until ctx is cancelled.
func run(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) error {
	cfg, err := loadConfig(args, getenv, stderr)
	if err != nil {
		return err
	}
	log.Logger = zerolog.New(stderr).With().Timestamp().Logger()
	ln, err := net.Listen("tcp", cfg.Server.Listen)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	return serve(ctx, cfg, ln, log.Logger)
}

// loadConfig parses flags, loads and validates the configuration.
func loadConfig(args []string, getenv func(string) string, stderr io.Writer) (*config.Config, error) {
	fs := flag.NewFlagSet("daka", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("config", "", "path to the YAML config file")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	cfg, err := config.Load(*path, getenv)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	if cfg.Auth.Issuer != "" {
		return nil, errors.New("auth.issuer (multi-user mode) is not supported yet; unset it for single-user mode")
	}
	return cfg, nil
}

const (
	defaultUserName = "default_user"
	purgeInterval   = 24 * time.Hour
	shutdownTimeout = 10 * time.Second
)

// bootstrapDefaultUser idempotently creates the single-user account and its
// settings. Existing rows are never modified.
func bootstrapDefaultUser(g *gorm.DB, timezone string) error {
	return g.Transaction(func(tx *gorm.DB) error {
		u := dbmodels.User{ID: auth.DefaultUserID, Name: defaultUserName}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&u).Error; err != nil {
			return err
		}
		st := dbmodels.UserSettings{UserID: auth.DefaultUserID, Timezone: timezone, WeekStart: 1}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&st).Error
	})
}

func newHandler(cfg *config.Config, d *db.DB) http.Handler {
	var verifier auth.SessionVerifier // nil: single-user without shared_secret
	if cfg.Auth.SharedSecret != "" {
		verifier = auth.NewSharedSecretVerifier(cfg.Auth.SharedSecret.Reveal())
	}
	apiHandler := api.NewRouter(api.Deps{DB: d, Auth: auth.Middleware(verifier)})
	mux := http.NewServeMux()
	mux.Handle("/api/", apiHandler)
	if cfg.Server.StaticDir != "" {
		mux.Handle("/", api.StaticHandler(cfg.Server.StaticDir))
	}
	return mux
}

// serve runs the server on ln until ctx is cancelled, then shuts down gracefully.
func serve(ctx context.Context, cfg *config.Config, ln net.Listener, logger zerolog.Logger) error {
	defer func() { _ = ln.Close() }()
	d, err := db.Open(cfg.DB)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = d.Close() }()
	if err := dbmodels.AutoMigrate(d.Write()); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err := bootstrapDefaultUser(d.Write(), cfg.Timezone); err != nil {
		return fmt.Errorf("bootstrap default user: %w", err)
	}

	srv := &http.Server{
		Handler:           newHandler(cfg, d),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}

	if loopback, err := config.IsLoopbackListen(ln.Addr().String()); err == nil && !loopback {
		logger.Warn().Str("addr", ln.Addr().String()).Msg("listening on a non-loopback address; make sure it is protected")
	}
	if cfg.Auth.SharedSecret == "" {
		logger.Info().Msg("single-user mode: all requests use default_user")
	}

	var wg sync.WaitGroup
	purgeCtx, cancelPurge := context.WithCancel(ctx)
	wg.Add(1)
	go func() {
		defer wg.Done()
		purgeLoop(purgeCtx, d, logger)
	}()
	defer func() {
		cancelPurge()
		wg.Wait()
	}()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	logger.Info().Str("addr", ln.Addr().String()).Msg("daka listening")

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	logger.Info().Msg("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// purgeLoop deletes expired idempotency keys at start and then daily.
func purgeLoop(ctx context.Context, d *db.DB, logger zerolog.Logger) {
	purge := func() {
		n, err := api.PurgeIdempotencyKeys(d, time.Now())
		if err != nil {
			logger.Error().Err(err).Msg("purge idempotency keys failed")
			return
		}
		if n > 0 {
			logger.Info().Int64("deleted", n).Msg("purged idempotency keys")
		}
	}
	purge()
	t := time.NewTicker(purgeInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			purge()
		}
	}
}

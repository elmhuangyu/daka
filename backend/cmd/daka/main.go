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
	"strings"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata" // embed the IANA database so timezones work in minimal images

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/elmhuangyu/daka/backend/lib/api"
	"github.com/elmhuangyu/daka/backend/lib/app"
	"github.com/elmhuangyu/daka/backend/lib/auth"
	"github.com/elmhuangyu/daka/backend/lib/config"
	"github.com/elmhuangyu/daka/backend/lib/db"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/holiday"
	"github.com/elmhuangyu/daka/backend/lib/notify"
	"github.com/elmhuangyu/daka/backend/lib/secret"
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
	if len(args) >= 2 && args[0] == "user" && args[1] == "claim" {
		return runUserClaim(args[2:], getenv, stderr)
	}
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
	if cfg.Secrets.SecretKey == "" && strings.TrimSpace(cfg.Secrets.SecretKeyOld.Reveal()) != "" {
		return nil, errors.New("invalid secrets: DAKA_SECRET_KEY_OLD is set but DAKA_SECRET_KEY is not")
	}
	if cfg.Secrets.SecretKey != "" {
		// Fail fast on a malformed key; the error never contains key material.
		if _, err := secret.ParseKeyring(cfg.Secrets.SecretKey.Reveal(), cfg.Secrets.SecretKeyOld.Reveal()); err != nil {
			return nil, fmt.Errorf("invalid secrets: %w", err)
		}
	}
	return cfg, nil
}

const (
	purgeInterval   = 24 * time.Hour
	shutdownTimeout = 10 * time.Second
)

// serve runs the server on ln until ctx is cancelled, then shuts down gracefully.
func serve(ctx context.Context, cfg *config.Config, ln net.Listener, logger zerolog.Logger) error {
	defer func() { _ = ln.Close() }()
	if cfg.Server.StaticDir != "" {
		abs, hasIndex, err := api.ResolveStaticDir(cfg.Server.StaticDir)
		if err != nil {
			return err
		}
		cfg.Server.StaticDir = abs
		if !hasIndex {
			logger.Warn().Str("static_dir", abs).Msg("static_dir has no index.html; build the frontend (just fe-build)")
		}
	}
	d, err := db.Open(cfg.DB)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = d.Close() }()
	if err := dbmodels.AutoMigrate(d.Write()); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err := app.BootstrapDefaultUser(d.Write(), cfg.Timezone); err != nil {
		return fmt.Errorf("bootstrap default user: %w", err)
	}

	hp, err := newHolidayProvider(cfg.Holiday)
	if err != nil {
		return fmt.Errorf("holiday data: %w", err)
	}
	tg := notify.New(notify.Options{DB: d.Write(), Token: cfg.Secrets.TelegramBotToken.Reveal(), Calendar: app.HolidayCalendars(hp)})
	handler, sessions, err := app.NewHandler(cfg, d, hp, tg)
	if err != nil {
		return fmt.Errorf("build handler: %w", err)
	}
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}

	if loopback, err := config.IsLoopbackListen(ln.Addr().String()); err == nil && !loopback {
		logger.Warn().Str("addr", ln.Addr().String()).Msg("listening on a non-loopback address; make sure it is protected")
	}
	if proxies, err := config.ParseTrustedProxies(cfg.Server.TrustedProxies); err == nil {
		for _, p := range proxies {
			if p.Bits() == 0 {
				logger.Warn().Str("prefix", p.String()).Msg("server.trusted_proxies contains a wildcard prefix: any client can spoof X-Forwarded-For and evade rate limiting")
			}
		}
	}
	switch {
	case cfg.Auth.Issuer != "":
		logger.Info().Str("issuer", cfg.Auth.Issuer).Msg("OIDC mode")
		if cfg.Auth.SharedSecret != "" {
			logger.Warn().Msg("auth.shared_secret is ignored when auth.issuer is set")
		}
	case cfg.Auth.SharedSecret != "":
		logger.Info().Msg("single-user mode with shared-secret login")
	default:
		logger.Info().Msg("single-user mode: all requests use default_user")
	}
	if !cfg.Auth.SecureCookie && cfg.Auth.Issuer != "" {
		logger.Warn().Msg("auth.secure_cookie=false: session cookies are sent over plain HTTP")
	}

	var wg sync.WaitGroup
	purgeCtx, cancelPurge := context.WithCancel(ctx)
	wg.Add(1)
	go func() {
		defer wg.Done()
		purgeLoop(purgeCtx, d, sessions, logger)
	}()
	if tg.Configured() {
		logger.Info().Msg("telegram reminders enabled (long polling)")
		wg.Add(1)
		go func() {
			defer wg.Done()
			tg.Run(purgeCtx)
		}()
	}
	if hp != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hp.RunDailyRefresh(purgeCtx) // embedded snapshot first, remote refresh in the background
		}()
	}
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
func purgeLoop(ctx context.Context, d *db.DB, sessions *auth.Manager, logger zerolog.Logger) {
	purge := func() {
		n, err := api.PurgeIdempotencyKeys(d, time.Now())
		if err != nil {
			logger.Error().Err(err).Msg("purge idempotency keys failed")
			return
		}
		if n > 0 {
			logger.Info().Int64("deleted", n).Msg("purged idempotency keys")
		}
		if sessions != nil {
			if n, err := sessions.Purge(); err != nil {
				logger.Error().Err(err).Msg("purge sessions failed")
			} else if n > 0 {
				logger.Info().Int64("deleted", n).Msg("purged expired sessions")
			}
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

// newHolidayProvider builds the holiday provider (embedded snapshot, no
// network); nil when holiday support is disabled.
func newHolidayProvider(c config.HolidayConfig) (*holiday.Provider, error) {
	if c.Disabled {
		return nil, nil
	}
	var opts []holiday.Option
	if c.URLTemplate != "" {
		opts = append(opts, holiday.WithURLTemplate(c.URLTemplate))
	}
	return holiday.NewProvider(opts...)
}

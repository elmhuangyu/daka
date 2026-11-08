// Package app wires the HTTP stack (authentication, API, static files) from a
// validated configuration. cmd/daka and the end-to-end tests share it, so the
// tests exercise exactly the production handler chain.
package app

import (
	"net/http"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/elmhuangyu/daka/backend/lib/api"
	"github.com/elmhuangyu/daka/backend/lib/auth"
	"github.com/elmhuangyu/daka/backend/lib/config"
	"github.com/elmhuangyu/daka/backend/lib/db"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/extcal"
	"github.com/elmhuangyu/daka/backend/lib/holiday"
	"github.com/elmhuangyu/daka/backend/lib/notify"
	"github.com/elmhuangyu/daka/backend/lib/ratelimit"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
	"github.com/elmhuangyu/daka/backend/lib/secret"
)

// DefaultUserName is the display name of the single-user account.
const DefaultUserName = "default_user"

// Options are optional knobs of NewHandler.
type Options struct {
	// Now is the clock used by the API, sessions, re-authentication and OIDC
	// verification; nil means time.Now. Tests inject a controllable clock.
	Now func() time.Time
	// OIDCHTTPClient is used for OIDC discovery, JWKS and token requests; nil
	// means a default client (tests point it at a fake provider).
	OIDCHTTPClient *http.Client
}

// BootstrapDefaultUser idempotently creates the single-user account and its
// settings. Existing rows are never modified.
func BootstrapDefaultUser(g *gorm.DB, timezone string) error {
	return g.Transaction(func(tx *gorm.DB) error {
		u := dbmodels.User{ID: auth.DefaultUserID, Name: DefaultUserName}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&u).Error; err != nil {
			return err
		}
		st := dbmodels.UserSettings{UserID: auth.DefaultUserID, Timezone: timezone, WeekStart: 1}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&st).Error
	})
}

// NewHandler wires authentication and the API. Login is required (cookie
// sessions) when OIDC or auth.shared_secret is configured; otherwise every
// request is default_user (loopback single-user mode). It returns the session
// manager (nil without login) so the caller can purge expired sessions.
func NewHandler(cfg *config.Config, d *db.DB, hp *holiday.Provider, tg *notify.Service, opts ...Options) (http.Handler, *auth.Manager, error) {
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	proxies, err := config.ParseTrustedProxies(cfg.Server.TrustedProxies)
	if err != nil {
		return nil, nil, err
	}
	var limiter *ratelimit.Limiter
	if rl := cfg.Server.RateLimit; !rl.Disabled {
		limiter = ratelimit.NewLimiter(ratelimit.LimiterConfig{RPS: rl.RPS, Burst: rl.Burst})
	}
	clientIP := ratelimit.ClientIPResolver{Trusted: proxies}
	var (
		manager  *auth.Manager
		verifier auth.SessionVerifier // stays a nil interface without login
		oidc     *auth.OIDC
	)
	loginRequired := cfg.Auth.Issuer != "" || cfg.Auth.SharedSecret != ""
	if loginRequired {
		manager = auth.NewManager(d.Write(), auth.ManagerOptions{SecureCookie: cfg.Auth.SecureCookie, Now: o.Now})
		verifier = manager
	}
	if cfg.Auth.Issuer != "" {
		oidc, err = auth.NewOIDC(auth.OIDCConfig{
			Issuer: cfg.Auth.Issuer, ClientID: cfg.Auth.ClientID, ClientSecret: cfg.Auth.ClientSecret.Reveal(),
			RedirectURL: cfg.Auth.RedirectURL, Scopes: cfg.Auth.Scopes,
			HTTPClient: o.OIDCHTTPClient, Now: o.Now,
		})
		if err != nil {
			return nil, nil, err
		}
	}
	authFail := ratelimit.NewAuthFailureLimiter(ratelimit.AuthFailureConfig{})
	authHandler := auth.NewHandler(auth.HandlerOptions{
		DB: d.Write(), Manager: manager, SharedSecret: cfg.Auth.SharedSecret.Reveal(), OIDC: oidc,
		Timezone: cfg.Timezone, SecureCookie: cfg.Auth.SecureCookie,
		Limiter: authFail, ClientIP: clientIP.ClientIP, Now: o.Now,
	})
	var keys *secret.Keyring // nil without DAKA_SECRET_KEY: saving external URLs answers 503
	if cfg.Secrets.SecretKey.Reveal() != "" {
		if keys, err = secret.ParseKeyring(cfg.Secrets.SecretKey.Reveal(), cfg.Secrets.SecretKeyOld.Reveal()); err != nil {
			return nil, nil, err
		}
	}
	extCal := extcal.NewService(extcal.ServiceOptions{Keys: keys, AllowHTTP: cfg.ExternalCalendar.AllowHTTP})
	apiHandler := api.NewRouter(api.Deps{DB: d, Auth: auth.Middleware(verifier), Limiter: limiter, ClientIP: clientIP, ExtCal: extCal, Now: o.Now, Reauth: auth.RequireReauth(o.Now),
		Calendar: HolidayCalendars(hp), Telegram: tg, SecureCookie: cfg.Auth.SecureCookie})
	mux := http.NewServeMux()
	// Public auth endpoints authenticate themselves; they share the global
	// per-IP rate limit (this covers GET /api/auth/oidc/login too).
	mux.Handle("/api/auth/", ratelimit.Middleware(limiter, clientIP)(authHandler))
	mux.Handle("/api/", apiHandler)
	if cfg.Server.StaticDir != "" {
		mux.Handle("/", api.StaticHandler(cfg.Server.StaticDir))
	}
	var h http.Handler = mux
	// DNS-rebinding defence: without login the only protection is the loopback
	// bind, so unknown Host headers are refused. With login (possibly on a LAN
	// address) the check applies only when server.allowed_hosts is configured.
	if !loginRequired || len(cfg.Server.AllowedHosts) > 0 {
		h = auth.HostGuard(auth.NewHostAllowlist(cfg.Server.Listen, cfg.Server.AllowedHosts))(h)
	}
	return h, manager, nil
}

// HolidayCalendars maps UserSettings.holiday_region to a calendar: CN has
// data, every other region (and a disabled provider) has none.
func HolidayCalendars(hp *holiday.Provider) func(dbmodels.HolidayRegion) schedule.DayCalendar {
	if hp == nil {
		return nil
	}
	cn := holiday.NewCalendar(hp, holiday.RegionCN)
	return func(r dbmodels.HolidayRegion) schedule.DayCalendar {
		if string(r) == holiday.RegionCN {
			return cn
		}
		return nil
	}
}

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/elmhuangyu/daka/backend/lib/auth"
	"github.com/elmhuangyu/daka/backend/lib/db"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/extcal"
	"github.com/elmhuangyu/daka/backend/lib/ratelimit"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
	"github.com/elmhuangyu/daka/backend/lib/stats"
)

// Deps are the dependencies of the HTTP API.
type Deps struct {
	DB *db.DB
	// Auth wraps handlers with authentication; it must inject the user into
	// the request context (see auth.Middleware). Nil means no wrapping.
	Auth func(http.Handler) http.Handler
	// Now returns the current time; nil means time.Now. Tests inject a fixed clock.
	Now func() time.Time
	// Limiter is the global per-client-IP rate limiter applied before
	// authentication (so unauthenticated floods are limited too). Nil disables it.
	Limiter *ratelimit.Limiter
	// ClientIP resolves the client IP for Limiter; the zero value uses only
	// RemoteAddr (X-Forwarded-For needs configured trusted proxies).
	ClientIP ratelimit.ClientIPResolver
	// Calendar returns the holiday calendar for a UserSettings.holiday_region;
	// nil, or a nil result, means no holiday awareness.
	Calendar func(region dbmodels.HolidayRegion) schedule.DayCalendar
	// Reauth guards dangerous routes (DELETE /api/items/{id}); it must run
	// after Auth (see auth.RequireReauth). Nil means no extra guard.
	Reauth func(http.Handler) http.Handler
	// Versions are the per-item write counters bumped by check-in writes (part
	// of the stats cache key); nil creates a private set.
	Versions *stats.Versions
	// StatsCache caches GET /api/items/{id}/stats; nil creates a default one.
	StatsCache *stats.Cache
	// ExtCal serves external calendar settings and overlay data. Nil means a
	// service without an encryption key (saving URLs answers 503).
	ExtCal *extcal.Service
	// FeedFailures tracks feed token failures per (client IP, token prefix);
	// FeedIPFailures is the per-IP aggregate (prefix-independent) cap. Nil
	// creates defaults (5 failures per pair, 20 per IP, exponential lockout).
	FeedFailures   *ratelimit.AuthFailureLimiter
	FeedIPFailures *ratelimit.AuthFailureLimiter
	// Telegram is the notify service; nil means Telegram is not configured.
	Telegram TelegramBot
	// SecureCookie must match auth.secure_cookie: DELETE /api/account clears
	// the session cookie with the same attributes.
	SecureCookie bool
}

type server struct {
	secureCookie bool
	db           *db.DB
	now          func() time.Time
	calendar     func(region dbmodels.HolidayRegion) schedule.DayCalendar
	versions     *stats.Versions
	cache        *stats.Cache
	ext          *extcal.Service
	telegram     TelegramBot

	clientIP   ratelimit.ClientIPResolver
	feedFail   *ratelimit.AuthFailureLimiter
	feedIPFail *ratelimit.AuthFailureLimiter
}

// NewRouter builds the API router. Only /api/ is rate limited (via Deps.Limiter);
// static frontend files are served outside this router and are not limited.
func NewRouter(d Deps) http.Handler {
	s := &server{db: d.DB, now: d.Now, calendar: d.Calendar, versions: d.Versions, cache: d.StatsCache, ext: d.ExtCal,
		clientIP: d.ClientIP, feedFail: d.FeedFailures, feedIPFail: d.FeedIPFailures, telegram: d.Telegram,
		secureCookie: d.SecureCookie}
	if s.versions == nil {
		s.versions = stats.NewVersions()
	}
	if s.cache == nil {
		s.cache = stats.NewCache(stats.DefaultCacheSize)
	}
	if s.ext == nil {
		s.ext = extcal.NewService(extcal.ServiceOptions{})
	}
	if s.feedFail == nil {
		s.feedFail = ratelimit.NewAuthFailureLimiter(ratelimit.AuthFailureConfig{})
	}
	if s.feedIPFail == nil {
		s.feedIPFail = ratelimit.NewAuthFailureLimiter(ratelimit.AuthFailureConfig{Threshold: 20})
	}
	if s.now == nil {
		s.now = time.Now
	}
	reauth := d.Reauth
	if reauth == nil {
		reauth = func(h http.Handler) http.Handler { return h }
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/checkins", s.postCheckin)
	mux.HandleFunc("PATCH /api/checkins/{id}", s.patchCheckin)
	mux.HandleFunc("DELETE /api/checkins/{id}", s.deleteCheckin)
	mux.HandleFunc("GET /api/items", s.listItems)
	mux.HandleFunc("POST /api/items", s.postItem)
	mux.HandleFunc("GET /api/items/{id}", s.getItem)
	mux.HandleFunc("PATCH /api/items/{id}", s.patchItem)
	mux.Handle("DELETE /api/items/{id}", reauth(http.HandlerFunc(s.deleteItem)))
	mux.HandleFunc("GET /api/items/{id}/options", s.listOptions)
	mux.HandleFunc("POST /api/items/{id}/options", s.postOption)
	mux.HandleFunc("PUT /api/items/{id}/options/{oid}", s.putOption)
	mux.HandleFunc("DELETE /api/items/{id}/options/{oid}", s.deleteOption)
	mux.HandleFunc("GET /api/checkins", s.listCheckins)
	mux.HandleFunc("GET /api/view/today", s.getToday)
	mux.HandleFunc("GET /api/items/{id}/stats", s.getItemStats)
	mux.HandleFunc("GET /api/categories", s.listCategories)
	mux.HandleFunc("POST /api/categories", s.postCategory)
	mux.HandleFunc("PUT /api/categories/{id}", s.putCategory)
	mux.Handle("DELETE /api/categories/{id}", reauth(http.HandlerFunc(s.deleteCategory)))
	mux.HandleFunc("GET /api/freezes", s.listFreezes)
	mux.HandleFunc("POST /api/freezes", s.postFreeze)
	mux.HandleFunc("DELETE /api/freezes/{id}", s.deleteFreeze)
	mux.HandleFunc("GET /api/settings/calendar", s.getCalendarSettings)
	mux.HandleFunc("PUT /api/settings/calendar", s.putCalendarSettings)
	mux.HandleFunc("GET /api/calendar/external", s.getExternalCalendar)
	mux.HandleFunc("GET /api/view/calendar", s.getCalendarView)
	mux.HandleFunc("GET /api/calendar/holidays", s.getHolidays)
	mux.HandleFunc("GET /api/tokens", s.listTokens)
	mux.Handle("POST /api/tokens", reauth(http.HandlerFunc(s.postToken)))
	mux.Handle("DELETE /api/tokens/{id}", reauth(http.HandlerFunc(s.deleteToken)))
	mux.HandleFunc("GET /api/telegram", s.getTelegram)
	mux.Handle("PUT /api/telegram", reauth(http.HandlerFunc(s.putTelegram)))
	mux.Handle("POST /api/telegram/link-code", reauth(http.HandlerFunc(s.postTelegramLinkCode)))
	mux.HandleFunc("POST /api/telegram/test", s.postTelegramTest)
	mux.Handle("DELETE /api/telegram", reauth(http.HandlerFunc(s.deleteTelegram)))
	mux.HandleFunc("PATCH /api/telegram/settings", s.patchTelegramSettings)
	mux.Handle("GET /api/export", reauth(http.HandlerFunc(s.getExport)))
	mux.Handle("POST /api/import", reauth(http.HandlerFunc(s.postImport)))
	mux.Handle("DELETE /api/account", reauth(http.HandlerFunc(s.deleteAccount)))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		errResult(http.StatusNotFound, "not found").write(w)
	})
	var h http.Handler = mux
	if d.Auth != nil {
		h = d.Auth(h)
	}
	h = s.tokenGate(mux, h)
	// The feed authenticates by its own feed:read token and is the only route
	// outside Auth; every other path, token-bearing or not, goes through Auth.
	top := http.NewServeMux()
	top.HandleFunc(feedPath, s.getFeed)
	top.HandleFunc("/api/openapi.json", s.getOpenAPI)
	top.Handle("/", h)
	return ratelimit.Middleware(d.Limiter, d.ClientIP)(top)
}

// result is a fully rendered JSON response.
type result struct {
	status int
	body   []byte
}

func jsonResult(status int, v any) result {
	b, err := json.Marshal(v)
	if err != nil {
		return errResult(http.StatusInternalServerError, "internal error")
	}
	return result{status: status, body: b}
}

// errResult never takes user data (item names must not leak into errors).
func errResult(status int, msg string) result {
	return jsonResult(status, map[string]string{"error": msg})
}

func (r result) write(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(r.status)
	_, _ = w.Write(r.body)
}

// tokenGate handles API tokens (Authorization: Bearer dk_...) before Auth runs,
// so a token can never act as a session credential even where Auth is
// permissive (single-user mode without login). The only thing a token may do
// here is a GET on the read allowlist with a read-scope token; every other
// token-bearing request is refused before any database lookup. The feed is
// mounted outside this gate and takes feed:read tokens only.
func (s *server) tokenGate(mux, authed http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := presentedBearer(r)
		if !strings.HasPrefix(tok, apiTokenPrefix) {
			authed.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet || !readAllowed(r.URL.Path) {
			errResult(http.StatusUnauthorized, "api token not accepted here").write(w)
			return
		}
		t, ok := s.authenticateToken(w, r, tok, dbmodels.ScopeRead)
		if !ok {
			return
		}
		s.touchToken(t)
		ctx := auth.WithUser(r.Context(), auth.User{ID: t.UserID})
		ctx = context.WithValue(ctx, readTokenKey{}, true)
		w.Header().Set("Cache-Control", "private, no-store")
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}

// readAllowed is the strict allowlist of paths a read token may GET. Anything
// else (tokens, telegram, settings, auth, external calendar, categories,
// options, freezes) is refused.
func readAllowed(path string) bool {
	switch path {
	case "/api/items", "/api/checkins", "/api/view/today", "/api/view/calendar", "/api/calendar/holidays":
		return true
	}
	rest, ok := strings.CutPrefix(path, "/api/items/")
	if !ok {
		return false
	}
	id, suffix, _ := strings.Cut(rest, "/")
	if id == "" || strings.ContainsAny(id, "/") {
		return false
	}
	return suffix == "" || suffix == "stats"
}

func presentedBearer(r *http.Request) string {
	if h := r.Header.Get("Authorization"); len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

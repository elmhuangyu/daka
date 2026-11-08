package api

import (
	"encoding/json"
	"net/http"
	"time"

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
}

type server struct {
	db       *db.DB
	now      func() time.Time
	calendar func(region dbmodels.HolidayRegion) schedule.DayCalendar
	versions *stats.Versions
	cache    *stats.Cache
	ext      *extcal.Service
}

// NewRouter builds the API router. Only /api/ is rate limited (via Deps.Limiter);
// static frontend files are served outside this router and are not limited.
func NewRouter(d Deps) http.Handler {
	s := &server{db: d.DB, now: d.Now, calendar: d.Calendar, versions: d.Versions, cache: d.StatsCache, ext: d.ExtCal}
	if s.versions == nil {
		s.versions = stats.NewVersions()
	}
	if s.cache == nil {
		s.cache = stats.NewCache(stats.DefaultCacheSize)
	}
	if s.ext == nil {
		s.ext = extcal.NewService(extcal.ServiceOptions{})
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
	mux.HandleFunc("GET /api/freezes", s.listFreezes)
	mux.HandleFunc("POST /api/freezes", s.postFreeze)
	mux.HandleFunc("DELETE /api/freezes/{id}", s.deleteFreeze)
	mux.HandleFunc("GET /api/settings/calendar", s.getCalendarSettings)
	mux.HandleFunc("PUT /api/settings/calendar", s.putCalendarSettings)
	mux.HandleFunc("GET /api/calendar/external", s.getExternalCalendar)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		errResult(http.StatusNotFound, "not found").write(w)
	})
	var h http.Handler = mux
	if d.Auth != nil {
		h = d.Auth(h)
	}
	return ratelimit.Middleware(d.Limiter, d.ClientIP)(h)
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

package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/elmhuangyu/daka/backend/lib/db"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
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
	// Versions are the per-item write counters bumped by check-in writes (part
	// of the stats cache key); nil creates a private set.
	Versions *stats.Versions
	// StatsCache caches GET /api/items/{id}/stats; nil creates a default one.
	StatsCache *stats.Cache
}

type server struct {
	db       *db.DB
	now      func() time.Time
	calendar func(region dbmodels.HolidayRegion) schedule.DayCalendar
	versions *stats.Versions
	cache    *stats.Cache
}

// NewRouter builds the API router. Only /api/ is rate limited (via Deps.Limiter);
// static frontend files are served outside this router and are not limited.
func NewRouter(d Deps) http.Handler {
	s := &server{db: d.DB, now: d.Now, calendar: d.Calendar, versions: d.Versions, cache: d.StatsCache}
	if s.versions == nil {
		s.versions = stats.NewVersions()
	}
	if s.cache == nil {
		s.cache = stats.NewCache(stats.DefaultCacheSize)
	}
	if s.now == nil {
		s.now = time.Now
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/checkins", s.postCheckin)
	mux.HandleFunc("DELETE /api/checkins/{id}", s.deleteCheckin)
	mux.HandleFunc("POST /api/items", s.postItem)
	mux.HandleFunc("GET /api/view/today", s.getToday)
	mux.HandleFunc("GET /api/items/{id}/stats", s.getItemStats)
	mux.HandleFunc("GET /api/freezes", s.listFreezes)
	mux.HandleFunc("POST /api/freezes", s.postFreeze)
	mux.HandleFunc("DELETE /api/freezes/{id}", s.deleteFreeze)
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

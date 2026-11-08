package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

// HandlerOptions configures the /api/auth/* endpoints.
type HandlerOptions struct {
	DB      *gorm.DB // write handle
	Manager *Manager // nil only when neither shared secret nor OIDC is configured
	// SharedSecret enables POST /api/auth/login in single-user mode; ignored
	// when OIDC is set.
	SharedSecret string
	OIDC         *OIDC
	Limiter      FailureLimiter // nil means NopLimiter
	// ClientIP resolves the client IP for Limiter (ratelimit.ClientIPResolver);
	// nil falls back to the TCP peer address.
	ClientIP func(*http.Request) string
	// Timezone seeds UserSettings of newly created OIDC users.
	Timezone     string
	SecureCookie bool
	Now          func() time.Time
}

type handler struct {
	HandlerOptions
}

// RedirectAfterLogin is the fixed post-login target; it is never taken from
// the request, so the flow cannot be abused as an open redirect.
const RedirectAfterLogin = "/"

// NewHandler returns a handler for the "/api/auth/" subtree.
func NewHandler(o HandlerOptions) http.Handler {
	if o.Limiter == nil {
		o.Limiter = NopLimiter{}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Timezone == "" {
		o.Timezone = "UTC"
	}
	h := &handler{o}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/auth/status", h.status)
	mux.HandleFunc("POST /api/auth/login", h.login)
	mux.HandleFunc("POST /api/auth/logout", h.logout)
	mux.HandleFunc("GET /api/auth/me", h.me)
	mux.HandleFunc("POST /api/auth/reauth", h.reauth)
	mux.HandleFunc("GET /api/auth/oidc/login", h.oidcLogin)
	mux.HandleFunc("GET /api/auth/oidc/callback", h.oidcCallback)
	mux.HandleFunc("/api/auth/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (h *handler) loginRequired() bool { return h.OIDC != nil || h.SharedSecret != "" }

func (h *handler) session(r *http.Request) (Session, bool) {
	if h.Manager == nil {
		return Session{}, false
	}
	return h.Manager.Session(r)
}

func (h *handler) status(w http.ResponseWriter, r *http.Request) {
	_, authed := h.session(r)
	required := h.loginRequired()
	writeJSON(w, http.StatusOK, map[string]bool{
		"oidc_enabled":   h.OIDC != nil,
		"login_required": required,
		"authenticated":  authed || !required,
	})
}

func (h *handler) me(w http.ResponseWriter, r *http.Request) {
	userID := DefaultUserID
	resp := map[string]string{}
	if h.loginRequired() {
		s, ok := h.session(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		userID = s.UserID
		resp["csrf_token"] = s.CSRFToken
	}
	var u dbmodels.User
	if err := h.DB.Where("id = ?", userID).Take(&u).Error; err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	resp["id"], resp["name"] = u.ID, u.Name
	if u.Email != "" {
		resp["email"] = u.Email
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) clientKey(r *http.Request) string {
	if h.ClientIP != nil {
		return h.ClientIP(r)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// checkSecret compares in constant time irrespective of length.
func checkSecret(got, want string) bool {
	a, b := sha256.Sum256([]byte(got)), sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

// readSecret parses {"secret": "..."} and applies the failure limiter and
// verification. It writes the error response and returns false on failure.
func (h *handler) readSecret(w http.ResponseWriter, r *http.Request) bool {
	if h.SharedSecret == "" {
		writeError(w, http.StatusNotFound, "not found")
		return false
	}
	key := h.clientKey(r)
	if ok, wait := h.Limiter.Check(key, "login"); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "too_many_attempts")
		return false
	}
	var body struct {
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return false
	}
	if !checkSecret(body.Secret, h.SharedSecret) {
		h.Limiter.Failure(key, "login")
		writeError(w, http.StatusUnauthorized, "invalid_secret")
		return false
	}
	h.Limiter.Success(key, "login")
	return true
}

func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	// Login has no session yet, hence no CSRF token; reject cross-site posts
	// by provenance headers instead (prevents login CSRF).
	if !SameOrigin(r) {
		writeError(w, http.StatusForbidden, "cross_origin")
		return
	}
	if h.OIDC != nil || h.SharedSecret == "" || h.Manager == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if !h.readSecret(w, r) {
		return
	}
	now := h.Now()
	if _, err := h.Manager.Create(w, r, DefaultUserID, dbmodels.AuthMethodSecret, now); err != nil {
		log.Error().Err(err).Msg("create session")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	if h.Manager != nil {
		if s, ok := h.Manager.Session(r); ok && !CheckCSRF(r, s) {
			writeError(w, http.StatusForbidden, "csrf")
			return
		}
		if err := h.Manager.Revoke(w, r); err != nil {
			log.Error().Err(err).Msg("revoke session")
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) reauth(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !CheckCSRF(r, s) {
		writeError(w, http.StatusForbidden, "csrf")
		return
	}
	if h.OIDC != nil {
		writeJSON(w, http.StatusOK, map[string]string{"redirect": "/api/auth/oidc/login?reauth=1"})
		return
	}
	if !h.readSecret(w, r) { // 404 when no shared secret is configured
		return
	}
	if err := h.Manager.MarkReauth(r); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *handler) oidcLogin(w http.ResponseWriter, r *http.Request) {
	if h.OIDC == nil || h.Manager == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	reauth := r.URL.Query().Get("reauth") == "1"
	if reauth {
		if _, ok := h.Manager.Session(r); !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
	}
	u, err := h.OIDC.Begin(r.Context(), w, h.SecureCookie, reauth)
	if err != nil {
		log.Error().Err(err).Msg("oidc begin")
		writeError(w, http.StatusBadGateway, "identity provider unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, u, http.StatusFound)
}

func (h *handler) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if h.OIDC == nil || h.Manager == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	id, err := h.OIDC.Complete(r.Context(), w, r, h.SecureCookie)
	if err != nil {
		log.Warn().Err(err).Msg("oidc callback failed")
		if errors.Is(err, ErrOIDC) {
			writeError(w, http.StatusBadRequest, "login_failed")
		} else {
			writeError(w, http.StatusBadGateway, "identity provider unavailable")
		}
		return
	}
	userID := id.UserID()
	if id.Reauth {
		s, ok := h.Manager.Session(r)
		if !ok || s.UserID != userID {
			log.Warn().Msg("oidc reauth by a different identity or without session")
			writeError(w, http.StatusForbidden, "login_failed")
			return
		}
		if err := h.Manager.MarkReauth(r); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		http.Redirect(w, r, RedirectAfterLogin, http.StatusFound)
		return
	}
	if err := h.upsertUser(userID, id); err != nil {
		log.Error().Err(err).Msg("upsert oidc user")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if _, err := h.Manager.Create(w, r, userID, dbmodels.AuthMethodOIDC, id.AuthTime); err != nil {
		log.Error().Err(err).Msg("create session")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	http.Redirect(w, r, RedirectAfterLogin, http.StatusFound)
}

// trunc cuts s to at most n bytes without splitting a UTF-8 sequence.
func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// upsertUser creates the user and default settings, or refreshes the purely
// informational name / email of an existing user.
func (h *handler) upsertUser(userID string, id Identity) error {
	return h.DB.Transaction(func(tx *gorm.DB) error {
		u := dbmodels.User{ID: userID, Name: trunc(id.Name, 255), Email: trunc(id.Email, 255)}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"name", "email"}),
		}).Create(&u).Error; err != nil {
			return err
		}
		st := dbmodels.UserSettings{UserID: userID, Timezone: h.Timezone, WeekStart: 1}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&st).Error
	})
}

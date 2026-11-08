package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

const (
	// DefaultSessionTTL is the absolute lifetime of a session.
	DefaultSessionTTL = 14 * 24 * time.Hour
	// ReauthWindow is how long a re-authentication stays valid (DESIGN 8).
	ReauthWindow = 5 * time.Minute
)

// Session is the authenticated state behind a session cookie.
type Session struct {
	UserID      string
	AuthMethod  string
	CSRFToken   string
	ExpiresAt   time.Time
	ReauthUntil time.Time // zero when never re-authenticated
}

// ManagerOptions configures a Manager.
type ManagerOptions struct {
	// SecureCookie sets the Secure cookie attribute (auth.secure_cookie).
	SecureCookie bool
	TTL          time.Duration    // 0 means DefaultSessionTTL
	Now          func() time.Time // nil means time.Now
}

// Manager is the database-backed session store. The cookie holds 256 random
// bits; only their SHA-256 is stored, so lookups are by hash and a database
// leak yields no usable cookie. It implements SessionVerifier and SessionSource.
type Manager struct {
	db     *gorm.DB
	secure bool
	ttl    time.Duration
	now    func() time.Time
}

// NewManager builds a Manager on the write handle of the database.
func NewManager(db *gorm.DB, o ManagerOptions) *Manager {
	m := &Manager{db: db, secure: o.SecureCookie, ttl: o.TTL, now: o.Now}
	if m.ttl == 0 {
		m.ttl = DefaultSessionTTL
	}
	if m.now == nil {
		m.now = time.Now
	}
	return m
}

func hashID(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// csrfToken derives the CSRF token from the raw cookie value. It is bound to
// the session, unpredictable without the cookie, and needs no extra storage.
func csrfToken(raw string) string {
	m := hmac.New(sha256.New, []byte(raw))
	m.Write([]byte("daka-csrf-v1"))
	return hex.EncodeToString(m.Sum(nil))
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Create starts a session for userID, sets the cookie on w, and revokes any
// session presented on r (fixation protection). authTime is when the user
// actually authenticated.
func (m *Manager) Create(w http.ResponseWriter, r *http.Request, userID, method string, authTime time.Time) (Session, error) {
	m.revokeRequest(r)
	raw, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	now := m.now()
	row := dbmodels.Session{
		ID: hashID(raw), UserID: userID, AuthMethod: method,
		AuthTime: authTime, CreatedAt: now, ExpiresAt: now.Add(m.ttl),
	}
	if err := m.db.Create(&row).Error; err != nil {
		return Session{}, err
	}
	SetCookie(w, SessionCookieName, raw, int(m.ttl/time.Second), m.secure)
	return toSession(row, raw), nil
}

func toSession(row dbmodels.Session, raw string) Session {
	s := Session{UserID: row.UserID, AuthMethod: row.AuthMethod, CSRFToken: csrfToken(raw), ExpiresAt: row.ExpiresAt}
	if row.ReauthUntil != nil {
		s.ReauthUntil = *row.ReauthUntil
	}
	return s
}

func (m *Manager) lookup(r *http.Request) (dbmodels.Session, string, bool) {
	c, err := r.Cookie(SessionCookieName)
	if err != nil || c.Value == "" || len(c.Value) > 128 {
		return dbmodels.Session{}, "", false
	}
	var row dbmodels.Session
	if err := m.db.Where("id = ? AND expires_at > ?", hashID(c.Value), m.now()).Take(&row).Error; err != nil {
		return dbmodels.Session{}, "", false
	}
	return row, c.Value, true
}

// Session implements SessionSource.
func (m *Manager) Session(r *http.Request) (Session, bool) {
	row, raw, ok := m.lookup(r)
	if !ok {
		return Session{}, false
	}
	return toSession(row, raw), true
}

// Verify implements SessionVerifier.
func (m *Manager) Verify(r *http.Request) (User, bool) {
	s, ok := m.Session(r)
	return User{ID: s.UserID}, ok
}

// MarkReauth records a fresh re-authentication on the session of r.
func (m *Manager) MarkReauth(r *http.Request) error {
	row, _, ok := m.lookup(r)
	if !ok {
		return errors.New("no session")
	}
	until := m.now().Add(ReauthWindow)
	return m.db.Model(&dbmodels.Session{}).Where("id = ?", row.ID).
		Updates(map[string]any{"reauth_until": until, "auth_time": m.now()}).Error
}

func (m *Manager) revokeRequest(r *http.Request) {
	if c, err := r.Cookie(SessionCookieName); err == nil && c.Value != "" {
		_ = m.db.Where("id = ?", hashID(c.Value)).Delete(&dbmodels.Session{}).Error
	}
}

// Revoke deletes the session presented on r and clears the cookie on w.
func (m *Manager) Revoke(w http.ResponseWriter, r *http.Request) error {
	var err error
	if c, cerr := r.Cookie(SessionCookieName); cerr == nil && c.Value != "" {
		err = m.db.Where("id = ?", hashID(c.Value)).Delete(&dbmodels.Session{}).Error
	}
	SetCookie(w, SessionCookieName, "", -1, m.secure)
	return err
}

// Purge deletes expired sessions and returns how many were removed.
func (m *Manager) Purge() (int64, error) {
	res := m.db.Where("expires_at <= ?", m.now()).Delete(&dbmodels.Session{})
	return res.RowsAffected, res.Error
}

// SetCookie writes an httpOnly, SameSite=Lax cookie with Path=/. maxAge < 0
// deletes it. Lax (not Strict) is required: the IdP redirect back to us is a
// cross-site top-level navigation and must carry the cookies.
func SetCookie(w http.ResponseWriter, name, value string, maxAge int, secure bool) {
	setCookiePath(w, name, value, "/", maxAge, secure)
}

func setCookiePath(w http.ResponseWriter, name, value, path string, maxAge int, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: path, MaxAge: maxAge,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

package dbmodels

import "time"

// Auth methods recorded on a Session.
const (
	AuthMethodSecret = "secret"
	AuthMethodOIDC   = "oidc"
)

// Session is a server-side login session (BFF). ID is the hex SHA-256 of the
// random cookie value, so a database leak does not yield usable cookies. OIDC
// access / refresh tokens are never stored; only the minimal facts below.
type Session struct {
	ID         string `gorm:"primaryKey;size:64"`
	UserID     string `gorm:"size:64;not null;index"`
	AuthMethod string `gorm:"size:16;not null"`
	// AuthTime is when the user last authenticated (OIDC auth_time or login time).
	AuthTime  time.Time
	CreatedAt time.Time
	ExpiresAt time.Time `gorm:"not null;index"`
	// ReauthUntil, when in the future, marks a recent re-authentication that
	// permits dangerous operations (DESIGN 8, 5 minutes).
	ReauthUntil *time.Time
}

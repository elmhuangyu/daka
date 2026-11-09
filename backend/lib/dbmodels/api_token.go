package dbmodels

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// ErrTokenExpiryRequired is returned when an ApiToken is saved without expires_at.
var ErrTokenExpiryRequired = errors.New("dbmodels: api token expires_at is required")

// NeverExpires is the sentinel timestamp for tokens that never expire.
var NeverExpires = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)

// ApiToken is a hashed bearer/feed token. Only the hash is stored; the
// plaintext is shown once at creation.
type ApiToken struct {
	ID        uint   `gorm:"primaryKey"`
	UserID    string `gorm:"size:64;not null;index"`
	TokenHash string `gorm:"size:128;not null;uniqueIndex"`
	Name      string `gorm:"size:255;not null"`
	// Scopes is a comma-separated list of Scope values.
	Scopes  string  `gorm:"size:64;not null"`
	Privacy Privacy `gorm:"size:32;not null;default:exclude_sensitive"`
	// ExpiresAt is mandatory; the upper bound per scope is enforced by the
	// service layer. Tokens that never expire use NeverExpires.
	ExpiresAt       time.Time `gorm:"not null"`
	IncludeNegative bool      `gorm:"not null;default:false"`
	CategoryID      *uint     `gorm:"index"`
	RevokedAt       *time.Time
	LastUsedAt      *time.Time
}

// IsNeverExpires reports whether the token was configured to never expire.
func (t *ApiToken) IsNeverExpires() bool {
	return t.ExpiresAt.Year() >= 9000
}

// BeforeSave rejects a zero expires_at (a zero time.Time would otherwise be
// stored as a valid timestamp and silently satisfy NOT NULL).
func (t *ApiToken) BeforeSave(*gorm.DB) error {
	if t.ExpiresAt.IsZero() {
		return ErrTokenExpiryRequired
	}
	return nil
}

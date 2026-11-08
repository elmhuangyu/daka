package dbmodels

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// ErrTokenExpiryRequired is returned when an ApiToken is saved without expires_at.
var ErrTokenExpiryRequired = errors.New("dbmodels: api token expires_at is required")

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
	// service layer.
	ExpiresAt       time.Time `gorm:"not null"`
	IncludeNegative bool      `gorm:"not null;default:false"`
	RevokedAt       *time.Time
	LastUsedAt      *time.Time
}

// BeforeSave rejects a zero expires_at (a zero time.Time would otherwise be
// stored as a valid timestamp and silently satisfy NOT NULL).
func (t *ApiToken) BeforeSave(*gorm.DB) error {
	if t.ExpiresAt.IsZero() {
		return ErrTokenExpiryRequired
	}
	return nil
}

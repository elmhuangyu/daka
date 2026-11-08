package dbmodels

import "time"

// User is an account. ID is an opaque string (e.g. OIDC subject).
type User struct {
	ID        string `gorm:"primaryKey;size:64"`
	Email     string `gorm:"size:255;index"`
	Name      string `gorm:"size:255"`
	AvatarURL string `gorm:"size:1024"`
	CreatedAt time.Time
}

// UserSettings is the one-to-one per-user configuration.
type UserSettings struct {
	UserID        string        `gorm:"primaryKey;size:64"`
	HolidayRegion HolidayRegion `gorm:"size:8"`
	// Timezone is an IANA name; "today" and checkin_date are interpreted in it.
	Timezone string `gorm:"size:64;not null;default:UTC"`
	// WeekStart: 0 = Sunday, 1 = Monday.
	WeekStart int `gorm:"not null;default:1"`
	// StateVersion is bumped on freeze / settings changes (user-level cache version).
	StateVersion int64 `gorm:"not null;default:0"`
	// ExternalCalendarURLs is a JSON array, stored encrypted by the service layer.
	ExternalCalendarURLs string `gorm:"type:text"`
	StreakHolidayFreeze  bool   `gorm:"not null;default:false"`
}

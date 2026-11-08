package dbmodels

import "time"

// CheckinLog is one log entry. There is intentionally NO unique index on
// (item, date): set-mode uniqueness is enforced by the application layer;
// counter/metric/negative items append many rows per day.
type CheckinLog struct {
	ID          uint      `gorm:"primaryKey"`
	UserID      string    `gorm:"size:64;not null;index"`
	ItemID      uint      `gorm:"not null;index:idx_checkin_item_date"`
	CheckinDate string    `gorm:"size:10;not null;index:idx_checkin_item_date"`
	CheckinTime time.Time `gorm:"not null"`
	OptionID    *uint     `gorm:"index"`
	Value       float64
	Note        string        `gorm:"type:text"`
	Source      CheckinSource `gorm:"size:16"`
	ExternalRef *string       `gorm:"size:255"`
	// IsSupplement is set by the backend: checkin_date is before the user's local today.
	IsSupplement bool `gorm:"not null;default:false"`
}

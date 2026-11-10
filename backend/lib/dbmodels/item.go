package dbmodels

// Item is the display/definition part of a trackable thing. Scheduling and
// targets deliberately do NOT live here: they are immutable ItemRuleVersion rows
// (current rule = latest effective_from <= date). last_completed_at and
// next_due_at are derived from CheckinLog and never stored.
type Item struct {
	ID          uint   `gorm:"primaryKey"`
	UserID      string `gorm:"size:64;not null;index"`
	CategoryID  *uint  `gorm:"index"`
	ProjectID   *uint  `gorm:"index"`
	Name        string `gorm:"size:255;not null"`
	Description string `gorm:"type:text"`

	TrackerType TrackerType `gorm:"size:16;not null"`
	Unit        string      `gorm:"size:32"`
	Aggregate   Aggregate   `gorm:"size:16"`
	Step        float64
	MinVal      *float64
	MaxVal      *float64
	WarningDays int

	Kind     Kind     `gorm:"size:16;not null;default:habit"`
	Polarity Polarity `gorm:"size:16;not null;default:positive"`
	Color    string   `gorm:"size:32"`

	Pinned      bool `gorm:"not null;default:false"`
	IsSensitive bool `gorm:"not null;default:false"`
	// RemindTime is nullable "HH:MM"; display-only, not versioned.
	RemindTime *string `gorm:"size:5"`
	SortOrder  int     `gorm:"not null;default:0"`
	Archived   bool    `gorm:"not null;default:false"`
}

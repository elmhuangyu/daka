package dbmodels

// FreezePeriod is a vacation/sick-leave window during which missed days do not
// break a streak. Dates are local "YYYY-MM-DD" strings (see enums.go),
// inclusive on both ends.
type FreezePeriod struct {
	ID        uint   `gorm:"primaryKey"`
	UserID    string `gorm:"size:64;not null;index:idx_freeze_user_range"`
	StartDate string `gorm:"size:10;not null;index:idx_freeze_user_range"`
	EndDate   string `gorm:"size:10;not null;index:idx_freeze_user_range"`
	Reason    string `gorm:"size:255"`
}

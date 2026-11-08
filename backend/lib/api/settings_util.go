package api

import (
	"errors"

	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
)

// userSettings loads the user's settings, applying defaults (UTC, week starts
// Monday) when the row is missing or a column is empty.
func userSettings(tx *gorm.DB, userID string) (dbmodels.UserSettings, error) {
	st := dbmodels.UserSettings{UserID: userID, Timezone: "UTC", WeekStart: 1}
	var row dbmodels.UserSettings
	err := tx.Where("user_id = ?", userID).Take(&row).Error
	switch {
	case err == nil:
		st = row
		if st.Timezone == "" {
			st.Timezone = "UTC"
		}
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return st, err
	}
	return st, nil
}

// calendarFor returns the holiday calendar for the user's region; nil when no
// calendar is configured or the region has no data.
func (s *server) calendarFor(st dbmodels.UserSettings) schedule.DayCalendar {
	if s.calendar == nil {
		return nil
	}
	return s.calendar(st.HolidayRegion)
}

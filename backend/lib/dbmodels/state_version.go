package dbmodels

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BumpStateVersion atomically increments UserSettings.state_version of the
// user (freeze and settings changes call it so derived-value caches miss). A
// missing settings row is created with the column defaults first.
func BumpStateVersion(tx *gorm.DB, userID string) error {
	bump := func() (int64, error) {
		r := tx.Model(&UserSettings{}).Where("user_id = ?", userID).
			UpdateColumn("state_version", gorm.Expr("state_version + 1"))
		return r.RowsAffected, r.Error
	}
	n, err := bump()
	if err != nil || n > 0 {
		return err
	}
	st := UserSettings{UserID: userID, Timezone: "UTC", WeekStart: 1}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&st).Error; err != nil {
		return err
	}
	_, err = bump()
	return err
}

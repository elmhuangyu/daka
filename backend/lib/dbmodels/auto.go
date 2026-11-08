package dbmodels

import "gorm.io/gorm"

// AutoMigrate creates or updates the schema. It is idempotent.
// ItemOption, FreezePeriod, ApiToken and IdempotencyKey arrive with P1-1.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&User{},
		&UserSettings{},
		&Category{},
		&Item{},
		&ItemRuleVersion{},
		&CheckinLog{},
	)
}

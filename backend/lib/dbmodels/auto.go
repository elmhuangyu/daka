package dbmodels

import "gorm.io/gorm"

// AutoMigrate creates or updates the schema. It is idempotent.
//
// Convention: no database foreign keys are declared. Cascades (deleting an
// item removes its options, rule versions and logs; deleting a user removes
// tokens, freezes, ...) are performed in the service layer, which keeps SQLite
// and PostgreSQL behaviour identical. The Telegram tables arrive with P3-6.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(AllModels()...)
}

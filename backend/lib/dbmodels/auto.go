package dbmodels

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// AutoMigrate brings the database up to date at startup. It is idempotent.
//
// Step one syncs the schema from the models (GORM AutoMigrate: creates tables
// and adds columns or indexes, never drops anything). Step two applies the
// versioned data migrations in dataMigrations, each exactly once, recorded in
// the "migrations" table.
//
// Convention: no database foreign keys are declared. Cascades (deleting an
// item removes its options, rule versions and logs; deleting a user removes
// tokens, freezes, ...) are performed in the service layer, which keeps SQLite
// and PostgreSQL behaviour identical.
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(AllModels()...); err != nil {
		return err
	}
	return gormigrate.New(db, gormigrate.DefaultOptions, dataMigrations()).Migrate()
}

// dataMigrations lists one-off data changes that AutoMigrate cannot express.
// Append new entries; never edit or reorder shipped ones. Each ID is unique and
// is stored once applied, so a migration only ever runs once per database.
func dataMigrations() []*gormigrate.Migration {
	return []*gormigrate.Migration{
		{
			// Kind value "habit" was renamed to "ongoing".
			ID: "20261112_item_kind_ongoing",
			Migrate: func(tx *gorm.DB) error {
				return tx.Model(&Item{}).Where("kind = ?", "habit").Update("kind", KindOngoing).Error
			},
		},
		{
			// Items now follow their project's category; backfill older ones that have none.
			ID: "20261112_item_category_from_project",
			Migrate: func(tx *gorm.DB) error {
				return tx.Exec(`UPDATE items SET category_id = (SELECT p.category_id FROM projects p WHERE p.id = items.project_id)
WHERE category_id IS NULL AND project_id IS NOT NULL
AND EXISTS (SELECT 1 FROM projects p WHERE p.id = items.project_id AND p.category_id IS NOT NULL)`).Error
			},
		},
	}
}

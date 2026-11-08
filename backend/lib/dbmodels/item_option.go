package dbmodels

// ItemOption is one selectable option of an Item (e.g. "running" in km).
// Targets are NOT stored here: they live in ItemRuleVersion.option_targets so
// that changing a target never rewrites history.
type ItemOption struct {
	ID        uint   `gorm:"primaryKey"`
	ItemID    uint   `gorm:"not null;index"`
	Name      string `gorm:"size:255;not null"`
	Unit      string `gorm:"size:32"`
	Step      float64
	MinVal    *float64
	MaxVal    *float64
	Aggregate Aggregate `gorm:"size:16"`
	SortOrder int       `gorm:"not null;default:0"`
	Archived  bool      `gorm:"not null;default:false"`
}

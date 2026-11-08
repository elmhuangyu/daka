package dbmodels

type Category struct {
	ID          uint   `gorm:"primaryKey"`
	UserID      string `gorm:"size:64;not null;index"`
	Name        string `gorm:"size:255;not null"`
	Color       string `gorm:"size:32"`
	Icon        string `gorm:"size:64"`
	IsSensitive bool   `gorm:"not null;default:false"`
	SortOrder   int    `gorm:"not null;default:0"`
}

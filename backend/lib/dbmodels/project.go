package dbmodels

import "time"

type ProjectStatus string

const (
	ProjectStatusActive    ProjectStatus = "active"
	ProjectStatusCompleted ProjectStatus = "completed"
	ProjectStatusArchived  ProjectStatus = "archived"
)

type Project struct {
	ID          uint          `gorm:"primaryKey"`
	UserID      string        `gorm:"size:64;not null;index"`
	CategoryID  *uint         `gorm:"index"`
	Name        string        `gorm:"size:255;not null"`
	Description string        `gorm:"type:text;not null;default:''"`
	Color       string        `gorm:"size:32;not null;default:''"`
	Emoji       string        `gorm:"size:32;not null;default:''"` // optional user-chosen icon
	Status      ProjectStatus `gorm:"size:32;not null;default:'active'"`
	TargetDate  *string       `gorm:"size:10"` // YYYY-MM-DD
	IsSensitive bool          `gorm:"not null;default:false"`
	SortOrder   int           `gorm:"not null;default:0"`
	CreatedAt   time.Time     `gorm:"not null"`
	UpdatedAt   time.Time     `gorm:"not null"`
}

func (Project) TableName() string { return "projects" }

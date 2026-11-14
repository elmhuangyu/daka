package dbmodels

import "time"

// ProjectShare grants a non-owner user view & edit access to a Project and its
// items. CategoryID is the collaborator's own personal category assignment for
// the shared project (independent of the owner's Project.CategoryID).
type ProjectShare struct {
	ID         uint      `gorm:"primaryKey"`
	ProjectID  uint      `gorm:"not null;uniqueIndex:idx_project_share_project_user"`
	UserID     string    `gorm:"size:64;not null;uniqueIndex:idx_project_share_project_user;index"`
	CategoryID *uint     `gorm:"index"`
	CreatedAt  time.Time `gorm:"not null"`
}

func (ProjectShare) TableName() string { return "project_shares" }

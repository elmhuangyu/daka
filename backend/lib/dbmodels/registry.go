package dbmodels

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// AllModels lists every table model, in migration order.
func AllModels() []any {
	return []any{
		&User{},
		&UserSettings{},
		&Category{},
		&Item{},
		&ItemOption{},
		&ItemRuleVersion{},
		&CheckinLog{},
		&FreezePeriod{},
		&ApiToken{},
		&IdempotencyKey{},
		&Session{},
	}
}

// UserOwnedTables lists every model that has a UserID column, so that
// `daka user claim` migrates all of them. A reflection test fails when a model
// with a UserID field is missing here; add new tables to both lists.
func UserOwnedTables() []any {
	return []any{
		&UserSettings{},
		&Category{},
		&Item{},
		&CheckinLog{},
		&FreezePeriod{},
		&ApiToken{},
		&IdempotencyKey{},
		&Session{},
	}
}

// ClaimUser moves all data of user from to user to inside one transaction
// (`daka user claim`). The target user must already exist (it has logged in
// once) and must not own any data yet; its freshly created default settings and
// existing sessions do not count. The source's sessions are deleted rather than
// moved, so old shared-secret cookies never become logins of the target.
func ClaimUser(g *gorm.DB, from, to string) error {
	if from == "" || to == "" {
		return errors.New("both source and target user are required")
	}
	if from == to {
		return errors.New("source and target user must differ")
	}
	return g.Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&User{}).Where("id = ?", to).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("target user %q does not exist; log in with it once first", to)
		}
		for _, m := range UserOwnedTables() {
			switch m.(type) {
			case *UserSettings, *Session:
				continue
			}
			if err := tx.Model(m).Where("user_id = ?", to).Count(&n).Error; err != nil {
				return err
			}
			if n > 0 {
				return fmt.Errorf("target user %q already has data (%T); refusing to merge", to, m)
			}
		}
		if err := tx.Where("user_id = ?", from).Delete(&Session{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&UserSettings{}).Where("user_id = ?", from).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 { // the source settings replace the target's untouched defaults
			if err := tx.Where("user_id = ?", to).Delete(&UserSettings{}).Error; err != nil {
				return err
			}
		}
		for _, m := range UserOwnedTables() {
			if _, ok := m.(*Session); ok {
				continue
			}
			if err := tx.Session(&gorm.Session{SkipHooks: true}).Model(m).Where("user_id = ?", from).UpdateColumn("user_id", to).Error; err != nil {
				return fmt.Errorf("move %T: %w", m, err)
			}
		}
		return nil
	})
}

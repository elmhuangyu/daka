package dbmodels

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ItemRuleVersion is an immutable schedule/target version of an Item.
// Dates are local "YYYY-MM-DD" strings (see enums.go).
type ItemRuleVersion struct {
	ID uint `gorm:"primaryKey"`
	// (item_id, effective_from) is deliberately NOT unique: several edits on
	// the same day each append an immutable version; the highest id wins.
	ItemID        uint   `gorm:"not null;index:idx_rule_item_from"`
	EffectiveFrom string `gorm:"size:10;not null;index:idx_rule_item_from"`

	ScheduleType ScheduleType `gorm:"size:16;not null"`
	DtStart      string       `gorm:"column:dtstart;size:10"`
	Until        *string      `gorm:"column:until;size:10"`
	RRuleString  string       `gorm:"column:rrule_string;type:text"`
	HolidayMode  HolidayMode  `gorm:"size:16;not null;default:none"`

	RollingInterval int
	RollingUnit     RollingUnit `gorm:"size:8"`

	QuotaComparator QuotaComparator `gorm:"size:16"`
	QuotaCount      float64
	QuotaPeriod     QuotaPeriod  `gorm:"size:8"`
	QuotaWindow     QuotaWindow  `gorm:"size:16"`
	QuotaMeasure    QuotaMeasure `gorm:"size:16"`
	QuotaOptionID   *uint

	TargetValue    *float64
	CompletionRule CompletionRule `gorm:"size:16"`
	CompletionN    int
	StaleAfterDays int
	// OptionTargets is a JSON {option_id: target} snapshot.
	OptionTargets string `gorm:"type:text"`
}

// ErrNoRuleVersion means no version is effective on the requested date.
var ErrNoRuleVersion = errors.New("dbmodels: no rule version effective on date")

// CurrentRuleVersion returns the rule of itemID in effect on date
// ("YYYY-MM-DD"): the version with the greatest effective_from <= date (ties broken by highest id).
// It returns ErrNoRuleVersion when date precedes the first version.
func CurrentRuleVersion(db *gorm.DB, itemID uint, date string) (*ItemRuleVersion, error) {
	if _, perr := time.Parse(DateLayout, date); perr != nil {
		return nil, fmt.Errorf("dbmodels: invalid date %q: %w", date, perr)
	}
	var v ItemRuleVersion
	err := db.Where("item_id = ? AND effective_from <= ?", itemID, date).
		Order("effective_from DESC").Order("id DESC").Limit(1).Take(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNoRuleVersion
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

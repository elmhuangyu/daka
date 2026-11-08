package schedule

import (
	"time"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

// Rolling / maintenance evaluation (DESIGN 3.4.3, 5.1).
//
// All functions here are pure and work on local "YYYY-MM-DD" strings; callers
// pass local dates, no timezone logic lives here.
//
// Streak is NOT applicable to rolling or maintenance items (findings A5,
// DESIGN section 2): they never produce a streak, so there is deliberately no
// streak function in this file.
//
// next_due is always derived from the completion-date list, never stored, so
// undo (delete the latest log) and backfill (add an older log) are handled by
// simply recomputing from the current list.

// MaxRollingInterval is the largest accepted rolling_interval.
const MaxRollingInterval = 1000

// Maintenance states.
const (
	StateOK      = "ok"
	StateWarning = "warning"
	StateOverdue = "overdue"
)

// Status is the maintenance progress of an item on a given day.
type Status struct {
	State        string // StateOK, StateWarning or StateOverdue
	DaysUntilDue int    // days from today to next_due; 0 when due today or overdue
	OverdueDays  int    // days past next_due; 0 unless State is overdue
}

// ValidateRolling checks rolling_interval (1..MaxRollingInterval) and
// rolling_unit (day|month|year).
func ValidateRolling(v dbmodels.ItemRuleVersion) error {
	if v.RollingInterval < 1 || v.RollingInterval > MaxRollingInterval {
		return invalidf("rolling_interval must be between 1 and %d", MaxRollingInterval)
	}
	switch v.RollingUnit {
	case dbmodels.RollingDay, dbmodels.RollingMonth, dbmodels.RollingYear:
		return nil
	}
	return invalidf("rolling_unit %q must be day, month or year", v.RollingUnit)
}

// addInterval adds n units to t, clamping to the end of the target month for
// month/year units (01-31 + 1 month = 02-28/29; 02-29 + 1 year = 02-28).
func addInterval(t time.Time, n int, unit dbmodels.RollingUnit) time.Time {
	if unit == dbmodels.RollingDay {
		return t.AddDate(0, 0, n)
	}
	months := n
	if unit == dbmodels.RollingYear {
		months = n * 12
	}
	y, m, d := t.Date()
	first := time.Date(y, m+time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	if d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, 0, 0, 0, 0, time.UTC)
}

// NextDue returns the single predicted next due date of a rolling item:
// latest completion date + rolling interval. With no completions the start
// point (dtstart, or effective_from when empty) is treated as the last
// completion, so next_due = start + interval.
// completionDates may be unsorted and contain duplicates; only the latest
// matters, so deleting the latest log falls back to the previous one and a
// back-dated log earlier than the latest changes nothing.
func NextDue(version dbmodels.ItemRuleVersion, completionDates []string) (string, error) {
	if err := ValidateRolling(version); err != nil {
		return "", err
	}
	var latest time.Time
	for _, s := range completionDates {
		t, err := parseDate("completion date", s)
		if err != nil {
			return "", err
		}
		if t.After(latest) {
			latest = t
		}
	}
	if latest.IsZero() {
		start, err := parseDate("effective_from", version.EffectiveFrom)
		if err != nil {
			return "", err
		}
		if version.DtStart != "" {
			if start, err = parseDate("dtstart", version.DtStart); err != nil {
				return "", err
			}
		}
		latest = start
	}
	return addInterval(latest, version.RollingInterval, version.RollingUnit).Format(dbmodels.DateLayout), nil
}

// MaintenanceStatus classifies today against nextDue: overdue only when
// today > nextDue; warning when today >= nextDue - warningDays (so the due
// day itself is warning); otherwise ok. warningDays must be >= 0.
func MaintenanceStatus(nextDue, today string, warningDays int) (Status, error) {
	if warningDays < 0 {
		return Status{}, invalidf("warning_days must be >= 0")
	}
	due, err := parseDate("next_due", nextDue)
	if err != nil {
		return Status{}, err
	}
	now, err := parseDate("today", today)
	if err != nil {
		return Status{}, err
	}
	days := int(due.Sub(now) / (24 * time.Hour))
	switch {
	case days < 0:
		return Status{State: StateOverdue, OverdueDays: -days}, nil
	case days <= warningDays:
		return Status{State: StateWarning, DaysUntilDue: days}, nil
	}
	return Status{State: StateOK, DaysUntilDue: days}, nil
}

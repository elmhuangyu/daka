package schedule

import (
	"strings"
	"time"

	"github.com/teambition/rrule-go"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

// DayCalendar is the holiday knowledge the schedule engine needs. The
// holiday package's Calendar satisfies it; tests may use a fake. A nil
// DayCalendar disables holiday handling: every holiday_mode then behaves as
// "none".
type DayCalendar interface {
	// IsStatutoryHoliday reports a listed statutory day off.
	IsStatutoryHoliday(date string) bool
	// IsWorkday reports a working day: a normal weekday or a makeup workday,
	// and not a weekend or statutory holiday.
	IsWorkday(date string) bool
	// IsMakeupWorkday reports a weekend day that is a working day by decree.
	IsMakeupWorkday(date string) bool
}

// maxPostpone bounds how far a workdays_only postponement may move an
// occurrence; it is also the look-back margin when expanding a window, so a
// postponed occurrence whose RRULE date lies before the window is found.
const maxPostpone = 60

// validHolidayMode reports whether m is a known mode (empty means none).
func validHolidayMode(m dbmodels.HolidayMode) bool {
	switch m {
	case "", dbmodels.HolidayNone, dbmodels.HolidaySkipHolidays, dbmodels.HolidayWorkdaysOnly:
		return true
	}
	return false
}

// ruleClass describes how workdays_only treats an RRULE.
type ruleClass struct {
	postpone  bool // MONTHLY / YEARLY: postpone to the next workday instead of dropping
	addMakeup bool // Mon-Fri weekly rule: makeup weekend workdays are added (DESIGN 3.4.1 rule 1)
}

func classify(rruleString string) ruleClass {
	s := strings.TrimPrefix(strings.TrimSpace(rruleString), "RRULE:")
	opt, err := rrule.StrToROption(s)
	if err != nil {
		return ruleClass{}
	}
	narrowed := len(opt.Bymonthday)+len(opt.Byyearday)+len(opt.Byweekno)+len(opt.Bysetpos)+len(opt.Bymonth) > 0
	switch opt.Freq {
	case rrule.MONTHLY, rrule.YEARLY:
		return ruleClass{postpone: true}
	case rrule.WEEKLY:
		if opt.Interval <= 1 && !narrowed && isMonToFri(opt.Byweekday) {
			return ruleClass{addMakeup: true}
		}
	}
	return ruleClass{}
}

// isMonToFri reports whether days is exactly {MO,TU,WE,TH,FR}, ignoring order
// and duplicates, with no ordinals.
func isMonToFri(days []rrule.Weekday) bool {
	var seen [7]bool
	for _, d := range days {
		if d.N() != 0 {
			return false
		}
		seen[d.Day()] = true
	}
	return seen == [7]bool{true, true, true, true, true, false, false}
}

// applyHolidayMode turns the unfiltered RRULE occurrences raw into the due
// dates inside [lo, hi]. raw must have been expanded from at least maxPostpone
// days before lo (clamped to dtstart / the span start) so occurrences that are
// postponed into the window are present. hi must already be capped at the
// span end and UNTIL: postponed dates beyond it are dropped. Filtering never
// alters raw, so INTERVAL phase is unaffected. The result may be unsorted and
// contain duplicates; the caller sorts and dedupes.
func applyHolidayMode(v dbmodels.ItemRuleVersion, cal DayCalendar, r *rrule.RRule, raw []time.Time, lo, hi time.Time) []string {
	mode := v.HolidayMode
	if cal == nil || mode == "" || mode == dbmodels.HolidayNone {
		return inWindow(raw, lo, hi, func(string) bool { return true })
	}
	if mode == dbmodels.HolidaySkipHolidays {
		return inWindow(raw, lo, hi, func(d string) bool { return !cal.IsStatutoryHoliday(d) })
	}
	cls := classify(v.RRuleString)
	if cls.postpone {
		var out []string
		for _, t := range raw {
			if !cal.IsWorkday(t.Format(dbmodels.DateLayout)) {
				var ok bool
				if t, ok = nextWorkday(cal, t); !ok {
					continue
				}
			}
			if t.Before(lo) || t.After(hi) {
				continue
			}
			out = append(out, t.Format(dbmodels.DateLayout))
		}
		return out
	}
	out := inWindow(raw, lo, hi, cal.IsWorkday)
	if cls.addMakeup {
		out = append(out, makeupDays(cal, r, lo, hi)...)
	}
	return out
}

// makeupDays returns the makeup workdays in [lo, hi] that an every-workday
// Mon-Fri weekly rule does not itself produce because they fall on a weekend.
// FREQ=DAILY needs no addition: weekends are already candidates and survive
// only on makeup days, with INTERVAL phase respected. With
// COUNT the rule ends after its last occurrence and so do makeup days.
func makeupDays(cal DayCalendar, r *rrule.RRule, lo, hi time.Time) []string {
	var out []string
	var last time.Time
	if r.OrigOptions.Count > 0 {
		if all := r.All(); len(all) > 0 {
			last = all[len(all)-1]
		}
	}
	for d := lo; !d.After(hi); d = d.AddDate(0, 0, 1) {
		if wd := d.Weekday(); wd != time.Saturday && wd != time.Sunday {
			continue
		}
		if r.OrigOptions.Count > 0 && (last.IsZero() || d.After(last)) {
			continue
		}
		s := d.Format(dbmodels.DateLayout)
		if cal.IsMakeupWorkday(s) {
			out = append(out, s)
		}
	}
	return out
}

func inWindow(raw []time.Time, lo, hi time.Time, keep func(string) bool) []string {
	var out []string
	for _, t := range raw {
		if t.Before(lo) || t.After(hi) {
			continue
		}
		if d := t.Format(dbmodels.DateLayout); keep(d) {
			out = append(out, d)
		}
	}
	return out
}

// nextWorkday returns the first workday on or after t, within maxPostpone.
func nextWorkday(cal DayCalendar, t time.Time) (time.Time, bool) {
	for i := 0; i <= maxPostpone; i++ {
		c := t.AddDate(0, 0, i)
		if cal.IsWorkday(c.Format(dbmodels.DateLayout)) {
			return c, true
		}
	}
	return time.Time{}, false
}

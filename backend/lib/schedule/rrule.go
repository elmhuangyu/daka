package schedule

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teambition/rrule-go"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

// ErrUnsupported is returned for schedule types the engine cannot expand
// into due dates yet (rolling and quota are implemented in later phases).
var ErrUnsupported = errors.New("schedule: unsupported schedule type")

// ErrInvalid wraps every validation failure (bad RRULE, malformed date,
// unknown timezone); API handlers can map it to HTTP 400.
var ErrInvalid = errors.New("schedule: invalid input")

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// parseDate parses a "YYYY-MM-DD" date as a UTC-midnight time. All
// evaluation happens in UTC with date-only values (RRULE_EVALUATION.md).
func parseDate(field, s string) (time.Time, error) {
	t, err := time.ParseInLocation(dbmodels.DateLayout, s, time.UTC)
	if err != nil {
		return time.Time{}, invalidf("%s %q is not a YYYY-MM-DD date", field, s)
	}
	if t.Before(minDate) {
		return time.Time{}, invalidf("%s %q is before 1900-01-01", field, s)
	}
	return t, nil
}

// minDate is the earliest accepted date: rrule-go cost grows with the years
// since dtstart and very old dates evaluate incorrectly.
var minDate = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)

// CurrentVersion returns the version governing date: the greatest
// effective_from <= date, ties broken by the highest id. Nil when none.
func CurrentVersion(versions []dbmodels.ItemRuleVersion, date string) *dbmodels.ItemRuleVersion {
	var cur *dbmodels.ItemRuleVersion
	for i := range versions {
		v := &versions[i]
		if v.EffectiveFrom > date {
			continue
		}
		if cur == nil || v.EffectiveFrom > cur.EffectiveFrom || (v.EffectiveFrom == cur.EffectiveFrom && v.ID > cur.ID) {
			cur = v
		}
	}
	return cur
}

// Today returns the current local calendar date ("YYYY-MM-DD") in the given
// IANA timezone.
func Today(now time.Time, tz string) (string, error) {
	if tz == "" {
		return "", invalidf("empty timezone")
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return "", invalidf("unknown timezone %q", tz)
	}
	return now.In(loc).Format(dbmodels.DateLayout), nil
}

// buildRule turns a fixed_rrule version into an rrule.RRule with an explicit
// UTC date-only dtstart (defaulting to effective_from) and the version's
// until (nil when unbounded by the version).
func buildRule(v dbmodels.ItemRuleVersion) (*rrule.RRule, time.Time, *time.Time, error) {
	eff, err := parseDate("effective_from", v.EffectiveFrom)
	if err != nil {
		return nil, time.Time{}, nil, err
	}
	start := eff
	if v.DtStart != "" {
		if start, err = parseDate("dtstart", v.DtStart); err != nil {
			return nil, time.Time{}, nil, err
		}
	}
	var until *time.Time
	if v.Until != nil && *v.Until != "" {
		u, err := parseDate("until", *v.Until)
		if err != nil {
			return nil, time.Time{}, nil, err
		}
		if u.Before(start) {
			return nil, time.Time{}, nil, invalidf("until %s is before dtstart %s", *v.Until, start.Format(dbmodels.DateLayout))
		}
		until = &u
	}
	s := strings.TrimPrefix(strings.TrimSpace(v.RRuleString), "RRULE:")
	if s == "" {
		return nil, time.Time{}, nil, invalidf("empty rrule_string")
	}
	if err := checkRuleString(s, start); err != nil {
		return nil, time.Time{}, nil, err
	}
	opt, err := rrule.StrToROption(s)
	if err != nil {
		return nil, time.Time{}, nil, invalidf("rrule %q: %v", v.RRuleString, err)
	}
	opt.Dtstart = start
	if until != nil {
		opt.Until = *until
	} else if !opt.Until.IsZero() {
		y, m, d := opt.Until.UTC().Date()
		opt.Until = time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	r, err := rrule.NewRRule(*opt)
	if err != nil {
		return nil, time.Time{}, nil, invalidf("rrule %q: %v", v.RRuleString, err)
	}
	return r, start, until, nil
}

// MaxRRuleLen caps rrule_string length (user input).
const MaxRRuleLen = 500

const maxInterval = 1000

// allowedParts are the RRULE parts a user rule may use. Sub-day parts
// (BYHOUR, BYMINUTE, BYSECOND) and embedded DTSTART/TZID are rejected: rules
// are date-only and sub-day parts multiply the expansion cost (CPU DoS, G10).
var allowedParts = map[string]bool{
	"FREQ": true, "INTERVAL": true, "COUNT": true, "UNTIL": true, "WKST": true,
	"BYDAY": true, "BYMONTHDAY": true, "BYMONTH": true, "BYYEARDAY": true,
	"BYWEEKNO": true, "BYSETPOS": true,
}

var allowedFreqs = map[string]bool{"DAILY": true, "WEEKLY": true, "MONTHLY": true, "YEARLY": true}

// checkRuleString applies the user-input hardening checks (findings G10) to
// an RRULE body (without the "RRULE:" prefix) before it reaches rrule-go.
func checkRuleString(s string, start time.Time) error {
	if len(s) > MaxRRuleLen {
		return invalidf("rrule_string longer than %d characters", MaxRRuleLen)
	}
	seen := map[string]bool{}
	for _, part := range strings.Split(s, ";") {
		k, val, ok := strings.Cut(part, "=")
		k = strings.ToUpper(strings.TrimSpace(k))
		if !ok || !allowedParts[k] {
			return invalidf("unsupported rrule part %q", k)
		}
		if seen[k] {
			return invalidf("duplicate rrule part %q", k)
		}
		seen[k] = true
		val = strings.TrimSpace(val)
		switch k {
		case "FREQ":
			if !allowedFreqs[strings.ToUpper(val)] {
				return invalidf("FREQ must be DAILY, WEEKLY, MONTHLY or YEARLY")
			}
		case "INTERVAL":
			if n, err := strconv.Atoi(val); err != nil || n < 1 || n > maxInterval {
				return invalidf("INTERVAL must be between 1 and %d", maxInterval)
			}
		case "COUNT":
			if n, err := strconv.Atoi(val); err != nil || n < 1 {
				return invalidf("COUNT must be a positive integer")
			}
		case "UNTIL":
			if len(val) < 8 {
				return invalidf("invalid UNTIL")
			}
			u, err := time.Parse("20060102", val[:8])
			if err != nil {
				return invalidf("invalid UNTIL")
			}
			if u.Before(start) {
				return invalidf("UNTIL is before dtstart")
			}
		}
	}
	if !seen["FREQ"] {
		return invalidf("rrule requires FREQ")
	}
	return nil
}

// ValidateRule checks the schedule-specific syntax of a rule version
// (RRULE, rolling interval) and rejects unknown schedule types. The full
// type x schedule matrix lives in ValidateItemRule.
func ValidateRule(v dbmodels.ItemRuleVersion) error {
	if _, err := parseDate("effective_from", v.EffectiveFrom); err != nil {
		return err
	}
	if !validHolidayMode(v.HolidayMode) {
		return invalidf("holiday_mode %q must be none, skip_holidays or workdays_only", v.HolidayMode)
	}
	switch v.ScheduleType {
	case dbmodels.ScheduleFixedRRule:
		_, _, _, err := buildRule(v)
		return err
	case dbmodels.ScheduleRolling:
		if err := ValidateRolling(v); err != nil {
			return err
		}
		if v.DtStart != "" {
			_, err := parseDate("dtstart", v.DtStart)
			return err
		}
	case dbmodels.ScheduleQuota, dbmodels.ScheduleNone:
	default:
		return invalidf("unknown schedule_type %q", v.ScheduleType)
	}
	return nil
}

// span is a version together with the inclusive date range it governs.
type span struct {
	v        dbmodels.ItemRuleVersion
	from, to time.Time // to is zero for the open-ended last span
}

// governing resolves versions into non-overlapping spans: for each
// effective_from the highest ID wins, and a version governs
// [effective_from, next effective_from - 1 day].
func governing(versions []dbmodels.ItemRuleVersion) ([]span, error) {
	best := map[string]dbmodels.ItemRuleVersion{}
	for _, v := range versions {
		if _, err := parseDate("effective_from", v.EffectiveFrom); err != nil {
			return nil, err
		}
		if cur, ok := best[v.EffectiveFrom]; !ok || v.ID > cur.ID {
			best[v.EffectiveFrom] = v
		}
	}
	keys := make([]string, 0, len(best))
	for k := range best {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	spans := make([]span, len(keys))
	for i, k := range keys {
		spans[i].v = best[k]
		spans[i].from, _ = parseDate("effective_from", k)
		if i+1 < len(keys) {
			next, _ := parseDate("effective_from", keys[i+1])
			spans[i].to = next.AddDate(0, 0, -1)
		}
	}
	return spans, nil
}

// DueDates returns the sorted, de-duplicated due dates in [from, to]
// (inclusive, "YYYY-MM-DD"). Each date is governed by the version with the
// greatest effective_from <= date, so adding a later version never changes
// earlier results. Dates before the first version have no rule and are never
// due. from > to yields an empty (non-nil) slice and no error. A governing
// rolling or quota version in the window yields ErrUnsupported; schedule type
// none has no due dates.
func DueDates(versions []dbmodels.ItemRuleVersion, from, to string) ([]string, error) {
	return DueDatesWith(versions, from, to, nil)
}

// DueDatesWith is DueDates with holiday_mode evaluation (DESIGN 3.4): the
// RRULE is expanded first, then filtered or postponed by each version's
// holiday_mode using cal, so INTERVAL phase never changes. A nil cal makes
// every mode behave as none. A workdays_only postponement of a monthly or
// yearly occurrence is dropped if it would land after the version's UNTIL or
// after the version's span end (the next version governs those days), or more
// than 60 days after the original date.
func DueDatesWith(versions []dbmodels.ItemRuleVersion, from, to string, cal DayCalendar) ([]string, error) {
	lo, err := parseDate("from", from)
	if err != nil {
		return nil, err
	}
	hi, err := parseDate("to", to)
	if err != nil {
		return nil, err
	}
	out := []string{}
	if lo.After(hi) {
		return out, nil
	}
	spans, err := governing(versions)
	if err != nil {
		return nil, err
	}
	for _, sp := range spans {
		a, b := lo, hi
		if sp.from.After(a) {
			a = sp.from
		}
		if !sp.to.IsZero() && sp.to.Before(b) {
			b = sp.to
		}
		if a.After(b) {
			continue
		}
		switch sp.v.ScheduleType {
		case dbmodels.ScheduleNone:
			continue
		case dbmodels.ScheduleFixedRRule:
		default:
			return nil, fmt.Errorf("%w: %q", ErrUnsupported, sp.v.ScheduleType)
		}
		r, start, until, err := buildRule(sp.v)
		if err != nil {
			return nil, err
		}
		if start.After(a) {
			a = start
		}
		if until != nil && until.Before(b) {
			b = *until
		}
		if a.After(b) {
			continue
		}
		// Expand a margin before a so occurrences postponed into the window
		// are found; never before dtstart.
		ra := a.AddDate(0, 0, -maxPostpone)
		if ra.Before(start) {
			ra = start
		}
		out = append(out, applyHolidayMode(sp.v, cal, r, r.Between(ra, b, true), a, b)...)
	}
	sort.Strings(out)
	return dedupe(out), nil
}

func dedupe(s []string) []string {
	w := 0
	for i, x := range s {
		if i == 0 || x != s[w-1] {
			s[w] = x
			w++
		}
	}
	return s[:w]
}

// IsDue reports whether date is a due date under the given versions.
func IsDue(versions []dbmodels.ItemRuleVersion, date string) (bool, error) {
	return IsDueWith(versions, date, nil)
}

// IsDueWith is IsDue with holiday_mode evaluation (see DueDatesWith).
func IsDueWith(versions []dbmodels.ItemRuleVersion, date string, cal DayCalendar) (bool, error) {
	ds, err := DueDatesWith(versions, date, date, cal)
	if err != nil {
		return false, err
	}
	return len(ds) > 0, nil
}

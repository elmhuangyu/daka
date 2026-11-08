package schedule

import (
	"errors"
	"fmt"
	"sort"
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
	return t, nil
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

// ValidateRule checks a rule version at creation time. Only fixed_rrule
// carries RRULE validation in Phase 0; other types are accepted as-is.
func ValidateRule(v dbmodels.ItemRuleVersion) error {
	if _, err := parseDate("effective_from", v.EffectiveFrom); err != nil {
		return err
	}
	if v.ScheduleType != dbmodels.ScheduleFixedRRule {
		return nil
	}
	_, _, _, err := buildRule(v)
	return err
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
		for _, t := range r.Between(a, b, true) {
			out = append(out, t.Format(dbmodels.DateLayout))
		}
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
	ds, err := DueDates(versions, date, date)
	if err != nil {
		return false, err
	}
	return len(ds) > 0, nil
}

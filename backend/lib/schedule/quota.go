package schedule

import (
	"errors"
	"math"
	"time"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

// Quota and "none" schedule evaluation (DESIGN 2, 2.2, 3.5, 7.1). Everything
// in this file is pure: no DB, no clock. Dates are local "YYYY-MM-DD" strings.

// ErrNoStreak is returned by QuotaStreak when the governing rule does not
// support a streak (rolling window, or not a calendar quota at all).
var ErrNoStreak = errors.New("schedule: quota has no streak")

// quotaEps absorbs float noise when comparing summed values to the limit.
const quotaEps = 1e-9

// QuotaLog is the minimal view of a check-in log needed by quota evaluation.
type QuotaLog struct {
	Date     string // local YYYY-MM-DD
	Value    float64
	OptionID *uint
}

// QuotaStatus is the status of one quota window.
type QuotaStatus struct {
	WindowStart string
	WindowEnd   string
	Comparator  dbmodels.QuotaComparator
	Used        float64
	Limit       float64
	// Remaining is Limit-Used and may be negative (over the limit).
	Remaining float64
	// Met: at_least => Used >= Limit; at_most => Used <= Limit.
	Met bool
	// Exceeded is only ever true for at_most (Used > Limit).
	Exceeded bool
	// Ratio is Used/Limit (0 when Limit is 0).
	Ratio float64
}

// Light is a traffic-light level for at_most quotas.
type Light string

const (
	LightNone   Light = ""       // not applicable (at_least quotas)
	LightGreen  Light = "green"  // < 70% of the limit used
	LightYellow Light = "yellow" // 70% <= used/limit < 100%
	LightRed    Light = "red"    // >= 100% used
)

// Period returns the inclusive calendar period [start, end] containing date.
// weekStart is the first day of the week (0=Sunday, 1=Monday, ... 6=Saturday).
func Period(date string, period dbmodels.QuotaPeriod, weekStart int) (start, end string, err error) {
	if weekStart < 0 || weekStart > 6 {
		return "", "", invalidf("week_start %d out of range 0-6", weekStart)
	}
	d, err := parseDate("date", date)
	if err != nil {
		return "", "", err
	}
	var s, e time.Time
	switch period {
	case dbmodels.QuotaWeek:
		off := (int(d.Weekday()) - weekStart + 7) % 7
		s = d.AddDate(0, 0, -off)
		e = s.AddDate(0, 0, 6)
	case dbmodels.QuotaMonth:
		s = time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
		e = s.AddDate(0, 1, -1)
	case dbmodels.QuotaYear:
		s = time.Date(d.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
		e = time.Date(d.Year(), time.December, 31, 0, 0, 0, 0, time.UTC)
	default:
		return "", "", invalidf("unknown quota_period %q", period)
	}
	return s.Format(dbmodels.DateLayout), e.Format(dbmodels.DateLayout), nil
}

// RollingDays is the length in days of a rolling window: 7 for week, 30 for
// month, 365 for year. The window is the last N days ending today inclusive,
// i.e. [today-(N-1), today].
func RollingDays(period dbmodels.QuotaPeriod) (int, error) {
	switch period {
	case dbmodels.QuotaWeek:
		return 7, nil
	case dbmodels.QuotaMonth:
		return 30, nil
	case dbmodels.QuotaYear:
		return 365, nil
	}
	return 0, invalidf("unknown quota_period %q", period)
}

// EffectiveMeasure applies the default measure: days for positive polarity,
// occurrences for negative.
func EffectiveMeasure(m dbmodels.QuotaMeasure, polarity dbmodels.Polarity) dbmodels.QuotaMeasure {
	if m != "" {
		return m
	}
	if polarity == dbmodels.PolarityNegative {
		return dbmodels.QuotaMeasureOccurrences
	}
	return dbmodels.QuotaMeasureDays
}

// measure computes the used amount of rule over the inclusive window
// [start, end]. An empty QuotaMeasure is treated as occurrences here because
// polarity is unknown at this level; callers should pass resolved rules
// (see EffectiveMeasure).
func measure(rule dbmodels.ItemRuleVersion, logs []QuotaLog, start, end string) float64 {
	days := map[string]struct{}{}
	var n, sum float64
	for _, l := range logs {
		if l.Date < start || l.Date > end {
			continue
		}
		switch rule.QuotaMeasure {
		case dbmodels.QuotaMeasureDays:
			days[l.Date] = struct{}{}
		case dbmodels.QuotaMeasureSum:
			if rule.QuotaOptionID != nil && (l.OptionID == nil || *l.OptionID != *rule.QuotaOptionID) {
				continue
			}
			sum += l.Value
		default:
			n++
		}
	}
	switch rule.QuotaMeasure {
	case dbmodels.QuotaMeasureDays:
		return float64(len(days))
	case dbmodels.QuotaMeasureSum:
		return sum
	}
	return n
}

func statusFor(rule dbmodels.ItemRuleVersion, logs []QuotaLog, start, end string) QuotaStatus {
	used := measure(rule, logs, start, end)
	limit := rule.QuotaCount
	st := QuotaStatus{
		WindowStart: start, WindowEnd: end, Comparator: rule.QuotaComparator,
		Used: used, Limit: limit, Remaining: limit - used,
	}
	if limit != 0 {
		st.Ratio = used / limit
	}
	switch rule.QuotaComparator {
	case dbmodels.QuotaAtLeast:
		st.Met = used >= limit-quotaEps
	case dbmodels.QuotaAtMost:
		st.Exceeded = used > limit+quotaEps
		st.Met = !st.Exceeded
	}
	return st
}

// EvaluateQuota evaluates the CURRENT window of rule as of today. Calendar
// window: the natural period containing today. Rolling window: the last
// N days ending today (N from RollingDays). The rule's QuotaMeasure should be
// resolved (empty is treated as occurrences; see EffectiveMeasure). Rolling
// windows carry no streak; see QuotaStreak.
func EvaluateQuota(rule dbmodels.ItemRuleVersion, logs []QuotaLog, today string, weekStart int) (QuotaStatus, error) {
	// Notes for later phases: the `days` measure for counter items needs
	// completion-aware plumbing in P1-7/P1-8 (only target-reaching days
	// qualify; QuotaLog carries no completion flag); a multi-option sum
	// requires quota_option_id, enforced at item-level validation (P1-6/P1-9).
	switch rule.QuotaWindow {
	case "", dbmodels.QuotaWindowCalendar, dbmodels.QuotaWindowRolling:
	default:
		return QuotaStatus{}, invalidf("unknown quota_window %q", rule.QuotaWindow)
	}
	var start, end string
	if rule.QuotaWindow == dbmodels.QuotaWindowRolling {
		n, err := RollingDays(rule.QuotaPeriod)
		if err != nil {
			return QuotaStatus{}, err
		}
		t, err := parseDate("today", today)
		if err != nil {
			return QuotaStatus{}, err
		}
		start, end = t.AddDate(0, 0, -(n-1)).Format(dbmodels.DateLayout), today
	} else {
		var err error
		if start, end, err = Period(today, rule.QuotaPeriod, weekStart); err != nil {
			return QuotaStatus{}, err
		}
	}
	return statusFor(rule, logs, start, end), nil
}

// Traffic maps an at_most status to a light: ratio < 0.7 green, 0.7 <= ratio
// < 1 yellow, ratio >= 1 red (so limit 4: used 0-2 green, 3 yellow, 4+ red).
// at_least statuses return LightNone.
func Traffic(st QuotaStatus) Light {
	if st.Comparator != dbmodels.QuotaAtMost {
		return LightNone
	}
	switch {
	case st.Ratio >= 1-quotaEps:
		return LightRed
	case st.Ratio >= 0.7-quotaEps:
		return LightYellow
	}
	return LightGreen
}

func isCalendarQuota(v *dbmodels.ItemRuleVersion) bool {
	return v != nil && v.ScheduleType == dbmodels.ScheduleQuota && v.QuotaWindow != dbmodels.QuotaWindowRolling
}

// QuotaStreak returns the current and longest streak, in periods, of a
// calendar quota.
//   - ErrNoStreak when the version governing today is not a calendar quota
//     (rolling windows overlap, so they have no streak).
//   - Each past period is judged with the version effective at the START of
//     that period (a rule change never rewrites ended periods). Periods that
//     start before the first version, or under a non-quota / rolling version,
//     are skipped: neither counted nor breaking.
//   - A completed period is "ok" if met (at_least: used >= limit; at_most:
//     used <= limit), otherwise it breaks the streak.
//   - The in-progress period: at_least counts only once already met, and an
//     unmet one neither counts nor breaks; at_most counts while not exceeded
//     and breaks (current becomes 0) once exceeded.
//   - Versions' measures should be resolved (see EffectiveMeasure).
func QuotaStreak(versions []dbmodels.ItemRuleVersion, logs []QuotaLog, today string, weekStart int) (current, longest int, err error) {
	if _, err = parseDate("today", today); err != nil {
		return 0, 0, err
	}
	cur := CurrentVersion(versions, today)
	if cur == nil {
		return 0, 0, nil
	}
	if !isCalendarQuota(cur) {
		return 0, 0, ErrNoStreak
	}
	first := ""
	for _, v := range versions {
		if first == "" || v.EffectiveFrom < first {
			first = v.EffectiveFrom
		}
	}
	if first < "1900-01-01" { // dates before the supported floor are skipped anyway
		first = "1900-01-01"
	}
	d := first
	run := 0
	apply := func(st QuotaStatus, inProgress bool) {
		switch {
		case st.Met:
			run++
		case inProgress && st.Comparator == dbmodels.QuotaAtLeast:
			// unmet in-progress period: neither counts nor breaks
		default:
			run = 0
		}
		if run > longest {
			longest = run
		}
	}
	for d <= today {
		v := CurrentVersion(versions, d)
		if !isCalendarQuota(v) {
			nx := nextEffective(versions, d)
			if nx == "" {
				break
			}
			d = nx
			continue
		}
		ps, pe, perr := Period(d, v.QuotaPeriod, weekStart)
		if perr != nil {
			return 0, 0, perr
		}
		// A version change to a different period type inside this period makes
		// it unjudgeable: skip to the change.
		end := pe
		if today < end {
			end = today
		}
		if sw := periodSwitch(versions, v, d, end); sw != "" {
			d = sw
			continue
		}
		if pe >= today {
			break // the open period is judged below with the version effective today
		}
		if ps == d { // otherwise the period began under another version: skip
			apply(statusFor(*v, logs, ps, pe), false)
		}
		t, perr := parseDate("date", pe)
		if perr != nil {
			return 0, 0, perr
		}
		d = t.AddDate(0, 0, 1).Format(dbmodels.DateLayout)
	}
	// The open period (containing today) is not frozen: rule edits take effect
	// today, so it is judged by the version effective today.
	ps, pe, err := Period(today, cur.QuotaPeriod, weekStart)
	if err != nil {
		return 0, 0, err
	}
	apply(statusFor(*cur, logs, ps, pe), true)
	return run, longest, nil
}

// periodSwitch returns the earliest EffectiveFrom in (d, end] whose version
// is not a calendar quota of the same period type as v, or "".
func periodSwitch(versions []dbmodels.ItemRuleVersion, v *dbmodels.ItemRuleVersion, d, end string) string {
	best := ""
	for i := range versions {
		x := &versions[i]
		if x.EffectiveFrom <= d || x.EffectiveFrom > end || (best != "" && x.EffectiveFrom >= best) {
			continue
		}
		w := CurrentVersion(versions, x.EffectiveFrom)
		if !isCalendarQuota(w) || w.QuotaPeriod != v.QuotaPeriod {
			best = x.EffectiveFrom
		}
	}
	return best
}

// nextEffective returns the smallest EffectiveFrom strictly after d, or "".
func nextEffective(versions []dbmodels.ItemRuleVersion, d string) string {
	best := ""
	for _, v := range versions {
		if v.EffectiveFrom > d && (best == "" || v.EffectiveFrom < best) {
			best = v.EffectiveFrom
		}
	}
	return best
}

// Stale is the soft-hint status of a "none" schedule item.
type Stale struct {
	Days  int  // whole days since the last occurrence (0 when never logged)
	Stale bool // true only when staleAfterDays > 0 and Days > staleAfterDays
}

// StaleStatus computes days since lastDate (empty = never logged, never
// stale) as of today.
func StaleStatus(lastDate, today string, staleAfterDays int) (Stale, error) {
	t, err := parseDate("today", today)
	if err != nil {
		return Stale{}, err
	}
	if lastDate == "" {
		return Stale{}, nil
	}
	l, err := parseDate("last_date", lastDate)
	if err != nil {
		return Stale{}, err
	}
	days := int(t.Sub(l).Hours() / 24)
	if days < 0 {
		days = 0
	}
	return Stale{Days: days, Stale: staleAfterDays > 0 && days > staleAfterDays}, nil
}

// ValidateQuota checks a quota rule against the item's tracker type and
// polarity. Empty window/measure take their defaults (calendar; days for
// positive, occurrences for negative). Failures wrap ErrInvalid.
func ValidateQuota(rule dbmodels.ItemRuleVersion, tracker dbmodels.TrackerType, polarity dbmodels.Polarity) error {
	switch rule.QuotaComparator {
	case dbmodels.QuotaAtLeast, dbmodels.QuotaAtMost:
	default:
		return invalidf("quota_comparator %q must be at_least or at_most", rule.QuotaComparator)
	}
	switch polarity {
	case dbmodels.PolarityPositive, dbmodels.PolarityNegative:
	default:
		return invalidf("unknown polarity %q", polarity)
	}
	if polarity == dbmodels.PolarityNegative && rule.QuotaComparator != dbmodels.QuotaAtMost {
		return invalidf("negative items only allow quota_comparator at_most")
	}
	if math.IsNaN(rule.QuotaCount) || math.IsInf(rule.QuotaCount, 0) || rule.QuotaCount <= 0 {
		return invalidf("quota_count must be a finite number > 0")
	}
	switch rule.QuotaPeriod {
	case dbmodels.QuotaWeek, dbmodels.QuotaMonth, dbmodels.QuotaYear:
	default:
		return invalidf("quota_period %q must be week, month or year", rule.QuotaPeriod)
	}
	switch rule.QuotaWindow {
	case "", dbmodels.QuotaWindowCalendar, dbmodels.QuotaWindowRolling:
	default:
		return invalidf("quota_window %q must be calendar or rolling", rule.QuotaWindow)
	}
	switch tracker {
	case dbmodels.TrackerBoolean, dbmodels.TrackerCounter, dbmodels.TrackerMetric:
	case dbmodels.TrackerMaintenance:
		return invalidf("maintenance items cannot use a quota schedule")
	default:
		return invalidf("unknown tracker_type %q", tracker)
	}
	if polarity == dbmodels.PolarityNegative && tracker == dbmodels.TrackerCounter {
		return invalidf("negative items cannot be counters")
	}
	switch EffectiveMeasure(rule.QuotaMeasure, polarity) {
	case dbmodels.QuotaMeasureDays, dbmodels.QuotaMeasureOccurrences:
	case dbmodels.QuotaMeasureSum:
		if tracker != dbmodels.TrackerMetric && tracker != dbmodels.TrackerCounter {
			return invalidf("quota_measure sum requires a metric or counter item")
		}
	default:
		return invalidf("quota_measure %q must be days, occurrences or sum", rule.QuotaMeasure)
	}
	return nil
}

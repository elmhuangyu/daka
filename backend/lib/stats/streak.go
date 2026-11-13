package stats

import (
	"errors"
	"sort"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
)

// Settings are the user settings that influence derived values.
type Settings struct {
	Timezone  string
	WeekStart int
	// StreakHolidayFreeze: due days on a statutory holiday never break a streak.
	StreakHolidayFreeze bool
}

// Input is everything the pure computations need. Logs are all logs of the
// item (any order); Freezes are the user's freeze periods; Calendar may be nil
// (no holiday awareness).
type Input struct {
	Item     dbmodels.Item
	Versions []dbmodels.ItemRuleVersion
	Options  []dbmodels.ItemOption
	Logs     []dbmodels.CheckinLog
	Freezes  []dbmodels.FreezePeriod
	Calendar schedule.DayCalendar
	Settings Settings
	// Today is the user's local date, "YYYY-MM-DD".
	Today string
}

// Streak units.
const (
	UnitDay    = "day"
	UnitPeriod = "period"
)

// StreakResult is the outcome of Streak.
type StreakResult struct {
	// Applicable is false when the item has no streak (rolling, maintenance,
	// none, negative daily, event, rolling quota window).
	Applicable bool   `json:"applicable"`
	Unit       string `json:"unit,omitempty"`
	Current    int    `json:"current"`
	Longest    int    `json:"longest"`
}

func logsByDate(logs []dbmodels.CheckinLog) map[string][]dbmodels.CheckinLog {
	m := map[string][]dbmodels.CheckinLog{}
	for _, l := range logs {
		m[l.CheckinDate] = append(m[l.CheckinDate], l)
	}
	return m
}

// completion evaluates one day with the rule version in effect on that day.
func completion(in Input, date string, dayLogs []dbmodels.CheckinLog) schedule.Completion {
	var v dbmodels.ItemRuleVersion
	if cur := schedule.CurrentVersion(in.Versions, date); cur != nil {
		v = *cur
	}
	return schedule.DayCompletion(in.Item.TrackerType, v, in.Options, dayLogs, in.Item.Aggregate)
}

// frozen reports whether date is covered by a FreezePeriod or, with
// streak_holiday_freeze, is a statutory holiday.
func frozen(in Input, date string) bool {
	for _, f := range in.Freezes {
		if f.StartDate <= date && date <= f.EndDate {
			return true
		}
	}
	return in.Settings.StreakHolidayFreeze && in.Calendar != nil && in.Calendar.IsStatutoryHoliday(date)
}

// Streak computes the current and longest streak (DESIGN 7.1).
//
// fixed_rrule ongoing items (positive, non-maintenance, kind ongoing): the unit is a
// DUE day. Walking the due days (holiday_mode applied via the calendar) from
// the first fixed_rrule version up to today:
//   - completed (DayCompletion with the version in effect that day): +1;
//   - not completed and frozen (FreezePeriod or, with streak_holiday_freeze,
//     a statutory holiday): skipped, neither adds nor breaks. A frozen day that
//     WAS completed still counts;
//   - today, not completed yet: skipped (it has not been missed yet);
//   - any other uncompleted past due day: breaks (run = 0).
//
// Non-due days never add or break. History is judged by the version in effect
// on each day, so rule edits never rewrite it.
//
// quota (calendar window): per-period streak via schedule.QuotaStreak. Freeze
// periods are IGNORED for quota streaks (periods are judged purely on usage).
// Rolling quota windows, rolling, maintenance, none, events and negative
// non-quota items have no streak (Applicable=false).
func Streak(in Input) (StreakResult, error) {
	cur := schedule.CurrentVersion(in.Versions, in.Today)
	if cur == nil {
		return StreakResult{}, nil
	}
	switch cur.ScheduleType {
	case dbmodels.ScheduleQuota:
		return quotaStreak(in)
	case dbmodels.ScheduleFixedRRule:
	default:
		return StreakResult{}, nil
	}
	it := in.Item
	if it.Kind != dbmodels.KindOngoing || it.Polarity != dbmodels.PolarityPositive || it.TrackerType == dbmodels.TrackerMaintenance {
		return StreakResult{}, nil
	}
	from := dailyStart(in.Versions)
	due, err := schedule.DueDatesWith(in.Versions, from, in.Today, in.Calendar)
	if err != nil {
		return StreakResult{}, err
	}
	byDate := logsByDate(in.Logs)
	res := StreakResult{Applicable: true, Unit: UnitDay}
	run := 0
	for _, d := range due {
		switch {
		case completion(in, d, byDate[d]).Done:
			run++
			if run > res.Longest {
				res.Longest = run
			}
		case d == in.Today, frozen(in, d):
		default:
			run = 0
		}
	}
	res.Current = run
	return res, nil
}

// dailyStart is the date from which due days are walked: the effective_from of
// the first version after the last rolling/quota version (those cannot be
// expanded as due days), or of the earliest version.
func dailyStart(versions []dbmodels.ItemRuleVersion) string {
	vs := append([]dbmodels.ItemRuleVersion(nil), versions...)
	sort.Slice(vs, func(i, j int) bool {
		if vs[i].EffectiveFrom != vs[j].EffectiveFrom {
			return vs[i].EffectiveFrom < vs[j].EffectiveFrom
		}
		return vs[i].ID < vs[j].ID
	})
	start := ""
	for i, v := range vs {
		if v.ScheduleType != dbmodels.ScheduleFixedRRule && v.ScheduleType != dbmodels.ScheduleNone && i+1 < len(vs) {
			start = vs[i+1].EffectiveFrom
		}
	}
	if start == "" && len(vs) > 0 {
		start = vs[0].EffectiveFrom
	}
	if start < "1900-01-01" {
		start = "1900-01-01"
	}
	return start
}

func quotaLogs(logs []dbmodels.CheckinLog) []schedule.QuotaLog {
	out := make([]schedule.QuotaLog, len(logs))
	for i, l := range logs {
		out[i] = schedule.QuotaLog{Date: l.CheckinDate, Value: l.Value, OptionID: l.OptionID}
	}
	return out
}

// resolvedVersions returns the versions with the default quota measure applied.
func resolvedVersions(in Input) []dbmodels.ItemRuleVersion {
	vs := append([]dbmodels.ItemRuleVersion(nil), in.Versions...)
	for i := range vs {
		vs[i].QuotaMeasure = schedule.EffectiveMeasure(vs[i].QuotaMeasure, in.Item.Polarity)
	}
	return vs
}

func quotaStreak(in Input) (StreakResult, error) {
	cur, longest, err := schedule.QuotaStreak(resolvedVersions(in), quotaLogs(in.Logs), in.Today, in.Settings.WeekStart)
	if errors.Is(err, schedule.ErrNoStreak) {
		return StreakResult{}, nil
	}
	if err != nil {
		return StreakResult{}, err
	}
	return StreakResult{Applicable: true, Unit: UnitPeriod, Current: cur, Longest: longest}, nil
}

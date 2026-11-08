package stats

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/holiday"
)

const (
	daily    = "FREQ=DAILY"
	weekdays = "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"
)

func habit() dbmodels.Item {
	return dbmodels.Item{ID: 1, UserID: "u", Name: "x", TrackerType: dbmodels.TrackerBoolean,
		Kind: dbmodels.KindHabit, Polarity: dbmodels.PolarityPositive}
}

func rrv(id uint, from, rule string) dbmodels.ItemRuleVersion {
	return dbmodels.ItemRuleVersion{ID: id, ItemID: 1, EffectiveFrom: from, ScheduleType: dbmodels.ScheduleFixedRRule,
		DtStart: from, RRuleString: rule, HolidayMode: dbmodels.HolidayNone}
}

func blogs(dates ...string) []dbmodels.CheckinLog {
	out := make([]dbmodels.CheckinLog, len(dates))
	for i, d := range dates {
		out[i] = dbmodels.CheckinLog{ID: uint(i + 1), ItemID: 1, CheckinDate: d,
			CheckinTime: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	}
	return out
}

func freeze(from, to string) dbmodels.FreezePeriod {
	return dbmodels.FreezePeriod{StartDate: from, EndDate: to}
}

func TestStreakDaily(t *testing.T) {
	v := []dbmodels.ItemRuleVersion{rrv(1, "2026-11-01", daily)}
	logs := blogs("2026-11-01", "2026-11-02", "2026-11-03", "2026-11-05", "2026-11-06")
	tests := []struct {
		name         string
		today        string
		logs         []dbmodels.CheckinLog
		freezes      []dbmodels.FreezePeriod
		cur, longest int
	}{
		{"gap breaks, today pending does not", "2026-11-07", logs, nil, 2, 3},
		{"freeze covers the missed day", "2026-11-07", logs, []dbmodels.FreezePeriod{freeze("2026-11-04", "2026-11-04")}, 5, 5},
		{"freeze elsewhere does not help", "2026-11-07", logs, []dbmodels.FreezePeriod{freeze("2026-11-20", "2026-11-25")}, 2, 3},
		{"today done counts", "2026-11-07", blogs("2026-11-05", "2026-11-06", "2026-11-07"), nil, 3, 3},
		{"day passes without a log: breaks", "2026-11-08", logs, nil, 0, 3},
		{"day passes inside freeze: still alive", "2026-11-08", logs,
			[]dbmodels.FreezePeriod{freeze("2026-11-04", "2026-11-04"), freeze("2026-11-07", "2026-11-07")}, 5, 5},
		{"no logs at all, today pending", "2026-11-01", nil, nil, 0, 0},
		{"no logs, past due days", "2026-11-03", nil, nil, 0, 0},
		{"multi-day freeze", "2026-11-07", blogs("2026-11-01", "2026-11-06"),
			[]dbmodels.FreezePeriod{freeze("2026-11-02", "2026-11-05")}, 2, 2},
		{"completed frozen day still counts", "2026-11-07", blogs("2026-11-01", "2026-11-02", "2026-11-03", "2026-11-04", "2026-11-05", "2026-11-06"),
			[]dbmodels.FreezePeriod{freeze("2026-11-02", "2026-11-03")}, 6, 6},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Streak(Input{Item: habit(), Versions: v, Logs: tc.logs, Freezes: tc.freezes, Today: tc.today})
			require.NoError(t, err)
			require.True(t, r.Applicable)
			require.Equal(t, UnitDay, r.Unit)
			require.Equal(t, tc.cur, r.Current, "current")
			require.Equal(t, tc.longest, r.Longest, "longest")
		})
	}
}

// C6: adding a FreezePeriod immediately changes the result; with no new logs
// the streak breaks once the day passes.
func TestStreakFreezeAndDayPassing(t *testing.T) {
	v := []dbmodels.ItemRuleVersion{rrv(1, "2026-11-01", daily)}
	in := Input{Item: habit(), Versions: v, Logs: blogs("2026-11-01", "2026-11-02", "2026-11-04"), Today: "2026-11-05"}
	r, _ := Streak(in)
	require.Equal(t, 1, r.Current)
	in.Freezes = []dbmodels.FreezePeriod{freeze("2026-11-03", "2026-11-03")}
	r, _ = Streak(in)
	require.Equal(t, 3, r.Current)
	in.Today = "2026-11-06" // 11-05 passed with no log
	r, _ = Streak(in)
	require.Equal(t, 0, r.Current)
	require.Equal(t, 3, r.Longest)
}

func TestStreakNonDueDaysNeitherAddNorBreak(t *testing.T) {
	// Fri 2026-11-06 .. Mon 2026-11-09: the weekend is not due.
	v := []dbmodels.ItemRuleVersion{rrv(1, "2026-11-02", weekdays)}
	in := Input{Item: habit(), Versions: v,
		Logs:  blogs("2026-11-02", "2026-11-03", "2026-11-04", "2026-11-05", "2026-11-06", "2026-11-09"),
		Today: "2026-11-09"}
	r, err := Streak(in)
	require.NoError(t, err)
	require.Equal(t, 6, r.Current)
	require.Equal(t, 6, r.Longest)
	in.Today = "2026-11-08" // a Sunday
	r, _ = Streak(in)
	require.Equal(t, 5, r.Current)
}

func TestStreakHolidayFreeze2025(t *testing.T) {
	p, err := holiday.NewProvider()
	require.NoError(t, err)
	cal := holiday.NewCalendar(p, holiday.RegionCN)
	require.True(t, cal.IsStatutoryHoliday("2025-10-03"))

	v := []dbmodels.ItemRuleVersion{rrv(1, "2025-09-29", daily)}
	in := Input{Item: habit(), Versions: v, Calendar: cal, Today: "2025-10-09",
		Logs: blogs("2025-09-29", "2025-09-30", "2025-10-09")}

	// feature off: the National Day week breaks the streak
	r, err := Streak(in)
	require.NoError(t, err)
	require.Equal(t, 1, r.Current)
	require.Equal(t, 2, r.Longest)

	// feature on: statutory holidays are skipped
	in.Settings.StreakHolidayFreeze = true
	r, _ = Streak(in)
	require.Equal(t, 3, r.Current)
	require.Equal(t, 3, r.Longest)

	// feature on but no calendar (region without data): no effect
	in.Calendar = nil
	r, _ = Streak(in)
	require.Equal(t, 1, r.Current)

	// the first working day after the holiday, still pending, keeps the run
	in.Calendar = cal
	in.Logs = blogs("2025-09-29", "2025-09-30")
	in.Today = "2025-10-09"
	r, _ = Streak(in)
	require.Equal(t, 2, r.Current)
}

func TestStreakVersionChangeKeepsHistory(t *testing.T) {
	// daily until 11-04, then Mondays only from 11-05; 11-05 and 11-06 are not
	// due under v2 even though they would have been under v1.
	v := []dbmodels.ItemRuleVersion{rrv(1, "2026-11-01", daily), rrv(2, "2026-11-05", "FREQ=WEEKLY;BYDAY=MO")}
	in := Input{Item: habit(), Versions: v, Logs: blogs("2026-11-01", "2026-11-02", "2026-11-03", "2026-11-04"), Today: "2026-11-07"}
	r, err := Streak(in)
	require.NoError(t, err)
	require.Equal(t, 4, r.Current)

	// a missed day under the old version stays missed
	in.Logs = blogs("2026-11-01", "2026-11-03", "2026-11-04")
	r, _ = Streak(in)
	require.Equal(t, 2, r.Current)
	require.Equal(t, 2, r.Longest)

	// v2 Monday 11-09 not completed once it has passed
	in.Logs = blogs("2026-11-01", "2026-11-02", "2026-11-03", "2026-11-04")
	in.Today = "2026-11-10"
	r, _ = Streak(in)
	require.Equal(t, 0, r.Current)
	require.Equal(t, 4, r.Longest)
}

func TestStreakCompletionRuleAllAny(t *testing.T) {
	one, two := uint(1), uint(2)
	opts := []dbmodels.ItemOption{{ID: 1, ItemID: 1, Name: "a"}, {ID: 2, ItemID: 1, Name: "b"}}
	item := habit()
	item.TrackerType = dbmodels.TrackerCounter
	mk := func(rule dbmodels.CompletionRule) []dbmodels.ItemRuleVersion {
		v := rrv(1, "2026-11-01", daily)
		v.CompletionRule = rule
		v.OptionTargets = `{"1":10,"2":5}`
		return []dbmodels.ItemRuleVersion{v}
	}
	lg := func(id uint, date string, opt *uint, val float64) dbmodels.CheckinLog {
		return dbmodels.CheckinLog{ID: id, ItemID: 1, CheckinDate: date, OptionID: opt, Value: val,
			CheckinTime: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	}
	logs := []dbmodels.CheckinLog{
		lg(1, "2026-11-01", &one, 10), // a met only
		lg(2, "2026-11-02", &one, 10), lg(3, "2026-11-02", &two, 5), // both met
		lg(4, "2026-11-03", &one, 4), lg(5, "2026-11-03", &two, 5), // b met only
	}
	in := Input{Item: item, Options: opts, Logs: logs, Today: "2026-11-04"}

	in.Versions = mk(dbmodels.CompletionAny)
	r, err := Streak(in)
	require.NoError(t, err)
	require.Equal(t, 3, r.Current)

	in.Versions = mk(dbmodels.CompletionAll)
	r, _ = Streak(in)
	require.Equal(t, 0, r.Current, "11-03 only partially met")
	require.Equal(t, 1, r.Longest, "only 11-02 is complete")
}

func TestStreakNotApplicable(t *testing.T) {
	base := func(mut func(*dbmodels.Item, *dbmodels.ItemRuleVersion)) Input {
		it := habit()
		v := rrv(1, "2026-11-01", daily)
		mut(&it, &v)
		return Input{Item: it, Versions: []dbmodels.ItemRuleVersion{v}, Logs: blogs("2026-11-01"), Today: "2026-11-02"}
	}
	cases := map[string]Input{
		"rolling": base(func(_ *dbmodels.Item, v *dbmodels.ItemRuleVersion) {
			v.ScheduleType, v.RollingInterval, v.RollingUnit = dbmodels.ScheduleRolling, 7, dbmodels.RollingDay
		}),
		"none": base(func(_ *dbmodels.Item, v *dbmodels.ItemRuleVersion) { v.ScheduleType = dbmodels.ScheduleNone }),
		"maintenance": base(func(it *dbmodels.Item, _ *dbmodels.ItemRuleVersion) {
			it.TrackerType = dbmodels.TrackerMaintenance
		}),
		"negative": base(func(it *dbmodels.Item, _ *dbmodels.ItemRuleVersion) { it.Polarity = dbmodels.PolarityNegative }),
		"event":    base(func(it *dbmodels.Item, _ *dbmodels.ItemRuleVersion) { it.Kind = dbmodels.KindEvent }),
		"rolling quota window": base(func(_ *dbmodels.Item, v *dbmodels.ItemRuleVersion) {
			v.ScheduleType, v.QuotaComparator, v.QuotaCount = dbmodels.ScheduleQuota, dbmodels.QuotaAtLeast, 3
			v.QuotaPeriod, v.QuotaWindow = dbmodels.QuotaWeek, dbmodels.QuotaWindowRolling
		}),
		"no version": {Item: habit(), Today: "2026-11-02"},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			r, err := Streak(in)
			require.NoError(t, err)
			require.False(t, r.Applicable)
			require.Zero(t, r.Current)
		})
	}
}

func TestStreakQuotaCalendar(t *testing.T) {
	v := dbmodels.ItemRuleVersion{ID: 1, ItemID: 1, EffectiveFrom: "2026-10-12", ScheduleType: dbmodels.ScheduleQuota,
		QuotaComparator: dbmodels.QuotaAtLeast, QuotaCount: 2, QuotaPeriod: dbmodels.QuotaWeek,
		QuotaWindow: dbmodels.QuotaWindowCalendar, QuotaMeasure: dbmodels.QuotaMeasureOccurrences}
	// weeks (Mon start): 10-12 miss, 10-19 ok, 10-26 ok, 11-02 in progress (1 so far)
	in := Input{Item: habit(), Versions: []dbmodels.ItemRuleVersion{v}, Settings: Settings{WeekStart: 1}, Today: "2026-11-07",
		Logs: blogs("2026-10-13", "2026-10-20", "2026-10-21", "2026-10-27", "2026-10-28", "2026-11-03")}
	r, err := Streak(in)
	require.NoError(t, err)
	require.True(t, r.Applicable)
	require.Equal(t, UnitPeriod, r.Unit)
	require.Equal(t, 2, r.Current, "unmet in-progress period neither counts nor breaks")
	require.Equal(t, 2, r.Longest)

	// freeze periods are ignored for quota streaks
	in.Freezes = []dbmodels.FreezePeriod{freeze("2026-10-12", "2026-10-18")}
	r2, _ := Streak(in)
	require.Equal(t, r, r2)
}

func TestStreakNegativeQuota(t *testing.T) {
	it := habit()
	it.Polarity = dbmodels.PolarityNegative
	it.Kind = dbmodels.KindEvent
	v := dbmodels.ItemRuleVersion{ID: 1, ItemID: 1, EffectiveFrom: "2026-01-01", ScheduleType: dbmodels.ScheduleQuota,
		QuotaComparator: dbmodels.QuotaAtMost, QuotaCount: 1, QuotaPeriod: dbmodels.QuotaMonth, QuotaWindow: dbmodels.QuotaWindowCalendar}
	// Jan ok(1), Feb exceeded(2), Mar..Oct ok (8 months), Nov ok so far
	logs := blogs("2026-01-05", "2026-02-05", "2026-02-06")
	r, err := Streak(Input{Item: it, Versions: []dbmodels.ItemRuleVersion{v}, Logs: logs, Settings: Settings{WeekStart: 1}, Today: "2026-11-07"})
	require.NoError(t, err)
	require.True(t, r.Applicable)
	require.Equal(t, 9, r.Current)
	require.Equal(t, 9, r.Longest)
}

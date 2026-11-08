package schedule

import (
	"errors"
	"math"
	"testing"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

func qlogs(dates ...string) []QuotaLog {
	out := make([]QuotaLog, len(dates))
	for i, d := range dates {
		out[i] = QuotaLog{Date: d, Value: 1}
	}
	return out
}

func qrule(cmp dbmodels.QuotaComparator, count float64, p dbmodels.QuotaPeriod, m dbmodels.QuotaMeasure) dbmodels.ItemRuleVersion {
	return dbmodels.ItemRuleVersion{
		ScheduleType: dbmodels.ScheduleQuota, EffectiveFrom: "2020-01-01",
		QuotaComparator: cmp, QuotaCount: count, QuotaPeriod: p,
		QuotaWindow: dbmodels.QuotaWindowCalendar, QuotaMeasure: m,
	}
}

func TestPeriod(t *testing.T) {
	tests := []struct {
		name       string
		date       string
		period     dbmodels.QuotaPeriod
		ws         int
		start, end string
	}{
		{"sat monday-start", "2026-11-07", dbmodels.QuotaWeek, 1, "2026-11-02", "2026-11-08"},
		{"sat sunday-start", "2026-11-07", dbmodels.QuotaWeek, 0, "2026-11-01", "2026-11-07"},
		{"sunday monday-start", "2026-11-08", dbmodels.QuotaWeek, 1, "2026-11-02", "2026-11-08"},
		{"sunday sunday-start", "2026-11-08", dbmodels.QuotaWeek, 0, "2026-11-08", "2026-11-14"},
		{"saturday-start", "2026-11-07", dbmodels.QuotaWeek, 6, "2026-11-07", "2026-11-13"},
		{"week across year", "2026-01-01", dbmodels.QuotaWeek, 1, "2025-12-29", "2026-01-04"},
		{"month leap", "2024-02-15", dbmodels.QuotaMonth, 1, "2024-02-01", "2024-02-29"},
		{"month non-leap", "2025-02-10", dbmodels.QuotaMonth, 1, "2025-02-01", "2025-02-28"},
		{"month december", "2026-12-31", dbmodels.QuotaMonth, 1, "2026-12-01", "2026-12-31"},
		{"year first day", "2026-01-01", dbmodels.QuotaYear, 1, "2026-01-01", "2026-12-31"},
		{"year last day", "2026-12-31", dbmodels.QuotaYear, 0, "2026-01-01", "2026-12-31"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, e, err := Period(tc.date, tc.period, tc.ws)
			if err != nil || s != tc.start || e != tc.end {
				t.Fatalf("got %s..%s err=%v, want %s..%s", s, e, err, tc.start, tc.end)
			}
		})
	}
	for _, bad := range []struct {
		date string
		p    dbmodels.QuotaPeriod
		ws   int
	}{{"2026-11-07", "day", 1}, {"2026-11-07", dbmodels.QuotaWeek, 7}, {"nope", dbmodels.QuotaWeek, 1}} {
		if _, _, err := Period(bad.date, bad.p, bad.ws); !errors.Is(err, ErrInvalid) {
			t.Errorf("Period(%v) err=%v, want ErrInvalid", bad, err)
		}
	}
}

func TestEvaluateQuota(t *testing.T) {
	opt1, opt2 := uint(1), uint(2)
	const today = "2026-11-07" // Saturday
	km := func(d string, v float64, o *uint) QuotaLog { return QuotaLog{Date: d, Value: v, OptionID: o} }

	sumRule := qrule(dbmodels.QuotaAtLeast, 10.5, dbmodels.QuotaWeek, dbmodels.QuotaMeasureSum)
	sumOpt := sumRule
	sumOpt.QuotaOptionID = &opt1
	rollWeek := qrule(dbmodels.QuotaAtLeast, 3, dbmodels.QuotaWeek, dbmodels.QuotaMeasureDays)
	rollWeek.QuotaWindow = dbmodels.QuotaWindowRolling
	rollMonth := rollWeek
	rollMonth.QuotaPeriod = dbmodels.QuotaMonth
	rollYear := rollWeek
	rollYear.QuotaPeriod = dbmodels.QuotaYear

	tests := []struct {
		name       string
		rule       dbmodels.ItemRuleVersion
		logs       []QuotaLog
		ws         int
		start, end string
		used, rem  float64
		met, exc   bool
	}{
		{"days at_least dedupes same day, monday week", qrule(dbmodels.QuotaAtLeast, 3, dbmodels.QuotaWeek, dbmodels.QuotaMeasureDays),
			qlogs("2026-11-02", "2026-11-02", "2026-11-04", "2026-11-01"), 1, "2026-11-02", "2026-11-08", 2, 1, false, false},
		{"same logs sunday week includes 11-01", qrule(dbmodels.QuotaAtLeast, 3, dbmodels.QuotaWeek, dbmodels.QuotaMeasureDays),
			qlogs("2026-11-02", "2026-11-02", "2026-11-04", "2026-11-01"), 0, "2026-11-01", "2026-11-07", 3, 0, true, false},
		{"occurrences counts every log (A13)", qrule(dbmodels.QuotaAtMost, 4, dbmodels.QuotaWeek, dbmodels.QuotaMeasureOccurrences),
			qlogs("2026-11-03", "2026-11-03", "2026-11-05"), 1, "2026-11-02", "2026-11-08", 3, 1, true, false},
		{"at_most exactly at limit is met", qrule(dbmodels.QuotaAtMost, 2, dbmodels.QuotaWeek, dbmodels.QuotaMeasureOccurrences),
			qlogs("2026-11-03", "2026-11-05"), 1, "2026-11-02", "2026-11-08", 2, 0, true, false},
		{"at_most exceeded", qrule(dbmodels.QuotaAtMost, 2, dbmodels.QuotaWeek, dbmodels.QuotaMeasureOccurrences),
			qlogs("2026-11-03", "2026-11-04", "2026-11-05"), 1, "2026-11-02", "2026-11-08", 3, -1, false, true},
		{"empty logs at_least", qrule(dbmodels.QuotaAtLeast, 3, dbmodels.QuotaWeek, dbmodels.QuotaMeasureDays),
			nil, 1, "2026-11-02", "2026-11-08", 0, 3, false, false},
		{"empty logs at_most", qrule(dbmodels.QuotaAtMost, 3, dbmodels.QuotaWeek, dbmodels.QuotaMeasureOccurrences),
			nil, 1, "2026-11-02", "2026-11-08", 0, 3, true, false},
		{"sum float 10.5 km (A12)", sumRule,
			[]QuotaLog{km("2026-11-03", 5.25, nil), km("2026-11-06", 5.25, nil)}, 1, "2026-11-02", "2026-11-08", 10.5, 0, true, false},
		{"sum float just short", sumRule,
			[]QuotaLog{km("2026-11-03", 5, nil), km("2026-11-06", 5.4, nil)}, 1, "2026-11-02", "2026-11-08", 10.4, 0.1, false, false},
		{"sum ignores other options", sumOpt,
			[]QuotaLog{km("2026-11-03", 6, &opt1), km("2026-11-04", 100, &opt2), km("2026-11-05", 100, nil)}, 1, "2026-11-02", "2026-11-08", 6, 4.5, false, false},
		{"month calendar", qrule(dbmodels.QuotaAtLeast, 2, dbmodels.QuotaMonth, dbmodels.QuotaMeasureDays),
			qlogs("2026-10-31", "2026-11-01", "2026-11-30"), 1, "2026-11-01", "2026-11-30", 2, 0, true, false},
		{"year calendar", qrule(dbmodels.QuotaAtMost, 4, dbmodels.QuotaYear, dbmodels.QuotaMeasureOccurrences),
			qlogs("2025-12-31", "2026-01-01", "2026-06-01"), 1, "2026-01-01", "2026-12-31", 2, 2, true, false},
		{"rolling week = last 7 days", rollWeek,
			qlogs("2026-10-31", "2026-11-01", "2026-11-07"), 1, "2026-11-01", "2026-11-07", 2, 1, false, false},
		{"rolling month = last 30 days", rollMonth,
			qlogs("2026-10-08", "2026-10-09", "2026-11-07"), 1, "2026-10-09", "2026-11-07", 2, 1, false, false},
		{"rolling year = last 365 days", rollYear,
			qlogs("2025-11-07", "2025-11-08"), 1, "2025-11-08", "2026-11-07", 1, 2, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, err := EvaluateQuota(tc.rule, tc.logs, today, tc.ws)
			if err != nil {
				t.Fatal(err)
			}
			if st.WindowStart != tc.start || st.WindowEnd != tc.end {
				t.Errorf("window %s..%s, want %s..%s", st.WindowStart, st.WindowEnd, tc.start, tc.end)
			}
			if math.Abs(st.Used-tc.used) > 1e-9 || math.Abs(st.Remaining-tc.rem) > 1e-9 || st.Limit != tc.rule.QuotaCount {
				t.Errorf("used=%v rem=%v limit=%v, want used=%v rem=%v", st.Used, st.Remaining, st.Limit, tc.used, tc.rem)
			}
			if st.Met != tc.met || st.Exceeded != tc.exc {
				t.Errorf("met=%v exceeded=%v, want %v %v", st.Met, st.Exceeded, tc.met, tc.exc)
			}
		})
	}
}

func TestEvaluateQuotaRatioAndErrors(t *testing.T) {
	st, err := EvaluateQuota(qrule(dbmodels.QuotaAtMost, 4, dbmodels.QuotaWeek, dbmodels.QuotaMeasureOccurrences),
		qlogs("2026-11-03"), "2026-11-07", 1)
	if err != nil || st.Ratio != 0.25 {
		t.Fatalf("ratio=%v err=%v", st.Ratio, err)
	}
	if _, err := EvaluateQuota(dbmodels.ItemRuleVersion{}, nil, "2026-11-07", 1); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty period err=%v", err)
	}
	if _, err := EvaluateQuota(qrule(dbmodels.QuotaAtMost, 4, dbmodels.QuotaWeek, ""), nil, "bad", 1); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad today err=%v", err)
	}
}

func TestTraffic(t *testing.T) {
	tests := []struct {
		limit, used float64
		want        Light
	}{
		{4, 0, LightGreen}, {4, 1, LightGreen}, {4, 2, LightGreen}, // 50%
		{4, 3, LightYellow}, // 75%
		{4, 4, LightRed},    // 100%
		{4, 5, LightRed},
		{10, 6, LightGreen},  // 60%
		{10, 7, LightYellow}, // exactly 70%
		{10, 9, LightYellow}, // 90%
		{10, 10, LightRed},   // exactly 100%
		{1, 0, LightGreen},   // 0%
		{1, 1, LightRed},     // 100%
		{2000, 1399, LightGreen},
		{2000, 1400, LightYellow},
	}
	for _, tc := range tests {
		logs := []QuotaLog{{Date: "2026-11-03", Value: tc.used}}
		st, err := EvaluateQuota(qrule(dbmodels.QuotaAtMost, tc.limit, dbmodels.QuotaYear, dbmodels.QuotaMeasureSum), logs, "2026-11-07", 1)
		if err != nil {
			t.Fatal(err)
		}
		if got := Traffic(st); got != tc.want {
			t.Errorf("limit %v used %v: %q, want %q", tc.limit, tc.used, got, tc.want)
		}
	}
	st, _ := EvaluateQuota(qrule(dbmodels.QuotaAtLeast, 3, dbmodels.QuotaWeek, dbmodels.QuotaMeasureDays), nil, "2026-11-07", 1)
	if Traffic(st) != LightNone {
		t.Error("at_least must have no traffic light")
	}
}

func TestQuotaStreak(t *testing.T) {
	// Weeks (Monday start): W1 10-05..11, W2 10-12..18, W3 10-19..25,
	// W4 10-26..11-01, W5 (current) 11-02..08. Today is Saturday 11-07.
	const today = "2026-11-07"
	atLeast2 := func(from string) dbmodels.ItemRuleVersion {
		r := qrule(dbmodels.QuotaAtLeast, 2, dbmodels.QuotaWeek, dbmodels.QuotaMeasureDays)
		r.EffectiveFrom = from
		return r
	}
	atMost4 := func(from string) dbmodels.ItemRuleVersion {
		r := qrule(dbmodels.QuotaAtMost, 4, dbmodels.QuotaWeek, dbmodels.QuotaMeasureOccurrences)
		r.EffectiveFrom = from
		return r
	}
	base := []string{"2026-10-05", "2026-10-06", "2026-10-12", "2026-10-19", "2026-10-20", "2026-10-26", "2026-10-27"}

	b := atLeast2("2026-10-26")
	b.QuotaCount = 5
	b.ID = 2
	a := atLeast2("2026-10-05")
	a.ID = 1
	b2 := b
	b2.EffectiveFrom = "2026-10-28" // mid-week W4
	roll := atLeast2("2026-10-05")
	roll.QuotaWindow = dbmodels.QuotaWindowRolling
	monthly := qrule(dbmodels.QuotaAtLeast, 1, dbmodels.QuotaMonth, dbmodels.QuotaMeasureOccurrences)
	monthly.EffectiveFrom = "2025-11-01"
	fixed := dbmodels.ItemRuleVersion{ScheduleType: dbmodels.ScheduleFixedRRule, EffectiveFrom: "2026-10-05"}
	lateStart := atLeast2("2026-10-14") // Wednesday of W2

	tests := []struct {
		name     string
		versions []dbmodels.ItemRuleVersion
		logs     []QuotaLog
		today    string
		wantCur  int
		wantLong int
	}{
		{"in-progress unmet at_least neither counts nor breaks", []dbmodels.ItemRuleVersion{atLeast2("2026-10-05")},
			qlogs(append(base, "2026-11-03")...), today, 2, 2},
		{"in-progress met at_least counts", []dbmodels.ItemRuleVersion{atLeast2("2026-10-05")},
			qlogs(append(base, "2026-11-03", "2026-11-04")...), today, 3, 3},
		{"longest remembers earlier run", []dbmodels.ItemRuleVersion{atLeast2("2026-10-05")},
			qlogs("2026-10-05", "2026-10-06", "2026-10-12", "2026-10-13", "2026-10-19", "2026-10-20", "2026-10-26"), today, 0, 3},
		{"empty logs at_least", []dbmodels.ItemRuleVersion{atLeast2("2026-10-05")}, nil, today, 0, 0},
		{"empty logs at_most: 4 done weeks + in-progress", []dbmodels.ItemRuleVersion{atMost4("2026-10-05")}, nil, today, 5, 5},
		{"at_most in-progress at limit still counts", []dbmodels.ItemRuleVersion{atMost4("2026-10-05")},
			qlogs("2026-11-02", "2026-11-03", "2026-11-04", "2026-11-05"), today, 5, 5},
		{"at_most in-progress exceeded breaks", []dbmodels.ItemRuleVersion{atMost4("2026-10-05")},
			qlogs("2026-11-02", "2026-11-03", "2026-11-04", "2026-11-05", "2026-11-06"), today, 0, 4},
		{"at_most completed week exceeded breaks", []dbmodels.ItemRuleVersion{atMost4("2026-10-05")},
			qlogs("2026-10-12", "2026-10-13", "2026-10-14", "2026-10-15", "2026-10-16"), today, 3, 3},
		{"new stricter rule applies to periods starting after it", []dbmodels.ItemRuleVersion{a, b},
			qlogs(base...), today, 0, 1},
		{"mid-week rule change does not rewrite ended period (A2)", []dbmodels.ItemRuleVersion{a, b2},
			qlogs(base...), today, 2, 2},
		{"periods before first version skipped, not broken", []dbmodels.ItemRuleVersion{lateStart},
			qlogs("2026-10-19", "2026-10-20", "2026-10-26", "2026-10-27"), today, 2, 2},
		{"month streak across year boundary", []dbmodels.ItemRuleVersion{monthly},
			qlogs("2025-11-15", "2025-12-31", "2026-01-01"), "2026-02-10", 3, 3},
		{"month streak broken by empty month", []dbmodels.ItemRuleVersion{monthly},
			qlogs("2025-11-15", "2026-01-01"), "2026-02-10", 1, 1},
		{"no versions", nil, nil, today, 0, 0},
		{"version not yet effective", []dbmodels.ItemRuleVersion{atLeast2("2027-01-01")}, nil, today, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cur, long, err := QuotaStreak(tc.versions, tc.logs, tc.today, 1)
			if err != nil {
				t.Fatal(err)
			}
			if cur != tc.wantCur || long != tc.wantLong {
				t.Errorf("current=%d longest=%d, want %d %d", cur, long, tc.wantCur, tc.wantLong)
			}
		})
	}

	for name, vs := range map[string][]dbmodels.ItemRuleVersion{"rolling": {roll}, "not quota": {fixed}} {
		if _, _, err := QuotaStreak(vs, nil, today, 1); !errors.Is(err, ErrNoStreak) {
			t.Errorf("%s: err=%v, want ErrNoStreak", name, err)
		}
	}
	if _, _, err := QuotaStreak([]dbmodels.ItemRuleVersion{a}, nil, "bad", 1); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad today err=%v", err)
	}
}

func TestQuotaStreakWeekStart(t *testing.T) {
	// A log on Sunday 11-01 belongs to the previous week for Monday start
	// (W4 10-26..11-01) but to the current week for Sunday start.
	v := qrule(dbmodels.QuotaAtLeast, 1, dbmodels.QuotaWeek, dbmodels.QuotaMeasureDays)
	v.EffectiveFrom = "2026-10-25" // a Sunday
	logs := qlogs("2026-11-01")
	cur, long, err := QuotaStreak([]dbmodels.ItemRuleVersion{v}, logs, "2026-11-07", 0)
	if err != nil || cur != 1 || long != 1 { // Sunday-start: 10-25..31 empty (broken), 11-01..07 met
		t.Fatalf("sunday start: %d %d %v", cur, long, err)
	}
	v.EffectiveFrom = "2026-10-26" // a Monday
	cur, long, err = QuotaStreak([]dbmodels.ItemRuleVersion{v}, logs, "2026-11-07", 1)
	if err != nil || cur != 1 || long != 1 { // Monday-start: 10-26..11-01 met, 11-02..08 unmet in progress
		t.Fatalf("monday start: %d %d %v", cur, long, err)
	}
}

func TestStaleStatus(t *testing.T) {
	tests := []struct {
		last      string
		after     int
		days      int
		stale     bool
		wantError bool
	}{
		{"2026-11-07", 7, 0, false, false},
		{"2026-10-31", 7, 7, false, false}, // exactly N days: not stale
		{"2026-10-30", 7, 8, true, false},
		{"2026-10-01", 0, 37, false, false}, // 0 disables
		{"", 7, 0, false, false},            // never logged
		{"2026-11-09", 7, 0, false, false},  // future clamps to 0
		{"2025-11-07", 30, 365, true, false},
		{"garbage", 7, 0, false, true},
	}
	for _, tc := range tests {
		s, err := StaleStatus(tc.last, "2026-11-07", tc.after)
		if (err != nil) != tc.wantError {
			t.Fatalf("%q err=%v", tc.last, err)
		}
		if err == nil && (s.Days != tc.days || s.Stale != tc.stale) {
			t.Errorf("%q: %+v, want days=%d stale=%v", tc.last, s, tc.days, tc.stale)
		}
	}
}

func TestValidateQuota(t *testing.T) {
	mk := func(mut func(*dbmodels.ItemRuleVersion)) dbmodels.ItemRuleVersion {
		r := qrule(dbmodels.QuotaAtLeast, 3, dbmodels.QuotaWeek, "")
		r.QuotaWindow = ""
		mut(&r)
		return r
	}
	none := func(*dbmodels.ItemRuleVersion) {}
	pos, neg := dbmodels.PolarityPositive, dbmodels.PolarityNegative
	tests := []struct {
		name string
		rule dbmodels.ItemRuleVersion
		tt   dbmodels.TrackerType
		pol  dbmodels.Polarity
		ok   bool
	}{
		{"positive defaults", mk(none), dbmodels.TrackerBoolean, pos, true},
		{"fractional count", mk(func(r *dbmodels.ItemRuleVersion) { r.QuotaCount = 10.5; r.QuotaMeasure = dbmodels.QuotaMeasureSum }), dbmodels.TrackerMetric, pos, true},
		{"counter sum", mk(func(r *dbmodels.ItemRuleVersion) { r.QuotaMeasure = dbmodels.QuotaMeasureSum }), dbmodels.TrackerCounter, pos, true},
		{"sum on boolean", mk(func(r *dbmodels.ItemRuleVersion) { r.QuotaMeasure = dbmodels.QuotaMeasureSum }), dbmodels.TrackerBoolean, pos, false},
		{"negative at_most boolean", mk(func(r *dbmodels.ItemRuleVersion) { r.QuotaComparator = dbmodels.QuotaAtMost }), dbmodels.TrackerBoolean, neg, true},
		{"negative metric sum", mk(func(r *dbmodels.ItemRuleVersion) {
			r.QuotaComparator = dbmodels.QuotaAtMost
			r.QuotaMeasure = dbmodels.QuotaMeasureSum
		}), dbmodels.TrackerMetric, neg, true},
		{"negative at_least", mk(none), dbmodels.TrackerBoolean, neg, false},
		{"negative counter", mk(func(r *dbmodels.ItemRuleVersion) { r.QuotaComparator = dbmodels.QuotaAtMost }), dbmodels.TrackerCounter, neg, false},
		{"maintenance", mk(none), dbmodels.TrackerMaintenance, pos, false},
		{"zero count", mk(func(r *dbmodels.ItemRuleVersion) { r.QuotaCount = 0 }), dbmodels.TrackerBoolean, pos, false},
		{"negative count", mk(func(r *dbmodels.ItemRuleVersion) { r.QuotaCount = -1 }), dbmodels.TrackerBoolean, pos, false},
		{"NaN count", mk(func(r *dbmodels.ItemRuleVersion) { r.QuotaCount = math.NaN() }), dbmodels.TrackerBoolean, pos, false},
		{"Inf count", mk(func(r *dbmodels.ItemRuleVersion) { r.QuotaCount = math.Inf(1) }), dbmodels.TrackerBoolean, pos, false},
		{"bad comparator", mk(func(r *dbmodels.ItemRuleVersion) { r.QuotaComparator = "eq" }), dbmodels.TrackerBoolean, pos, false},
		{"empty comparator", mk(func(r *dbmodels.ItemRuleVersion) { r.QuotaComparator = "" }), dbmodels.TrackerBoolean, pos, false},
		{"bad period", mk(func(r *dbmodels.ItemRuleVersion) { r.QuotaPeriod = "day" }), dbmodels.TrackerBoolean, pos, false},
		{"bad window", mk(func(r *dbmodels.ItemRuleVersion) { r.QuotaWindow = "sliding" }), dbmodels.TrackerBoolean, pos, false},
		{"rolling window ok", mk(func(r *dbmodels.ItemRuleVersion) { r.QuotaWindow = dbmodels.QuotaWindowRolling }), dbmodels.TrackerBoolean, pos, true},
		{"bad measure", mk(func(r *dbmodels.ItemRuleVersion) { r.QuotaMeasure = "avg" }), dbmodels.TrackerBoolean, pos, false},
		{"bad tracker", mk(none), "weird", pos, false},
		{"bad polarity", mk(none), dbmodels.TrackerBoolean, "neutral", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateQuota(tc.rule, tc.tt, tc.pol)
			if tc.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrInvalid) {
				t.Fatalf("err=%v, want ErrInvalid", err)
			}
		})
	}
}

func TestQuotaStreakOpenPeriodUsesCurrentVersion(t *testing.T) {
	// Open period is judged by the version effective today, not the one at
	// the period start.
	a := qrule(dbmodels.QuotaAtMost, 4, dbmodels.QuotaWeek, dbmodels.QuotaMeasureOccurrences)
	a.EffectiveFrom, a.ID = "2026-10-05", 1
	b := a
	b.QuotaCount, b.EffectiveFrom, b.ID = 10, "2026-11-05", 2 // Thursday, mid-week
	logs := qlogs("2026-11-02", "2026-11-02", "2026-11-03", "2026-11-04", "2026-11-05", "2026-11-06")
	st, err := EvaluateQuota(b, logs, "2026-11-07", 1)
	if err != nil || st.Exceeded {
		t.Fatalf("status %+v err=%v", st, err)
	}
	cur, long, err := QuotaStreak([]dbmodels.ItemRuleVersion{a, b}, logs, "2026-11-07", 1)
	if err != nil || cur != 5 || long != 5 { // 4 empty finished weeks + open week
		t.Fatalf("got %d %d %v, want 5 5", cur, long, err)
	}

	// Yearly -> weekly switch effective 2026-06-01 (Monday): the open year
	// must not swallow the weekly periods that followed.
	y := qrule(dbmodels.QuotaAtLeast, 1, dbmodels.QuotaYear, dbmodels.QuotaMeasureOccurrences)
	y.EffectiveFrom, y.ID = "2026-01-01", 1
	w := qrule(dbmodels.QuotaAtLeast, 1, dbmodels.QuotaWeek, dbmodels.QuotaMeasureOccurrences)
	w.EffectiveFrom, w.ID = "2026-06-01", 2
	cur, long, err = QuotaStreak([]dbmodels.ItemRuleVersion{y, w}, qlogs("2026-10-26", "2026-11-03"), "2026-11-07", 1)
	if err != nil || cur != 2 || long != 2 {
		t.Fatalf("got %d %d %v, want 2 2", cur, long, err)
	}
}

func TestEvaluateQuotaUnknownWindow(t *testing.T) {
	r := qrule(dbmodels.QuotaAtLeast, 1, dbmodels.QuotaWeek, dbmodels.QuotaMeasureDays)
	r.QuotaWindow = "sliding"
	if _, err := EvaluateQuota(r, nil, "2026-11-07", 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err=%v, want ErrInvalid", err)
	}
}

func TestEffectiveMeasure(t *testing.T) {
	if EffectiveMeasure("", dbmodels.PolarityPositive) != dbmodels.QuotaMeasureDays ||
		EffectiveMeasure("", dbmodels.PolarityNegative) != dbmodels.QuotaMeasureOccurrences ||
		EffectiveMeasure(dbmodels.QuotaMeasureSum, dbmodels.PolarityNegative) != dbmodels.QuotaMeasureSum {
		t.Fatal("unexpected measure defaults")
	}
}

package schedule

import (
	"errors"
	"reflect"
	"testing"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/holiday"
)

// Reference facts from the embedded CN data (not computed by the code under
// test): 2025 National Day break is Wed 10-01 .. Wed 10-08; makeup workdays
// are Sun 2025-09-28 and Sat 2025-10-11; Labour Day 2025-05-01..05-05 with
// makeup Sun 2025-04-27; New Year 2026 break is 01-01..01-03 with makeup Sun
// 2026-01-04. 2027 has no data (plain weekends).

func cnCal(t *testing.T) *holiday.Calendar {
	t.Helper()
	p, err := holiday.NewProvider()
	if err != nil {
		t.Fatal(err)
	}
	return holiday.NewCalendar(p, holiday.RegionCN)
}

func hv(id uint, eff, rule string, mode dbmodels.HolidayMode) dbmodels.ItemRuleVersion {
	return dbmodels.ItemRuleVersion{ID: id, EffectiveFrom: eff, ScheduleType: dbmodels.ScheduleFixedRRule, RRuleString: rule, HolidayMode: mode}
}

const (
	none = dbmodels.HolidayNone
	skip = dbmodels.HolidaySkipHolidays
	wdo  = dbmodels.HolidayWorkdaysOnly
)

func TestHolidayModeDueDates(t *testing.T) {
	cal := cnCal(t)
	tests := []struct {
		name     string
		versions []dbmodels.ItemRuleVersion
		from, to string
		want     []string
	}{
		{"mon-fri workdays_only fires on makeup days, not on National Day",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR", wdo)},
			"2025-09-26", "2025-10-13",
			[]string{"2025-09-26", "2025-09-28", "2025-09-29", "2025-09-30", "2025-10-09", "2025-10-10", "2025-10-11", "2025-10-13"}},
		{"mon-fri order/duplicates in BYDAY still every-workday",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=WEEKLY;BYDAY=FR,TH,WE,TU,MO,MO", wdo)},
			"2025-10-09", "2025-10-12",
			[]string{"2025-10-09", "2025-10-10", "2025-10-11"}},
		{"mon-fri skip_holidays: makeup days do not trigger, weekends stay off",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR", skip)},
			"2025-09-26", "2025-10-13",
			[]string{"2025-09-26", "2025-09-29", "2025-09-30", "2025-10-09", "2025-10-10", "2025-10-13"}},
		{"daily skip_holidays keeps plain weekends, drops 10-01..10-08",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=DAILY", skip)},
			"2025-09-26", "2025-10-12",
			[]string{"2025-09-26", "2025-09-27", "2025-09-28", "2025-09-29", "2025-09-30", "2025-10-09", "2025-10-10", "2025-10-11", "2025-10-12"}},
		{"daily workdays_only drops weekends, keeps makeup days",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=DAILY", wdo)},
			"2025-09-26", "2025-10-12",
			[]string{"2025-09-26", "2025-09-28", "2025-09-29", "2025-09-30", "2025-10-09", "2025-10-10", "2025-10-11"}},
		{"daily none unchanged",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=DAILY", none)},
			"2025-10-01", "2025-10-03",
			[]string{"2025-10-01", "2025-10-02", "2025-10-03"}},
		{"TU,TH workdays_only: makeup days never trigger",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=WEEKLY;BYDAY=TU,TH", wdo)},
			"2025-09-23", "2025-10-16",
			[]string{"2025-09-23", "2025-09-25", "2025-09-30", "2025-10-09", "2025-10-14", "2025-10-16"}},
		{"SA,SU workdays_only: RRULE candidates on makeup days survive (workdays), other weekends do not",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=WEEKLY;BYDAY=SA,SU", wdo)},
			"2025-09-26", "2025-10-13", []string{"2025-09-28", "2025-10-11"}},
		{"TU,TH rule never gains makeup Sat/Sun",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=WEEKLY;BYDAY=TU,TH", wdo)},
			"2025-10-10", "2025-10-12", []string{}},
		{"monthly day 1 in the National Day break is postponed to 10-09",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=MONTHLY;BYMONTHDAY=1", wdo)},
			"2025-10-01", "2025-10-31", []string{"2025-10-09"}},
		{"postponed occurrence found when window starts after its RRULE date",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=MONTHLY;BYMONTHDAY=1", wdo)},
			"2025-10-05", "2025-10-31", []string{"2025-10-09"}},
		{"postponed occurrence not in window ending before it",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=MONTHLY;BYMONTHDAY=1", wdo)},
			"2025-10-01", "2025-10-08", []string{}},
		{"monthly day 1 skip_holidays skips the break",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=MONTHLY;BYMONTHDAY=1", skip)},
			"2025-10-01", "2025-10-31", []string{}},
		{"monthly day 1 on Saturday: workdays_only postpones, skip/none keep",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=MONTHLY;BYMONTHDAY=1", wdo)},
			"2025-11-01", "2025-11-30", []string{"2025-11-03"}},
		{"monthly day 1 on Saturday skip_holidays keeps it",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=MONTHLY;BYMONTHDAY=1", skip)},
			"2025-11-01", "2025-11-30", []string{"2025-11-01"}},
		{"monthly day 1 on Saturday none keeps it",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=MONTHLY;BYMONTHDAY=1", none)},
			"2025-11-01", "2025-11-30", []string{"2025-11-01"}},
		{"monthly 15th on Saturday 2025-03-15 moves to Monday",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=MONTHLY;BYMONTHDAY=15", wdo)},
			"2025-03-01", "2025-03-31", []string{"2025-03-17"}},
		{"monthly 15th on Sunday 2025-06-15 moves to Monday",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=MONTHLY;BYMONTHDAY=15", wdo)},
			"2025-06-01", "2025-06-30", []string{"2025-06-16"}},
		{"monthly day 3 in Labour Day break: skip drops it",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=MONTHLY;BYMONTHDAY=3", skip)},
			"2025-05-01", "2025-05-31", []string{}},
		{"monthly day 3 in Labour Day break: workdays_only -> Tue 05-06",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=MONTHLY;BYMONTHDAY=3", wdo)},
			"2025-05-01", "2025-05-31", []string{"2025-05-06"}},
		{"monthly day 27 on makeup Sunday 2025-04-27 stays (makeup is a workday)",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=MONTHLY;BYMONTHDAY=27", wdo)},
			"2025-04-01", "2025-04-30", []string{"2025-04-27"}},
		{"postponement crosses the month boundary: Sat 2026-01-31 -> Mon 02-02",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=MONTHLY;BYMONTHDAY=31", wdo)},
			"2026-01-01", "2026-02-28", []string{"2026-02-02"}},
		{"cross-boundary occurrence absent from the window it left",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=MONTHLY;BYMONTHDAY=31", wdo)},
			"2026-01-01", "2026-01-31", []string{}},
		{"monthly nth weekday (2SU = 2025-11-09 Sunday) postponed to Monday",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=MONTHLY;BYDAY=2SU", wdo)},
			"2025-11-01", "2025-11-30", []string{"2025-11-10"}},
		{"yearly Oct 1: 2025 -> 10-09, 2026 -> 10-08",
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=YEARLY;BYMONTH=10;BYMONTHDAY=1", wdo)},
			"2025-01-01", "2026-12-31", []string{"2025-10-09", "2026-10-08"}},
		{"collision: two occurrences postponed to one day are de-duplicated",
			// BYMONTHDAY=1,2 in Oct 2025 both land on 10-09.
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=MONTHLY;BYMONTHDAY=1,2", wdo)},
			"2025-10-01", "2025-10-31", []string{"2025-10-09"}},
		{"collision: postponed date equals the next occurrence",
			// 2025-11-01 (Sat) -> 11-03, equals the BYMONTHDAY=3 occurrence.
			[]dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=MONTHLY;BYMONTHDAY=1,3", wdo)},
			"2025-11-01", "2025-11-30", []string{"2025-11-03"}},
		{"unpublished year 2027 is plain weekends: Fri 10-01 due, Sat 10-02 not",
			[]dbmodels.ItemRuleVersion{hv(1, "2027-01-01", "FREQ=DAILY", wdo)},
			"2027-10-01", "2027-10-04", []string{"2027-10-01", "2027-10-04"}},
		{"unpublished year: workdays_only monthly Sat is postponed",
			[]dbmodels.ItemRuleVersion{hv(1, "2027-01-01", "FREQ=MONTHLY;BYMONTHDAY=2", wdo)},
			"2027-10-01", "2027-10-31", []string{"2027-10-04"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DueDatesWith(tc.versions, tc.from, tc.to, cal)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v\nwant %v", got, tc.want)
			}
		})
	}
}

func TestHolidayModeNilCalendarIsNone(t *testing.T) {
	v := []dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=DAILY", wdo)}
	got, err := DueDatesWith(v, "2025-10-01", "2025-10-03", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2025-10-01", "2025-10-02", "2025-10-03"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
	old, _ := DueDates(v, "2025-10-01", "2025-10-03")
	if !reflect.DeepEqual(old, want) {
		t.Errorf("DueDates got %v want %v", old, want)
	}
}

// INTERVAL phase must follow the unfiltered sequence. dtstart Fri 2025-09-26,
// every 3 days: 09-26, 09-29, 10-02, 10-05, 10-08, 10-11, 10-14, 10-17.
func TestHolidayModePhaseUnchanged(t *testing.T) {
	cal := cnCal(t)
	v := func(m dbmodels.HolidayMode) []dbmodels.ItemRuleVersion {
		x := hv(1, "2025-09-26", "FREQ=DAILY;INTERVAL=3", m)
		return []dbmodels.ItemRuleVersion{x}
	}
	tests := []struct {
		name     string
		mode     dbmodels.HolidayMode
		from, to string
		want     []string
	}{
		{"none", none, "2025-09-26", "2025-10-17",
			[]string{"2025-09-26", "2025-09-29", "2025-10-02", "2025-10-05", "2025-10-08", "2025-10-11", "2025-10-14", "2025-10-17"}},
		{"skip_holidays", skip, "2025-09-26", "2025-10-17",
			[]string{"2025-09-26", "2025-09-29", "2025-10-11", "2025-10-14", "2025-10-17"}},
		{"workdays_only", wdo, "2025-09-26", "2025-10-17",
			[]string{"2025-09-26", "2025-09-29", "2025-10-11", "2025-10-14", "2025-10-17"}},
		{"window starting mid-sequence keeps phase", wdo, "2025-10-09", "2025-10-17",
			[]string{"2025-10-11", "2025-10-14", "2025-10-17"}},
		{"single-day window on a phase day", wdo, "2025-10-14", "2025-10-14", []string{"2025-10-14"}},
		{"single-day window off phase", wdo, "2025-10-13", "2025-10-13", []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DueDatesWith(v(tc.mode), tc.from, tc.to, cal)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v\nwant %v", got, tc.want)
			}
		})
	}

	// Monthly INTERVAL=2 from 2025-09-01: raw 09-01, 11-01, 2026-01-01,
	// 2026-03-01. workdays_only: Sep 1 Mon stays; Nov 1 Sat -> 11-03; Jan 1
	// (holiday) -> Sun 2026-01-04 (makeup workday counts); Mar 1 Sun -> 03-02.
	m := []dbmodels.ItemRuleVersion{hv(1, "2025-09-01", "FREQ=MONTHLY;INTERVAL=2;BYMONTHDAY=1", wdo)}
	got, err := DueDatesWith(m, "2025-09-01", "2026-03-31", cal)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2025-09-01", "2025-11-03", "2026-01-04", "2026-03-02"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("monthly interval: got %v want %v", got, want)
	}
}

func TestHolidayModeVersionsAndSpans(t *testing.T) {
	cal := cnCal(t)

	t.Run("switching mode does not rewrite history", func(t *testing.T) {
		v1 := hv(1, "2025-09-01", "FREQ=DAILY", none)
		v2 := hv(2, "2025-10-01", "FREQ=DAILY", skip)
		before, err := DueDatesWith([]dbmodels.ItemRuleVersion{v1}, "2025-09-28", "2025-09-30", cal)
		if err != nil {
			t.Fatal(err)
		}
		after, err := DueDatesWith([]dbmodels.ItemRuleVersion{v1, v2}, "2025-09-28", "2025-09-30", cal)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Errorf("history changed: %v -> %v", before, after)
		}
		got, err := DueDatesWith([]dbmodels.ItemRuleVersion{v1, v2}, "2025-09-30", "2025-10-10", cal)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"2025-09-30", "2025-10-09", "2025-10-10"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v want %v", got, want)
		}
	})

	t.Run("each version uses its own mode", func(t *testing.T) {
		v1 := hv(1, "2025-09-01", "FREQ=DAILY", wdo)
		v2 := hv(2, "2025-09-29", "FREQ=DAILY", none)
		got, err := DueDatesWith([]dbmodels.ItemRuleVersion{v1, v2}, "2025-09-27", "2025-09-30", cal)
		if err != nil {
			t.Fatal(err)
		}
		// 09-27 Sat dropped by v1; 09-28 makeup Sun kept; v2 plain from 09-29.
		want := []string{"2025-09-28", "2025-09-29", "2025-09-30"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v want %v", got, want)
		}
	})

	t.Run("postponement past the span end is dropped", func(t *testing.T) {
		v1 := hv(1, "2025-09-01", "FREQ=MONTHLY;BYMONTHDAY=1", wdo)
		v2 := dbmodels.ItemRuleVersion{ID: 2, EffectiveFrom: "2025-10-05", ScheduleType: dbmodels.ScheduleNone}
		got, err := DueDatesWith([]dbmodels.ItemRuleVersion{v1, v2}, "2025-10-01", "2025-10-31", cal)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("got %v want none", got)
		}
	})

	t.Run("postponement past UNTIL is dropped, within UNTIL kept", func(t *testing.T) {
		for _, tc := range []struct {
			until string
			want  []string
		}{{"2025-10-05", []string{}}, {"2025-10-09", []string{"2025-10-09"}}, {"2025-10-31", []string{"2025-10-09"}}} {
			v := hv(1, "2025-09-01", "FREQ=MONTHLY;BYMONTHDAY=1", wdo)
			u := tc.until
			v.Until = &u
			got, err := DueDatesWith([]dbmodels.ItemRuleVersion{v}, "2025-10-01", "2025-10-31", cal)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("until %s: got %v want %v", tc.until, got, tc.want)
			}
		}
	})

	t.Run("previous span occurrence does not leak into a later span", func(t *testing.T) {
		// v1 monthly day 1 ends 2025-10-04 (v2 starts 10-05, daily workdays_only).
		v1 := hv(1, "2025-09-01", "FREQ=MONTHLY;BYMONTHDAY=1", wdo)
		v2 := hv(2, "2025-10-05", "FREQ=WEEKLY;BYDAY=MO", wdo)
		got, err := DueDatesWith([]dbmodels.ItemRuleVersion{v1, v2}, "2025-10-06", "2025-10-14", cal)
		if err != nil {
			t.Fatal(err)
		}
		// Mondays 10-06 (holiday) and 10-13; Tuesday 10-14 not a Monday.
		want := []string{"2025-10-13"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v want %v", got, want)
		}
	})
}

func TestIsDueWith(t *testing.T) {
	cal := cnCal(t)
	v := []dbmodels.ItemRuleVersion{hv(1, "2025-01-01", "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR", wdo)}
	for date, want := range map[string]bool{
		"2025-10-11": true, "2025-09-28": true, "2025-10-01": false, "2025-10-12": false, "2025-10-09": true,
	} {
		got, err := IsDueWith(v, date, cal)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("IsDueWith(%s) = %v want %v", date, got, want)
		}
	}
	if ok, _ := IsDue(v, "2025-10-11"); ok {
		t.Error("IsDue without calendar must not add makeup days")
	}
}

func TestValidateRuleHolidayMode(t *testing.T) {
	for _, m := range []dbmodels.HolidayMode{"", none, skip, wdo} {
		if err := ValidateRule(hv(1, "2025-01-01", "FREQ=DAILY", m)); err != nil {
			t.Errorf("mode %q: %v", m, err)
		}
	}
	for _, m := range []dbmodels.HolidayMode{"bogus", "SKIP_HOLIDAYS", "workdays"} {
		if err := ValidateRule(hv(1, "2025-01-01", "FREQ=DAILY", m)); !errors.Is(err, ErrInvalid) {
			t.Errorf("mode %q: got %v want ErrInvalid", m, err)
		}
	}
}

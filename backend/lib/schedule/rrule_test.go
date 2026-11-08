package schedule

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

func fv(id uint, eff, dtstart, rule string) dbmodels.ItemRuleVersion {
	return dbmodels.ItemRuleVersion{
		ID: id, EffectiveFrom: eff, DtStart: dtstart, RRuleString: rule,
		ScheduleType: dbmodels.ScheduleFixedRRule,
	}
}

func withUntil(v dbmodels.ItemRuleVersion, u string) dbmodels.ItemRuleVersion {
	v.Until = &u
	return v
}

func TestDueDates(t *testing.T) {
	tests := []struct {
		name     string
		versions []dbmodels.ItemRuleVersion
		from, to string
		want     []string
	}{
		{
			// Phase anchored at 2026-01-01: Jan 1,4,...,31, Feb 3,6,9.
			name:     "every 3 days, window starts mid-phase",
			versions: []dbmodels.ItemRuleVersion{fv(1, "2026-01-01", "2026-01-01", "FREQ=DAILY;INTERVAL=3")},
			from:     "2026-01-20", to: "2026-02-10",
			want: []string{"2026-01-22", "2026-01-25", "2026-01-28", "2026-01-31", "2026-02-03", "2026-02-06", "2026-02-09"},
		},
		{
			// Empty dtstart defaults to effective_from (2026-01-02): Jan 2,5,8.
			name:     "empty dtstart defaults to effective_from",
			versions: []dbmodels.ItemRuleVersion{fv(1, "2026-01-02", "", "FREQ=DAILY;INTERVAL=3")},
			from:     "2026-01-01", to: "2026-01-10",
			want: []string{"2026-01-02", "2026-01-05", "2026-01-08"},
		},
		{
			// 2026-03-02 is a Monday.
			name:     "weekly MO,WE,FR",
			versions: []dbmodels.ItemRuleVersion{fv(1, "2026-01-01", "2026-01-01", "FREQ=WEEKLY;BYDAY=MO,WE,FR")},
			from:     "2026-03-02", to: "2026-03-13",
			want: []string{"2026-03-02", "2026-03-04", "2026-03-06", "2026-03-09", "2026-03-11", "2026-03-13"},
		},
		{
			// 31st skips Feb, Apr, Jun.
			name:     "monthly day 31 skips short months",
			versions: []dbmodels.ItemRuleVersion{fv(1, "2026-01-01", "2026-01-01", "FREQ=MONTHLY;BYMONTHDAY=31")},
			from:     "2026-01-01", to: "2026-07-31",
			want: []string{"2026-01-31", "2026-03-31", "2026-05-31", "2026-07-31"},
		},
		{
			name:     "monthly last day incl. leap February 2028, cross-month",
			versions: []dbmodels.ItemRuleVersion{fv(1, "2028-01-01", "2028-01-01", "FREQ=MONTHLY;BYMONTHDAY=-1")},
			from:     "2028-01-15", to: "2028-04-30",
			want: []string{"2028-01-31", "2028-02-29", "2028-03-31", "2028-04-30"},
		},
		{
			name:     "monthly last day, non-leap February",
			versions: []dbmodels.ItemRuleVersion{fv(1, "2027-01-01", "2027-01-01", "FREQ=MONTHLY;BYMONTHDAY=-1")},
			from:     "2027-02-01", to: "2027-03-01",
			want: []string{"2027-02-28"},
		},
		{
			// Months Jan, Apr, Jul, Oct 2026; 2nd Sundays: Jan 11, Apr 12, Jul 12, Oct 11.
			name:     "INTERVAL=3 BYDAY=2SU",
			versions: []dbmodels.ItemRuleVersion{fv(1, "2026-01-01", "2026-01-01", "FREQ=MONTHLY;INTERVAL=3;BYDAY=2SU")},
			from:     "2026-01-01", to: "2026-12-31",
			want: []string{"2026-01-11", "2026-04-12", "2026-07-12", "2026-10-11"},
		},
		{
			// Last Fridays: Jan 30, Apr 24, Jul 31, Oct 30 2026.
			name:     "INTERVAL=3 BYDAY=-1FR",
			versions: []dbmodels.ItemRuleVersion{fv(1, "2026-01-01", "2026-01-01", "FREQ=MONTHLY;INTERVAL=3;BYDAY=-1FR")},
			from:     "2026-01-01", to: "2026-12-31",
			want: []string{"2026-01-30", "2026-04-24", "2026-07-31", "2026-10-30"},
		},
		{
			name:     "yearly Oct 1",
			versions: []dbmodels.ItemRuleVersion{fv(1, "2026-01-01", "2026-01-01", "FREQ=YEARLY;BYMONTH=10;BYMONTHDAY=1")},
			from:     "2026-01-01", to: "2028-12-31",
			want: []string{"2026-10-01", "2027-10-01", "2028-10-01"},
		},
		{
			name:     "until is inclusive (version until)",
			versions: []dbmodels.ItemRuleVersion{withUntil(fv(1, "2026-01-01", "2026-01-01", "FREQ=DAILY"), "2026-01-03")},
			from:     "2026-01-01", to: "2026-01-10",
			want: []string{"2026-01-01", "2026-01-02", "2026-01-03"},
		},
		{
			name:     "UNTIL inside rrule string is inclusive",
			versions: []dbmodels.ItemRuleVersion{fv(1, "2026-01-01", "2026-01-01", "FREQ=DAILY;UNTIL=20260102T000000Z")},
			from:     "2026-01-01", to: "2026-01-10",
			want: []string{"2026-01-01", "2026-01-02"},
		},
		{
			name:     "dtstart after window",
			versions: []dbmodels.ItemRuleVersion{fv(1, "2026-01-01", "2026-06-01", "FREQ=DAILY")},
			from:     "2026-05-01", to: "2026-05-31",
			want: []string{},
		},
		{
			name:     "dtstart inside window clips start",
			versions: []dbmodels.ItemRuleVersion{fv(1, "2026-01-01", "2026-01-05", "FREQ=DAILY")},
			from:     "2026-01-03", to: "2026-01-06",
			want: []string{"2026-01-05", "2026-01-06"},
		},
		{
			name:     "window before first effective_from",
			versions: []dbmodels.ItemRuleVersion{fv(1, "2026-06-01", "2026-06-01", "FREQ=DAILY")},
			from:     "2026-05-01", to: "2026-05-31",
			want: []string{},
		},
		{
			// Old: daily through Jan 3; new from Jan 4: Mondays (Jan 5, 12).
			name: "rule change mid-range",
			versions: []dbmodels.ItemRuleVersion{
				fv(1, "2026-01-01", "2026-01-01", "FREQ=DAILY"),
				fv(2, "2026-01-04", "2026-01-04", "FREQ=WEEKLY;BYDAY=MO"),
			},
			from: "2026-01-01", to: "2026-01-14",
			want: []string{"2026-01-01", "2026-01-02", "2026-01-03", "2026-01-05", "2026-01-12"},
		},
		{
			// Highest ID (3) wins on Jan 4: Sundays only (Jan 4 and 11 are Sundays).
			name: "same-day versions, highest id wins",
			versions: []dbmodels.ItemRuleVersion{
				fv(3, "2026-01-04", "2026-01-04", "FREQ=WEEKLY;BYDAY=SU"),
				fv(2, "2026-01-04", "2026-01-04", "FREQ=DAILY"),
			},
			from: "2026-01-01", to: "2026-01-12",
			want: []string{"2026-01-04", "2026-01-11"},
		},
		{
			// Old (every 2 days from Jan 1): Jan 1,3,5,7,9. New phase anchors on Jan 10: 10, 13, 16.
			name: "new version phase anchored on own dtstart",
			versions: []dbmodels.ItemRuleVersion{
				fv(1, "2026-01-01", "2026-01-01", "FREQ=DAILY;INTERVAL=2"),
				fv(2, "2026-01-10", "2026-01-10", "FREQ=DAILY;INTERVAL=3"),
			},
			from: "2026-01-08", to: "2026-01-16",
			want: []string{"2026-01-09", "2026-01-10", "2026-01-13", "2026-01-16"},
		},
		{
			name:     "from after to is empty",
			versions: []dbmodels.ItemRuleVersion{fv(1, "2026-01-01", "2026-01-01", "FREQ=DAILY")},
			from:     "2026-01-10", to: "2026-01-01",
			want: []string{},
		},
		{
			name:     "schedule type none has no due dates",
			versions: []dbmodels.ItemRuleVersion{{ID: 1, EffectiveFrom: "2026-01-01", ScheduleType: dbmodels.ScheduleNone}},
			from:     "2026-01-01", to: "2026-01-10",
			want: []string{},
		},
		{
			name:     "no versions",
			versions: nil,
			from:     "2026-01-01", to: "2026-01-10",
			want: []string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DueDates(tc.versions, tc.from, tc.to)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHistoryImmutableAfterRuleChange(t *testing.T) {
	old := fv(1, "2026-01-01", "2026-01-01", "FREQ=DAILY;INTERVAL=2")
	before, err := DueDates([]dbmodels.ItemRuleVersion{old}, "2026-01-01", "2026-02-14")
	if err != nil {
		t.Fatal(err)
	}
	// Change takes effect 2026-02-15; query only the past range.
	both := []dbmodels.ItemRuleVersion{old, fv(2, "2026-02-15", "2026-02-15", "FREQ=WEEKLY;BYDAY=TU")}
	after, err := DueDates(both, "2026-01-01", "2026-02-14")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("history changed:\nbefore %v\nafter  %v", before, after)
	}
	// Every 2 days from Jan 1: 1,3,...,31 (16 dates), Feb 2,4,...,14 (7 dates) = 23; last is Feb 14.
	if len(before) != 23 || before[0] != "2026-01-01" || before[22] != "2026-02-14" {
		t.Fatalf("unexpected baseline %v", before)
	}
	// New rule applies from the change date: Feb 17 is a Tuesday.
	got, err := DueDates(both, "2026-02-15", "2026-02-18")
	if err != nil || !reflect.DeepEqual(got, []string{"2026-02-17"}) {
		t.Fatalf("got %v err %v", got, err)
	}
}

func TestIsDue(t *testing.T) {
	vs := []dbmodels.ItemRuleVersion{fv(1, "2026-01-01", "2026-01-01", "FREQ=DAILY;INTERVAL=3")}
	for date, want := range map[string]bool{"2026-01-04": true, "2026-01-05": false, "2025-12-31": false} {
		got, err := IsDue(vs, date)
		if err != nil || got != want {
			t.Errorf("IsDue(%s) = %v, %v; want %v", date, got, err, want)
		}
	}
}

func TestUnsupportedTypes(t *testing.T) {
	for _, st := range []dbmodels.ScheduleType{dbmodels.ScheduleRolling, dbmodels.ScheduleQuota} {
		_, err := DueDates([]dbmodels.ItemRuleVersion{{ID: 1, EffectiveFrom: "2026-01-01", ScheduleType: st}}, "2026-01-01", "2026-01-31")
		if !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: err = %v, want ErrUnsupported", st, err)
		}
	}
	// Unsupported version outside the queried window: no error.
	vs := []dbmodels.ItemRuleVersion{
		fv(1, "2026-01-01", "2026-01-01", "FREQ=DAILY"),
		{ID: 2, EffectiveFrom: "2026-03-01", ScheduleType: dbmodels.ScheduleRolling},
	}
	if _, err := DueDates(vs, "2026-01-01", "2026-01-02"); err != nil {
		t.Errorf("unexpected err %v", err)
	}
}

func TestInvalidInputs(t *testing.T) {
	good := fv(1, "2026-01-01", "2026-01-01", "FREQ=DAILY")
	bad := []dbmodels.ItemRuleVersion{
		fv(1, "2026-01-01", "2026-01-01", "FREQ=SOMETIMES"),
		fv(1, "2026-01-01", "2026-01-01", "FREQ=MONTHLY;BYMONTHDAY=99"),
		fv(1, "2026-01-01", "2026-01-01", ""),
		fv(1, "2026-13-01", "", "FREQ=DAILY"),
		fv(1, "2026-01-01", "01/01/2026", "FREQ=DAILY"),
		withUntil(fv(1, "2026-01-01", "2026-01-10", "FREQ=DAILY"), "2026-01-05"),
		withUntil(fv(1, "2026-01-01", "", "FREQ=DAILY"), "bogus"),
	}
	for i, v := range bad {
		if err := ValidateRule(v); !errors.Is(err, ErrInvalid) {
			t.Errorf("case %d: ValidateRule = %v, want ErrInvalid", i, err)
		}
		if _, err := DueDates([]dbmodels.ItemRuleVersion{v}, "2026-01-01", "2026-01-31"); !errors.Is(err, ErrInvalid) {
			t.Errorf("case %d: DueDates err = %v, want ErrInvalid", i, err)
		}
	}
	if err := ValidateRule(good); err != nil {
		t.Errorf("good rule rejected: %v", err)
	}
	if err := ValidateRule(fv(1, "2026-01-01", "", "RRULE:FREQ=DAILY")); err != nil {
		t.Errorf("RRULE: prefix rejected: %v", err)
	}
	for _, r := range [][2]string{{"2026-1-1", "2026-01-02"}, {"2026-01-01", "x"}} {
		if _, err := DueDates([]dbmodels.ItemRuleVersion{good}, r[0], r[1]); !errors.Is(err, ErrInvalid) {
			t.Errorf("range %v: err = %v", r, err)
		}
	}
}

func TestToday(t *testing.T) {
	utc := func(y int, m time.Month, d, h, mi int) time.Time { return time.Date(y, m, d, h, mi, 0, 0, time.UTC) }
	tests := []struct {
		name string
		now  time.Time
		tz   string
		want string
	}{
		{"Shanghai crosses midnight (+8)", utc(2026, 11, 6, 16, 30), "Asia/Shanghai", "2026-11-07"},
		{"Shanghai just before midnight", utc(2026, 11, 6, 15, 59), "Asia/Shanghai", "2026-11-06"},
		{"UTC", utc(2026, 11, 6, 16, 30), "UTC", "2026-11-06"},
		// DST starts 2026-03-08 02:00 local; before it New York is UTC-5.
		{"New York before DST start", utc(2026, 3, 8, 4, 59), "America/New_York", "2026-03-07"},
		{"New York midnight Mar 8 (EST)", utc(2026, 3, 8, 5, 0), "America/New_York", "2026-03-08"},
		// After DST start, UTC-4.
		{"New York Mar 8 23:59 EDT", utc(2026, 3, 9, 3, 59), "America/New_York", "2026-03-08"},
		{"New York Mar 9 00:00 EDT", utc(2026, 3, 9, 4, 0), "America/New_York", "2026-03-09"},
		// DST ends 2026-11-01 02:00 local; before it UTC-4, after UTC-5.
		{"New York Oct 31 23:59 EDT", utc(2026, 11, 1, 3, 59), "America/New_York", "2026-10-31"},
		{"New York Nov 1 00:00 EDT", utc(2026, 11, 1, 4, 0), "America/New_York", "2026-11-01"},
		{"New York Nov 1 23:59 EST", utc(2026, 11, 2, 4, 59), "America/New_York", "2026-11-01"},
		{"New York Nov 2 00:00 EST", utc(2026, 11, 2, 5, 0), "America/New_York", "2026-11-02"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Today(tc.now, tc.tz)
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	for _, tz := range []string{"Mars/Olympus", ""} {
		if _, err := Today(time.Now(), tz); !errors.Is(err, ErrInvalid) {
			t.Errorf("tz %q: err = %v, want ErrInvalid", tz, err)
		}
	}
}

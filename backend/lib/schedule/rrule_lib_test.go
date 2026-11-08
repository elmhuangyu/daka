package schedule

import (
	"reflect"
	"testing"
	"time"

	"github.com/teambition/rrule-go"
)

// These tests pin the behaviour of github.com/teambition/rrule-go that the
// Daka schedule engine (DESIGN 3.2 / 3.4) depends on. Expected dates are
// derived by hand from the 2026-2032 calendar, not from library output.
// See RRULE_EVALUATION.md for the findings.

const dateLayout = "2006-01-02"

func utcDate(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func fmtDates(ts []time.Time) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Format(dateLayout))
	}
	return out
}

func mustRule(t *testing.T, s string) *rrule.RRule {
	t.Helper()
	r, err := rrule.StrToRRule(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return r
}

// TestLib_ExpansionTable covers the DESIGN 3.2 rule shapes, all with a UTC
// date-only dtstart, expanded over [from, to] inclusive.
func TestLib_ExpansionTable(t *testing.T) {
	cases := []struct {
		name     string
		rule     string
		from, to time.Time
		want     []string
	}{
		{
			name: "daily interval 3 anchored at dtstart",
			rule: "DTSTART:20260101T000000Z\nRRULE:FREQ=DAILY;INTERVAL=3",
			from: utcDate(2026, 1, 1), to: utcDate(2026, 1, 12),
			want: []string{"2026-01-01", "2026-01-04", "2026-01-07", "2026-01-10"},
		},
		{
			// Day offsets divisible by 3 from Jan 1: 30 = Jan 31, 33 = Feb 3,
			// 36 = Feb 6, 39 = Feb 9.
			name: "daily interval 3 phase preserved when window starts later",
			rule: "DTSTART:20260101T000000Z\nRRULE:FREQ=DAILY;INTERVAL=3",
			from: utcDate(2026, 2, 1), to: utcDate(2026, 2, 10),
			want: []string{"2026-02-03", "2026-02-06", "2026-02-09"},
		},
		{
			name: "monthly BYDAY=2SU",
			rule: "DTSTART:20260101T000000Z\nRRULE:FREQ=MONTHLY;BYDAY=2SU",
			from: utcDate(2026, 1, 1), to: utcDate(2026, 6, 30),
			want: []string{"2026-01-11", "2026-02-08", "2026-03-08", "2026-04-12", "2026-05-10", "2026-06-14"},
		},
		{
			name: "monthly BYDAY=-1FR last friday",
			rule: "DTSTART:20260101T000000Z\nRRULE:FREQ=MONTHLY;BYDAY=-1FR",
			from: utcDate(2026, 1, 1), to: utcDate(2026, 4, 30),
			want: []string{"2026-01-30", "2026-02-27", "2026-03-27", "2026-04-24"},
		},
		{
			name: "monthly BYMONTHDAY=-1 last day",
			rule: "DTSTART:20260101T000000Z\nRRULE:FREQ=MONTHLY;BYMONTHDAY=-1",
			from: utcDate(2026, 1, 1), to: utcDate(2026, 4, 30),
			want: []string{"2026-01-31", "2026-02-28", "2026-03-31", "2026-04-30"},
		},
		{
			name: "BYMONTHDAY=-1 in leap February",
			rule: "DTSTART:20280201T000000Z\nRRULE:FREQ=MONTHLY;BYMONTHDAY=-1",
			from: utcDate(2028, 2, 1), to: utcDate(2028, 3, 31),
			want: []string{"2028-02-29", "2028-03-31"},
		},
		{
			name: "monthly interval 3 BYDAY=2SU phase from dtstart month",
			rule: "DTSTART:20260101T000000Z\nRRULE:FREQ=MONTHLY;INTERVAL=3;BYDAY=2SU",
			from: utcDate(2026, 1, 1), to: utcDate(2026, 12, 31),
			want: []string{"2026-01-11", "2026-04-12", "2026-07-12", "2026-10-11"},
		},
		{
			// dtstart Jan 20 2026: January's 2nd Sunday (Jan 11) precedes dtstart so
			// it is not emitted; the phase stays anchored on January: Apr 12, Jul 12.
			name: "monthly interval 3 BYDAY=2SU dtstart after that month's 2nd Sunday",
			rule: "DTSTART:20260120T000000Z\nRRULE:FREQ=MONTHLY;INTERVAL=3;BYDAY=2SU",
			from: utcDate(2026, 1, 1), to: utcDate(2026, 8, 31),
			want: []string{"2026-04-12", "2026-07-12"},
		},
		{
			name: "BYMONTHDAY=31 skips short months",
			rule: "DTSTART:20260101T000000Z\nRRULE:FREQ=MONTHLY;BYMONTHDAY=31",
			from: utcDate(2026, 1, 1), to: utcDate(2026, 8, 31),
			want: []string{"2026-01-31", "2026-03-31", "2026-05-31", "2026-07-31", "2026-08-31"},
		},
		{
			name: "BYMONTHDAY=30 skips non-leap February",
			rule: "DTSTART:20260101T000000Z\nRRULE:FREQ=MONTHLY;BYMONTHDAY=30",
			from: utcDate(2026, 1, 1), to: utcDate(2026, 4, 30),
			want: []string{"2026-01-30", "2026-03-30", "2026-04-30"},
		},
		{
			name: "BYMONTHDAY=29 skips Feb in non-leap years, hits it in leap years",
			rule: "DTSTART:20270101T000000Z\nRRULE:FREQ=MONTHLY;BYMONTHDAY=29;BYMONTH=2",
			from: utcDate(2027, 1, 1), to: utcDate(2032, 12, 31),
			want: []string{"2028-02-29", "2032-02-29"},
		},
		{
			name: "weekly BYDAY=MO,WE,FR; dtstart (Thursday) itself is not emitted",
			rule: "DTSTART:20260101T000000Z\nRRULE:FREQ=WEEKLY;BYDAY=MO,WE,FR",
			from: utcDate(2026, 1, 1), to: utcDate(2026, 1, 14),
			want: []string{"2026-01-02", "2026-01-05", "2026-01-07", "2026-01-09", "2026-01-12", "2026-01-14"},
		},
		{
			// dtstart Sunday Jan 4 2026. WKST=MO: week Dec 29..Jan 4 holds Jan 4;
			// next period is 2 weeks later (Jan 12..18): Mon 12, Sun 18; then Jan 26.
			name: "weekly interval 2 BYDAY=MO,SU WKST=MO",
			rule: "DTSTART:20260104T000000Z\nRRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,SU;WKST=MO",
			from: utcDate(2026, 1, 1), to: utcDate(2026, 1, 31),
			want: []string{"2026-01-04", "2026-01-12", "2026-01-18", "2026-01-26"},
		},
		{
			// WKST=SU: week Jan 4..10 holds Sun 4 and Mon 5; skip a week;
			// Jan 18..24 holds Sun 18 and Mon 19.
			name: "weekly interval 2 BYDAY=MO,SU WKST=SU differs from WKST=MO",
			rule: "DTSTART:20260104T000000Z\nRRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,SU;WKST=SU",
			from: utcDate(2026, 1, 1), to: utcDate(2026, 1, 24),
			want: []string{"2026-01-04", "2026-01-05", "2026-01-18", "2026-01-19"},
		},
		{
			name: "yearly BYMONTH=10;BYMONTHDAY=1",
			rule: "DTSTART:20260101T000000Z\nRRULE:FREQ=YEARLY;BYMONTH=10;BYMONTHDAY=1",
			from: utcDate(2026, 1, 1), to: utcDate(2028, 12, 31),
			want: []string{"2026-10-01", "2027-10-01", "2028-10-01"},
		},
		{
			name: "UNTIL is inclusive (date-only UNTIL as UTC midnight)",
			rule: "DTSTART:20260101T000000Z\nRRULE:FREQ=DAILY;UNTIL=20260105T000000Z",
			from: utcDate(2026, 1, 1), to: utcDate(2026, 1, 31),
			want: []string{"2026-01-01", "2026-01-02", "2026-01-03", "2026-01-04", "2026-01-05"},
		},
		{
			name: "dtstart is the first occurrence when it matches the rule",
			rule: "DTSTART:20260105T000000Z\nRRULE:FREQ=DAILY;INTERVAL=7",
			from: utcDate(2026, 1, 1), to: utcDate(2026, 1, 20),
			want: []string{"2026-01-05", "2026-01-12", "2026-01-19"},
		},
		{
			name: "nothing is emitted before dtstart",
			rule: "DTSTART:20260110T000000Z\nRRULE:FREQ=DAILY",
			from: utcDate(2026, 1, 1), to: utcDate(2026, 1, 12),
			want: []string{"2026-01-10", "2026-01-11", "2026-01-12"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := mustRule(t, tc.rule)
			got := fmtDates(r.Between(tc.from, tc.to, true))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestLib_BetweenInclusivity pins the inc flag: true includes both bounds,
// false excludes both. The engine should pass inc=true with date-only bounds.
func TestLib_BetweenInclusivity(t *testing.T) {
	r := mustRule(t, "DTSTART:20260101T000000Z\nRRULE:FREQ=DAILY")
	from, to := utcDate(2026, 1, 3), utcDate(2026, 1, 5)

	if got, want := fmtDates(r.Between(from, to, true)), []string{"2026-01-03", "2026-01-04", "2026-01-05"}; !reflect.DeepEqual(got, want) {
		t.Errorf("inc=true: got %v, want %v", got, want)
	}
	if got, want := fmtDates(r.Between(from, to, false)), []string{"2026-01-04"}; !reflect.DeepEqual(got, want) {
		t.Errorf("inc=false: got %v, want %v", got, want)
	}
}

// TestLib_SingleDayMembership shows the "is this date due" check:
// Between(d, d, true) is non-empty iff d is an occurrence.
func TestLib_SingleDayMembership(t *testing.T) {
	r := mustRule(t, "DTSTART:20260101T000000Z\nRRULE:FREQ=DAILY;INTERVAL=3")
	cases := []struct {
		day time.Time
		due bool
	}{
		{utcDate(2026, 1, 1), true},
		{utcDate(2026, 1, 2), false},
		{utcDate(2026, 1, 4), true},
		{utcDate(2026, 2, 3), true},
		{utcDate(2026, 2, 4), false},
		{utcDate(2025, 12, 29), false}, // on phase but before dtstart
	}
	for _, c := range cases {
		got := len(r.Between(c.day, c.day, true)) > 0
		if got != c.due {
			t.Errorf("%s due=%v, want %v", c.day.Format(dateLayout), got, c.due)
		}
	}
}

// TestLib_NoDtstartDefaultsToNow documents the gotcha: without DTSTART the
// library silently anchors at time.Now(), making INTERVAL>1 non-deterministic.
// The engine must ALWAYS set Dtstart explicitly (DESIGN 3.4).
func TestLib_NoDtstartDefaultsToNow(t *testing.T) {
	before := time.Now().Add(-time.Minute)
	r := mustRule(t, "FREQ=DAILY;INTERVAL=3")
	dt := r.GetDTStart()
	if dt.Before(before) || dt.After(time.Now().Add(time.Minute)) {
		t.Fatalf("expected default dtstart near now, got %v", dt)
	}
}

// TestLib_ROptionDtstartEquivalentToString verifies that building the rule via
// ROption (what the engine would do with a DB date) matches the string form.
func TestLib_ROptionDtstartEquivalentToString(t *testing.T) {
	r, err := rrule.NewRRule(rrule.ROption{
		Freq:      rrule.MONTHLY,
		Interval:  3,
		Byweekday: []rrule.Weekday{rrule.SU.Nth(2)},
		Dtstart:   utcDate(2026, 1, 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	got := fmtDates(r.Between(utcDate(2026, 1, 1), utcDate(2026, 12, 31), true))
	want := []string{"2026-01-11", "2026-04-12", "2026-07-12", "2026-10-11"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestLib_Timezone documents the timezone behaviour. Daka stores local calendar
// dates; the recommended approach is to treat them as UTC-midnight values and
// compare only the YYYY-MM-DD part.
func TestLib_Timezone(t *testing.T) {
	sh, err := time.LoadLocation("Asia/Shanghai") // UTC+8, no DST
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}

	t.Run("utc date-only dtstart: output dates equal the stored calendar dates", func(t *testing.T) {
		r := mustRule(t, "DTSTART:20260101T000000Z\nRRULE:FREQ=DAILY;COUNT=3")
		got := fmtDates(r.All())
		want := []string{"2026-01-01", "2026-01-02", "2026-01-03"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("local-midnight dtstart keeps its location: local dates are right", func(t *testing.T) {
		r, err := rrule.NewRRule(rrule.ROption{
			Freq: rrule.DAILY, Count: 3,
			Dtstart: time.Date(2026, 1, 1, 0, 0, 0, 0, sh),
		})
		if err != nil {
			t.Fatal(err)
		}
		all := r.All()
		if got, want := fmtDates(all), []string{"2026-01-01", "2026-01-02", "2026-01-03"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("local format: got %v, want %v", got, want)
		}
		// Gotcha: the same instants viewed in UTC are one calendar day EARLIER.
		var utc []string
		for _, x := range all {
			utc = append(utc, x.UTC().Format(dateLayout))
		}
		if want := []string{"2025-12-31", "2026-01-01", "2026-01-02"}; !reflect.DeepEqual(utc, want) {
			t.Fatalf("utc view: got %v, want %v", utc, want)
		}
	})

	t.Run("local-midnight dtstart with UTC-midnight window bounds misses day one", func(t *testing.T) {
		// dtstart 2026-01-01T00:00+08 == 2025-12-31T16:00Z, so querying the single
		// UTC instant 2026-01-01T00:00Z finds nothing. Mixing zones is the failure
		// mode to avoid.
		r, err := rrule.NewRRule(rrule.ROption{
			Freq:    rrule.DAILY,
			Dtstart: time.Date(2026, 1, 1, 0, 0, 0, 0, sh),
		})
		if err != nil {
			t.Fatal(err)
		}
		got := r.Between(utcDate(2026, 1, 1), utcDate(2026, 1, 1), true)
		if len(got) != 0 {
			t.Fatalf("expected no occurrence at the UTC-midnight instant, got %v", got)
		}
	})

	t.Run("all-UTC date-only evaluation is independent of the user's timezone", func(t *testing.T) {
		r := mustRule(t, "DTSTART:20260101T000000Z\nRRULE:FREQ=MONTHLY;BYMONTHDAY=-1")
		got := fmtDates(r.Between(utcDate(2026, 2, 1), utcDate(2026, 2, 28), true))
		if want := []string{"2026-02-28"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
}

// TestLib_InvalidInput pins that bad rule strings are rejected at parse time,
// which the API layer can map to HTTP 400.
func TestLib_InvalidInput(t *testing.T) {
	for _, s := range []string{
		"FREQ=BOGUS",
		"FREQ=MONTHLY;BYDAY=XX",
		"FREQ=MONTHLY;BYMONTHDAY=99",
	} {
		if _, err := rrule.StrToRRule(s); err == nil {
			t.Errorf("expected error for %q", s)
		}
	}
}

package e2e

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

type calItem struct {
	ItemID uint   `json:"item_id"`
	Due    bool   `json:"due"`
	Status string `json:"status"`
}

type calDay struct {
	Date    string    `json:"date"`
	DayType string    `json:"day_type"`
	Makeup  bool      `json:"makeup_workday"`
	Items   []calItem `json:"items"`
}

type calView struct {
	Today            string   `json:"today"`
	HolidayPublished bool     `json:"holiday_published"`
	Days             []calDay `json:"days"`
}

func (c *client) calendar(start, end string) calView {
	c.t.Helper()
	q := url.Values{"start": {start}, "end": {end}}
	return jsonOf[calView](c.t, c.ok(200, "GET", "/api/view/calendar?"+q.Encode(), nil))
}

// dueDates returns the dates on which the item is due.
func (v calView) dueDates(id uint) map[string]bool {
	out := map[string]bool{}
	for _, d := range v.Days {
		for _, it := range d.Items {
			if it.ItemID == id && it.Due {
				out[d.Date] = true
			}
		}
	}
	return out
}

func fixedRule(rrule, mode string) obj {
	return obj{"schedule_type": "fixed_rrule", "rrule_string": rrule, "holiday_mode": mode, "dtstart": "2025-09-01"}
}

// F2 / A1 / A14 / A15: holiday and make-up-workday scenarios through the API.
// 2025 National Day: 10-01..10-08 off; make-up workdays Sun 09-28 and Sat 10-11.
func TestHolidayMakeupScenarios(t *testing.T) {
	s := startStack(t, "2025-09-01", stackOpts{})
	c := s.newClient()
	c.setCalendar(obj{"holiday_region": "CN"})

	const (
		weekdays = "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"
	)
	mk := func(name, rrule, mode string) uint {
		return c.createItem(obj{"name": name, "tracker_type": "boolean", "rule": fixedRule(rrule, mode)})
	}
	weekdaysWO := mk("weekdays workdays_only", weekdays, "workdays_only")
	dailyWO := mk("daily workdays_only", "FREQ=DAILY", "workdays_only")
	subsetWO := mk("tu,th workdays_only", "FREQ=WEEKLY;BYDAY=TU,TH", "workdays_only")
	weekendWO := mk("sa,su workdays_only", "FREQ=WEEKLY;BYDAY=SA,SU", "workdays_only")
	dailySkip := mk("daily skip_holidays", "FREQ=DAILY", "skip_holidays")
	dailyNone := mk("daily none", "FREQ=DAILY", "none")
	weekdaysNone := mk("weekdays none", weekdays, "none")
	monthlySkip := mk("monthly 1st skip", "FREQ=MONTHLY;BYMONTHDAY=1", "skip_holidays")
	monthlyWO := mk("monthly 1st workdays_only", "FREQ=MONTHLY;BYMONTHDAY=1", "workdays_only")
	bimonthlyWO := mk("bimonthly 1st workdays_only", "FREQ=MONTHLY;INTERVAL=2;BYMONTHDAY=1", "workdays_only")

	view := c.calendar("2025-09-22", "2025-11-09")
	require.True(t, view.HolidayPublished)

	day := func(date string) calDay {
		for _, d := range view.Days {
			if d.Date == date {
				return d
			}
		}
		t.Fatalf("day %s missing", date)
		return calDay{}
	}
	require.True(t, day("2025-09-28").Makeup, "Sunday 09-28 is a make-up workday")
	require.True(t, day("2025-10-11").Makeup, "Saturday 10-11 is a make-up workday")
	require.False(t, day("2025-09-27").Makeup)

	type row struct {
		name string
		id   uint
		due  []string // dates inside the checked probe set on which the item is due
	}
	probes := []string{
		"2025-09-26", // Fri, normal workday
		"2025-09-27", // Sat, normal weekend
		"2025-09-28", // Sun, make-up workday
		"2025-09-30", // Tue
		"2025-10-01", // Wed, statutory holiday
		"2025-10-02", // Thu, statutory holiday
		"2025-10-04", // Sat inside the holiday span
		"2025-10-06", // Mon, statutory holiday
		"2025-10-09", // Thu, first workday after the holiday
		"2025-10-10", // Fri
		"2025-10-11", // Sat, make-up workday
		"2025-10-12", // Sun
		"2025-11-01", // Sat, monthly rules (second month in the bimonthly phase)
		"2025-11-03", // Mon, postponed target of 11-01
	}
	rows := []row{
		// A1/A14: every-workday rules fire on make-up days, not on holidays.
		{"weekdays workdays_only", weekdaysWO, []string{"2025-09-26", "2025-09-28", "2025-09-30", "2025-10-09", "2025-10-10", "2025-10-11", "2025-11-03"}},
		{"daily workdays_only", dailyWO, []string{"2025-09-26", "2025-09-28", "2025-09-30", "2025-10-09", "2025-10-10", "2025-10-11", "2025-11-03"}},
		// Other BYDAY subsets: make-up days do not fire (A14); holidays are removed.
		{"tu,th workdays_only", subsetWO, []string{"2025-09-30", "2025-10-09"}},
		// SA,SU generates the make-up days itself, so they are kept (DESIGN
		// 3.4.1(2)); ordinary weekend days are non-workdays and are removed.
		{"sa,su workdays_only", weekendWO, []string{"2025-09-28", "2025-10-11"}},
		{"daily skip_holidays", dailySkip, []string{"2025-09-26", "2025-09-27", "2025-09-28", "2025-09-30", "2025-10-09", "2025-10-10", "2025-10-11", "2025-10-12", "2025-11-01", "2025-11-03"}},
		{"daily none", dailyNone, probes},
		{"weekdays none", weekdaysNone, []string{"2025-09-26", "2025-09-30", "2025-10-01", "2025-10-02", "2025-10-06", "2025-10-09", "2025-10-10", "2025-11-03"}},
		// A15: monthly rule on the 1st: skip_holidays drops 10-01, workdays_only postpones it.
		{"monthly skip", monthlySkip, []string{"2025-11-01"}},
		{"monthly workdays_only", monthlyWO, []string{"2025-10-09", "2025-11-03"}},
		// A15: INTERVAL phase is computed on the unfiltered series: Sep, Nov (not Oct).
		{"bimonthly workdays_only", bimonthlyWO, []string{"2025-11-03"}},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			due := view.dueDates(r.id)
			want := map[string]bool{}
			for _, d := range r.due {
				want[d] = true
			}
			for _, p := range probes {
				require.Equal(t, want[p], due[p], "%s due on %s", r.name, p)
			}
		})
	}

	// "today" agrees with the calendar when the clock reaches the make-up
	// Sunday, a holiday Wednesday and a normal weekend Saturday.
	for _, tc := range []struct {
		date string
		id   uint
		due  bool
	}{
		{"2025-09-28", weekdaysWO, true},
		{"2025-09-28", subsetWO, false},
		{"2025-10-01", weekdaysWO, false},
		{"2025-10-01", dailySkip, false},
		{"2025-09-27", weekdaysWO, false},
		{"2025-10-11", dailyWO, true},
	} {
		s.clk.Set(shanghaiNoon(tc.date))
		td := jsonOf[struct {
			Date  string `json:"date"`
			Items []struct {
				ID  uint `json:"id"`
				Due bool `json:"due"`
			} `json:"items"`
		}](t, c.ok(200, "GET", "/api/view/today", nil))
		require.Equal(t, tc.date, td.Date)
		got := false
		for _, it := range td.Items {
			if it.ID == tc.id {
				got = it.Due
			}
		}
		require.Equal(t, tc.due, got, fmt.Sprintf("today %s item %d", tc.date, tc.id))
	}

	// Switching the region off removes holiday awareness: the make-up Sunday
	// stops being a workday and the holiday Wednesday becomes due again.
	c.setCalendar(obj{"holiday_region": "US"})
	view = c.calendar("2025-09-27", "2025-10-02")
	require.False(t, view.dueDates(weekdaysWO)["2025-09-28"])
	require.True(t, view.dueDates(weekdaysWO)["2025-10-01"])
}

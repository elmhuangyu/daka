package e2e

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// streakOf fetches stats twice (the second read is served from the cache) and
// asserts both agree, so a stale entry can never hide behind a cache miss.
func (c *client) streakOf(id uint) (current, longest int, today string) {
	c.t.Helper()
	a, b := c.stats(id), c.stats(id)
	require.Equal(c.t, a, b)
	return a.Streak.Current, a.Streak.Longest, a.Today
}

func (c *client) addFreeze(start, end string) uint {
	c.t.Helper()
	r := c.ok(201, "POST", "/api/freezes", obj{"start_date": start, "end_date": end, "reason": "trip"})
	return uint(jsonOf[obj](c.t, r)["id"].(float64))
}

// F6 / C6: cached stats are invalidated by freezes, day changes, rule changes
// and settings changes. A stale value must never be served.
func TestStatsCacheInvalidation(t *testing.T) {
	s := startStack(t, "2025-09-01", stackOpts{})
	c := s.newClient()
	id := c.createItem(obj{"name": "Habit", "tracker_type": "boolean", "rule": fixedRule("FREQ=DAILY", "none")})
	for _, d := range []string{"2025-09-01", "2025-09-02", "2025-09-03"} { // 09-04 is missed
		s.clk.Set(shanghaiNoon(d))
		c.checkin(id, "")
	}
	s.clk.Set(shanghaiNoon("2025-09-05"))

	cur, longest, today := c.streakOf(id)
	require.Equal(t, []any{"2025-09-05", 0, 3}, []any{today, cur, longest}, "the 09-04 miss broke the streak")

	// Freeze creation: the missed day no longer counts; deletion restores it.
	fid := c.addFreeze("2025-09-04", "2025-09-04")
	cur, longest, _ = c.streakOf(id)
	require.Equal(t, []int{3, 3}, []int{cur, longest}, "streak survives the frozen day immediately")
	c.ok(200, "DELETE", fmt.Sprintf("/api/freezes/%d", fid), nil)
	cur, _, _ = c.streakOf(id)
	require.Equal(t, 0, cur, "deleting the freeze brings the miss back immediately")

	// Check-in writes show up at once.
	c.checkin(id, "")
	cur, _, _ = c.streakOf(id)
	require.Equal(t, 1, cur)

	// Day boundary without any write: 09-06 is missed once the clock moves to 09-07.
	s.clk.Set(shanghaiNoon("2025-09-06"))
	cur, _, today = c.streakOf(id)
	require.Equal(t, []any{"2025-09-06", 1}, []any{today, cur}, "today still open, yesterday done")
	s.clk.Set(shanghaiNoon("2025-09-07"))
	cur, _, today = c.streakOf(id)
	require.Equal(t, []any{"2025-09-07", 0}, []any{today, cur}, "a day with no check-in breaks the streak")
	// Just before and after local midnight (Asia/Shanghai = UTC+8).
	s.clk.Set(time.Date(2025, 9, 7, 15, 59, 0, 0, time.UTC))
	_, _, today = c.streakOf(id)
	require.Equal(t, "2025-09-07", today)
	s.clk.Set(time.Date(2025, 9, 7, 16, 0, 1, 0, time.UTC))
	_, _, today = c.streakOf(id)
	require.Equal(t, "2025-09-08", today)

	// Rule change: a retroactive edit to Mondays only makes the history valid
	// again (09-01 done, 09-08 still open), so the streak is recomputed.
	cur, _, _ = c.streakOf(id)
	require.Equal(t, 0, cur)
	c.ok(200, "PATCH", fmt.Sprintf("/api/items/%d", id), obj{"rule": obj{"rrule_string": "FREQ=WEEKLY;BYDAY=MO", "effective_from": "2025-09-01"}})
	cur, _, _ = c.streakOf(id)
	require.Equal(t, 1, cur, "rule change invalidates cached stats")

	// The streak is also correct when the rule version list changes without logs.
	c.ok(200, "PATCH", fmt.Sprintf("/api/items/%d", id), obj{"rule": obj{"rrule_string": "FREQ=DAILY", "effective_from": "2025-09-01"}})
	cur, _, _ = c.streakOf(id)
	require.Equal(t, 0, cur)
}

// F6: settings changes (holiday region, timezone) invalidate cached stats.
func TestStatsCacheInvalidatedBySettings(t *testing.T) {
	s := startStack(t, "2025-09-01", stackOpts{})
	c := s.newClient()
	c.setCalendar(obj{"holiday_region": "CN"})
	id := c.createItem(obj{"name": "Work habit", "tracker_type": "boolean",
		"rule": fixedRule("FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR", "workdays_only")})
	// Done on the make-up Sunday 09-28, 09-29, 09-30 and after the National Day holiday.
	for _, d := range []string{"2025-09-28", "2025-09-29", "2025-09-30", "2025-10-09", "2025-10-10"} {
		s.clk.Set(shanghaiNoon(d))
		c.checkin(id, "")
	}
	s.clk.Set(shanghaiNoon("2025-10-11")) // Saturday, a make-up workday, still open

	cur, _, _ := c.streakOf(id)
	require.Equal(t, 5, cur, "holiday days 10-01..10-08 are not due with region CN")

	c.setCalendar(obj{"holiday_region": "US"}) // no calendar: 10-01..10-03 and 10-06..10-08 become due and missed
	cur, _, _ = c.streakOf(id)
	require.Equal(t, 2, cur, "region change invalidates the cache")

	c.setCalendar(obj{"holiday_region": "CN"})
	cur, _, _ = c.streakOf(id)
	require.Equal(t, 5, cur)

	// Streak holiday freeze: with no calendar there are no holidays to freeze,
	// and toggling the flag must not serve a stale value either.
	c.setCalendar(obj{"holiday_region": "US", "streak_holiday_freeze": true})
	cur, _, _ = c.streakOf(id)
	require.Equal(t, 2, cur)
	c.setCalendar(obj{"streak_holiday_freeze": false})
	cur, _, _ = c.streakOf(id)
	require.Equal(t, 2, cur)

	// Timezone: at 13:00 UTC+8 on 10-11 it is still 10-10 in Los Angeles.
	c.setCalendar(obj{"holiday_region": "CN"})
	s.clk.Set(time.Date(2025, 10, 11, 5, 0, 0, 0, time.UTC))
	_, _, today := c.streakOf(id)
	require.Equal(t, "2025-10-11", today)
	c.setCalendar(obj{"timezone": "America/Los_Angeles"})
	_, _, today = c.streakOf(id)
	require.Equal(t, "2025-10-10", today, "timezone change invalidates the cache")
}

package e2e

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type statsView struct {
	Today  string `json:"today"`
	Streak struct {
		Applicable bool `json:"applicable"`
		Current    int  `json:"current"`
		Longest    int  `json:"longest"`
	} `json:"streak"`
	Counts struct {
		Total int `json:"total"`
	} `json:"counts"`
	CompletionRate90 *float64 `json:"completion_rate_90"`
}

func (c *client) stats(id uint) statsView {
	c.t.Helper()
	return jsonOf[statsView](c.t, c.ok(200, "GET", fmt.Sprintf("/api/items/%d/stats", id), nil))
}

func (c *client) checkin(id uint, date string) {
	c.t.Helper()
	body := obj{"item_id": id, "mode": "set"}
	if date != "" {
		body["date"] = date
	}
	c.ok(200, "POST", "/api/checkins", body)
}

type itemView struct {
	CurrentRule struct {
		RRuleString   string `json:"rrule_string"`
		HolidayMode   string `json:"holiday_mode"`
		EffectiveFrom string `json:"effective_from"`
	} `json:"current_rule"`
}

func (c *client) item(id uint) itemView {
	c.t.Helper()
	return jsonOf[itemView](c.t, c.ok(200, "GET", fmt.Sprintf("/api/items/%d", id), nil))
}

// F3 / A2 / A6: changing a rule never rewrites history; only a retroactive
// edit (explicit effective_from in the past) recalculates it.
func TestRuleVersioningHistory(t *testing.T) {
	s := startStack(t, "2025-09-01", stackOpts{})
	c := s.newClient()
	id := c.createItem(obj{"name": "Habit", "tracker_type": "boolean", "rule": fixedRule("FREQ=DAILY", "none")})

	// Check in 09-01..09-03, miss Thursday 09-04, check in 09-05..09-08.
	for _, d := range []string{"2025-09-01", "2025-09-02", "2025-09-03", "2025-09-05", "2025-09-06", "2025-09-07", "2025-09-08"} {
		s.clk.Set(shanghaiNoon(d))
		c.checkin(id, "")
	}
	s.clk.Set(shanghaiNoon("2025-09-09"))
	st := c.stats(id)
	require.True(t, st.Streak.Applicable)
	require.Equal(t, 4, st.Streak.Current, "09-05..09-08")
	require.Equal(t, 4, st.Streak.Longest)
	require.Equal(t, 7, st.Counts.Total)

	// A2: from today (Tue 09-09) the item is only due on Mondays.
	c.ok(200, "PATCH", fmt.Sprintf("/api/items/%d", id), obj{"rule": obj{"rrule_string": "FREQ=WEEKLY;BYDAY=MO"}})
	require.Equal(t, "FREQ=WEEKLY;BYDAY=MO", c.item(id).CurrentRule.RRuleString)
	require.Equal(t, "2025-09-09", c.item(id).CurrentRule.EffectiveFrom)

	view := c.calendar("2025-09-01", "2025-09-22")
	due := view.dueDates(id)
	for d := 1; d <= 8; d++ { // history keeps the daily rule, including the missed 09-04
		require.True(t, due[fmt.Sprintf("2025-09-%02d", d)], "history 09-%02d stays due", d)
	}
	require.False(t, due["2025-09-09"])
	require.False(t, due["2025-09-12"])
	require.True(t, due["2025-09-15"])
	require.True(t, due["2025-09-22"])
	status := map[string]string{}
	for _, d := range view.Days {
		for _, it := range d.Items {
			if it.ItemID == id {
				status[d.Date] = it.Status
			}
		}
	}
	require.NotEqual(t, status["2025-09-03"], status["2025-09-04"], "the old miss is still a miss")
	st = c.stats(id)
	require.Equal(t, 4, st.Streak.Current, "history and the streak are untouched by the new rule")
	require.Equal(t, 4, st.Streak.Longest)

	// A6: a version effective in the future does not become current early; the
	// current rule is the latest version with effective_from <= today.
	c.ok(200, "PATCH", fmt.Sprintf("/api/items/%d", id), obj{"rule": obj{"rrule_string": "FREQ=WEEKLY;BYDAY=FR", "effective_from": "2025-09-19"}})
	require.Equal(t, "FREQ=WEEKLY;BYDAY=MO", c.item(id).CurrentRule.RRuleString)
	due = c.calendar("2025-09-09", "2025-09-30").dueDates(id)
	require.True(t, due["2025-09-15"])
	require.False(t, due["2025-09-17"])
	require.False(t, due["2025-09-22"], "Mondays stop with the new version")
	require.True(t, due["2025-09-19"])
	require.True(t, due["2025-09-26"])
	s.clk.Set(shanghaiNoon("2025-09-19"))
	require.Equal(t, "FREQ=WEEKLY;BYDAY=FR", c.item(id).CurrentRule.RRuleString)
	require.Equal(t, "2025-09-19", c.item(id).CurrentRule.EffectiveFrom)

	// Retroactive edit: from 09-01 the item is only due on Mondays, so the
	// Thursday miss disappears and the history is recalculated.
	c.ok(200, "PATCH", fmt.Sprintf("/api/items/%d", id), obj{"rule": obj{"rrule_string": "FREQ=WEEKLY;BYDAY=MO", "effective_from": "2025-09-01"}})
	due = c.calendar("2025-09-01", "2025-09-08").dueDates(id)
	require.True(t, due["2025-09-01"])
	require.True(t, due["2025-09-08"])
	require.False(t, due["2025-09-04"])
	require.False(t, due["2025-09-02"])
}

// Display-only edits (name) do not create rule versions and do not alter history.
func TestDisplayEditKeepsRuleVersion(t *testing.T) {
	s := startStack(t, "2025-09-01", stackOpts{})
	c := s.newClient()
	id := c.createItem(obj{"name": "Before", "tracker_type": "boolean", "rule": fixedRule("FREQ=DAILY", "none")})
	before := c.item(id).CurrentRule
	s.clk.Advance(48 * time.Hour)
	c.ok(200, "PATCH", fmt.Sprintf("/api/items/%d", id), obj{"name": "After"})
	require.Equal(t, before, c.item(id).CurrentRule, "a rename must not add a rule version")
}

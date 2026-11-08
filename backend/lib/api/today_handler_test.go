package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

func (e *env) createItem(user string, body m) (int, itemResponse) {
	e.t.Helper()
	w := e.do("POST", "/api/items", user, "", body)
	var it itemResponse
	if w.Code == 201 {
		require.NoError(e.t, json.Unmarshal(w.Body.Bytes(), &it))
	}
	return w.Code, it
}

func rruleItem(name, rrule string) m {
	return m{"name": name, "tracker_type": "boolean", "rule": m{"schedule_type": "fixed_rrule", "rrule_string": rrule, "holiday_mode": "none"}}
}

func (e *env) today(user, query string) todayResponse {
	e.t.Helper()
	w := e.do("GET", "/api/view/today"+query, user, "", nil)
	require.Equal(e.t, 200, w.Code, w.Body.String())
	var r todayResponse
	require.NoError(e.t, json.Unmarshal(w.Body.Bytes(), &r))
	return r
}

func TestCreateItemDefaults(t *testing.T) {
	e := newEnv(t)
	code, it := e.createItem("", rruleItem("Water plants", "FREQ=DAILY;INTERVAL=3"))
	require.Equal(t, 201, code)
	require.Equal(t, "habit", it.Kind)
	require.Equal(t, "positive", it.Polarity)
	require.Equal(t, "2026-11-07", it.CurrentRule.DtStart)
	require.Equal(t, "2026-11-07", it.CurrentRule.EffectiveFrom)
	require.Equal(t, "none", it.CurrentRule.HolidayMode)
	var n int64
	require.NoError(t, e.db.Write().Model(&dbmodels.ItemRuleVersion{}).Where("item_id = ?", it.ID).Count(&n).Error)
	require.EqualValues(t, 1, n)
}

func TestCreateItemRejects(t *testing.T) {
	rule := func(mut func(m)) m {
		b := rruleItem("Secret Name", "FREQ=DAILY")
		if mut != nil {
			mut(b)
		}
		return b
	}
	ruleOf := func(b m) m { return b["rule"].(m) }
	tests := []struct {
		name string
		body any
		want string
	}{
		{"empty name", rule(func(b m) { b["name"] = "  " }), "name is required"},
		{"long name", rule(func(b m) { b["name"] = strings.Repeat("x", 201) }), "name too long"},
		{"negative", rule(func(b m) { b["polarity"] = "negative" }), "negative_schedule_not_allowed"},
		{"rolling", rule(func(b m) { ruleOf(b)["schedule_type"] = "rolling" }), "foreign_rrule_fields"},
		{"bad kind", rule(func(b m) { b["kind"] = "x" }), "unknown_kind"},
		{"hourly", rule(func(b m) { ruleOf(b)["rrule_string"] = "FREQ=HOURLY" }), "invalid_rule"},
		{"interval zero", rule(func(b m) { ruleOf(b)["rrule_string"] = "FREQ=DAILY;INTERVAL=0" }), "invalid_rule"},
		{"bad dtstart", rule(func(b m) { ruleOf(b)["dtstart"] = "07/11/2026" }), "invalid_rule"},
		{"foreign category", rule(func(b m) { b["category_id"] = 9999 }), "invalid category"},
		{"bad json", "{", "invalid JSON body"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			w := e.do("POST", "/api/items", "", "", tc.body)
			require.Equal(t, 400, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), tc.want)
			require.NotContains(t, w.Body.String(), "Secret Name")
			var n int64
			require.NoError(t, e.db.Write().Model(&dbmodels.Item{}).Count(&n).Error)
			require.Zero(t, n)
		})
	}
}

func TestCreateItemCategoryOfOtherUser(t *testing.T) {
	e := newEnv(t)
	cat := dbmodels.Category{UserID: "u2", Name: "c"}
	require.NoError(t, e.db.Write().Create(&cat).Error)
	b := rruleItem("x", "FREQ=DAILY")
	b["category_id"] = cat.ID
	code, _ := e.createItem("u1", b)
	require.Equal(t, 400, code)
}

func TestTodayIntervalThreeLifecycle(t *testing.T) {
	e := newEnv(t)
	code, it := e.createItem("", rruleItem("Water plants", "FREQ=DAILY;INTERVAL=3"))
	require.Equal(t, 201, code)

	tests := []struct {
		date string
		due  bool
	}{
		{"2026-11-07", true}, {"2026-11-08", false}, {"2026-11-09", false},
		{"2026-11-10", true}, {"2026-11-11", false}, {"2026-11-13", true},
	}
	for _, tc := range tests {
		t.Run(tc.date, func(t *testing.T) {
			d, err := time.ParseInLocation(dbmodels.DateLayout, tc.date, time.UTC)
			require.NoError(t, err)
			// 00:30 local (UTC+8) on the date.
			e.now = d.Add(-8*time.Hour + 30*time.Minute)
			td := e.today("", "")
			require.Equal(t, tc.date, td.Date)
			if !tc.due {
				require.Empty(t, td.Items)
				require.Equal(t, todayProgress{}, td.Progress)
				return
			}
			require.Len(t, td.Items, 1)
			require.Equal(t, it.ID, td.Items[0].ID)
			require.True(t, td.Items[0].Due)
			require.False(t, td.Items[0].Done)
			require.Equal(t, []uint{}, td.Items[0].LogIDs)
			require.Equal(t, todayProgress{Done: 0, Total: 1}, td.Progress)

			w := e.do("POST", "/api/checkins", "", "", m{"item_id": it.ID, "mode": "set"})
			require.Equal(t, 200, w.Code, w.Body.String())
			var ck checkinResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ck))
			td = e.today("", "")
			require.True(t, td.Items[0].Done)
			require.Equal(t, []uint{ck.ID}, td.Items[0].LogIDs)
			require.Equal(t, todayProgress{Done: 1, Total: 1}, td.Progress)

			w = e.do("DELETE", fmt.Sprintf("/api/checkins/%d", ck.ID), "", "", nil)
			require.Equal(t, 200, w.Code)
			td = e.today("", "")
			require.False(t, td.Items[0].Done)
			require.Equal(t, []uint{}, td.Items[0].LogIDs)
			require.Equal(t, todayProgress{Done: 0, Total: 1}, td.Progress)

			w = e.do("POST", "/api/checkins", "", "", m{"item_id": it.ID, "mode": "set"})
			require.Equal(t, 200, w.Code)
			w = e.do("POST", "/api/checkins", "", "", m{"item_id": it.ID, "mode": "unset"})
			require.Equal(t, 200, w.Code)
			require.False(t, e.today("", "").Items[0].Done)
		})
	}
}

func TestTodayTimezoneBoundary(t *testing.T) {
	e := newEnv(t)
	e.createItem("", rruleItem("x", "FREQ=DAILY"))
	e.now = time.Date(2026, 11, 6, 16, 30, 0, 0, time.UTC)
	td := e.today("", "")
	require.Equal(t, "2026-11-07", td.Date)
	require.Equal(t, 6, td.Weekday) // Saturday
	require.Equal(t, "Asia/Shanghai", td.Timezone)
	// u2 shares the timezone; switch it to a zone still on 11-06.
	require.NoError(t, e.db.Write().Model(&dbmodels.UserSettings{}).Where("user_id = ?", "u1").Update("timezone", "America/New_York").Error)
	td = e.today("", "")
	require.Equal(t, "2026-11-06", td.Date)
	require.Equal(t, 5, td.Weekday)
	require.Empty(t, td.Items, "rule effective from 2026-11-07 has no version for 11-06")
}

func TestTodayIsolationArchivedOrderProgress(t *testing.T) {
	e := newEnv(t)
	_, a := e.createItem("u1", rruleItem("A", "FREQ=DAILY"))
	_, b := e.createItem("u1", rruleItem("B", "FREQ=DAILY"))
	_, arch := e.createItem("u1", rruleItem("Archived", "FREQ=DAILY"))
	_, other := e.createItem("u2", rruleItem("Other", "FREQ=DAILY"))
	_, ev := e.createItem("u1", func() m { x := rruleItem("Event", "FREQ=DAILY"); x["kind"] = "event"; return x }())
	require.NoError(t, e.db.Write().Model(&dbmodels.Item{}).Where("id = ?", arch.ID).Update("archived", true).Error)
	require.NoError(t, e.db.Write().Model(&dbmodels.Item{}).Where("id = ?", a.ID).Update("sort_order", 5).Error)
	// A non-due item does not count.
	e.createItem("u1", rruleItem("Weekly other day", "FREQ=WEEKLY;BYDAY=MO")) // 2026-11-07 is Saturday

	td := e.today("u1", "")
	var got []uint
	for _, it := range td.Items {
		got = append(got, it.ID)
		require.NotEqual(t, other.ID, it.ID)
	}
	require.Equal(t, []uint{b.ID, ev.ID, a.ID}, got, "sorted by sort_order then id; archived and other user excluded")
	require.Equal(t, todayProgress{Done: 0, Total: 2}, td.Progress, "event does not count in the ring")

	require.Equal(t, 200, e.do("POST", "/api/checkins", "u1", "", m{"item_id": ev.ID, "mode": "set"}).Code)
	require.Equal(t, 200, e.do("POST", "/api/checkins", "u1", "", m{"item_id": a.ID, "mode": "set"}).Code)
	td = e.today("u1", "")
	require.Equal(t, todayProgress{Done: 1, Total: 2}, td.Progress)

	// u2 cannot see u1's logs.
	td2 := e.today("u2", "")
	require.Len(t, td2.Items, 1)
	require.False(t, td2.Items[0].Done)
}

func TestTodayRedact(t *testing.T) {
	e := newEnv(t)
	cat := dbmodels.Category{UserID: "u1", Name: "Health", IsSensitive: true}
	require.NoError(t, e.db.Write().Create(&cat).Error)
	plainCat := dbmodels.Category{UserID: "u1", Name: "Plain"}
	require.NoError(t, e.db.Write().Create(&plainCat).Error)

	flag := rruleItem("Flagged", "FREQ=DAILY")
	flag["is_sensitive"] = true
	inCat := rruleItem("InSensitiveCat", "FREQ=DAILY")
	inCat["category_id"] = cat.ID
	plain := rruleItem("Plain", "FREQ=DAILY")
	plain["category_id"] = plainCat.ID
	for _, b := range []m{flag, inCat, plain} {
		code, _ := e.createItem("u1", b)
		require.Equal(t, 201, code)
	}

	names := func(q string) []string {
		var out []string
		for _, it := range e.today("u1", q).Items {
			out = append(out, it.Name)
		}
		return out
	}
	require.Equal(t, []string{"Flagged", "InSensitiveCat", "Plain"}, names(""))
	require.Equal(t, []string{"Flagged", "InSensitiveCat", "Plain"}, names("?redact=0"))
	require.Equal(t, []string{"Private item", "Private item", "Plain"}, names("?redact=1"))

	td := e.today("u1", "?redact=1")
	require.True(t, td.Items[0].IsSensitive)
	require.Equal(t, 3, td.Progress.Total)
	require.Equal(t, 400, e.do("GET", "/api/view/today?redact=maybe", "u1", "", nil).Code)
}

func TestCreateItemInheritsSensitiveCategory(t *testing.T) {
	e := newEnv(t)
	cat := dbmodels.Category{UserID: "u1", Name: "Health", IsSensitive: true}
	require.NoError(t, e.db.Write().Create(&cat).Error)
	b := rruleItem("x", "FREQ=DAILY")
	b["category_id"] = cat.ID
	b["is_sensitive"] = false
	code, it := e.createItem("u1", b)
	require.Equal(t, 201, code)
	require.True(t, it.IsSensitive)
	var stored dbmodels.Item
	require.NoError(t, e.db.Write().First(&stored, it.ID).Error)
	require.True(t, stored.IsSensitive)
}

func TestUnknownAPIPathIsJSON404(t *testing.T) {
	e := newEnv(t)
	w := e.do("GET", "/api/nope", "", "", nil)
	require.Equal(t, 404, w.Code)
	require.JSONEq(t, `{"error":"not found"}`, w.Body.String())
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
}

func TestTodayEmptyItemsIsArray(t *testing.T) {
	e := newEnv(t)
	w := e.do("GET", "/api/view/today", "", "", nil)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"items":[]`)
}

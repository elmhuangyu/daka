package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
	"github.com/elmhuangyu/daka/backend/lib/stats"
)

// statsEnv is an env whose router shares a stats cache the test can inspect.
func statsEnv(t *testing.T, cal func(dbmodels.HolidayRegion) schedule.DayCalendar) (*env, *stats.Cache) {
	t.Helper()
	e := newEnv(t)
	cache := stats.NewCache(100)
	e.h = NewRouter(Deps{DB: e.db, Now: func() time.Time { return e.now }, Auth: userHeaderAuth,
		StatsCache: cache, Calendar: cal})
	return e, cache
}

// dailyItem creates a boolean item with a daily fixed_rrule rule from 2026-11-01.
func (e *env) dailyItem(user string, mut func(*dbmodels.Item)) uint {
	e.t.Helper()
	id := e.item(user, mut)
	require.NoError(e.t, e.db.Write().Where("item_id = ?", id).Delete(&dbmodels.ItemRuleVersion{}).Error)
	require.NoError(e.t, e.db.Write().Create(&dbmodels.ItemRuleVersion{ItemID: id, EffectiveFrom: "2026-11-01",
		ScheduleType: dbmodels.ScheduleFixedRRule, DtStart: "2026-11-01", RRuleString: "FREQ=DAILY",
		HolidayMode: dbmodels.HolidayNone}).Error)
	return id
}

func (e *env) checkin(id uint, date string) *httptest.ResponseRecorder {
	e.t.Helper()
	w := e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "set", "date": date})
	require.Equal(e.t, 200, w.Code, w.Body.String())
	return w
}

func (e *env) stats(id uint, query string) statsResponse {
	e.t.Helper()
	w := e.do("GET", fmt.Sprintf("/api/items/%d/stats%s", id, query), "", "", nil)
	require.Equal(e.t, 200, w.Code, w.Body.String())
	var r statsResponse
	require.NoError(e.t, json.Unmarshal(w.Body.Bytes(), &r))
	return r
}

func TestStatsEndpointBasics(t *testing.T) {
	e, _ := statsEnv(t, nil)
	id := e.dailyItem("u1", nil)
	for _, d := range []string{"2026-11-05", "2026-11-06", "2026-11-07"} {
		e.checkin(id, d)
	}
	r := e.stats(id, "")
	require.Equal(t, "Secret Medication", r.Name)
	require.Equal(t, 3, r.Counts.Total)
	require.Equal(t, "2026-11-07", *r.LastDate)
	require.Equal(t, 0, *r.DaysSinceLast)
	require.True(t, r.Streak.Applicable)
	require.Equal(t, 3, r.Streak.Current)
	require.Len(t, r.Heatmap, 365)
	require.Equal(t, "2026-11-07", r.Today)

	require.Equal(t, 404, e.do("GET", fmt.Sprintf("/api/items/%d/stats", id), "u2", "", nil).Code, "other user's item")
	require.Equal(t, 404, e.do("GET", "/api/items/9999/stats", "", "", nil).Code)
	require.Equal(t, 400, e.do("GET", "/api/items/abc/stats", "", "", nil).Code)
	require.Equal(t, 400, e.do("GET", fmt.Sprintf("/api/items/%d/stats?redact=maybe", id), "", "", nil).Code)
}

func TestStatsRedact(t *testing.T) {
	e, _ := statsEnv(t, nil)
	sens := e.dailyItem("u1", func(it *dbmodels.Item) { it.IsSensitive = true })
	plain := e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Water" })
	cat := dbmodels.Category{UserID: "u1", Name: "Health", IsSensitive: true}
	require.NoError(t, e.db.Write().Create(&cat).Error)
	inCat := e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Pills"; it.CategoryID = &cat.ID })

	require.Equal(t, "Private item", e.stats(sens, "?redact=1").Name)
	require.Equal(t, "Secret Medication", e.stats(sens, "?redact=0").Name)
	require.Equal(t, "Secret Medication", e.stats(sens, "").Name)
	require.Equal(t, "Water", e.stats(plain, "?redact=1").Name)
	require.Equal(t, "Private item", e.stats(inCat, "?redact=1").Name)
	// the redacted response never leaks the name, even from the cache
	require.NotContains(t, e.do("GET", fmt.Sprintf("/api/items/%d/stats?redact=1", sens), "", "", nil).Body.String(), "Secret")
}

// The cache can never serve a stale value (DESIGN 8 derived value cache).
func TestStatsCacheInvalidation(t *testing.T) {
	e, cache := statsEnv(t, nil)
	id := e.dailyItem("u1", nil)
	for _, d := range []string{"2026-11-01", "2026-11-02", "2026-11-03", "2026-11-05", "2026-11-06"} {
		e.checkin(id, d)
	}

	// expectMiss/expectHit assert on the counters around one GET.
	get := func(wantHit bool, why string) statsResponse {
		t.Helper()
		h0, m0 := cache.Hits(), cache.Misses()
		r := e.stats(id, "")
		if wantHit {
			require.EqualValues(t, h0+1, cache.Hits(), "%s: expected a cache hit", why)
			require.EqualValues(t, m0, cache.Misses(), why)
		} else {
			require.EqualValues(t, m0+1, cache.Misses(), "%s: expected a cache miss", why)
			require.EqualValues(t, h0, cache.Hits(), why)
		}
		return r
	}

	r := get(false, "first read")
	require.Equal(t, 2, r.Streak.Current)
	require.Equal(t, 5, r.Counts.Total)
	get(true, "repeat read")

	// new log through the API
	e.checkin(id, "2026-11-04")
	r = get(false, "new log")
	require.Equal(t, 6, r.Streak.Current)
	get(true, "repeat read")

	// deleted log through DELETE /api/checkins/{id}
	var l dbmodels.CheckinLog
	require.NoError(t, e.db.Write().Where("item_id = ? AND checkin_date = ?", id, "2026-11-04").Take(&l).Error)
	require.Equal(t, 200, e.do("DELETE", fmt.Sprintf("/api/checkins/%d", l.ID), "", "", nil).Code)
	r = get(false, "deleted log")
	require.Equal(t, 2, r.Streak.Current)
	require.Equal(t, 5, r.Counts.Total)
	get(true, "repeat read")

	// a write from a code path that forgets to Bump still misses (DB signature)
	require.NoError(t, e.db.Write().Create(&dbmodels.CheckinLog{UserID: "u1", ItemID: id, CheckinDate: "2026-11-04",
		CheckinTime: time.Now().UTC()}).Error)
	r = get(false, "direct DB insert")
	require.Equal(t, 6, r.Streak.Current)
	require.NoError(t, e.db.Write().Where("item_id = ? AND checkin_date = ?", id, "2026-11-04").Delete(&dbmodels.CheckinLog{}).Error)
	// (SQLite reuses the freed id, so the state equals an already cached one: a
	// hit on identical data is correct.)
	r = get(true, "direct DB delete back to a previously cached state")
	require.Equal(t, 2, r.Streak.Current)

	// rule version change (history stays judged by the old version)
	require.NoError(t, e.db.Write().Create(&dbmodels.ItemRuleVersion{ItemID: id, EffectiveFrom: "2026-11-07",
		ScheduleType: dbmodels.ScheduleFixedRRule, DtStart: "2026-11-07", RRuleString: "FREQ=WEEKLY;BYDAY=MO",
		HolidayMode: dbmodels.HolidayNone}).Error)
	r = get(false, "rule version")
	require.Equal(t, 2, r.Streak.Current)
	get(true, "repeat read")

	// adding a FreezePeriod immediately changes the streak (C6)
	w := e.do("POST", "/api/freezes", "", "", m{"start_date": "2026-11-04", "end_date": "2026-11-04", "reason": "sick"})
	require.Equal(t, 201, w.Code, w.Body.String())
	var fz freezeResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fz))
	r = get(false, "freeze created")
	require.Equal(t, 5, r.Streak.Current)
	get(true, "repeat read")

	// deleting it restores the old result
	require.Equal(t, 200, e.do("DELETE", fmt.Sprintf("/api/freezes/%d", fz.ID), "", "", nil).Code)
	r = get(false, "freeze deleted")
	require.Equal(t, 2, r.Streak.Current)
	get(true, "repeat read")

	// a state_version bump alone (settings/freeze change elsewhere) invalidates
	require.NoError(t, dbmodels.BumpStateVersion(e.db.Write(), "u1"))
	get(false, "state_version bump")
	get(true, "repeat read")

	// settings changes miss even when written without a bump
	for _, upd := range []map[string]any{{"timezone": "UTC"}, {"week_start": 0}, {"holiday_region": "CN"}, {"streak_holiday_freeze": true}} {
		require.NoError(t, e.db.Write().Model(&dbmodels.UserSettings{}).Where("user_id = ?", "u1").Updates(upd).Error)
		get(false, fmt.Sprintf("settings %v", upd))
		get(true, "repeat read")
	}
}

// With no new logs the streak breaks once the day passes (today is in the key).
func TestStatsStreakBreaksWhenDayPasses(t *testing.T) {
	e, cache := statsEnv(t, nil)
	id := e.dailyItem("u1", nil)
	e.checkin(id, "2026-11-06")
	e.checkin(id, "2026-11-07")
	r := e.stats(id, "")
	require.Equal(t, 2, r.Streak.Current)
	require.Equal(t, 2, e.stats(id, "").Streak.Current)
	require.EqualValues(t, 1, cache.Hits())

	e.now = e.now.Add(24 * time.Hour) // 2026-11-08 in Asia/Shanghai
	r = e.stats(id, "")
	require.Equal(t, "2026-11-08", r.Today)
	require.Equal(t, 2, r.Streak.Current, "today still pending")
	e.now = e.now.Add(24 * time.Hour) // 11-08 passed without a log
	r = e.stats(id, "")
	require.Equal(t, 0, r.Streak.Current)
	require.Equal(t, 2, r.Streak.Longest)
}

type fakeCal struct{ holidays map[string]bool }

func (f fakeCal) IsStatutoryHoliday(d string) bool { return f.holidays[d] }
func (f fakeCal) IsWorkday(d string) bool          { return !f.holidays[d] }
func (f fakeCal) IsMakeupWorkday(string) bool      { return false }

func TestStatsHolidayFreezeUsesRegionCalendar(t *testing.T) {
	cal := func(r dbmodels.HolidayRegion) schedule.DayCalendar {
		if r == dbmodels.RegionCN {
			return fakeCal{holidays: map[string]bool{"2026-11-04": true}}
		}
		return nil
	}
	e, _ := statsEnv(t, cal)
	id := e.dailyItem("u1", nil)
	for _, d := range []string{"2026-11-01", "2026-11-02", "2026-11-03", "2026-11-05", "2026-11-06"} {
		e.checkin(id, d)
	}
	set := func(upd map[string]any) {
		require.NoError(t, e.db.Write().Model(&dbmodels.UserSettings{}).Where("user_id = ?", "u1").Updates(upd).Error)
	}
	require.Equal(t, 2, e.stats(id, "").Streak.Current)
	set(map[string]any{"holiday_region": "CN", "streak_holiday_freeze": true})
	require.Equal(t, 5, e.stats(id, "").Streak.Current)
	set(map[string]any{"holiday_region": "US"})
	require.Equal(t, 2, e.stats(id, "").Streak.Current, "US has no calendar")
	set(map[string]any{"holiday_region": "CN", "streak_holiday_freeze": false})
	require.Equal(t, 2, e.stats(id, "").Streak.Current, "feature off")
}

func TestFreezeEndpoints(t *testing.T) {
	e, _ := statsEnv(t, nil)
	version := func(u string) int64 {
		var st dbmodels.UserSettings
		require.NoError(t, e.db.Write().Where("user_id = ?", u).Take(&st).Error)
		return st.StateVersion
	}
	bad := []m{
		{"start_date": "2026-11-05", "end_date": "2026-11-04"},
		{"start_date": "2026-11-0", "end_date": "2026-11-04"},
		{"start_date": "", "end_date": ""},
		{"start_date": "2026-02-30", "end_date": "2026-03-01"},
		{"start_date": "2026-01-01", "end_date": "2027-01-02"}, // 367 days
		{"start_date": "2026-01-01", "end_date": "2026-01-02", "reason": string(make([]rune, 256))},
	}
	for i, b := range bad {
		if r, ok := b["reason"].(string); ok {
			b["reason"] = fmt.Sprintf("%0*d", len(r), 0)
		}
		require.Equal(t, 400, e.do("POST", "/api/freezes", "", "", b).Code, "case %d", i)
	}
	require.Equal(t, 400, e.do("POST", "/api/freezes", "", "", "{").Code)
	require.EqualValues(t, 0, version("u1"))

	// exactly 366 days is accepted (2024 is a leap year); overlaps are fine
	require.Equal(t, 201, e.do("POST", "/api/freezes", "", "", m{"start_date": "2024-01-01", "end_date": "2024-12-31"}).Code)
	require.Equal(t, 201, e.do("POST", "/api/freezes", "", "", m{"start_date": "2026-11-01", "end_date": "2026-11-03", "reason": "trip"}).Code)
	require.Equal(t, 201, e.do("POST", "/api/freezes", "", "", m{"start_date": "2026-11-02", "end_date": "2026-11-09"}).Code)
	require.Equal(t, 201, e.do("POST", "/api/freezes", "u2", "", m{"start_date": "2027-01-01", "end_date": "2027-01-02"}).Code)
	require.EqualValues(t, 3, version("u1"))
	require.EqualValues(t, 1, version("u2"))

	w := e.do("GET", "/api/freezes", "", "", nil)
	require.Equal(t, 200, w.Code)
	var list struct{ Freezes []freezeResponse }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list.Freezes, 3, "user-scoped")
	require.Equal(t, "2026-11-02", list.Freezes[0].StartDate, "start desc")
	require.Equal(t, "2026-11-01", list.Freezes[1].StartDate)
	require.Equal(t, "trip", list.Freezes[1].Reason)
	require.Equal(t, "2024-01-01", list.Freezes[2].StartDate)

	// another user's freeze cannot be deleted and does not bump
	id := list.Freezes[0].ID
	require.Equal(t, http.StatusNotFound, e.do("DELETE", fmt.Sprintf("/api/freezes/%d", id), "u2", "", nil).Code)
	require.EqualValues(t, 1, version("u2"))
	require.Equal(t, 200, e.do("DELETE", fmt.Sprintf("/api/freezes/%d", id), "", "", nil).Code)
	require.EqualValues(t, 4, version("u1"))
	require.Equal(t, http.StatusNotFound, e.do("DELETE", fmt.Sprintf("/api/freezes/%d", id), "", "", nil).Code)
	require.EqualValues(t, 4, version("u1"), "failed delete does not bump")
	require.Equal(t, 400, e.do("DELETE", "/api/freezes/x", "", "", nil).Code)
}

func TestTodayUsesDayCompletionAndCalendar(t *testing.T) {
	cal := func(dbmodels.HolidayRegion) schedule.DayCalendar { return fakeCal{holidays: map[string]bool{"2026-11-07": true}} }
	e, _ := statsEnv(t, cal)
	// workdays_only daily-ish rule: 2026-11-07 (Saturday, holiday) is skipped.
	id := e.item("u1", nil)
	require.NoError(t, e.db.Write().Where("item_id = ?", id).Delete(&dbmodels.ItemRuleVersion{}).Error)
	require.NoError(t, e.db.Write().Create(&dbmodels.ItemRuleVersion{ItemID: id, EffectiveFrom: "2026-11-01",
		ScheduleType: dbmodels.ScheduleFixedRRule, DtStart: "2026-11-01", RRuleString: "FREQ=DAILY",
		HolidayMode: dbmodels.HolidaySkipHolidays}).Error)
	require.Empty(t, e.today("u1", "").Items, "holiday skipped via Deps.Calendar")

	// counter with a target: a partial log is not done yet
	e.now = fixedNow.Add(48 * time.Hour) // 2026-11-09
	c := e.item("u1", func(it *dbmodels.Item) { it.TrackerType = dbmodels.TrackerCounter; it.Name = "Water" })
	target := 8.0
	require.NoError(t, e.db.Write().Where("item_id = ?", c).Delete(&dbmodels.ItemRuleVersion{}).Error)
	require.NoError(t, e.db.Write().Create(&dbmodels.ItemRuleVersion{ItemID: c, EffectiveFrom: "2026-11-01",
		ScheduleType: dbmodels.ScheduleFixedRRule, DtStart: "2026-11-01", RRuleString: "FREQ=DAILY",
		HolidayMode: dbmodels.HolidayNone, TargetValue: &target}).Error)
	require.Equal(t, 200, e.do("POST", "/api/checkins", "", "", m{"item_id": c, "mode": "delta", "value": 3, "date": "2026-11-09"}).Code)
	for _, it := range e.today("u1", "").Items {
		if it.ID == c {
			require.False(t, it.Done)
		}
	}
	require.Equal(t, 200, e.do("POST", "/api/checkins", "", "", m{"item_id": c, "mode": "delta", "value": 5, "date": "2026-11-09"}).Code)
	for _, it := range e.today("u1", "").Items {
		if it.ID == c {
			require.True(t, it.Done)
		}
	}
}

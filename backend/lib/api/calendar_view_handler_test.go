package api

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/holiday"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
)

func (e *env) calView(user, query string) calendarViewResponse {
	e.t.Helper()
	w := e.do("GET", "/api/view/calendar"+query, user, "", nil)
	require.Equal(e.t, 200, w.Code, w.Body.String())
	var r calendarViewResponse
	require.NoError(e.t, json.Unmarshal(w.Body.Bytes(), &r))
	return r
}

func (r calendarViewResponse) day(t *testing.T, date string) calendarDay {
	t.Helper()
	for _, d := range r.Days {
		if d.Date == date {
			return d
		}
	}
	t.Fatalf("day %s missing", date)
	return calendarDay{}
}

func (d calendarDay) item(t *testing.T, id uint) calendarDayItem {
	t.Helper()
	for _, it := range d.Items {
		if it.ItemID == id {
			return it
		}
	}
	t.Fatalf("item %d missing on %s", id, d.Date)
	return calendarDayItem{}
}

func (d calendarDay) has(id uint) bool {
	for _, it := range d.Items {
		if it.ItemID == id {
			return true
		}
	}
	return false
}

func cnCalendars(t *testing.T) func(dbmodels.HolidayRegion) schedule.DayCalendar {
	t.Helper()
	p, err := holiday.NewProvider()
	require.NoError(t, err)
	cn := holiday.NewCalendar(p, holiday.RegionCN)
	return func(r dbmodels.HolidayRegion) schedule.DayCalendar {
		if r == dbmodels.RegionCN {
			return cn
		}
		return nil
	}
}

func TestCalendarViewValidation(t *testing.T) {
	e := newEnv(t)
	for _, q := range []string{
		"", "?start=2026-11-01", "?start=2026-11-01&end=2026-11-0x", "?start=2026-13-01&end=2026-13-02",
		"?start=2026-11-10&end=2026-11-09", "?start=2026-01-01&end=2026-12-31", "?start=2026-01-01&end=2026-04-04",
		"?start=2099-01-01&end=2101-01-01", "?start=2026-11-01&end=2026-11-02&redact=maybe",
		"?start=2026-11-01&end=2026-11-02&item=abc", "?start=2026-11-01&end=2026-11-02&item=0",
		"?start=1800-01-01&end=1800-01-02",
	} {
		require.Equal(t, 400, e.do("GET", "/api/view/calendar"+q, "", "", nil).Code, q)
	}
	// Exactly the maximum range is accepted.
	require.Equal(t, 200, e.do("GET", "/api/view/calendar?start=2026-01-01&end=2026-04-03", "", "", nil).Code)
	require.Equal(t, 400, e.do("GET", "/api/view/calendar?start=2026-01-01&end=2026-04-04", "", "", nil).Code)
}

func TestCalendarViewDueDoneMissedForecast(t *testing.T) {
	e := newEnv(t)
	id := e.dailyItem("u1", nil) // daily from 2026-11-01, today is 2026-11-07
	e.checkin(id, "2026-11-02")
	e.checkin(id, "2026-11-07")

	r := e.calView("", "?start=2026-10-30&end=2026-11-09")
	require.Equal(t, "2026-11-07", r.Today)
	require.Len(t, r.Days, 11)
	require.Equal(t, 6, r.day(t, "2026-11-07").Weekday)
	require.False(t, r.day(t, "2026-10-31").has(id), "before the first rule version")
	require.Equal(t, dayNone, r.day(t, "2026-10-31").Status)

	d := r.day(t, "2026-11-02")
	require.Equal(t, calDone, d.item(t, id).Status)
	require.Equal(t, dayComplete, d.Status)
	require.True(t, d.item(t, id).HasSupplement, "backdated check-in")
	require.Equal(t, 1, d.DueCount)

	require.Equal(t, calMissed, r.day(t, "2026-11-03").item(t, id).Status)
	require.Equal(t, dayIncomplete, r.day(t, "2026-11-03").Status)
	require.Equal(t, calDone, r.day(t, "2026-11-07").item(t, id).Status)
	require.False(t, r.day(t, "2026-11-07").item(t, id).HasSupplement)

	f := r.day(t, "2026-11-08").item(t, id)
	require.Equal(t, calUpcoming, f.Status)
	require.True(t, f.Forecast)
	require.Equal(t, dayUpcoming, r.day(t, "2026-11-08").Status)
}

func TestCalendarViewPendingAndPartial(t *testing.T) {
	e := newEnv(t)
	pending := e.dailyItem("u1", nil)
	cnt := e.dailyItem("u1", func(it *dbmodels.Item) {
		it.TrackerType = dbmodels.TrackerCounter
		it.Name = "Water"
	})
	require.NoError(t, e.db.Write().Model(&dbmodels.ItemRuleVersion{}).Where("item_id = ?", cnt).
		Update("target_value", 8).Error)
	w := e.do("POST", "/api/checkins", "", "", m{"item_id": cnt, "mode": "delta", "date": "2026-11-07", "value": 3})
	require.Equal(t, 200, w.Code, w.Body.String())

	d := e.calView("", "?start=2026-11-07&end=2026-11-07").day(t, "2026-11-07")
	require.Equal(t, calPending, d.item(t, pending).Status)
	require.Equal(t, calPartial, d.item(t, cnt).Status)
	require.False(t, d.item(t, cnt).Done)
	require.Equal(t, 2, d.DueCount)
	require.Equal(t, 1, d.Partial)
	require.Equal(t, dayPartial, d.Status)

	// Reaching the target completes it.
	w = e.do("POST", "/api/checkins", "", "", m{"item_id": cnt, "mode": "delta", "date": "2026-11-07", "value": 5})
	require.Equal(t, 200, w.Code, w.Body.String())
	d = e.calView("", "?start=2026-11-07&end=2026-11-07").day(t, "2026-11-07")
	require.Equal(t, calDone, d.item(t, cnt).Status)
	require.Equal(t, 2, d.item(t, cnt).LogCount)
}

func TestCalendarViewExtraLogOnNonDueDay(t *testing.T) {
	e := newEnv(t)
	id := e.dailyItem("u1", nil)
	// Weekly on Mondays only: 2026-11-02 and 2026-11-09 are Mondays.
	require.NoError(t, e.db.Write().Model(&dbmodels.ItemRuleVersion{}).Where("item_id = ?", id).
		Update("rrule_string", "FREQ=WEEKLY;BYDAY=MO").Error)
	e.checkin(id, "2026-11-04")
	r := e.calView("", "?start=2026-11-01&end=2026-11-09")
	require.Equal(t, calExtra, r.day(t, "2026-11-04").item(t, id).Status)
	require.False(t, r.day(t, "2026-11-04").item(t, id).Due)
	require.Equal(t, dayNone, r.day(t, "2026-11-04").Status)
	require.Equal(t, calMissed, r.day(t, "2026-11-02").item(t, id).Status)
	require.False(t, r.day(t, "2026-11-03").has(id))
	require.True(t, r.day(t, "2026-11-09").item(t, id).Forecast)
}

func TestCalendarViewFreezes(t *testing.T) {
	e := newEnv(t)
	id := e.dailyItem("u1", nil)
	for _, u := range []string{"u1", "u2"} {
		w := e.do("POST", "/api/freezes", u, "", m{"start_date": "2026-11-03", "end_date": "2026-11-04", "reason": "trip-" + u})
		require.Equal(t, 201, w.Code, w.Body.String())
	}
	r := e.calView("", "?start=2026-11-02&end=2026-11-05")
	require.Len(t, r.Freezes, 1, "only own freezes")
	require.Equal(t, "2026-11-03", r.Freezes[0].StartDate)
	require.False(t, r.day(t, "2026-11-02").Frozen)
	require.True(t, r.day(t, "2026-11-03").Frozen)
	require.True(t, r.day(t, "2026-11-04").Frozen)
	require.False(t, r.day(t, "2026-11-05").Frozen)
	require.Equal(t, calFrozen, r.day(t, "2026-11-03").item(t, id).Status)
	require.Equal(t, dayFrozen, r.day(t, "2026-11-03").Status)
	require.Equal(t, calMissed, r.day(t, "2026-11-05").item(t, id).Status)

	// A freeze entirely outside the window is not returned.
	require.Empty(t, e.calView("", "?start=2026-12-01&end=2026-12-05").Freezes)
	// A logged day inside a freeze still counts as done.
	e.checkin(id, "2026-11-03")
	require.Equal(t, calDone, e.calView("", "?start=2026-11-03&end=2026-11-03").day(t, "2026-11-03").item(t, id).Status)
}

func TestCalendarViewHolidayMarkers(t *testing.T) {
	e, _ := statsEnv(t, cnCalendars(t))
	require.NoError(t, e.db.Write().Model(&dbmodels.UserSettings{}).Where("user_id = ?", "u1").
		Update("holiday_region", "CN").Error)
	id := e.dailyItem("u1", nil)
	require.NoError(t, e.db.Write().Model(&dbmodels.ItemRuleVersion{}).Where("item_id = ?", id).
		Updates(map[string]any{"rrule_string": "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR", "holiday_mode": "workdays_only",
			"dtstart": "2026-01-01", "effective_from": "2026-01-01"}).Error)

	r := e.calView("", "?start=2026-09-18&end=2026-10-03")
	require.Equal(t, "CN", r.Region)
	require.True(t, r.HolidayPublished)
	d := r.day(t, "2026-09-20") // Sunday makeup workday
	require.Equal(t, "workday", d.DayType)
	require.True(t, d.Makeup)
	require.False(t, d.IsOffDay)
	require.NotEmpty(t, d.HolidayName)
	require.True(t, d.has(id), "workdays_only fires on a makeup workday")
	h := r.day(t, "2026-10-01")
	require.Equal(t, "holiday", h.DayType)
	require.True(t, h.IsOffDay)
	require.False(t, h.Makeup)
	require.False(t, h.has(id), "skipped on statutory holiday")
	require.Equal(t, "normal", r.day(t, "2026-09-18").DayType)
	require.True(t, r.day(t, "2026-09-19").IsOffDay, "plain Saturday is off")

	// Unpublished year: plain weekend semantics and published=false.
	r = e.calView("", "?start=2027-02-01&end=2027-02-07")
	require.False(t, r.HolidayPublished)
	require.Equal(t, "normal", r.day(t, "2027-02-06").DayType)
	require.True(t, r.day(t, "2027-02-06").IsOffDay)

	// A range crossing into the unpublished year is flagged as well.
	require.False(t, e.calView("", "?start=2026-12-28&end=2027-01-03").HolidayPublished)

	// No region: no holiday awareness at all.
	require.NoError(t, e.db.Write().Model(&dbmodels.UserSettings{}).Where("user_id = ?", "u1").
		Update("holiday_region", "").Error)
	r = e.calView("", "?start=2026-10-01&end=2026-10-01")
	require.False(t, r.HolidayPublished)
	require.Equal(t, "normal", r.day(t, "2026-10-01").DayType)
}

func TestCalendarViewRollingShowsOnePoint(t *testing.T) {
	e := newEnv(t)
	id := e.item("u1", func(it *dbmodels.Item) {
		it.TrackerType = dbmodels.TrackerMaintenance
		it.Name = "Filter"
	})
	require.NoError(t, e.db.Write().Where("item_id = ?", id).Delete(&dbmodels.ItemRuleVersion{}).Error)
	require.NoError(t, e.db.Write().Create(&dbmodels.ItemRuleVersion{ItemID: id, EffectiveFrom: "2026-01-01",
		ScheduleType: dbmodels.ScheduleRolling, DtStart: "2026-01-01", RollingInterval: 10, RollingUnit: dbmodels.RollingDay,
		HolidayMode: dbmodels.HolidayNone}).Error)
	e.checkin(id, "2026-11-05") // next due 2026-11-15

	r := e.calView("", "?start=2026-11-01&end=2026-12-31")
	points := 0
	for _, d := range r.Days {
		if d.has(id) && d.item(t, id).Due {
			points++
		}
	}
	require.Equal(t, 1, points)
	f := r.day(t, "2026-11-15").item(t, id)
	require.True(t, f.Forecast)
	require.False(t, f.Overdue)
	require.Equal(t, calUpcoming, f.Status)
	// The past completion is shown as an (extra) log, not as a forecast.
	require.Equal(t, calExtra, r.day(t, "2026-11-05").item(t, id).Status)

	// Window that does not contain next_due: no point.
	r = e.calView("", "?start=2026-11-16&end=2026-11-30")
	for _, d := range r.Days {
		require.False(t, d.has(id))
	}
	// Purely historical window: no forecast.
	r = e.calView("", "?start=2026-10-01&end=2026-10-31")
	for _, d := range r.Days {
		require.False(t, d.has(id))
	}
}

func TestCalendarViewRollingOverdueShownToday(t *testing.T) {
	e := newEnv(t)
	id := e.item("u1", func(it *dbmodels.Item) { it.TrackerType = dbmodels.TrackerMaintenance })
	require.NoError(t, e.db.Write().Where("item_id = ?", id).Delete(&dbmodels.ItemRuleVersion{}).Error)
	require.NoError(t, e.db.Write().Create(&dbmodels.ItemRuleVersion{ItemID: id, EffectiveFrom: "2026-01-01",
		ScheduleType: dbmodels.ScheduleRolling, DtStart: "2026-10-01", RollingInterval: 10, RollingUnit: dbmodels.RollingDay,
		HolidayMode: dbmodels.HolidayNone}).Error) // next due 2026-10-11, long overdue
	r := e.calView("", "?start=2026-11-01&end=2026-11-10")
	d := r.day(t, "2026-11-07")
	it := d.item(t, id)
	require.True(t, it.Overdue)
	require.False(t, it.Forecast)
	require.Equal(t, calPending, it.Status)
	require.True(t, d.HasOverdue)
	require.False(t, r.day(t, "2026-11-08").has(id))
}

func TestCalendarViewQuotaItemOnlyLogs(t *testing.T) {
	e := newEnv(t)
	id := e.item("u1", func(it *dbmodels.Item) { it.Polarity = dbmodels.PolarityNegative })
	require.NoError(t, e.db.Write().Where("item_id = ?", id).Delete(&dbmodels.ItemRuleVersion{}).Error)
	require.NoError(t, e.db.Write().Create(&dbmodels.ItemRuleVersion{ItemID: id, EffectiveFrom: "2026-01-01",
		ScheduleType: dbmodels.ScheduleNone, HolidayMode: dbmodels.HolidayNone}).Error)
	w := e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "append", "date": "2026-11-05"})
	require.Equal(t, 200, w.Code, w.Body.String())
	r := e.calView("", "?start=2026-11-04&end=2026-11-08")
	require.Equal(t, calExtra, r.day(t, "2026-11-05").item(t, id).Status)
	require.Equal(t, 0, r.day(t, "2026-11-05").DueCount)
	require.False(t, r.day(t, "2026-11-06").has(id))
}

func TestCalendarViewItemFilterArchivedAndOwnership(t *testing.T) {
	e := newEnv(t)
	a := e.dailyItem("u1", nil)
	b := e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "B" })
	arch := e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Old"; it.Archived = true })
	other := e.dailyItem("u2", nil)

	r := e.calView("", "?start=2026-11-08&end=2026-11-08")
	d := r.day(t, "2026-11-08")
	require.True(t, d.has(a))
	require.True(t, d.has(b))
	require.False(t, d.has(arch))
	require.False(t, d.has(other))

	r = e.calView("", fmt.Sprintf("?start=2026-11-08&end=2026-11-08&item=%d", b))
	d = r.day(t, "2026-11-08")
	require.False(t, d.has(a))
	require.True(t, d.has(b))
	// Another user's item id yields an empty (not an error) calendar.
	r = e.calView("", fmt.Sprintf("?start=2026-11-08&end=2026-11-08&item=%d", other))
	require.Empty(t, r.day(t, "2026-11-08").Items)
}

func TestCalendarViewRedact(t *testing.T) {
	e := newEnv(t)
	sens := e.dailyItem("u1", func(it *dbmodels.Item) { it.IsSensitive = true })
	cat := dbmodels.Category{UserID: "u1", Name: "Private", IsSensitive: true}
	require.NoError(t, e.db.Write().Create(&cat).Error)
	inCat := e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Category Secret"; it.CategoryID = &cat.ID })
	plain := e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Water" })

	d := e.calView("", "?start=2026-11-08&end=2026-11-08&redact=1").day(t, "2026-11-08")
	require.Equal(t, privateItemName, d.item(t, sens).Name)
	require.Equal(t, privateItemName, d.item(t, inCat).Name)
	require.Equal(t, "Water", d.item(t, plain).Name)

	w := e.do("GET", "/api/view/calendar?start=2026-11-08&end=2026-11-08&redact=1", "", "", nil)
	require.NotContains(t, w.Body.String(), "Secret Medication")
	require.NotContains(t, w.Body.String(), "Category Secret")

	d = e.calView("", "?start=2026-11-08&end=2026-11-08").day(t, "2026-11-08")
	require.Equal(t, "Secret Medication", d.item(t, sens).Name)
}

func TestCalendarViewUsesRuleVersionAtDate(t *testing.T) {
	e := newEnv(t)
	id := e.dailyItem("u1", nil)
	// From 2026-11-05 the item is due only on Mondays (new rule version).
	require.NoError(t, e.db.Write().Create(&dbmodels.ItemRuleVersion{ItemID: id, EffectiveFrom: "2026-11-05",
		ScheduleType: dbmodels.ScheduleFixedRRule, DtStart: "2026-11-05", RRuleString: "FREQ=WEEKLY;BYDAY=MO",
		HolidayMode: dbmodels.HolidayNone}).Error)
	r := e.calView("", "?start=2026-11-03&end=2026-11-10")
	require.True(t, r.day(t, "2026-11-03").has(id))
	require.True(t, r.day(t, "2026-11-04").has(id))
	require.False(t, r.day(t, "2026-11-06").has(id))
	require.True(t, r.day(t, "2026-11-09").has(id))
}

func TestCalendarViewBrokenRuleDoesNotFail(t *testing.T) {
	e := newEnv(t)
	good := e.dailyItem("u1", nil)
	bad := e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Bad" })
	require.NoError(t, e.db.Write().Model(&dbmodels.ItemRuleVersion{}).Where("item_id = ?", bad).
		Update("rrule_string", "FREQ=NONSENSE").Error)
	r := e.calView("", "?start=2026-11-08&end=2026-11-08")
	require.True(t, r.day(t, "2026-11-08").has(good))
	require.False(t, r.day(t, "2026-11-08").has(bad))
}

func TestCalendarViewTimezone(t *testing.T) {
	e := newEnv(t)
	id := e.dailyItem("u1", nil)
	// 16:30 UTC is already 2026-11-07 in Shanghai; switch to a western zone.
	require.NoError(t, e.db.Write().Model(&dbmodels.UserSettings{}).Where("user_id = ?", "u1").
		Update("timezone", "America/Los_Angeles").Error)
	r := e.calView("", "?start=2026-11-06&end=2026-11-08")
	require.Equal(t, "2026-11-06", r.Today)
	require.Equal(t, calPending, r.day(t, "2026-11-06").item(t, id).Status)
	require.Equal(t, calUpcoming, r.day(t, "2026-11-07").item(t, id).Status)
}

func TestCalendarViewEmptyAndShape(t *testing.T) {
	e := newEnv(t)
	w := e.do("GET", "/api/view/calendar?start=2026-11-01&end=2026-11-02", "", "", nil)
	require.Equal(t, 200, w.Code)
	require.Equal(t, "private, no-store", w.Header().Get("Cache-Control"))
	var raw map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	require.Equal(t, []any{}, raw["freezes"])
	days := raw["days"].([]any)
	require.Len(t, days, 2)
	require.Equal(t, []any{}, days[0].(map[string]any)["items"])
	require.Equal(t, 1, int(raw["week_start"].(float64)))
}

func TestHolidaysEndpoint(t *testing.T) {
	e, _ := statsEnv(t, cnCalendars(t))
	get := func(user, q string) (int, holidaysResponse) {
		w := e.do("GET", "/api/calendar/holidays"+q, user, "", nil)
		var r holidaysResponse
		if w.Code == 200 {
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &r))
		}
		return w.Code, r
	}
	// Default region (unset): nothing published.
	code, r := get("", "?year=2026")
	require.Equal(t, 200, code)
	require.False(t, r.Published)
	require.NotNil(t, r.Days)
	require.Empty(t, r.Days)

	require.NoError(t, e.db.Write().Model(&dbmodels.UserSettings{}).Where("user_id = ?", "u1").
		Update("holiday_region", "CN").Error)
	code, r = get("", "?year=2026")
	require.Equal(t, 200, code)
	require.True(t, r.Published)
	require.Equal(t, 2026, r.Year)
	require.Equal(t, "CN", r.Region)
	kinds := map[holiday.Kind]bool{}
	for _, d := range r.Days {
		kinds[d.Kind] = true
	}
	require.True(t, kinds[holiday.Holiday])
	require.True(t, kinds[holiday.Workday])

	// Default year is the current local year.
	_, r = get("", "")
	require.Equal(t, 2026, r.Year)

	_, r = get("", "?year=2030")
	require.False(t, r.Published)
	require.Empty(t, r.Days)

	// Another user keeps their own (unset) region.
	_, r = get("u2", "?year=2026")
	require.False(t, r.Published)

	for _, q := range []string{"?year=abc", "?year=1899", "?year=2101", "?year=-1"} {
		code, _ = get("", q)
		require.Equal(t, 400, code, q)
	}
}

func TestCalendarViewRequiresDayFormatStrictness(t *testing.T) {
	e := newEnv(t)
	// Non-zero-padded dates are rejected rather than silently normalized.
	require.Equal(t, 400, e.do("GET", "/api/view/calendar?start=2026-1-1&end=2026-1-2", "", "", nil).Code)
}

func TestCalendarViewTodoRescheduleAndUndate(t *testing.T) {
	e := newEnv(t)
	// 1. Create an overdue event due on 2026-11-05 (today is 2026-11-07).
	w := e.do("POST", "/api/items", "", "", m{
		"name":         "Overdue Todo",
		"kind":         "event",
		"tracker_type": "boolean",
		"rule": m{
			"schedule_type": "fixed_rrule",
			"rrule_string":  "FREQ=DAILY;COUNT=1",
			"dtstart":       "2026-11-05",
			"holiday_mode":  "none",
		},
	})
	require.Equal(t, 201, w.Code)
	var ev itemResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ev))
	require.NoError(t, e.db.Write().Model(&dbmodels.ItemRuleVersion{}).
		Where("item_id = ?", ev.ID).Update("effective_from", "2026-11-05").Error)

	r := e.calView("", "?start=2026-11-01&end=2026-11-10")
	// On past due date 2026-11-05: missed.
	d5 := r.day(t, "2026-11-05")
	require.True(t, d5.has(ev.ID))
	require.Equal(t, calMissed, d5.item(t, ev.ID).Status)

	// On today 2026-11-07: surfaced as overdue.
	d7 := r.day(t, "2026-11-07")
	require.True(t, d7.has(ev.ID))
	require.True(t, d7.item(t, ev.ID).Overdue)
	require.Equal(t, calPending, d7.item(t, ev.ID).Status)

	// 2. Rescheduling to a future date (2026-11-10) clears overdue status and updates due date.
	w = e.do("PATCH", fmt.Sprintf("/api/items/%d", ev.ID), "", "", m{
		"rule": m{
			"schedule_type": "fixed_rrule",
			"rrule_string":  "FREQ=DAILY;COUNT=1",
			"dtstart":       "2026-11-10",
			"until":         "2026-11-10",
			"holiday_mode":  "none",
		},
	})
	require.Equal(t, 200, w.Code)

	r = e.calView("", "?start=2026-11-01&end=2026-11-10")
	// No longer due or missed on 2026-11-05.
	require.False(t, r.day(t, "2026-11-05").has(ev.ID))
	// No longer overdue on 2026-11-07.
	require.False(t, r.day(t, "2026-11-07").has(ev.ID))
	// Due on 2026-11-10.
	d10 := r.day(t, "2026-11-10")
	require.True(t, d10.has(ev.ID))
	require.True(t, d10.item(t, ev.ID).Due)
	require.Equal(t, calUpcoming, d10.item(t, ev.ID).Status)

	// 3. Clearing to schedule_type: "none" makes it undated (not due anywhere).
	w = e.do("PATCH", fmt.Sprintf("/api/items/%d", ev.ID), "", "", m{
		"rule": m{
			"schedule_type": "none",
			"holiday_mode":  "none",
		},
	})
	require.Equal(t, 200, w.Code)

	r = e.calView("", "?start=2026-11-01&end=2026-11-10")
	for _, day := range r.Days {
		require.False(t, day.has(ev.ID), fmt.Sprintf("day %s should not have undated item", day.Date))
	}

	// 4. Assigning a due date to undated event makes it due on that date.
	w = e.do("PATCH", fmt.Sprintf("/api/items/%d", ev.ID), "", "", m{
		"rule": m{
			"schedule_type": "fixed_rrule",
			"rrule_string":  "FREQ=DAILY;COUNT=1",
			"dtstart":       "2026-11-08",
			"until":         "2026-11-08",
			"holiday_mode":  "none",
		},
	})
	require.Equal(t, 200, w.Code)

	r = e.calView("", "?start=2026-11-01&end=2026-11-10")
	d8 := r.day(t, "2026-11-08")
	require.True(t, d8.has(ev.ID))
	require.True(t, d8.item(t, ev.ID).Due)
	require.Equal(t, calUpcoming, d8.item(t, ev.ID).Status)

	// 5. Completing an event on a later date does NOT mark original due date as calMissed.
	w = e.do("POST", "/api/items", "", "", m{
		"name":         "Late completed event",
		"kind":         "event",
		"tracker_type": "boolean",
		"rule": m{
			"schedule_type": "fixed_rrule",
			"rrule_string":  "FREQ=DAILY;COUNT=1",
			"dtstart":       "2026-11-03",
			"holiday_mode":  "none",
		},
	})
	require.Equal(t, 201, w.Code)
	var ev2 itemResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ev2))
	require.NoError(t, e.db.Write().Model(&dbmodels.ItemRuleVersion{}).
		Where("item_id = ?", ev2.ID).Update("effective_from", "2026-11-03").Error)

	// Check-in on 2026-11-07 (later than 2026-11-03).
	e.checkin(ev2.ID, "2026-11-07")

	r = e.calView("", "?start=2026-11-01&end=2026-11-10")
	d3 := r.day(t, "2026-11-03")
	require.True(t, d3.has(ev2.ID))
	require.Equal(t, calDone, d3.item(t, ev2.ID).Status)
	require.True(t, d3.item(t, ev2.ID).Done)
}

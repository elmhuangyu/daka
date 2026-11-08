package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/auth"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/holiday"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
)

const (
	// maxCalendarViewDays bounds GET /api/view/calendar (a 6-week month grid
	// is 42 days; the rest is headroom for agenda/forecast pages).
	maxCalendarViewDays = 93
	minHolidayYear      = 1900
	maxHolidayYear      = 2100
)

// Item states within one calendar day.
const (
	calDone     = "done"
	calPartial  = "partial"
	calMissed   = "missed"
	calFrozen   = "frozen"
	calPending  = "pending"  // due today, nothing logged yet
	calUpcoming = "upcoming" // due in the future (forecast)
	calExtra    = "extra"    // logged although not due (or unscheduled item)
)

// Day-level states (the colored ribbon of DESIGN 5.2).
const (
	dayNone       = "none"
	dayComplete   = "complete"
	dayPartial    = "partial"
	dayIncomplete = "incomplete"
	dayPending    = "pending"
	dayUpcoming   = "upcoming"
	dayFrozen     = "frozen"
)

// holidayDetail is implemented by *holiday.Calendar; plain schedule.DayCalendar
// implementations (tests, future regions) only provide the three booleans.
type holidayDetail interface {
	Day(date string) holiday.DayInfo
	YearInfo(year int) holiday.YearData
}

type calendarDayItem struct {
	ItemID      uint   `json:"item_id"`
	Name        string `json:"name"`
	CategoryID  *uint  `json:"category_id"`
	TrackerType string `json:"tracker_type"`
	IsSensitive bool   `json:"is_sensitive"`
	Due         bool   `json:"due"`
	Status      string `json:"status"`
	Done        bool   `json:"done"`
	LogCount    int    `json:"log_count"`
	// HasSupplement is true when any log of the day was a retroactive check-in.
	HasSupplement bool `json:"has_supplement"`
	// Forecast marks a predicted (future) occurrence; rolling items have one.
	Forecast bool `json:"forecast"`
	// Overdue marks a rolling item whose next_due already passed (shown today).
	Overdue bool `json:"overdue"`
}

type calendarDay struct {
	Date    string `json:"date"`
	Weekday int    `json:"weekday"`
	// DayType is normal, holiday (statutory day off) or workday (makeup working day).
	DayType     string            `json:"day_type"`
	HolidayName string            `json:"holiday_name,omitempty"`
	IsOffDay    bool              `json:"is_off_day"`
	Makeup      bool              `json:"makeup_workday"`
	Frozen      bool              `json:"frozen"`
	Status      string            `json:"status"`
	DueCount    int               `json:"due_count"`
	DoneCount   int               `json:"done_count"`
	Partial     int               `json:"partial_count"`
	HasOverdue  bool              `json:"has_overdue"`
	Items       []calendarDayItem `json:"items"`
}

type calendarFreeze struct {
	ID        uint   `json:"id"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Reason    string `json:"reason"`
}

type calendarViewResponse struct {
	Start     string `json:"start"`
	End       string `json:"end"`
	Timezone  string `json:"timezone"`
	Today     string `json:"today"`
	WeekStart int    `json:"week_start"`
	Region    string `json:"holiday_region"`
	// HolidayPublished is false when any year in the range has no holiday data
	// for the region (the UI shows "schedule not published").
	HolidayPublished bool             `json:"holiday_published"`
	Freezes          []calendarFreeze `json:"freezes"`
	Days             []calendarDay    `json:"days"`
}

// GET /api/view/calendar?start=&end=[&item=][&redact=1] (DESIGN 5.2, 11.3).
func (s *server) getCalendarView(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		errResult(http.StatusUnauthorized, "unauthorized").write(w)
		return
	}
	q := r.URL.Query()
	redact := false
	switch q.Get("redact") {
	case "", "0", "false":
	case "1", "true":
		redact = true
	default:
		errResult(http.StatusBadRequest, "invalid redact").write(w)
		return
	}
	start, ok1 := strictDate(q.Get("start"))
	end, ok2 := strictDate(q.Get("end"))
	switch {
	case !ok1 || !ok2:
		errResult(http.StatusBadRequest, "start and end must be YYYY-MM-DD").write(w)
		return
	case end.Before(start):
		errResult(http.StatusBadRequest, "start must not be after end").write(w)
		return
	case end.Year() > maxHolidayYear:
		errResult(http.StatusBadRequest, "end too far in the future").write(w)
		return
	case int(end.Sub(start).Hours()/24)+1 > maxCalendarViewDays:
		errResult(http.StatusBadRequest, "range too large (max 93 days)").write(w)
		return
	}
	var onlyItem uint
	if v := q.Get("item"); v != "" {
		id, err := strconv.ParseUint(v, 10, 64)
		if err != nil || id == 0 {
			errResult(http.StatusBadRequest, "invalid item").write(w)
			return
		}
		onlyItem = uint(id)
	}
	resp, err := s.calendarView(s.db.Read(), u.ID, q.Get("start"), q.Get("end"), onlyItem, redact)
	if err != nil {
		log.Error().Err(err).Msg("view/calendar failed")
		errResult(http.StatusInternalServerError, "internal error").write(w)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	jsonResult(http.StatusOK, resp).write(w)
}

type calendarData struct {
	items    []dbmodels.Item
	versions map[uint][]dbmodels.ItemRuleVersion
	options  map[uint][]dbmodels.ItemOption
	// logs[item][date], restricted to the requested window.
	logs map[uint]map[string][]dbmodels.CheckinLog
	// rollingDates holds every completion date of items governed by a rolling
	// rule today (NextDue needs the whole history, not just the window).
	rollingDates map[uint][]string
}

func (s *server) calendarView(db *gorm.DB, userID, start, end string, onlyItem uint, redact bool) (*calendarViewResponse, error) {
	st, err := userSettings(db, userID)
	if err != nil {
		return nil, err
	}
	today, err := schedule.Today(s.now(), st.Timezone)
	if err != nil {
		return nil, err
	}
	cal := s.calendarFor(st)
	resp := &calendarViewResponse{
		Start: start, End: end, Timezone: st.Timezone, Today: today, WeekStart: st.WeekStart,
		Region: string(st.HolidayRegion), Freezes: []calendarFreeze{}, Days: []calendarDay{},
	}

	var freezes []dbmodels.FreezePeriod
	if err := db.Where("user_id = ? AND start_date <= ? AND end_date >= ?", userID, end, start).
		Order("start_date, id").Find(&freezes).Error; err != nil {
		return nil, err
	}
	for _, f := range freezes {
		resp.Freezes = append(resp.Freezes, calendarFreeze{ID: f.ID, StartDate: f.StartDate, EndDate: f.EndDate, Reason: f.Reason})
	}

	d, err := s.loadCalendarData(db, userID, start, end, today, onlyItem)
	if err != nil {
		return nil, err
	}
	sensitiveCat := map[uint]bool{}
	if redact {
		var cats []dbmodels.Category
		if err := db.Where("user_id = ? AND is_sensitive = ?", userID, true).Find(&cats).Error; err != nil {
			return nil, err
		}
		for _, c := range cats {
			sensitiveCat[c.ID] = true
		}
	}

	// Day skeletons with holiday markers.
	startT, _ := time.Parse(dbmodels.DateLayout, start)
	endT, _ := time.Parse(dbmodels.DateLayout, end)
	published := cal != nil
	for t := startT; !t.After(endT); t = t.AddDate(0, 0, 1) {
		ds := t.Format(dbmodels.DateLayout)
		day := calendarDay{Date: ds, Weekday: int(t.Weekday()), DayType: string(holiday.Normal),
			IsOffDay: t.Weekday() == time.Saturday || t.Weekday() == time.Sunday, Status: dayNone, Items: []calendarDayItem{}}
		if cal != nil {
			switch {
			case cal.IsStatutoryHoliday(ds):
				day.DayType, day.IsOffDay = string(holiday.Holiday), true
			case cal.IsMakeupWorkday(ds):
				day.DayType, day.IsOffDay, day.Makeup = string(holiday.Workday), false, true
			default:
				day.IsOffDay = !cal.IsWorkday(ds)
			}
			if hd, ok := cal.(holidayDetail); ok {
				day.HolidayName = hd.Day(ds).Name
			}
		}
		for _, f := range freezes {
			if f.StartDate <= ds && ds <= f.EndDate {
				day.Frozen = true
				break
			}
		}
		resp.Days = append(resp.Days, day)
	}
	index := map[string]*calendarDay{}
	for i := range resp.Days {
		index[resp.Days[i].Date] = &resp.Days[i]
	}
	if hd, ok := cal.(holidayDetail); ok {
		for y := startT.Year(); y <= endT.Year(); y++ {
			if !hd.YearInfo(y).Published {
				published = false
			}
		}
	}
	resp.HolidayPublished = published

	for _, it := range d.items {
		vs := d.versions[it.ID]
		entries := map[string]*calendarDayItem{}
		get := func(date string) *calendarDayItem {
			e := entries[date]
			if e == nil {
				e = &calendarDayItem{ItemID: it.ID, Name: it.Name, CategoryID: it.CategoryID,
					TrackerType: string(it.TrackerType), IsSensitive: it.IsSensitive}
				if redact && (it.IsSensitive || (it.CategoryID != nil && sensitiveCat[*it.CategoryID])) {
					e.Name = privateItemName
				}
				entries[date] = e
			}
			return e
		}

		// Non-fixed spans (rolling / quota) have no RRULE dates; neutralizing
		// them keeps the span boundaries while fixed spans still yield dates.
		due, err := schedule.DueDatesWith(fixedOnly(vs), start, end, cal)
		if err == nil {
			for _, ds := range due {
				get(ds).Due = true
			}
		} else {
			// A broken stored rule must not take the whole view down. Log ids only.
			log.Warn().Err(err).Uint("item_id", it.ID).Msg("view/calendar: skipping due dates of item with unevaluable rule")
		}
		for ds, logs := range d.logs[it.ID] {
			e := get(ds)
			e.LogCount = len(logs)
			for _, l := range logs {
				if l.IsSupplement {
					e.HasSupplement = true
				}
			}
			e.Done = true
			if cur := schedule.CurrentVersion(vs, ds); cur != nil {
				e.Done = schedule.DayCompletion(it.TrackerType, *cur, d.options[it.ID], logs, it.Aggregate).Done
			}
		}
		addRollingForecast(it, vs, d, today, start, end, get)

		for ds, e := range entries {
			day := index[ds]
			if day == nil {
				continue
			}
			finishEntry(e, ds, today, day.Frozen)
			day.Items = append(day.Items, *e)
		}
	}
	for i := range resp.Days {
		summarizeDay(&resp.Days[i], today)
	}
	return resp, nil
}

// fixedOnly returns versions with every non-fixed_rrule schedule turned into
// "none": same effective_from boundaries, no due dates.
func fixedOnly(vs []dbmodels.ItemRuleVersion) []dbmodels.ItemRuleVersion {
	out := make([]dbmodels.ItemRuleVersion, len(vs))
	for i, v := range vs {
		if v.ScheduleType != dbmodels.ScheduleFixedRRule && v.ScheduleType != dbmodels.ScheduleNone {
			v = dbmodels.ItemRuleVersion{ID: v.ID, ItemID: v.ItemID, EffectiveFrom: v.EffectiveFrom, ScheduleType: dbmodels.ScheduleNone}
		}
		out[i] = v
	}
	return out
}

// addRollingForecast adds the single predicted point of a rolling item: the
// next due date (DESIGN 3.4.3), or today when it is already overdue.
func addRollingForecast(it dbmodels.Item, vs []dbmodels.ItemRuleVersion, d *calendarData, today, start, end string,
	get func(string) *calendarDayItem) {
	cur := schedule.CurrentVersion(vs, today)
	if cur == nil || cur.ScheduleType != dbmodels.ScheduleRolling || end < today {
		return
	}
	next, err := schedule.NextDue(*cur, d.rollingDates[it.ID])
	if err != nil {
		log.Warn().Err(err).Uint("item_id", it.ID).Msg("view/calendar: skipping rolling forecast of item with unevaluable rule")
		return
	}
	overdue := next < today
	at := next
	if overdue {
		at = today
	}
	if at < start || at > end {
		return
	}
	e := get(at)
	e.Due, e.Forecast, e.Overdue = true, at > today, overdue
}

func finishEntry(e *calendarDayItem, date, today string, frozen bool) {
	switch {
	case e.Due && e.LogCount > 0 && e.Done:
		e.Status = calDone
	case e.Due && e.LogCount > 0:
		e.Status = calPartial
	case e.Due && date < today && frozen:
		e.Status = calFrozen
	case e.Due && date < today:
		e.Status = calMissed
	case e.Due && date == today:
		e.Status = calPending
	case e.Due:
		e.Status, e.Forecast = calUpcoming, true
	default:
		e.Status = calExtra
	}
	if e.LogCount == 0 {
		e.Done = false
	}
}

func summarizeDay(day *calendarDay, today string) {
	for _, e := range day.Items {
		if !e.Due {
			continue
		}
		day.DueCount++
		switch e.Status {
		case calDone:
			day.DoneCount++
		case calPartial:
			day.Partial++
		}
		if e.Overdue {
			day.HasOverdue = true
		}
	}
	switch {
	case day.DueCount == 0:
		day.Status = dayNone
	case day.Date > today:
		day.Status = dayUpcoming
	case day.DoneCount == day.DueCount:
		day.Status = dayComplete
	case day.DoneCount+day.Partial > 0:
		day.Status = dayPartial
	case day.Date == today:
		day.Status = dayPending
	case day.Frozen:
		day.Status = dayFrozen
	default:
		day.Status = dayIncomplete
	}
}

func (s *server) loadCalendarData(db *gorm.DB, userID, start, end, today string, onlyItem uint) (*calendarData, error) {
	d := &calendarData{
		versions: map[uint][]dbmodels.ItemRuleVersion{}, options: map[uint][]dbmodels.ItemOption{},
		logs: map[uint]map[string][]dbmodels.CheckinLog{}, rollingDates: map[uint][]string{},
	}
	q := db.Where("user_id = ?", userID)
	if onlyItem != 0 {
		q = q.Where("id = ?", onlyItem) // an explicitly requested item may be archived
	} else {
		q = q.Where("archived = ?", false)
	}
	if err := q.Order("sort_order, id").Find(&d.items).Error; err != nil {
		return nil, err
	}
	if len(d.items) == 0 {
		return d, nil
	}
	ids := make([]uint, len(d.items))
	for i, it := range d.items {
		ids[i] = it.ID
	}
	horizon := end
	if today > horizon {
		horizon = today
	}
	var versions []dbmodels.ItemRuleVersion
	if err := db.Where("item_id IN ? AND effective_from <= ?", ids, horizon).Find(&versions).Error; err != nil {
		return nil, err
	}
	for _, v := range versions {
		d.versions[v.ItemID] = append(d.versions[v.ItemID], v)
	}
	rolling := []uint{}
	for _, id := range ids {
		if cur := schedule.CurrentVersion(d.versions[id], today); cur != nil && cur.ScheduleType == dbmodels.ScheduleRolling {
			rolling = append(rolling, id)
		}
	}
	var options []dbmodels.ItemOption
	if err := db.Where("item_id IN ?", ids).Order("sort_order, id").Find(&options).Error; err != nil {
		return nil, err
	}
	for _, o := range options {
		d.options[o.ItemID] = append(d.options[o.ItemID], o)
	}
	var logs []dbmodels.CheckinLog
	if err := db.Where("user_id = ? AND item_id IN ? AND checkin_date >= ? AND checkin_date <= ?", userID, ids, start, end).
		Order("id").Find(&logs).Error; err != nil {
		return nil, err
	}
	for _, l := range logs {
		if d.logs[l.ItemID] == nil {
			d.logs[l.ItemID] = map[string][]dbmodels.CheckinLog{}
		}
		d.logs[l.ItemID][l.CheckinDate] = append(d.logs[l.ItemID][l.CheckinDate], l)
	}
	if len(rolling) > 0 {
		var rows []struct {
			ItemID      uint
			CheckinDate string
		}
		if err := db.Model(&dbmodels.CheckinLog{}).Select("item_id, checkin_date").
			Where("user_id = ? AND item_id IN ?", userID, rolling).Group("item_id, checkin_date").Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			d.rollingDates[r.ItemID] = append(d.rollingDates[r.ItemID], r.CheckinDate)
		}
	}
	return d, nil
}

type holidaysResponse struct {
	Year      int           `json:"year"`
	Region    string        `json:"region"`
	Published bool          `json:"published"`
	Days      []holiday.Day `json:"days"`
}

// GET /api/calendar/holidays?year=2026: statutory holidays and makeup workdays
// of the user's region. Unpublished or unsupported regions answer
// published=false with no days (DESIGN 6.2).
func (s *server) getHolidays(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		errResult(http.StatusUnauthorized, "unauthorized").write(w)
		return
	}
	st, err := userSettings(s.db.Read(), u.ID)
	if err != nil {
		log.Error().Err(err).Msg("holidays: load settings failed")
		errResult(http.StatusInternalServerError, "internal error").write(w)
		return
	}
	year := 0
	if v := r.URL.Query().Get("year"); v != "" {
		y, err := strconv.Atoi(v)
		if err != nil || y < minHolidayYear || y > maxHolidayYear {
			errResult(http.StatusBadRequest, "invalid year").write(w)
			return
		}
		year = y
	} else {
		today, err := schedule.Today(s.now(), st.Timezone)
		if err != nil {
			log.Error().Err(err).Msg("holidays: today failed")
			errResult(http.StatusInternalServerError, "internal error").write(w)
			return
		}
		year, _ = strconv.Atoi(today[:4])
	}
	out := holidaysResponse{Year: year, Region: string(st.HolidayRegion), Days: []holiday.Day{}}
	if hd, ok := s.calendarFor(st).(holidayDetail); ok {
		yd := hd.YearInfo(year)
		out.Published = yd.Published
		if yd.Days != nil {
			out.Days = yd.Days
		}
	}
	jsonResult(http.StatusOK, out).write(w)
}

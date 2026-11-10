package api

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/auth"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
)

// privateItemName replaces the names of sensitive items when redact=1 (DESIGN 4.2).
const privateItemName = "Private item"

type todayProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

type todayItem struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	CategoryID  *uint  `json:"category_id"`
	ProjectID   *uint  `json:"project_id"`
	TrackerType string `json:"tracker_type"`
	Kind        string `json:"kind"`
	Color       string `json:"color,omitempty"`
	IsSensitive bool   `json:"is_sensitive"`
	Due         bool   `json:"due"`
	Done        bool   `json:"done"`
	Overdue     bool   `json:"overdue,omitempty"`
	DueDate     string `json:"due_date,omitempty"`
	LogIDs      []uint `json:"log_ids"`
}

type upcomingItem struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	CategoryID  *uint  `json:"category_id"`
	ProjectID   *uint  `json:"project_id"`
	TrackerType string `json:"tracker_type"`
	Kind        string `json:"kind"`
	Color       string `json:"color,omitempty"`
	IsSensitive bool   `json:"is_sensitive"`
	DueDate     string `json:"due_date"`
	DaysUntil   int    `json:"days_until"`
}

type todayResponse struct {
	Date     string         `json:"date"`
	Weekday  int            `json:"weekday"`
	Timezone string         `json:"timezone"`
	Progress todayProgress  `json:"progress"`
	Items    []todayItem    `json:"items"`
	Upcoming []upcomingItem `json:"upcoming,omitempty"`
}

func (s *server) getToday(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		errResult(http.StatusUnauthorized, "unauthorized").write(w)
		return
	}
	redact, ok := parseRedact(w, r)
	if !ok {
		return
	}
	targetDate := r.URL.Query().Get("date")
	if targetDate != "" {
		if t, perr := time.Parse(dbmodels.DateLayout, targetDate); perr != nil || t.Format(dbmodels.DateLayout) != targetDate {
			errResult(http.StatusBadRequest, "invalid date, expected YYYY-MM-DD").write(w)
			return
		}
	}
	resp, err := s.today(s.db.Read(), u.ID, targetDate, redact, isReadToken(r))
	if err != nil {
		log.Error().Err(err).Msg("view/today failed")
		errResult(http.StatusInternalServerError, "internal error").write(w)
		return
	}
	jsonResult(http.StatusOK, resp).write(w)
}

func (s *server) today(db *gorm.DB, userID string, requestedDate string, redact, hideSensitive bool) (*todayResponse, error) {
	st, err := userSettings(db, userID)
	if err != nil {
		return nil, err
	}
	tz := st.Timezone
	cal := s.calendarFor(st)
	date := requestedDate
	if date == "" {
		date, err = schedule.Today(s.now(), tz)
		if err != nil {
			return nil, err
		}
	}
	day, err := time.Parse(dbmodels.DateLayout, date)
	if err != nil {
		return nil, err
	}
	resp := &todayResponse{
		Date:     date,
		Weekday:  int(day.Weekday()),
		Timezone: tz,
		Items:    []todayItem{},
		Upcoming: []upcomingItem{},
	}

	var items []dbmodels.Item
	if err := db.Where("user_id = ? AND archived = ?", userID, false).
		Order("sort_order, id").Find(&items).Error; err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return resp, nil
	}
	ids := make([]uint, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}

	endRange := day.AddDate(0, 0, 14).Format(dbmodels.DateLayout)
	var versions []dbmodels.ItemRuleVersion
	if err := db.Where("item_id IN ?", ids).Find(&versions).Error; err != nil {
		return nil, err
	}
	byItem := map[uint][]dbmodels.ItemRuleVersion{}
	for _, v := range versions {
		byItem[v.ItemID] = append(byItem[v.ItemID], v)
	}

	var logs []dbmodels.CheckinLog
	if err := db.Where("user_id = ? AND checkin_date = ? AND item_id IN ?", userID, date, ids).
		Order("id").Find(&logs).Error; err != nil {
		return nil, err
	}
	logIDs := map[uint][]uint{}
	dayLogs := map[uint][]dbmodels.CheckinLog{}
	for _, l := range logs {
		logIDs[l.ItemID] = append(logIDs[l.ItemID], l.ID)
		dayLogs[l.ItemID] = append(dayLogs[l.ItemID], l)
	}
	var options []dbmodels.ItemOption
	if err := db.Where("item_id IN ?", ids).Find(&options).Error; err != nil {
		return nil, err
	}
	optsByItem := map[uint][]dbmodels.ItemOption{}
	for _, o := range options {
		optsByItem[o.ItemID] = append(optsByItem[o.ItemID], o)
	}

	sens, err := sensitiveFn(db, userID)
	if err != nil {
		return nil, err
	}

	lookbackStart := day.AddDate(0, 0, -60).Format(dbmodels.DateLayout)
	yesterday := day.AddDate(0, 0, -1).Format(dbmodels.DateLayout)

	for _, it := range items {
		if hideSensitive && sens(it) {
			continue
		}
		vs := effectiveVersionsFor(it, byItem[it.ID])
		due, err := schedule.IsDueWith(vs, date, cal)
		if errors.Is(err, schedule.ErrUnsupported) {
			continue // rolling/quota items have their own sections (later phases)
		}
		if err != nil {
			// A broken stored rule must not take the whole view down. Log ids only.
			log.Warn().Err(err).Uint("item_id", it.ID).Msg("view/today: skipping item with unevaluable rule")
			continue
		}

		isOverdue := false
		overdueDate := ""
		hasLogsToday := len(logIDs[it.ID]) > 0
		var eventCompletedLog *dbmodels.CheckinLog
		if it.Kind == dbmodels.KindEvent {
			pastDates, pErr := schedule.DueDatesWith(fixedOnly(vs), lookbackStart, yesterday, cal)
			if pErr == nil && len(pastDates) > 0 {
				lastDue := pastDates[len(pastDates)-1]
				var logCount int64
				if err := db.Model(&dbmodels.CheckinLog{}).
					Where("user_id = ? AND item_id = ? AND checkin_date >= ?", userID, it.ID, pastDates[0]).
					Count(&logCount).Error; err == nil {
					if logCount == 0 {
						due = true
						isOverdue = true
						overdueDate = lastDue
					} else if hasLogsToday {
						// Completed today.
						overdueDate = lastDue
					}
				}
			}
			// If an event is due on this date, but has no logs on this date, check if it was completed on a later date.
			if due && !hasLogsToday {
				var l dbmodels.CheckinLog
				if err := db.Where("user_id = ? AND item_id = ? AND checkin_date >= ?", userID, it.ID, date).
					Order("checkin_date ASC, id ASC").First(&l).Error; err == nil {
					eventCompletedLog = &l
				}
			}
		}

		if !due && hasLogsToday {
			due = true
		}

		if !due {
			continue
		}
		itemLogs := logIDs[it.ID]
		if len(itemLogs) == 0 && eventCompletedLog != nil {
			itemLogs = []uint{eventCompletedLog.ID}
		}
		ti := todayItem{
			ID: it.ID, Name: it.Name, CategoryID: it.CategoryID, ProjectID: it.ProjectID,
			TrackerType: string(it.TrackerType), Kind: string(it.Kind),
			Color:       it.Color,
			IsSensitive: sens(it),
			Due:         true, Overdue: isOverdue, DueDate: overdueDate,
			LogIDs: itemLogs,
		}
		if ti.LogIDs == nil {
			ti.LogIDs = []uint{}
		}
		ti.Done = len(ti.LogIDs) > 0
		if cur := schedule.CurrentVersion(vs, date); cur != nil && ti.Done && len(dayLogs[it.ID]) > 0 {
			// DayCompletion (DESIGN 2/2.1): targets, options and completion_rule.
			ti.Done = schedule.DayCompletion(it.TrackerType, *cur, optsByItem[it.ID], dayLogs[it.ID], it.Aggregate).Done
		}
		if redact && ti.IsSensitive {
			ti.Name = privateItemName
		}
		resp.Items = append(resp.Items, ti)
		if countsInProgress(it, vs, date) {
			resp.Progress.Total++
			if ti.Done {
				resp.Progress.Done++
			}
		}
	}

	tomorrow := day.AddDate(0, 0, 1).Format(dbmodels.DateLayout)
	for _, it := range items {
		if hideSensitive && sens(it) {
			continue
		}
		vs := effectiveVersionsFor(it, byItem[it.ID])
		fvs := fixedOnly(vs)
		if cur := schedule.CurrentVersion(fvs, tomorrow); isDailyFixedRule(cur) {
			continue
		}
		futureDates, err := schedule.DueDatesWith(fvs, tomorrow, endRange, cal)
		if err != nil {
			continue
		}
		for _, fd := range futureDates {
			tFd, pErr := time.Parse(dbmodels.DateLayout, fd)
			if pErr != nil {
				continue
			}
			daysUntil := int(tFd.Sub(day).Hours() / 24)
			name := it.Name
			if redact && sens(it) {
				name = privateItemName
			}
			resp.Upcoming = append(resp.Upcoming, upcomingItem{
				ID:          it.ID,
				Name:        name,
				CategoryID:  it.CategoryID,
				ProjectID:   it.ProjectID,
				TrackerType: string(it.TrackerType),
				Kind:        string(it.Kind),
				Color:       it.Color,
				IsSensitive: sens(it),
				DueDate:     fd,
				DaysUntil:   daysUntil,
			})
		}
	}

	sort.SliceStable(resp.Upcoming, func(i, j int) bool {
		if resp.Upcoming[i].DueDate != resp.Upcoming[j].DueDate {
			return resp.Upcoming[i].DueDate < resp.Upcoming[j].DueDate
		}
		return resp.Upcoming[i].DaysUntil < resp.Upcoming[j].DaysUntil
	})

	return resp, nil
}

// countsInProgress implements the completion-ring rule (DESIGN 2): positive,
// non-maintenance habits governed by a fixed_rrule rule today. Events are
// excluded as well (DESIGN 11.3 / completion-ring note).
func countsInProgress(it dbmodels.Item, versions []dbmodels.ItemRuleVersion, date string) bool {
	if it.Polarity != dbmodels.PolarityPositive || it.TrackerType == dbmodels.TrackerMaintenance || it.Kind == dbmodels.KindEvent {
		return false
	}
	cur := schedule.CurrentVersion(versions, date)
	return cur != nil && cur.ScheduleType == dbmodels.ScheduleFixedRRule
}

// isDailyFixedRule identifies rules that repeat every single day. Daily recurring
// habits already belong on the daily Today dashboard and must not flood the
// Upcoming schedule with duplicate entries for every day.
func isDailyFixedRule(v *dbmodels.ItemRuleVersion) bool {
	if v == nil || v.ScheduleType != dbmodels.ScheduleFixedRRule {
		return false
	}
	s := strings.ToUpper(v.RRuleString)
	if !strings.Contains(s, "FREQ=DAILY") {
		return false
	}
	if strings.Contains(s, "COUNT=1") {
		return false
	}
	if strings.Contains(s, "BYDAY") || strings.Contains(s, "BYMONTHDAY") || strings.Contains(s, "BYYEARDAY") {
		return false
	}
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "INTERVAL=") {
			var n int
			if _, err := fmt.Sscanf(part, "INTERVAL=%d", &n); err == nil && n > 1 {
				return false
			}
		}
	}
	return true
}

package api

import (
	"errors"
	"net/http"
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
	TrackerType string `json:"tracker_type"`
	IsSensitive bool   `json:"is_sensitive"`
	Due         bool   `json:"due"`
	Done        bool   `json:"done"`
	LogIDs      []uint `json:"log_ids"`
}

type todayResponse struct {
	Date     string        `json:"date"`
	Weekday  int           `json:"weekday"`
	Timezone string        `json:"timezone"`
	Progress todayProgress `json:"progress"`
	Items    []todayItem   `json:"items"`
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
	resp, err := s.today(s.db.Read(), u.ID, redact)
	if err != nil {
		log.Error().Err(err).Msg("view/today failed")
		errResult(http.StatusInternalServerError, "internal error").write(w)
		return
	}
	jsonResult(http.StatusOK, resp).write(w)
}

func (s *server) today(db *gorm.DB, userID string, redact bool) (*todayResponse, error) {
	st, err := userSettings(db, userID)
	if err != nil {
		return nil, err
	}
	tz := st.Timezone
	cal := s.calendarFor(st)
	date, err := schedule.Today(s.now(), tz)
	if err != nil {
		return nil, err
	}
	day, err := time.Parse(dbmodels.DateLayout, date)
	if err != nil {
		return nil, err
	}
	resp := &todayResponse{Date: date, Weekday: int(day.Weekday()), Timezone: tz, Items: []todayItem{}}

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

	var versions []dbmodels.ItemRuleVersion
	if err := db.Where("item_id IN ? AND effective_from <= ?", ids, date).Find(&versions).Error; err != nil {
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

	for _, it := range items {
		vs := byItem[it.ID]
		due, err := schedule.IsDueWith(vs, date, cal)
		if errors.Is(err, schedule.ErrUnsupported) {
			continue // rolling/quota items have their own sections (later phases)
		}
		if err != nil {
			// A broken stored rule must not take the whole view down. Log ids only.
			log.Warn().Err(err).Uint("item_id", it.ID).Msg("view/today: skipping item with unevaluable rule")
			continue
		}
		if !due {
			continue
		}
		ti := todayItem{
			ID: it.ID, Name: it.Name, CategoryID: it.CategoryID,
			TrackerType: string(it.TrackerType), IsSensitive: sens(it),
			Due: true, LogIDs: logIDs[it.ID],
		}
		if ti.LogIDs == nil {
			ti.LogIDs = []uint{}
		}
		ti.Done = len(ti.LogIDs) > 0
		if cur := schedule.CurrentVersion(vs, date); cur != nil && ti.Done {
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

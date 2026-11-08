package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/elmhuangyu/daka/backend/lib/auth"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/extcal"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
	"github.com/elmhuangyu/daka/backend/lib/secret"
)

// maxExternalWindowDays bounds GET /api/calendar/external.
const maxExternalWindowDays = 62

type externalCalendarOut struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URLHint string `json:"url_hint"`
}

type calendarSettings struct {
	Timezone            string                `json:"timezone"`
	WeekStart           int                   `json:"week_start"`
	HolidayRegion       string                `json:"holiday_region"`
	StreakHolidayFreeze bool                  `json:"streak_holiday_freeze"`
	ExternalCalendars   []externalCalendarOut `json:"external_calendars"`
}

type externalCalendarIn struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	URL  *string `json:"url"` // absent / null: keep the stored URL
	// URLHint is accepted (clients may echo a GET response back) and ignored.
	URLHint string `json:"url_hint"`
}

type calendarSettingsPut struct {
	Timezone            *string               `json:"timezone"`
	WeekStart           *int                  `json:"week_start"`
	HolidayRegion       *string               `json:"holiday_region"`
	StreakHolidayFreeze *bool                 `json:"streak_holiday_freeze"`
	ExternalCalendars   *[]externalCalendarIn `json:"external_calendars"`
}

func (s *server) calendarSettingsOf(st dbmodels.UserSettings) (calendarSettings, error) {
	views, err := s.ext.Store.Views(st.UserID, st.ExternalCalendarURLs)
	if err != nil {
		return calendarSettings{}, err
	}
	out := make([]externalCalendarOut, 0, len(views))
	for _, v := range views {
		out = append(out, externalCalendarOut{ID: v.ID, Name: v.Name, URLHint: v.URLHint})
	}
	tz := st.Timezone
	if tz == "" {
		tz = "UTC"
	}
	return calendarSettings{
		Timezone: tz, WeekStart: st.WeekStart, HolidayRegion: string(st.HolidayRegion),
		StreakHolidayFreeze: st.StreakHolidayFreeze, ExternalCalendars: out,
	}, nil
}

func (s *server) loadSettings(tx *gorm.DB, userID string) (dbmodels.UserSettings, error) {
	st := dbmodels.UserSettings{UserID: userID, Timezone: "UTC", WeekStart: 1}
	err := tx.Where("user_id = ?", userID).Take(&st).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return st, err
	}
	return st, nil
}

func (s *server) getCalendarSettings(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		errResult(http.StatusUnauthorized, "unauthorized").write(w)
		return
	}
	st, err := s.loadSettings(s.db.Read(), u.ID)
	if err == nil {
		var out calendarSettings
		if out, err = s.calendarSettingsOf(st); err == nil {
			jsonResult(http.StatusOK, out).write(w)
			return
		}
	}
	log.Error().Err(err).Msg("get calendar settings failed")
	errResult(http.StatusInternalServerError, "internal error").write(w)
}

func (s *server) putCalendarSettings(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		errResult(http.StatusUnauthorized, "unauthorized").write(w)
		return
	}
	var req calendarSettingsPut
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		errResult(http.StatusBadRequest, "invalid request body").write(w)
		return
	}
	var out calendarSettings
	err := s.db.Write().Transaction(func(tx *gorm.DB) error {
		st, err := s.loadSettings(tx, u.ID)
		if err != nil {
			return err
		}
		upd := map[string]any{}
		bump := false
		if req.Timezone != nil {
			if _, err := schedule.Today(s.now(), *req.Timezone); err != nil {
				return inputErr("invalid timezone")
			}
			if *req.Timezone != st.Timezone {
				st.Timezone, upd["timezone"], bump = *req.Timezone, *req.Timezone, true
			}
		}
		if req.WeekStart != nil {
			if *req.WeekStart != 0 && *req.WeekStart != 1 {
				return inputErr("invalid week_start")
			}
			if *req.WeekStart != st.WeekStart {
				st.WeekStart, upd["week_start"], bump = *req.WeekStart, *req.WeekStart, true
			}
		}
		if req.HolidayRegion != nil {
			switch dbmodels.HolidayRegion(*req.HolidayRegion) {
			case dbmodels.RegionCN, dbmodels.RegionUS:
			case st.HolidayRegion: // echoing the current (possibly unset) value is a no-op
			default:
				return inputErr("invalid holiday_region")
			}
			if *req.HolidayRegion != string(st.HolidayRegion) {
				st.HolidayRegion, upd["holiday_region"], bump = dbmodels.HolidayRegion(*req.HolidayRegion), *req.HolidayRegion, true
			}
		}
		if req.StreakHolidayFreeze != nil && *req.StreakHolidayFreeze != st.StreakHolidayFreeze {
			st.StreakHolidayFreeze, upd["streak_holiday_freeze"], bump = *req.StreakHolidayFreeze, *req.StreakHolidayFreeze, true
		}
		if req.ExternalCalendars != nil {
			in := make([]extcal.Input, 0, len(*req.ExternalCalendars))
			for _, c := range *req.ExternalCalendars {
				in = append(in, extcal.Input{ID: c.ID, Name: c.Name, URL: c.URL})
			}
			col, err := s.ext.Store.Apply(u.ID, st.ExternalCalendarURLs, in)
			if err != nil {
				return err
			}
			if col != st.ExternalCalendarURLs {
				st.ExternalCalendarURLs, upd["external_calendar_urls"] = col, col
			}
		}
		if bump {
			upd["state_version"] = gorm.Expr("state_version + 1")
		}
		if len(upd) > 0 {
			// Make sure the row exists, then apply the partial update.
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
				Create(&dbmodels.UserSettings{UserID: u.ID, Timezone: "UTC", WeekStart: 1}).Error; err != nil {
				return err
			}
			if err := tx.Model(&dbmodels.UserSettings{}).Where("user_id = ?", u.ID).Updates(upd).Error; err != nil {
				return err
			}
		}
		out, err = s.calendarSettingsOf(st)
		return err
	})
	var ie extcal.InputError
	switch {
	case err == nil:
		jsonResult(http.StatusOK, out).write(w)
	case errors.As(err, &ie):
		errResult(http.StatusBadRequest, ie.Error()).write(w)
	case errors.Is(err, secret.ErrNoKey):
		errResult(http.StatusServiceUnavailable, "encryption key not configured").write(w)
	default:
		log.Error().Err(err).Msg("put calendar settings failed")
		errResult(http.StatusInternalServerError, "internal error").write(w)
	}
}

func inputErr(msg string) error { return extcal.InputError(msg) }

type externalEventOut struct {
	Title  string `json:"title"`
	Start  string `json:"start"`
	End    string `json:"end"`
	AllDay bool   `json:"all_day"`
}

type externalSourceOut struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	OK        bool               `json:"ok"`
	Error     string             `json:"error,omitempty"`
	Events    []externalEventOut `json:"events"`
	Skipped   int                `json:"skipped,omitempty"`
	Truncated bool               `json:"truncated,omitempty"`
}

// getExternalCalendar serves the overlay layer. It is deliberately independent
// of the main calendar view: every source is fetched on its own and a failing
// source only marks itself as failed.
func (s *server) getExternalCalendar(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		errResult(http.StatusUnauthorized, "unauthorized").write(w)
		return
	}
	st, err := s.loadSettings(s.db.Read(), u.ID)
	if err != nil {
		log.Error().Err(err).Msg("external calendar: load settings failed")
		errResult(http.StatusInternalServerError, "internal error").write(w)
		return
	}
	loc, err := time.LoadLocation(st.Timezone)
	if err != nil {
		loc = time.UTC
	}
	q := r.URL.Query()
	start, err1 := time.ParseInLocation(dbmodels.DateLayout, q.Get("start"), loc)
	end, err2 := time.ParseInLocation(dbmodels.DateLayout, q.Get("end"), loc)
	if err1 != nil || err2 != nil || end.Before(start) {
		errResult(http.StatusBadRequest, "invalid start or end").write(w)
		return
	}
	to := end.AddDate(0, 0, 1)
	if start.AddDate(0, 0, maxExternalWindowDays).Before(to) {
		errResult(http.StatusBadRequest, "window too large").write(w)
		return
	}
	results, err := s.ext.Load(r.Context(), u.ID, st.ExternalCalendarURLs, q.Get("source_id"),
		extcal.ParseOptions{Loc: loc, From: start, To: to})
	switch {
	case errors.Is(err, extcal.ErrUnknownSource):
		errResult(http.StatusNotFound, "unknown source").write(w)
		return
	case err != nil:
		log.Error().Err(err).Msg("external calendar: load failed")
		errResult(http.StatusInternalServerError, "internal error").write(w)
		return
	}
	out := make([]externalSourceOut, 0, len(results))
	for _, sr := range results {
		o := externalSourceOut{ID: sr.ID, Name: sr.Name, OK: sr.OK, Error: sr.Error, Skipped: sr.Skipped, Truncated: sr.Truncated,
			Events: make([]externalEventOut, 0, len(sr.Events))}
		for _, e := range sr.Events {
			if e.AllDay {
				last := e.End.AddDate(0, 0, -1)
				if last.Before(e.Start) {
					last = e.Start
				}
				o.Events = append(o.Events, externalEventOut{Title: e.Title, AllDay: true,
					Start: e.Start.Format(dbmodels.DateLayout), End: last.Format(dbmodels.DateLayout)})
				continue
			}
			o.Events = append(o.Events, externalEventOut{Title: e.Title,
				Start: e.Start.Format(time.RFC3339), End: e.End.Format(time.RFC3339)})
		}
		out = append(out, o)
	}
	w.Header().Set("Cache-Control", "private, no-store")
	jsonResult(http.StatusOK, map[string]any{"start": q.Get("start"), "end": q.Get("end"), "timezone": loc.String(), "sources": out}).write(w)
}

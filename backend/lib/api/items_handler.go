package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/auth"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
)

const maxNameLen = 200

type ruleRequest struct {
	ScheduleType string  `json:"schedule_type"`
	RRuleString  string  `json:"rrule_string"`
	DtStart      string  `json:"dtstart"`
	Until        *string `json:"until"`
	HolidayMode  string  `json:"holiday_mode"`
}

type createItemRequest struct {
	Name        string      `json:"name"`
	TrackerType string      `json:"tracker_type"`
	Kind        string      `json:"kind"`
	Polarity    string      `json:"polarity"`
	CategoryID  *uint       `json:"category_id"`
	IsSensitive bool        `json:"is_sensitive"`
	Rule        ruleRequest `json:"rule"`
}

type ruleResponse struct {
	ScheduleType  string  `json:"schedule_type"`
	RRuleString   string  `json:"rrule_string"`
	DtStart       string  `json:"dtstart"`
	Until         *string `json:"until"`
	HolidayMode   string  `json:"holiday_mode"`
	EffectiveFrom string  `json:"effective_from"`
}

type itemResponse struct {
	ID          uint         `json:"id"`
	Name        string       `json:"name"`
	TrackerType string       `json:"tracker_type"`
	Kind        string       `json:"kind"`
	Polarity    string       `json:"polarity"`
	CategoryID  *uint        `json:"category_id"`
	IsSensitive bool         `json:"is_sensitive"`
	CurrentRule ruleResponse `json:"current_rule"`
}

// postItem is the minimal Phase-0 item creation (P1-9 extends it). Error
// messages never include user-supplied values such as the item name.
func (s *server) postItem(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		errResult(http.StatusUnauthorized, "unauthorized").write(w)
		return
	}
	var req createItemRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err := dec.Decode(&req); err != nil {
		errResult(http.StatusBadRequest, "invalid JSON body").write(w)
		return
	}
	if _, err := dec.Token(); err != io.EOF {
		errResult(http.StatusBadRequest, "invalid JSON body").write(w)
		return
	}
	res, err := s.createItem(u.ID, req)
	if err != nil {
		errResult(http.StatusInternalServerError, "internal error").write(w)
		return
	}
	res.write(w)
}

func (s *server) createItem(userID string, req createItemRequest) (result, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return errResult(http.StatusBadRequest, "name is required"), nil
	}
	if utf8.RuneCountInString(name) > maxNameLen {
		return errResult(http.StatusBadRequest, "name too long"), nil
	}
	if req.TrackerType != string(dbmodels.TrackerBoolean) {
		return errResult(http.StatusBadRequest, "not supported yet"), nil
	}
	kind := dbmodels.Kind(req.Kind)
	switch kind {
	case "":
		kind = dbmodels.KindHabit
	case dbmodels.KindHabit, dbmodels.KindEvent:
	default:
		return errResult(http.StatusBadRequest, "invalid kind"), nil
	}
	switch req.Polarity {
	case "", string(dbmodels.PolarityPositive):
	default:
		return errResult(http.StatusBadRequest, "not supported yet"), nil
	}
	if req.Rule.ScheduleType != string(dbmodels.ScheduleFixedRRule) {
		return errResult(http.StatusBadRequest, "not supported yet"), nil
	}
	switch req.Rule.HolidayMode {
	case "", string(dbmodels.HolidayNone):
	default:
		return errResult(http.StatusBadRequest, "not supported yet"), nil
	}

	var out result
	err := s.db.Write().Transaction(func(tx *gorm.DB) error {
		today, err := s.userToday(tx, userID)
		if err != nil {
			return err
		}
		dtstart := req.Rule.DtStart
		if dtstart == "" {
			dtstart = today
		}
		ver := dbmodels.ItemRuleVersion{
			EffectiveFrom: today, ScheduleType: dbmodels.ScheduleFixedRRule,
			DtStart: dtstart, Until: req.Rule.Until, RRuleString: req.Rule.RRuleString,
			HolidayMode: dbmodels.HolidayNone,
		}
		if ver.Until != nil && *ver.Until == "" {
			ver.Until = nil
		}
		if err := schedule.ValidateRule(ver); err != nil {
			out = errResult(http.StatusBadRequest, "invalid rule")
			return nil
		}
		sensitive := req.IsSensitive
		if req.CategoryID != nil {
			var cat dbmodels.Category
			err := tx.Where("id = ? AND user_id = ?", *req.CategoryID, userID).Take(&cat).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				out = errResult(http.StatusBadRequest, "invalid category")
				return nil
			}
			if err != nil {
				return err
			}
			// DESIGN 4.1 rule 1: a sensitive category makes its items sensitive.
			sensitive = sensitive || cat.IsSensitive
		}
		item := dbmodels.Item{
			UserID: userID, CategoryID: req.CategoryID, Name: name,
			TrackerType: dbmodels.TrackerBoolean, Kind: kind,
			Polarity: dbmodels.PolarityPositive, IsSensitive: sensitive,
		}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		ver.ItemID = item.ID
		if err := tx.Create(&ver).Error; err != nil {
			return err
		}
		out = jsonResult(http.StatusCreated, itemResponse{
			ID: item.ID, Name: item.Name, TrackerType: string(item.TrackerType),
			Kind: string(item.Kind), Polarity: string(item.Polarity),
			CategoryID: item.CategoryID, IsSensitive: item.IsSensitive,
			CurrentRule: ruleResponse{
				ScheduleType: string(ver.ScheduleType), RRuleString: ver.RRuleString,
				DtStart: ver.DtStart, Until: ver.Until, HolidayMode: string(ver.HolidayMode),
				EffectiveFrom: ver.EffectiveFrom,
			},
		})
		return nil
	})
	return out, err
}

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/auth"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
)

const (
	maxNameLen = 200
	maxOptions = 50
)

type optionRequest struct {
	Name        string   `json:"name"`
	Unit        string   `json:"unit"`
	Step        float64  `json:"step"`
	MinVal      *float64 `json:"min_val"`
	MaxVal      *float64 `json:"max_val"`
	Aggregate   string   `json:"aggregate"`
	SortOrder   int      `json:"sort_order"`
	TargetValue *float64 `json:"target_value"`
}

type ruleRequest struct {
	ScheduleType string  `json:"schedule_type"`
	RRuleString  string  `json:"rrule_string"`
	DtStart      string  `json:"dtstart"`
	Until        *string `json:"until"`
	HolidayMode  string  `json:"holiday_mode"`

	RollingInterval int    `json:"rolling_interval"`
	RollingUnit     string `json:"rolling_unit"`

	QuotaComparator string  `json:"quota_comparator"`
	QuotaCount      float64 `json:"quota_count"`
	QuotaPeriod     string  `json:"quota_period"`
	QuotaWindow     string  `json:"quota_window"`
	QuotaMeasure    string  `json:"quota_measure"`
	// QuotaOptionIndex is the 0-based index into the request's options (the
	// option ids do not exist yet at creation time).
	QuotaOptionIndex *int `json:"quota_option_index"`

	TargetValue    *float64 `json:"target_value"`
	CompletionRule string   `json:"completion_rule"`
	CompletionN    int      `json:"completion_n"`
	StaleAfterDays int      `json:"stale_after_days"`
}

type createItemRequest struct {
	Name        string          `json:"name"`
	TrackerType string          `json:"tracker_type"`
	Kind        string          `json:"kind"`
	Polarity    string          `json:"polarity"`
	Color       string          `json:"color,omitempty"`
	CategoryID  *uint           `json:"category_id"`
	ProjectID   *uint           `json:"project_id"`
	ParentID    *uint           `json:"parent_id"`
	IsSensitive bool            `json:"is_sensitive"`
	Unit        string          `json:"unit"`
	Step        float64         `json:"step"`
	MinVal      *float64        `json:"min_val"`
	MaxVal      *float64        `json:"max_val"`
	Aggregate   string          `json:"aggregate"`
	WarningDays int             `json:"warning_days"`
	Options     []optionRequest `json:"options"`
	Rule        ruleRequest     `json:"rule"`
}

type optionResponse struct {
	ID          uint     `json:"id"`
	Name        string   `json:"name"`
	Unit        string   `json:"unit"`
	Step        float64  `json:"step"`
	MinVal      *float64 `json:"min_val"`
	MaxVal      *float64 `json:"max_val"`
	Aggregate   string   `json:"aggregate"`
	SortOrder   int      `json:"sort_order"`
	TargetValue *float64 `json:"target_value"`
	Archived    bool     `json:"archived,omitempty"`
}

type ruleResponse struct {
	ScheduleType  string  `json:"schedule_type"`
	RRuleString   string  `json:"rrule_string"`
	DtStart       string  `json:"dtstart"`
	Until         *string `json:"until"`
	HolidayMode   string  `json:"holiday_mode"`
	EffectiveFrom string  `json:"effective_from"`

	RollingInterval int    `json:"rolling_interval,omitempty"`
	RollingUnit     string `json:"rolling_unit,omitempty"`

	QuotaComparator string  `json:"quota_comparator,omitempty"`
	QuotaCount      float64 `json:"quota_count,omitempty"`
	QuotaPeriod     string  `json:"quota_period,omitempty"`
	QuotaWindow     string  `json:"quota_window,omitempty"`
	QuotaMeasure    string  `json:"quota_measure,omitempty"`
	QuotaOptionID   *uint   `json:"quota_option_id,omitempty"`

	TargetValue    *float64           `json:"target_value,omitempty"`
	CompletionRule string             `json:"completion_rule,omitempty"`
	CompletionN    int                `json:"completion_n,omitempty"`
	StaleAfterDays int                `json:"stale_after_days,omitempty"`
	OptionTargets  map[string]float64 `json:"option_targets"`
}

type itemResponse struct {
	ID          uint    `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Pinned      bool    `json:"pinned"`
	SortOrder   int     `json:"sort_order"`
	Archived    bool    `json:"archived"`
	RemindTime  *string `json:"remind_time"`
	TrackerType string  `json:"tracker_type"`
	Kind        string  `json:"kind"`
	Polarity    string  `json:"polarity"`
	Color       string  `json:"color,omitempty"`
	CategoryID  *uint   `json:"category_id"`
	ProjectID   *uint   `json:"project_id"`
	ParentID    *uint   `json:"parent_id"`
	IsSensitive bool    `json:"is_sensitive"`
	// EffectiveSensitive is Item.is_sensitive OR the category's flag (DESIGN 4.1).
	EffectiveSensitive bool             `json:"effective_sensitive"`
	Unit               string           `json:"unit,omitempty"`
	Step               float64          `json:"step,omitempty"`
	MinVal             *float64         `json:"min_val,omitempty"`
	MaxVal             *float64         `json:"max_val,omitempty"`
	Aggregate          string           `json:"aggregate,omitempty"`
	WarningDays        int              `json:"warning_days,omitempty"`
	Options            []optionResponse `json:"options"`
	CurrentRule        ruleResponse     `json:"current_rule"`
	// Stats is only set by GET /api/items?include=stats.
	Stats *itemStatsSummary `json:"stats,omitempty"`
}

func newRuleResponse(ver dbmodels.ItemRuleVersion) ruleResponse {
	r := ruleResponse{
		ScheduleType: string(ver.ScheduleType), RRuleString: ver.RRuleString,
		DtStart: ver.DtStart, Until: ver.Until, HolidayMode: string(ver.HolidayMode),
		EffectiveFrom:   ver.EffectiveFrom,
		RollingInterval: ver.RollingInterval, RollingUnit: string(ver.RollingUnit),
		QuotaComparator: string(ver.QuotaComparator), QuotaCount: ver.QuotaCount,
		QuotaPeriod: string(ver.QuotaPeriod), QuotaWindow: string(ver.QuotaWindow),
		QuotaMeasure: string(ver.QuotaMeasure), QuotaOptionID: ver.QuotaOptionID,
		TargetValue: ver.TargetValue, CompletionRule: string(ver.CompletionRule),
		CompletionN: ver.CompletionN, StaleAfterDays: ver.StaleAfterDays,
		OptionTargets: map[string]float64{},
	}
	if ver.OptionTargets != "" {
		_ = json.Unmarshal([]byte(ver.OptionTargets), &r.OptionTargets)
	}
	return r
}

// invalidCombination is the 400 for every ErrInvalid from the schedule
// package. It carries a stable reason code and never echoes user input.
func invalidCombination(reason string) result {
	return jsonResult(http.StatusBadRequest, map[string]string{"error": "invalid combination", "reason": reason})
}

// postItem creates an item with optional inline options and its first rule
// version in one transaction. Error messages never include user-supplied
// values such as the item name.
func (s *server) postItem(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		errResult(http.StatusUnauthorized, "unauthorized").write(w)
		return
	}
	var req createItemRequest
	if !decodeStrict(w, r, &req) {
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
	if len(req.Options) > maxOptions {
		return invalidCombination(schedule.ReasonInvalidOption), nil
	}
	for _, o := range req.Options {
		if strings.TrimSpace(o.Name) == "" || utf8.RuneCountInString(o.Name) > maxNameLen {
			return invalidCombination(schedule.ReasonInvalidOption), nil
		}
	}

	kind := dbmodels.Kind(req.Kind)
	if kind == "" {
		kind = dbmodels.KindHabit
	}
	polarity := dbmodels.Polarity(req.Polarity)
	if polarity == "" {
		polarity = dbmodels.PolarityPositive
	}
	tracker := dbmodels.TrackerType(req.TrackerType)
	st := dbmodels.ScheduleType(req.Rule.ScheduleType)
	if st == "" {
		// DESIGN 2.2: events default to no schedule.
		if kind == dbmodels.KindEvent {
			st = dbmodels.ScheduleNone
		} else if tracker == dbmodels.TrackerMaintenance {
			st = dbmodels.ScheduleRolling
		}
	}
	holiday := dbmodels.HolidayMode(req.Rule.HolidayMode)
	if holiday == "" {
		holiday = dbmodels.HolidayNone
	}

	spec := schedule.ItemSpec{
		TrackerType: tracker, Kind: kind, Polarity: polarity,
		Unit: req.Unit, Step: req.Step, MinVal: req.MinVal, MaxVal: req.MaxVal,
		Aggregate: dbmodels.Aggregate(req.Aggregate), WarningDays: req.WarningDays,
	}
	// Options get placeholder ids (index+1) for validation; real ids replace
	// them once the rows exist.
	optSpecs := make([]schedule.OptionSpec, len(req.Options))
	placeholder := map[string]float64{}
	for i, o := range req.Options {
		optSpecs[i] = schedule.OptionSpec{
			ID: uint(i + 1), Unit: o.Unit, Step: o.Step, MinVal: o.MinVal,
			MaxVal: o.MaxVal, Aggregate: dbmodels.Aggregate(o.Aggregate),
		}
		if o.TargetValue != nil {
			placeholder[strconv.Itoa(i+1)] = *o.TargetValue
		}
	}
	var out result
	err := s.db.Write().Transaction(func(tx *gorm.DB) error {
		today, err := s.userToday(tx, userID)
		if err != nil {
			return err
		}
		ver := dbmodels.ItemRuleVersion{
			EffectiveFrom: today, ScheduleType: st, DtStart: req.Rule.DtStart,
			Until: req.Rule.Until, RRuleString: req.Rule.RRuleString, HolidayMode: holiday,
			RollingInterval: req.Rule.RollingInterval, RollingUnit: dbmodels.RollingUnit(req.Rule.RollingUnit),
			QuotaComparator: dbmodels.QuotaComparator(req.Rule.QuotaComparator), QuotaCount: req.Rule.QuotaCount,
			QuotaPeriod: dbmodels.QuotaPeriod(req.Rule.QuotaPeriod), QuotaWindow: dbmodels.QuotaWindow(req.Rule.QuotaWindow),
			QuotaMeasure: dbmodels.QuotaMeasure(req.Rule.QuotaMeasure),
			TargetValue:  req.Rule.TargetValue, CompletionRule: dbmodels.CompletionRule(req.Rule.CompletionRule),
			CompletionN: req.Rule.CompletionN, StaleAfterDays: req.Rule.StaleAfterDays,
		}
		if ver.Until != nil && *ver.Until == "" {
			ver.Until = nil
		}
		if idx := req.Rule.QuotaOptionIndex; idx != nil {
			// An out-of-range index maps to an id no option has.
			id := uint(0)
			if *idx >= 0 && *idx < len(req.Options) {
				id = uint(*idx + 1)
			}
			ver.QuotaOptionID = &id
		}
		if len(placeholder) > 0 {
			b, _ := json.Marshal(placeholder)
			ver.OptionTargets = string(b)
		}
		if st == dbmodels.ScheduleFixedRRule && ver.DtStart == "" {
			ver.DtStart = today
		}
		if err := schedule.ValidateItemRule(spec, ver, optSpecs); err != nil {
			if !errors.Is(err, schedule.ErrInvalid) {
				return err
			}
			out = invalidCombination(schedule.Reason(err))
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
		if req.ParentID != nil {
			var parent dbmodels.Item
			err := tx.Where("id = ? AND user_id = ?", *req.ParentID, userID).Take(&parent).Error
			if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && parent.ParentID != nil) {
				out = errResult(http.StatusBadRequest, "invalid parent")
				return nil
			}
			if err != nil {
				return err
			}
			// Sub-items live in the parent's project and inherit its sensitivity.
			if req.ProjectID != nil && (parent.ProjectID == nil || *req.ProjectID != *parent.ProjectID) {
				out = errResult(http.StatusBadRequest, "sub-items follow their parent's project")
				return nil
			}
			req.ProjectID = parent.ProjectID
			sensFn, err := sensitiveFn(tx, userID)
			if err != nil {
				return err
			}
			sensitive = sensitive || sensFn(parent)
		}
		if req.ProjectID != nil {
			proj, found, err := loadOwnedProject(tx, userID, *req.ProjectID)
			if err != nil {
				return err
			}
			if !found {
				out = errResult(http.StatusBadRequest, "invalid project")
				return nil
			}
			projSens, err := projectSensitiveFn(tx, userID)
			if err != nil {
				return err
			}
			// DESIGN 4.1 rule 1: a sensitive project makes its items sensitive.
			sensitive = sensitive || projSens(proj)
		}
		if ver.QuotaMeasure == "" && st == dbmodels.ScheduleQuota {
			ver.QuotaMeasure = schedule.EffectiveMeasure("", polarity)
		}
		if ver.QuotaWindow == "" && st == dbmodels.ScheduleQuota {
			ver.QuotaWindow = dbmodels.QuotaWindowCalendar
		}
		item := dbmodels.Item{
			UserID: userID, CategoryID: req.CategoryID, ProjectID: req.ProjectID, ParentID: req.ParentID, Name: name, Color: req.Color,
			TrackerType: tracker, Kind: kind, Polarity: polarity, IsSensitive: sensitive,
			Unit: req.Unit, Step: req.Step, MinVal: req.MinVal, MaxVal: req.MaxVal,
			Aggregate: dbmodels.Aggregate(req.Aggregate), WarningDays: req.WarningDays,
		}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		opts := make([]dbmodels.ItemOption, len(req.Options))
		realTargets := map[string]float64{}
		for i, o := range req.Options {
			opts[i] = dbmodels.ItemOption{
				ItemID: item.ID, Name: strings.TrimSpace(o.Name), Unit: o.Unit, Step: o.Step,
				MinVal: o.MinVal, MaxVal: o.MaxVal, Aggregate: dbmodels.Aggregate(o.Aggregate),
				SortOrder: o.SortOrder,
			}
			if err := tx.Create(&opts[i]).Error; err != nil {
				return err
			}
			if o.TargetValue != nil {
				realTargets[strconv.FormatUint(uint64(opts[i].ID), 10)] = *o.TargetValue
			}
		}
		if idx := req.Rule.QuotaOptionIndex; idx != nil && *idx >= 0 && *idx < len(opts) {
			id := opts[*idx].ID
			ver.QuotaOptionID = &id
		}
		ver.OptionTargets = ""
		if len(realTargets) > 0 {
			b, err := json.Marshal(realTargets)
			if err != nil {
				return err
			}
			ver.OptionTargets = string(b)
		}
		ver.ItemID = item.ID
		if err := tx.Create(&ver).Error; err != nil {
			return err
		}
		resp := itemResponse{
			ID: item.ID, Name: item.Name, TrackerType: string(item.TrackerType),
			Kind: string(item.Kind), Polarity: string(item.Polarity),
			CategoryID: item.CategoryID, ProjectID: item.ProjectID, ParentID: item.ParentID, IsSensitive: item.IsSensitive, EffectiveSensitive: item.IsSensitive,
			Unit: item.Unit, Step: item.Step, MinVal: item.MinVal, MaxVal: item.MaxVal,
			Aggregate: string(item.Aggregate), WarningDays: item.WarningDays,
			Options:     make([]optionResponse, len(opts)),
			CurrentRule: newRuleResponse(ver),
		}
		for i, o := range opts {
			resp.Options[i] = optionResponse{
				ID: o.ID, Name: o.Name, Unit: o.Unit, Step: o.Step, MinVal: o.MinVal,
				MaxVal: o.MaxVal, Aggregate: string(o.Aggregate), SortOrder: o.SortOrder,
				TargetValue: req.Options[i].TargetValue,
			}
		}
		out = jsonResult(http.StatusCreated, resp)
		return nil
	})
	return out, err
}

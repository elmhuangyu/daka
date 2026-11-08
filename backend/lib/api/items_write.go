package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
)

const maxDescriptionLen = 2000

var remindTimeRe = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// nullable distinguishes an absent JSON field from an explicit null.
type nullable[T any] struct {
	Set  bool
	Null bool
	Val  T
}

func (n *nullable[T]) UnmarshalJSON(b []byte) error {
	n.Set = true
	if string(b) == "null" {
		n.Null = true
		return nil
	}
	return json.Unmarshal(b, &n.Val)
}

// patchRule is the rule/target part of PATCH /api/items/{id}. Absent fields
// keep the current value (when schedule_type is unchanged); "" clears until
// and 0 clears quota_option_id. option_targets merges per option id; a null
// value removes that option's target.
type patchRule struct {
	ScheduleType *string `json:"schedule_type"`
	RRuleString  *string `json:"rrule_string"`
	DtStart      *string `json:"dtstart"`
	Until        *string `json:"until"`
	HolidayMode  *string `json:"holiday_mode"`

	RollingInterval *int    `json:"rolling_interval"`
	RollingUnit     *string `json:"rolling_unit"`

	QuotaComparator *string  `json:"quota_comparator"`
	QuotaCount      *float64 `json:"quota_count"`
	QuotaPeriod     *string  `json:"quota_period"`
	QuotaWindow     *string  `json:"quota_window"`
	QuotaMeasure    *string  `json:"quota_measure"`
	QuotaOptionID   *uint    `json:"quota_option_id"`

	TargetValue    nullable[float64]   `json:"target_value"`
	CompletionRule *string             `json:"completion_rule"`
	CompletionN    *int                `json:"completion_n"`
	StaleAfterDays *int                `json:"stale_after_days"`
	OptionTargets  map[string]*float64 `json:"option_targets"`

	// EffectiveFrom defaults to the user's local today; a past date is a
	// retroactive edit.
	EffectiveFrom *string `json:"effective_from"`
}

type patchItemRequest struct {
	// Immutable: sending a different value is a 400.
	TrackerType *string `json:"tracker_type"`
	Kind        *string `json:"kind"`
	Polarity    *string `json:"polarity"`

	// Display fields, edited in place.
	Name        *string          `json:"name"`
	Description *string          `json:"description"`
	CategoryID  nullable[uint]   `json:"category_id"`
	IsSensitive *bool            `json:"is_sensitive"`
	Pinned      *bool            `json:"pinned"`
	SortOrder   *int             `json:"sort_order"`
	Archived    *bool            `json:"archived"`
	RemindTime  nullable[string] `json:"remind_time"`

	// Item-level numeric definition, edited in place (not versioned).
	Unit        *string           `json:"unit"`
	Step        *float64          `json:"step"`
	MinVal      nullable[float64] `json:"min_val"`
	MaxVal      nullable[float64] `json:"max_val"`
	Aggregate   *string           `json:"aggregate"`
	WarningDays *int              `json:"warning_days"`

	Rule *patchRule `json:"rule"`

	// confirmExposure comes from ?confirm_exposure=1, never from the body.
	confirmExposure bool
}

func str[T ~string](p *string, dst *T) {
	if p != nil {
		*dst = T(*p)
	}
}

// applyRulePatch merges p over cur. Changing schedule_type starts from a blank
// rule (schedule-specific fields do not carry over) but keeps the targets and
// completion settings, which are not tied to a schedule.
func applyRulePatch(cur dbmodels.ItemRuleVersion, p *patchRule) dbmodels.ItemRuleVersion {
	v := cur
	if p.ScheduleType != nil && dbmodels.ScheduleType(*p.ScheduleType) != cur.ScheduleType {
		v = dbmodels.ItemRuleVersion{
			ScheduleType: dbmodels.ScheduleType(*p.ScheduleType), HolidayMode: dbmodels.HolidayNone,
			TargetValue: cur.TargetValue, CompletionRule: cur.CompletionRule, CompletionN: cur.CompletionN,
			OptionTargets: cur.OptionTargets,
		}
	}
	str(p.ScheduleType, &v.ScheduleType)
	str(p.RRuleString, &v.RRuleString)
	str(p.DtStart, &v.DtStart)
	str(p.HolidayMode, &v.HolidayMode)
	str(p.RollingUnit, &v.RollingUnit)
	str(p.QuotaComparator, &v.QuotaComparator)
	str(p.QuotaPeriod, &v.QuotaPeriod)
	str(p.QuotaWindow, &v.QuotaWindow)
	str(p.QuotaMeasure, &v.QuotaMeasure)
	str(p.CompletionRule, &v.CompletionRule)
	if p.Until != nil {
		if *p.Until == "" {
			v.Until = nil
		} else {
			u := *p.Until
			v.Until = &u
		}
	}
	if p.RollingInterval != nil {
		v.RollingInterval = *p.RollingInterval
	}
	if p.QuotaCount != nil {
		v.QuotaCount = *p.QuotaCount
	}
	if p.QuotaOptionID != nil {
		if *p.QuotaOptionID == 0 {
			v.QuotaOptionID = nil
		} else {
			id := *p.QuotaOptionID
			v.QuotaOptionID = &id
		}
	}
	if p.TargetValue.Set {
		if p.TargetValue.Null {
			v.TargetValue = nil
		} else {
			t := p.TargetValue.Val
			v.TargetValue = &t
		}
	}
	if p.CompletionN != nil {
		v.CompletionN = *p.CompletionN
	}
	if p.StaleAfterDays != nil {
		v.StaleAfterDays = *p.StaleAfterDays
	}
	if p.OptionTargets != nil {
		t := optionTargetsOf(v)
		for k, val := range p.OptionTargets {
			if val == nil {
				delete(t, k)
			} else {
				t[k] = *val
			}
		}
		v.OptionTargets = marshalTargets(t)
	}
	return v
}

func marshalTargets(t map[string]float64) string {
	if len(t) == 0 {
		return ""
	}
	b, _ := json.Marshal(t) // map[string]float64 with finite values cannot fail
	return string(b)
}

// activeTargets returns the option targets of v restricted to active options.
func activeTargets(v dbmodels.ItemRuleVersion, active []dbmodels.ItemOption) map[string]float64 {
	all := optionTargetsOf(v)
	out := map[string]float64{}
	for _, o := range active {
		k := strconv.FormatUint(uint64(o.ID), 10)
		if t, ok := all[k]; ok {
			out[k] = t
		}
	}
	return out
}

func optionSpecs(opts []dbmodels.ItemOption) []schedule.OptionSpec {
	out := make([]schedule.OptionSpec, len(opts))
	for i, o := range opts {
		out[i] = schedule.OptionSpec{ID: o.ID, Unit: o.Unit, Step: o.Step, MinVal: o.MinVal, MaxVal: o.MaxVal, Aggregate: o.Aggregate}
	}
	return out
}

func itemSpec(it dbmodels.Item) schedule.ItemSpec {
	return schedule.ItemSpec{
		TrackerType: it.TrackerType, Kind: it.Kind, Polarity: it.Polarity, Unit: it.Unit, Step: it.Step,
		MinVal: it.MinVal, MaxVal: it.MaxVal, Aggregate: it.Aggregate, WarningDays: it.WarningDays,
	}
}

// loadActiveOptions returns the item's non-archived options. Archived options
// are excluded from every validation (quota_option_id, option_targets,
// completion_n).
func loadActiveOptions(tx *gorm.DB, itemID uint) ([]dbmodels.ItemOption, error) {
	var opts []dbmodels.ItemOption
	err := tx.Where("item_id = ? AND archived = ?", itemID, false).Order("sort_order, id").Find(&opts).Error
	return opts, err
}

func loadVersions(tx *gorm.DB, itemID uint) ([]dbmodels.ItemRuleVersion, error) {
	var vs []dbmodels.ItemRuleVersion
	err := tx.Where("item_id = ?", itemID).Find(&vs).Error
	return vs, err
}

// fillRuleDefaults applies the same defaults as item creation.
func fillRuleDefaults(v *dbmodels.ItemRuleVersion, polarity dbmodels.Polarity, today string) {
	if v.HolidayMode == "" {
		v.HolidayMode = dbmodels.HolidayNone
	}
	if v.ScheduleType == dbmodels.ScheduleFixedRRule && v.DtStart == "" {
		v.DtStart = today
	}
	if v.ScheduleType == dbmodels.ScheduleQuota {
		if v.QuotaMeasure == "" {
			v.QuotaMeasure = schedule.EffectiveMeasure("", polarity)
		}
		if v.QuotaWindow == "" {
			v.QuotaWindow = dbmodels.QuotaWindowCalendar
		}
	}
}

func sameRule(a, b dbmodels.ItemRuleVersion) bool {
	a.ID, b.ID, a.ItemID, b.ItemID, a.EffectiveFrom, b.EffectiveFrom = 0, 0, 0, 0, "", ""
	return reflect.DeepEqual(a, b)
}

// appendVersion stores ver as a new immutable version of the item.
func appendVersion(tx *gorm.DB, itemID uint, ver dbmodels.ItemRuleVersion, effectiveFrom string) error {
	ver.ID = 0
	ver.ItemID = itemID
	ver.EffectiveFrom = effectiveFrom
	return tx.Create(&ver).Error
}

// PATCH /api/items/{id}
func (s *server) patchItem(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req patchItemRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	req.confirmExposure = r.URL.Query().Get("confirm_exposure") == "1"
	res, err := s.txResult(func(tx *gorm.DB) (result, error) { return s.applyPatch(tx, userID, id, req) })
	if err != nil {
		internalError(w, err, "patch item failed")
		return
	}
	s.versions.Bump(id)
	res.write(w)
}

func (s *server) applyPatch(tx *gorm.DB, userID string, id uint, req patchItemRequest) (result, error) {
	// Row lock (PostgreSQL; SQLite serializes writers anyway) so a concurrent
	// category freeze cannot be overwritten by this read-modify-write.
	item, found, err := loadOwnedItem(tx.Clauses(clause.Locking{Strength: "UPDATE"}), userID, id)
	if err != nil {
		return result{}, err
	}
	if !found {
		return errResult(http.StatusNotFound, "item not found"), nil
	}
	if (req.TrackerType != nil && *req.TrackerType != string(item.TrackerType)) ||
		(req.Kind != nil && *req.Kind != string(item.Kind)) ||
		(req.Polarity != nil && *req.Polarity != string(item.Polarity)) {
		return errResult(http.StatusBadRequest, "tracker_type, kind and polarity cannot be changed"), nil
	}
	today, err := s.userToday(tx, userID)
	if err != nil {
		return result{}, err
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return errResult(http.StatusBadRequest, "name is required"), nil
		}
		if utf8.RuneCountInString(name) > maxNameLen {
			return errResult(http.StatusBadRequest, "name too long"), nil
		}
		item.Name = name
	}
	if req.Description != nil {
		if utf8.RuneCountInString(*req.Description) > maxDescriptionLen {
			return errResult(http.StatusBadRequest, "description too long"), nil
		}
		item.Description = *req.Description
	}
	if req.Pinned != nil {
		item.Pinned = *req.Pinned
	}
	if req.SortOrder != nil {
		item.SortOrder = *req.SortOrder
	}
	if req.Archived != nil {
		item.Archived = *req.Archived
	}
	if req.IsSensitive != nil {
		item.IsSensitive = *req.IsSensitive
	}
	if req.RemindTime.Set {
		if req.RemindTime.Null {
			item.RemindTime = nil
		} else {
			if !remindTimeRe.MatchString(req.RemindTime.Val) {
				return errResult(http.StatusBadRequest, "remind_time must be HH:MM"), nil
			}
			rt := req.RemindTime.Val
			item.RemindTime = &rt
		}
	}
	if req.CategoryID.Set {
		// DESIGN 4.1 rule 3: leaving a sensitive category freezes the item as
		// sensitive unless the caller confirms the exposure. Moving into another
		// sensitive category is covered by the rules below.
		leaving := false
		if item.CategoryID != nil && (req.CategoryID.Null || req.CategoryID.Val != *item.CategoryID) {
			var n int64
			if err := tx.Model(&dbmodels.Category{}).Where("id = ? AND user_id = ? AND is_sensitive = ?",
				*item.CategoryID, userID, true).Count(&n).Error; err != nil {
				return result{}, err
			}
			leaving = n > 0
		}
		if leaving && !req.confirmExposure {
			item.IsSensitive = true
		}
		if req.CategoryID.Null {
			item.CategoryID = nil
		} else {
			var cat dbmodels.Category
			err := tx.Where("id = ? AND user_id = ?", req.CategoryID.Val, userID).Take(&cat).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errResult(http.StatusBadRequest, "invalid category"), nil
			}
			if err != nil {
				return result{}, err
			}
			cid := cat.ID
			item.CategoryID = &cid
			// DESIGN 4.1 rule 1: a sensitive category makes its items sensitive.
			item.IsSensitive = item.IsSensitive || cat.IsSensitive
		}
	}

	defChanged := req.Unit != nil || req.Step != nil || req.MinVal.Set || req.MaxVal.Set ||
		req.Aggregate != nil || req.WarningDays != nil
	if req.Unit != nil {
		item.Unit = *req.Unit
	}
	if req.Step != nil {
		item.Step = *req.Step
	}
	if req.MinVal.Set {
		item.MinVal = nil
		if !req.MinVal.Null {
			v := req.MinVal.Val
			item.MinVal = &v
		}
	}
	if req.MaxVal.Set {
		item.MaxVal = nil
		if !req.MaxVal.Null {
			v := req.MaxVal.Val
			item.MaxVal = &v
		}
	}
	str(req.Aggregate, &item.Aggregate)
	if req.WarningDays != nil {
		item.WarningDays = *req.WarningDays
	}

	vs, err := loadVersions(tx, id)
	if err != nil {
		return result{}, err
	}
	cur, _ := currentOrLatest(vs, today)
	active, err := loadActiveOptions(tx, id)
	if err != nil {
		return result{}, err
	}

	var newVer *dbmodels.ItemRuleVersion
	effective := today
	if req.Rule != nil {
		if ef := req.Rule.EffectiveFrom; ef != nil {
			if _, ok := strictDate(*ef); !ok {
				return errResult(http.StatusBadRequest, "effective_from must be YYYY-MM-DD"), nil
			}
			effective = *ef
		}
		// Option targets of archived options never carry over.
		base := cur
		base.OptionTargets = marshalTargets(activeTargets(cur, active))
		v := applyRulePatch(base, req.Rule)
		fillRuleDefaults(&v, item.Polarity, today)
		v.EffectiveFrom = effective
		if req.Rule.EffectiveFrom != nil || !sameRule(cur, v) {
			newVer = &v
		}
	}

	if defChanged || newVer != nil {
		check := cur
		check.OptionTargets = marshalTargets(activeTargets(cur, active))
		if newVer != nil {
			check = *newVer
		}
		if err := schedule.ValidateItemRule(itemSpec(item), check, optionSpecs(active)); err != nil {
			return ruleErrResult(err)
		}
	}

	if err := tx.Model(&dbmodels.Item{}).Where("id = ?", item.ID).Select(
		"Name", "Description", "CategoryID", "IsSensitive", "Pinned", "SortOrder", "Archived", "RemindTime",
		"Unit", "Step", "MinVal", "MaxVal", "Aggregate", "WarningDays").Updates(&item).Error; err != nil {
		return result{}, err
	}
	if newVer != nil {
		if err := appendVersion(tx, id, *newVer, effective); err != nil {
			return result{}, err
		}
	}
	if err := dbmodels.BumpStateVersion(tx, userID); err != nil {
		return result{}, err
	}
	resp, err := s.itemResponseOf(tx, userID, item)
	if err != nil {
		return result{}, err
	}
	return jsonResult(http.StatusOK, resp), nil
}

// ruleErrResult maps a schedule validation failure to a 400; other errors are
// infrastructure errors.
func ruleErrResult(err error) (result, error) {
	if errors.Is(err, schedule.ErrInvalid) {
		return invalidCombination(schedule.Reason(err)), nil
	}
	return result{}, err
}

// DELETE /api/items/{id}: permanent delete, cascading at the service layer
// (the schema has no foreign keys). Guarded by reauth in the router.
func (s *server) deleteItem(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	res, err := s.txResult(func(tx *gorm.DB) (result, error) {
		_, found, err := loadOwnedItem(tx, userID, id)
		if err != nil {
			return result{}, err
		}
		if !found {
			return errResult(http.StatusNotFound, "item not found"), nil
		}
		if err := tx.Where("item_id = ? AND user_id = ?", id, userID).Delete(&dbmodels.CheckinLog{}).Error; err != nil {
			return result{}, err
		}
		if err := tx.Where("item_id = ?", id).Delete(&dbmodels.ItemRuleVersion{}).Error; err != nil {
			return result{}, err
		}
		if err := tx.Where("item_id = ?", id).Delete(&dbmodels.ItemOption{}).Error; err != nil {
			return result{}, err
		}
		if err := tx.Where("id = ? AND user_id = ?", id, userID).Delete(&dbmodels.Item{}).Error; err != nil {
			return result{}, err
		}
		if err := dbmodels.BumpStateVersion(tx, userID); err != nil {
			return result{}, err
		}
		return jsonResult(http.StatusOK, map[string]bool{"ok": true}), nil
	})
	if err != nil {
		internalError(w, err, "delete item failed")
		return
	}
	s.versions.Bump(id)
	res.write(w)
}

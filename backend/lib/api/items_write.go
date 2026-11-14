package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"regexp"
	"slices"
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
	Polarity    *string `json:"polarity"`

	// Display and classification fields, edited in place.
	Kind        *string          `json:"kind"`
	Name        *string          `json:"name"`
	Description *string          `json:"description"`
	Color       *string          `json:"color"`
	Emoji       *string          `json:"emoji"`
	CategoryID  nullable[uint]   `json:"category_id"`
	ProjectID   nullable[uint]   `json:"project_id"`
	ParentID    nullable[uint]   `json:"parent_id"` // see applyReparent
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

	Version *int64 `json:"version"`

	// confirmExposure comes from ?confirm_exposure=1, never from the body.
	confirmExposure bool

	// move is set by POST /api/items/{id}/move; see applyMoveOrder.
	move *moveSpec
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
	item, isOwner, found, err := loadAccessibleItem(tx, userID, id)
	if err != nil {
		return result{}, err
	}
	if !found {
		return errResult(http.StatusNotFound, "item not found"), nil
	}
	if req.Version != nil && *req.Version != item.Version {
		return errResult(http.StatusConflict, "conflict: item was modified, please refresh"), nil
	}
	if !isOwner {
		if req.CategoryID.Set {
			return errResult(http.StatusForbidden, "only the owner can change item category"), nil
		}
		if req.ProjectID.Set && (req.ProjectID.Null || item.ProjectID == nil || req.ProjectID.Val != *item.ProjectID) {
			return errResult(http.StatusForbidden, "only the owner can move item between projects"), nil
		}
	}
	ownerID := item.UserID

	// Row locks (PostgreSQL; SQLite serializes writers anyway) so a concurrent
	// category freeze cannot be overwritten by this read-modify-write, and so
	// concurrent reparents cannot form a cycle. See lockItemTree.
	var newParent *uint
	if req.ParentID.Set && !req.ParentID.Null {
		newParent = &req.ParentID.Val
	}
	var scope *siblingScope
	if req.move != nil {
		scope = &siblingScope{parentSet: req.ParentID.Set, parent: newParent}
	}
	subtree, locked, err := lockItemTree(tx, ownerID, id, newParent, scope)
	if err != nil {
		return result{}, err
	}
	if !locked {
		return errResult(http.StatusConflict, "the item tree changed concurrently, retry"), nil
	}
	// Reload after acquiring row locks.
	if err := tx.Where("id = ?", id).Take(&item).Error; err != nil {
		return result{}, err
	}
	if (req.TrackerType != nil && *req.TrackerType != string(item.TrackerType)) ||
		(req.Polarity != nil && *req.Polarity != string(item.Polarity)) {
		return errResult(http.StatusBadRequest, "tracker_type, kind and polarity cannot be changed"), nil
	}
	kindChanged := false
	if req.Kind != nil && *req.Kind != string(item.Kind) {
		if *req.Kind != string(dbmodels.KindOngoing) && *req.Kind != string(dbmodels.KindEvent) {
			return invalidCombination(schedule.ReasonUnknownKind), nil
		}
		if item.Kind != dbmodels.KindEvent || *req.Kind != string(dbmodels.KindOngoing) {
			return errResult(http.StatusBadRequest, "tracker_type, kind and polarity cannot be changed"), nil
		}
		item.Kind = dbmodels.KindOngoing
		kindChanged = true
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
	if req.Color != nil {
		item.Color = *req.Color
	}
	if req.Emoji != nil {
		if !validEmoji(*req.Emoji) {
			return errResult(http.StatusBadRequest, "invalid emoji"), nil
		}
		item.Emoji = *req.Emoji
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
	origParent := item.ParentID
	if req.ParentID.Set {
		if res, bad, err := s.applyReparent(tx, ownerID, &item, subtree, req); err != nil {
			return result{}, err
		} else if bad {
			return res, nil
		}
	}
	if req.ProjectID.Set && item.ParentID != nil {
		return errResult(http.StatusBadRequest, "sub-items follow their parent's project"), nil
	}
	if req.ProjectID.Set {
		// DESIGN 4.1 rule 3: leaving a sensitive project freezes the item as
		// sensitive unless the caller confirms the exposure. Moving into another
		// sensitive project is covered by the rules below.
		leaving := false
		if item.ProjectID != nil && (req.ProjectID.Null || req.ProjectID.Val != *item.ProjectID) {
			oldProj, found, err := loadOwnedProject(tx, userID, *item.ProjectID)
			if err != nil {
				return result{}, err
			}
			if found {
				projSens, err := projectSensitiveFn(tx, userID)
				if err != nil {
					return result{}, err
				}
				leaving = projSens(oldProj)
			}
		}
		if leaving && !req.confirmExposure {
			item.IsSensitive = true
		}
		if req.ProjectID.Null {
			item.ProjectID = nil
		} else {
			proj, found, err := loadOwnedProject(tx, userID, req.ProjectID.Val)
			if err != nil {
				return result{}, err
			}
			if !found {
				return errResult(http.StatusBadRequest, "invalid project"), nil
			}
			projSens, err := projectSensitiveFn(tx, userID)
			if err != nil {
				return result{}, err
			}
			pid := proj.ID
			item.ProjectID = &pid
			// An uncategorized item joining a categorized project adopts its category.
			if item.CategoryID == nil && !req.CategoryID.Set {
				item.CategoryID = proj.CategoryID
			}
			// DESIGN 4.1 rule 1: a sensitive project makes its items sensitive.
			item.IsSensitive = item.IsSensitive || projSens(proj)
		}
	}

	if req.move != nil {
		parentChanged := (origParent == nil) != (item.ParentID == nil) ||
			(origParent != nil && *origParent != *item.ParentID)
		res, bad, noop, err := s.applyMoveOrder(tx, ownerID, &item, req.move, parentChanged)
		if err != nil {
			return result{}, err
		}
		if bad {
			return res, nil
		}
		if noop {
			resp, err := s.itemResponseOf(tx, userID, item)
			if err != nil {
				return result{}, err
			}
			return jsonResult(http.StatusOK, resp), nil
		}
	}

	defChanged := kindChanged || req.Unit != nil || req.Step != nil || req.MinVal.Set || req.MaxVal.Set ||
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

	if len(subtree) > 0 && (req.move == nil || req.ParentID.Set) {
		// Sub-items follow their parent's project and sensitivity.
		if err := tx.Model(&dbmodels.Item{}).Where("user_id = ? AND id IN ?", ownerID, subtree).
			Updates(map[string]any{"project_id": item.ProjectID, "is_sensitive": gorm.Expr("is_sensitive OR ?", item.IsSensitive)}).Error; err != nil {
			return result{}, err
		}
	}
	item.Version++
	if err := tx.Model(&dbmodels.Item{}).Where("id = ?", item.ID).Select(
		"Name", "Description", "Color", "Emoji", "Kind", "CategoryID", "ProjectID", "ParentID", "IsSensitive", "Pinned", "SortOrder", "Archived", "RemindTime",
		"Unit", "Step", "MinVal", "MaxVal", "Aggregate", "WarningDays", "Version").Updates(&item).Error; err != nil {
		return result{}, err
	}
	if newVer != nil {
		if err := appendVersion(tx, id, *newVer, effective); err != nil {
			return result{}, err
		}
	}
	if err := dbmodels.BumpStateVersion(tx, ownerID); err != nil {
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
// (the schema has no foreign keys).
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
		item, _, found, err := loadAccessibleItem(tx, userID, id)
		if err != nil {
			return result{}, err
		}
		if !found {
			return errResult(http.StatusNotFound, "item not found"), nil
		}
		// Sub-items are deleted together with their parent.
		ids, err := itemDescendants(tx, item.UserID, id)
		if err != nil {
			return result{}, err
		}
		ids = append(ids, id)
		if err := tx.Where("item_id IN ?", ids).Delete(&dbmodels.CheckinLog{}).Error; err != nil {
			return result{}, err
		}
		if err := tx.Where("item_id IN ?", ids).Delete(&dbmodels.ItemRuleVersion{}).Error; err != nil {
			return result{}, err
		}
		if err := tx.Where("item_id IN ?", ids).Delete(&dbmodels.ItemOption{}).Error; err != nil {
			return result{}, err
		}
		if err := tx.Where("id IN ?", ids).Delete(&dbmodels.Item{}).Error; err != nil {
			return result{}, err
		}
		if err := dbmodels.BumpStateVersion(tx, item.UserID); err != nil {
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

// itemDescendants returns the ids of every sub-item below id (any depth), in
// breadth-first order. A new sub-item always points at an existing parent, so
// the tree cannot contain cycles; the seen set guards against bad legacy data.
func itemDescendants(tx *gorm.DB, userID string, id uint) ([]uint, error) {
	seen := map[uint]bool{id: true}
	var out []uint
	frontier := []uint{id}
	for len(frontier) > 0 {
		var next []uint
		if err := tx.Model(&dbmodels.Item{}).Where("user_id = ? AND parent_id IN ?", userID, frontier).Pluck("id", &next).Error; err != nil {
			return nil, err
		}
		frontier = frontier[:0]
		for _, c := range next {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
				frontier = append(frontier, c)
			}
		}
	}
	return out, nil
}

// reparentRaceHook is a test seam. It runs inside lockItemTree after the
// candidate lock set is computed and before the rows are locked, which is where
// a concurrent transaction could commit a change.
var reparentRaceHook func(tx *gorm.DB, round int)

const lockTreeRounds = 4

// itemAncestors returns the ids of id's ancestor chain (nearest first), not
// including id. Unknown or foreign ids end the walk; a seen set guards loops.
func itemAncestors(tx *gorm.DB, userID string, id uint) ([]uint, error) {
	seen := map[uint]bool{id: true}
	var out []uint
	cur := id
	for {
		var rows []dbmodels.Item
		if err := tx.Select("id", "parent_id").Where("user_id = ? AND id = ?", userID, cur).Limit(1).Find(&rows).Error; err != nil {
			return nil, err
		}
		if len(rows) == 0 || rows[0].ParentID == nil || seen[*rows[0].ParentID] {
			return out, nil
		}
		cur = *rows[0].ParentID
		seen[cur] = true
		out = append(out, cur)
	}
}

// lockItemTree row-locks (FOR UPDATE, in ascending id order in one statement,
// so concurrent writers cannot deadlock) every row a move of id under newParent
// reads: id, its subtree, the new parent and the new parent's ancestors. Locking
// the new parent chain too is what serializes "A under B" against "B under A".
// Because the set is derived from rows that may change before the locks are
// granted, it is recomputed after locking and compared; on a mismatch it
// retries, and gives up (locked=false, a 409) after lockTreeRounds. The
// returned subtree (descendants of id) therefore comes from locked, committed
// state. SQLite ignores FOR UPDATE; its single writer makes the check trivial.
//
// With a non-nil scope (POST /move) the target sibling set is part of the same
// ordered statement, so the lock order stays a single ascending-id statement.
func lockItemTree(tx *gorm.DB, userID string, id uint, newParent *uint, scope *siblingScope) (subtree []uint, locked bool, err error) {
	compute := func() ([]uint, []uint, error) {
		sub, err := itemDescendants(tx, userID, id)
		if err != nil {
			return nil, nil, err
		}
		set := append([]uint{id}, sub...)
		if newParent != nil {
			set = append(set, *newParent)
			anc, err := itemAncestors(tx, userID, *newParent)
			if err != nil {
				return nil, nil, err
			}
			set = append(set, anc...)
		}
		if scope != nil {
			sibs, err := scope.siblingIDs(tx, userID, id)
			if err != nil {
				return nil, nil, err
			}
			set = append(set, sibs...)
		}
		slices.Sort(set)
		return sub, slices.Compact(set), nil
	}
	_, set, err := compute()
	if err != nil {
		return nil, false, err
	}
	for round := 0; round < lockTreeRounds; round++ {
		if reparentRaceHook != nil {
			reparentRaceHook(tx, round)
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Model(&dbmodels.Item{}).
			Where("user_id = ? AND id IN ?", userID, set).Order("id").
			Pluck("id", &[]uint{}).Error; err != nil {
			return nil, false, err
		}
		sub2, set2, err := compute()
		if err != nil {
			return nil, false, err
		}
		if slices.Equal(set, set2) {
			return sub2, true, nil
		}
		set = set2
	}
	return nil, false, nil
}

// maxItemDepth is the deepest allowed sub-task nesting, counting a top-level
// item as level 1. It is enforced on create and when reparenting (PATCH parent_id); moving to
// top level is always allowed. Import does not enforce it (see import_handler).
const maxItemDepth = 5

const errItemTooDeep = "sub-tasks can nest at most 5 levels"

// applyReparent handles a parent_id in PATCH. It mutates item in memory (the
// caller persists it and propagates project and sensitivity to subtree, the
// moved item's descendants). The bool reports that res is a rejection.
//
// Rules: the parent must be one of the user's items (404 otherwise, the same
// answer for other users' ids); no self-parenting or cycles; at most
// maxItemDepth levels in total. Kinds are not restricted (create does not
// restrict them either). Under a parent the whole moved subtree takes the
// parent's project (null included) and becomes sensitive when the parent is
// effectively sensitive. Moving to top level keeps the project. History
// (logs, rule versions) is never touched.
//
// Exposure (DESIGN 4.1 rule 3): when the item leaves a sensitive parent or a
// sensitive project, it is frozen as sensitive (and with it its subtree)
// unless ?confirm_exposure=1. Stored flags are never cleared.
func (s *server) applyReparent(tx *gorm.DB, userID string, item *dbmodels.Item, subtree []uint, req patchItemRequest) (result, bool, error) {
	sensFn, err := sensitiveFn(tx, userID)
	if err != nil {
		return result{}, false, err
	}
	projSens, err := projectSensitiveFn(tx, userID)
	if err != nil {
		return result{}, false, err
	}
	var newParent *dbmodels.Item
	if !req.ParentID.Null {
		if req.ParentID.Val == 0 {
			return errResult(http.StatusBadRequest, "invalid parent"), true, nil
		}
		if req.ParentID.Val == item.ID {
			return errResult(http.StatusBadRequest, "an item cannot be its own parent"), true, nil
		}
		p, found, err := loadOwnedItem(tx, userID, req.ParentID.Val)
		if err != nil {
			return result{}, false, err
		}
		if !found {
			return errResult(http.StatusNotFound, "parent not found"), true, nil
		}
		for _, d := range subtree {
			if d == p.ID {
				return errResult(http.StatusBadRequest, "the parent cannot be a descendant of the item"), true, nil
			}
		}
		newParent = &p
	}
	if sameParent(item.ParentID, newParent) {
		return result{}, false, nil
	}

	var rows []dbmodels.Item
	if newParent != nil {
		if err := tx.Select("id", "parent_id").Where("user_id = ?", userID).Find(&rows).Error; err != nil {
			return result{}, false, err
		}
	}
	parentOf := make(map[uint]*uint, len(rows))
	children := make(map[uint][]uint, len(rows))
	for _, r := range rows {
		parentOf[r.ID] = r.ParentID
		if r.ParentID != nil {
			children[*r.ParentID] = append(children[*r.ParentID], r.ID)
		}
	}
	// height: levels of the moved subtree (a leaf is 1). The subtree is
	// cycle-free here (descendants came from a seen-set walk).
	height, level := 0, []uint{item.ID}
	for len(level) > 0 && height <= maxItemDepth {
		height++
		var next []uint
		for _, id := range level {
			next = append(next, children[id]...)
		}
		level = next
	}
	depth := 0
	if newParent != nil {
		for cur := &newParent.ID; cur != nil && depth <= maxItemDepth; cur = parentOf[*cur] {
			depth++
		}
	}
	// Moving to top level (depth 0) is always allowed, even for a deeper
	// legacy subtree, so users can always escape an over-deep chain.
	if newParent != nil && depth+height > maxItemDepth {
		return errResult(http.StatusBadRequest, errItemTooDeep), true, nil
	}

	// Does the move take the item out of a sensitive container?
	leaving := false
	if item.ParentID != nil {
		old, found, err := loadOwnedItem(tx, userID, *item.ParentID)
		if err != nil {
			return result{}, false, err
		}
		leaving = found && sensFn(old)
	}
	if newParent != nil && !sameProject(item.ProjectID, newParent.ProjectID) && item.ProjectID != nil {
		oldProj, found, err := loadOwnedProject(tx, userID, *item.ProjectID)
		if err != nil {
			return result{}, false, err
		}
		leaving = leaving || (found && projSens(oldProj))
	}
	if leaving && !req.confirmExposure {
		item.IsSensitive = true
	}
	if newParent == nil {
		item.ParentID = nil
		return result{}, false, nil
	}
	pid := newParent.ID
	item.ParentID = &pid
	item.ProjectID = newParent.ProjectID
	item.IsSensitive = item.IsSensitive || sensFn(*newParent)
	return result{}, false, nil
}

func sameParent(cur *uint, np *dbmodels.Item) bool {
	if cur == nil || np == nil {
		return cur == nil && np == nil
	}
	return *cur == np.ID
}

func sameProject(a, b *uint) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

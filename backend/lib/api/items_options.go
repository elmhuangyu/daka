package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
)

// optionBody is the body of POST (create) and PUT (full replace) of an option.
// target_value is stored in the rule version's option_targets, not on the row.
type optionBody struct {
	Name        string   `json:"name"`
	Unit        string   `json:"unit"`
	Step        float64  `json:"step"`
	MinVal      *float64 `json:"min_val"`
	MaxVal      *float64 `json:"max_val"`
	Aggregate   string   `json:"aggregate"`
	SortOrder   int      `json:"sort_order"`
	TargetValue *float64 `json:"target_value"`
}

func (b optionBody) validName() bool {
	n := strings.TrimSpace(b.Name)
	return n != "" && utf8.RuneCountInString(n) <= maxNameLen
}

func (b optionBody) apply(o *dbmodels.ItemOption) {
	o.Name = strings.TrimSpace(b.Name)
	o.Unit, o.Step, o.MinVal, o.MaxVal = b.Unit, b.Step, b.MinVal, b.MaxVal
	o.Aggregate, o.SortOrder = dbmodels.Aggregate(b.Aggregate), b.SortOrder
}

// GET /api/items/{id}/options[?include_archived=1]
func (s *server) listOptions(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	redact, ok := parseRedact(w, r)
	if !ok {
		return
	}
	db := s.db.Read()
	item, found, err := loadOwnedItem(db, userID, id)
	if err != nil {
		internalError(w, err, "list options failed")
		return
	}
	if !found {
		errResult(http.StatusNotFound, "item not found").write(w)
		return
	}
	today, err := s.userToday(db, userID)
	if err != nil {
		internalError(w, err, "list options failed")
		return
	}
	vs, err := loadVersions(db, id)
	if err != nil {
		internalError(w, err, "list options failed")
		return
	}
	cur, _ := currentOrLatest(vs, today)
	q := db.Where("item_id = ?", id)
	if v := r.URL.Query().Get("include_archived"); v != "1" && v != "true" {
		q = q.Where("archived = ?", false)
	}
	var opts []dbmodels.ItemOption
	if err := q.Order("sort_order, id").Find(&opts).Error; err != nil {
		internalError(w, err, "list options failed")
		return
	}
	hide := false
	if redact {
		sens, err := sensitiveFn(db, userID)
		if err != nil {
			internalError(w, err, "list options failed")
			return
		}
		hide = sens(item)
	}
	targets := optionTargetsOf(cur)
	out := make([]optionResponse, len(opts))
	for i, o := range opts {
		out[i] = newOptionResponse(o, targets)
		if hide {
			out[i].Name = "Option " + strconv.Itoa(i+1)
		}
	}
	jsonResult(http.StatusOK, map[string]any{"options": out}).write(w)
}

// optionCtx is the shared state of an option mutation.
type optionCtx struct {
	tx    *gorm.DB
	item  dbmodels.Item
	today string
	cur   dbmodels.ItemRuleVersion
}

// optionTx runs fn against the owned item inside a transaction after loading
// today and the current rule; user state_version is bumped on success.
func (s *server) optionTx(userID string, itemID uint, fn func(c *optionCtx) (result, error)) (result, error) {
	res, err := s.txResult(func(tx *gorm.DB) (result, error) {
		item, found, err := loadOwnedItem(tx, userID, itemID)
		if err != nil {
			return result{}, err
		}
		if !found {
			return errResult(http.StatusNotFound, "item not found"), nil
		}
		today, err := s.userToday(tx, userID)
		if err != nil {
			return result{}, err
		}
		vs, err := loadVersions(tx, itemID)
		if err != nil {
			return result{}, err
		}
		cur, _ := currentOrLatest(vs, today)
		r, err := fn(&optionCtx{tx: tx, item: item, today: today, cur: cur})
		if err != nil || r.status >= 400 {
			return r, err
		}
		if err := dbmodels.BumpStateVersion(tx, userID); err != nil {
			return result{}, err
		}
		return r, nil
	})
	s.versions.Bump(itemID)
	return res, err
}

// finish validates the item as it will be after the option change (targets is
// the new option_targets snapshot), appends a rule version when the snapshot
// changed, and returns the answer.
func (c *optionCtx) finish(targets map[string]float64, status int, optID uint) (result, error) {
	active, err := loadActiveOptions(c.tx, c.item.ID)
	if err != nil {
		return result{}, err
	}
	ver := c.cur
	ver.OptionTargets = marshalTargets(targets)
	if err := schedule.ValidateItemRule(itemSpec(c.item), ver, optionSpecs(active)); err != nil {
		return ruleErrResult(err)
	}
	if ver.OptionTargets != marshalTargets(optionTargetsOf(c.cur)) {
		if err := appendVersion(c.tx, c.item.ID, ver, c.today); err != nil {
			return result{}, err
		}
	}
	for _, o := range active {
		if o.ID == optID {
			return jsonResult(status, newOptionResponse(o, targets)), nil
		}
	}
	return jsonResult(status, map[string]bool{"ok": true}), nil
}

// POST /api/items/{id}/options
func (s *server) postOption(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body optionBody
	if !decodeStrict(w, r, &body) {
		return
	}
	if !body.validName() {
		invalidCombination(schedule.ReasonInvalidOption).write(w)
		return
	}
	res, err := s.optionTx(userID, id, func(c *optionCtx) (result, error) {
		active, err := loadActiveOptions(c.tx, id)
		if err != nil {
			return result{}, err
		}
		if len(active) >= maxOptions {
			return invalidCombination(schedule.ReasonInvalidOption), nil
		}
		opt := dbmodels.ItemOption{ItemID: id}
		body.apply(&opt)
		if err := c.tx.Create(&opt).Error; err != nil {
			return result{}, err
		}
		targets := activeTargets(c.cur, active)
		if body.TargetValue != nil {
			targets[strconv.FormatUint(uint64(opt.ID), 10)] = *body.TargetValue
		}
		return c.finish(targets, http.StatusCreated, opt.ID)
	})
	if err != nil {
		internalError(w, err, "create option failed")
		return
	}
	res.write(w)
}

// PUT /api/items/{id}/options/{oid}: full replace of an option, including its
// target (an absent target_value removes the target).
func (s *server) putOption(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	oid, ok := pathID(w, r, "oid")
	if !ok {
		return
	}
	var body optionBody
	if !decodeStrict(w, r, &body) {
		return
	}
	if !body.validName() {
		invalidCombination(schedule.ReasonInvalidOption).write(w)
		return
	}
	res, err := s.optionTx(userID, id, func(c *optionCtx) (result, error) {
		var opt dbmodels.ItemOption
		err := c.tx.Where("id = ? AND item_id = ? AND archived = ?", oid, id, false).Take(&opt).Error
		if err != nil {
			return notFoundOr(err, "option not found")
		}
		body.apply(&opt)
		if err := c.tx.Save(&opt).Error; err != nil {
			return result{}, err
		}
		active, err := loadActiveOptions(c.tx, id)
		if err != nil {
			return result{}, err
		}
		targets := activeTargets(c.cur, active)
		key := strconv.FormatUint(uint64(oid), 10)
		delete(targets, key)
		if body.TargetValue != nil {
			targets[key] = *body.TargetValue
		}
		return c.finish(targets, http.StatusOK, oid)
	})
	if err != nil {
		internalError(w, err, "update option failed")
		return
	}
	res.write(w)
}

// DELETE /api/items/{id}/options/{oid}: archives the option (history keeps
// its logs). Fails with 400 when the remaining state would be invalid, e.g.
// the option is the quota option.
func (s *server) deleteOption(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	oid, ok := pathID(w, r, "oid")
	if !ok {
		return
	}
	res, err := s.optionTx(userID, id, func(c *optionCtx) (result, error) {
		var opt dbmodels.ItemOption
		err := c.tx.Where("id = ? AND item_id = ? AND archived = ?", oid, id, false).Take(&opt).Error
		if err != nil {
			return notFoundOr(err, "option not found")
		}
		if err := c.tx.Model(&dbmodels.ItemOption{}).Where("id = ?", oid).Update("archived", true).Error; err != nil {
			return result{}, err
		}
		active, err := loadActiveOptions(c.tx, id)
		if err != nil {
			return result{}, err
		}
		return c.finish(activeTargets(c.cur, active), http.StatusOK, 0)
	})
	if err != nil {
		internalError(w, err, "delete option failed")
		return
	}
	res.write(w)
}

func notFoundOr(err error, msg string) (result, error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errResult(http.StatusNotFound, msg), nil
	}
	return result{}, err
}

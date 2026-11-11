package api

import (
	"net/http"
	"slices"

	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

// moveRequest is the body of POST /api/items/{id}/move. An absent parent_id
// keeps the current parent, null moves to top level. At most one of before_id
// and after_id; neither appends as the last sibling.
type moveRequest struct {
	ParentID nullable[uint] `json:"parent_id"`
	BeforeID *uint          `json:"before_id"`
	AfterID  *uint          `json:"after_id"`
}

type moveSpec struct {
	beforeID, afterID *uint
	changed           bool // set when anything was written
}

// siblingScope describes the target sibling set of a move for lockItemTree.
// When parentSet is false the target parent is the item's current parent.
type siblingScope struct {
	parentSet bool
	parent    *uint
}

// siblingIDs returns the ids of the items that share the target parent with
// the moved item (excluding it). Top-level siblings are those with no parent
// and the moved item's project: moving to top level keeps the project.
func (sc *siblingScope) siblingIDs(tx *gorm.DB, userID string, id uint) ([]uint, error) {
	var rows []dbmodels.Item
	if err := tx.Select("id", "parent_id", "project_id").Where("user_id = ? AND id = ?", userID, id).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	parent := sc.parent
	if !sc.parentSet {
		parent = rows[0].ParentID
	}
	var ids []uint
	err := siblingQuery(tx, userID, id, parent, rows[0].ProjectID).Pluck("id", &ids).Error
	return ids, err
}

// siblingQuery selects the siblings of except under parent (or, for a nil
// parent, the top-level items of project), in (sort_order, id) order.
func siblingQuery(tx *gorm.DB, userID string, except uint, parent, project *uint) *gorm.DB {
	q := tx.Model(&dbmodels.Item{}).Where("user_id = ? AND id <> ?", userID, except)
	switch {
	case parent != nil:
		q = q.Where("parent_id = ?", *parent)
	case project != nil:
		q = q.Where("parent_id IS NULL AND project_id = ?", *project)
	default:
		q = q.Where("parent_id IS NULL AND project_id IS NULL")
	}
	return q.Order("sort_order, id")
}

// POST /api/items/{id}/move
//
// Reparents (exactly like PATCH parent_id, same rules, errors and
// ?confirm_exposure=1 handling) and positions the item among its new siblings
// in one transaction with one state_version bump. The response is the same
// item JSON as PATCH /api/items/{id}.
func (s *server) moveItem(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body moveRequest
	if !decodeStrict(w, r, &body) {
		return
	}
	if body.BeforeID != nil && body.AfterID != nil {
		errResult(http.StatusBadRequest, "use at most one of before_id and after_id").write(w)
		return
	}
	req := patchItemRequest{ParentID: body.ParentID, move: &moveSpec{beforeID: body.BeforeID, afterID: body.AfterID}}
	req.confirmExposure = r.URL.Query().Get("confirm_exposure") == "1"
	var changed bool
	res, err := s.txResult(func(tx *gorm.DB) (result, error) {
		res, err := s.applyPatch(tx, userID, id, req)
		changed = err == nil && res.status == http.StatusOK && req.move.changed
		return res, err
	})
	if err != nil {
		internalError(w, err, "move item failed")
		return
	}
	if changed {
		s.versions.Bump(id)
	}
	res.write(w)
}

// applyMoveOrder positions item among its target siblings. item already holds
// the post-reparent parent and project. Lock order: lockItemTree has already
// locked the tree set and the sibling set in one ascending-id statement, so no
// second lock statement is issued here. Sibling sort_order values are
// renumbered to base+index (base = the smallest existing value of the set, 0
// when empty) so ties are resolved; only rows whose value changes are written.
// An unchanged position in an already strictly ordered set writes nothing
// (noop). A moved item only changes its own sort_order and, through the
// reparent, parent, project and sensitivity.
func (s *server) applyMoveOrder(tx *gorm.DB, userID string, item *dbmodels.Item, mv *moveSpec, parentChanged bool) (res result, bad, noop bool, err error) {
	anchor, side := mv.beforeID, 0
	if mv.afterID != nil {
		anchor, side = mv.afterID, 1
	}
	var sibs []dbmodels.Item
	q := siblingQuery(tx.Select("id", "sort_order"), userID, item.ID, item.ParentID, item.ProjectID)
	if err := q.Find(&sibs).Error; err != nil {
		return result{}, false, false, err
	}
	pos := len(sibs)
	if anchor != nil {
		if *anchor == 0 || *anchor == item.ID {
			return errResult(http.StatusBadRequest, "invalid anchor"), true, false, nil
		}
		_, found, err := loadOwnedItem(tx, userID, *anchor)
		if err != nil {
			return result{}, false, false, err
		}
		if !found {
			return errResult(http.StatusNotFound, "anchor not found"), true, false, nil
		}
		idx := slices.IndexFunc(sibs, func(x dbmodels.Item) bool { return x.ID == *anchor })
		if idx < 0 {
			return errResult(http.StatusBadRequest, "anchor is not a sibling under the target parent"), true, false, nil
		}
		pos = idx + side
	}

	base := 0
	if len(sibs) > 0 {
		base = sibs[0].SortOrder
	}
	// Idempotence: same parent, same relative position and already distinct
	// strictly increasing values.
	if !parentChanged {
		cur := append(slices.Clone(sibs), dbmodels.Item{ID: item.ID, SortOrder: item.SortOrder})
		slices.SortFunc(cur, func(a, b dbmodels.Item) int {
			if a.SortOrder != b.SortOrder {
				return a.SortOrder - b.SortOrder
			}
			return int(a.ID) - int(b.ID)
		})
		want := slices.Clone(sibs)
		want = slices.Insert(want, pos, dbmodels.Item{ID: item.ID})
		same, strict := true, true
		for i := range cur {
			same = same && cur[i].ID == want[i].ID
			strict = strict && (i == 0 || cur[i].SortOrder > cur[i-1].SortOrder)
		}
		if same && strict {
			return result{}, false, true, nil
		}
	}
	order := slices.Insert(slices.Clone(sibs), pos, dbmodels.Item{ID: item.ID})
	for i, x := range order {
		v := base + i
		if x.ID == item.ID {
			item.SortOrder = v
			continue
		}
		if x.SortOrder == v {
			continue
		}
		if err := tx.Model(&dbmodels.Item{}).Where("id = ? AND user_id = ?", x.ID, userID).Update("sort_order", v).Error; err != nil {
			return result{}, false, false, err
		}
	}
	mv.changed = true
	return result{}, false, false, nil
}

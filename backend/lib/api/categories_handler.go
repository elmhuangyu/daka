package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

type categoryResponse struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Icon        string `json:"icon"`
	IsSensitive bool   `json:"is_sensitive"`
	SortOrder   int    `json:"sort_order"`
	// AffectedItems is set on PUT when the category stopped being sensitive: the
	// number of items that were frozen (or exposed, with confirm_exposure=1).
	AffectedItems *int64 `json:"affected_items,omitempty"`
}

func newCategoryResponse(c dbmodels.Category) categoryResponse {
	return categoryResponse{ID: c.ID, Name: c.Name, Color: c.Color, Icon: c.Icon, IsSensitive: c.IsSensitive, SortOrder: c.SortOrder}
}

type categoryRequest struct {
	Name        *string `json:"name"`
	Color       *string `json:"color"`
	Icon        *string `json:"icon"`
	IsSensitive *bool   `json:"is_sensitive"`
	SortOrder   *int    `json:"sort_order"`
}

func validCategoryFields(req categoryRequest) (result, bool) {
	bad := func(msg string) (result, bool) { return errResult(http.StatusBadRequest, msg), false }
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if n == "" {
			return bad("name is required")
		}
		if utf8.RuneCountInString(n) > maxNameLen {
			return bad("name too long")
		}
	}
	if req.Color != nil && len(*req.Color) > 32 {
		return bad("color too long")
	}
	if req.Icon != nil && len(*req.Icon) > 64 {
		return bad("icon too long")
	}
	return result{}, true
}

// GET /api/categories
func (s *server) listCategories(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	var cats []dbmodels.Category
	if err := s.db.Read().Where("user_id = ?", userID).Order("sort_order, id").Find(&cats).Error; err != nil {
		internalError(w, err, "list categories failed")
		return
	}
	out := make([]categoryResponse, len(cats))
	for i, c := range cats {
		out[i] = newCategoryResponse(c)
	}
	jsonResult(http.StatusOK, map[string]any{"categories": out}).write(w)
}

// POST /api/categories
func (s *server) postCategory(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	var req categoryRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	if req.Name == nil {
		errResult(http.StatusBadRequest, "name is required").write(w)
		return
	}
	if bad, ok := validCategoryFields(req); !ok {
		bad.write(w)
		return
	}
	c := dbmodels.Category{UserID: userID, Name: strings.TrimSpace(*req.Name)}
	if req.Color != nil {
		c.Color = *req.Color
	}
	if req.Icon != nil {
		c.Icon = *req.Icon
	}
	if req.IsSensitive != nil {
		c.IsSensitive = *req.IsSensitive
	}
	if req.SortOrder != nil {
		c.SortOrder = *req.SortOrder
	}
	if err := s.db.Write().Create(&c).Error; err != nil {
		internalError(w, err, "create category failed")
		return
	}
	jsonResult(http.StatusCreated, newCategoryResponse(c)).write(w)
}

func loadOwnedCategory(tx *gorm.DB, userID string, id uint) (dbmodels.Category, bool, error) {
	var c dbmodels.Category
	err := tx.Where("id = ? AND user_id = ?", id, userID).Take(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c, false, nil
	}
	return c, err == nil, err
}

// lockCategoryItems takes row locks on the items of category id (ordered, so
// concurrent category operations cannot deadlock) before they are frozen or
// moved. PATCH /api/items locks its item row first and then only reads the
// category, so both paths serialize on the item rows.
func lockCategoryItems(tx *gorm.DB, userID string, id uint) error {
	var ids []uint
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).Model(&dbmodels.Item{}).
		Where("user_id = ? AND category_id = ?", userID, id).Order("id").Pluck("id", &ids).Error
}

// lockCategoryProjects takes row locks on the projects of category id (ordered)
// before they are frozen or moved.
func lockCategoryProjects(tx *gorm.DB, userID string, id uint) error {
	var ids []uint
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).Model(&dbmodels.Project{}).
		Where("user_id = ? AND category_id = ?", userID, id).Order("id").Pluck("id", &ids).Error
}

// freezeCategoryItems applies DESIGN 4.1 rule 3 to the items of category id
// that are not yet flagged themselves and returns how many there are. Without
// confirm they are flagged sensitive, so they stay protected after the category
// stops being sensitive; with confirm they are left as they are (exposed).
func freezeCategoryItems(tx *gorm.DB, userID string, id uint, confirm bool) (int64, error) {
	where := "user_id = ? AND category_id = ? AND is_sensitive = ?"
	var n int64
	if err := tx.Model(&dbmodels.Item{}).Where(where, userID, id, false).Count(&n).Error; err != nil {
		return 0, err
	}
	if n > 0 && !confirm {
		if err := tx.Model(&dbmodels.Item{}).Where(where, userID, id, false).Update("is_sensitive", true).Error; err != nil {
			return 0, err
		}
	}
	return n, nil
}

// freezeCategoryProjects applies sensitivity protection to the projects of category id
// that are not yet flagged themselves. Without confirm they are flagged sensitive;
// with confirm they are left as they are (exposed).
func freezeCategoryProjects(tx *gorm.DB, userID string, id uint, confirm bool) error {
	where := "user_id = ? AND category_id = ? AND is_sensitive = ?"
	if !confirm {
		if err := tx.Model(&dbmodels.Project{}).Where(where, userID, id, false).Update("is_sensitive", true).Error; err != nil {
			return err
		}
	}
	return nil
}

// PUT /api/categories/{id}[?confirm_exposure=1]. Absent fields keep their value.
func (s *server) putCategory(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req categoryRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	if bad, ok := validCategoryFields(req); !ok {
		bad.write(w)
		return
	}
	confirm := r.URL.Query().Get("confirm_exposure") == "1"
	res, err := s.txResult(func(tx *gorm.DB) (result, error) {
		c, found, err := loadOwnedCategory(tx, userID, id)
		if err != nil {
			return result{}, err
		}
		if !found {
			return errResult(http.StatusNotFound, "category not found"), nil
		}
		var affected *int64
		if req.IsSensitive != nil && !c.IsSensitive && *req.IsSensitive {
			var sharedOwned int64
			ownedProjIDs := tx.Model(&dbmodels.Project{}).Select("id").Where("user_id = ? AND category_id = ?", userID, id)
			if err := tx.Model(&dbmodels.ProjectShare{}).Where("project_id IN (?)", ownedProjIDs).Count(&sharedOwned).Error; err != nil {
				return result{}, err
			}
			var sharedReceived int64
			if err := tx.Model(&dbmodels.ProjectShare{}).Where("user_id = ? AND category_id = ?", userID, id).Count(&sharedReceived).Error; err != nil {
				return result{}, err
			}
			if sharedOwned > 0 || sharedReceived > 0 {
				return errResult(http.StatusBadRequest, "cannot mark category sensitive while it contains shared projects"), nil
			}
		}
		if req.IsSensitive != nil && c.IsSensitive && !*req.IsSensitive {
			if err := lockCategoryItems(tx, userID, id); err != nil {
				return result{}, err
			}
			if err := lockCategoryProjects(tx, userID, id); err != nil {
				return result{}, err
			}
			n, err := freezeCategoryItems(tx, userID, id, confirm)
			if err != nil {
				return result{}, err
			}
			if err := freezeCategoryProjects(tx, userID, id, confirm); err != nil {
				return result{}, err
			}
			affected = &n
		}
		if req.Name != nil {
			c.Name = strings.TrimSpace(*req.Name)
		}
		if req.Color != nil {
			c.Color = *req.Color
		}
		if req.Icon != nil {
			c.Icon = *req.Icon
		}
		if req.IsSensitive != nil {
			c.IsSensitive = *req.IsSensitive
		}
		if req.SortOrder != nil {
			c.SortOrder = *req.SortOrder
		}
		if err := tx.Save(&c).Error; err != nil {
			return result{}, err
		}
		resp := newCategoryResponse(c)
		resp.AffectedItems = affected
		return jsonResult(http.StatusOK, resp), nil
	})
	if err != nil {
		internalError(w, err, "update category failed")
		return
	}
	res.write(w)
}

// DELETE /api/categories/{id}?move_to=:id | ?uncategorize=1 [&confirm_exposure=1]
func (s *server) deleteCategory(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	q := r.URL.Query()
	confirm := q.Get("confirm_exposure") == "1"
	uncategorize := q.Get("uncategorize") == "1"
	var moveTo *uint
	if v := q.Get("move_to"); v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil || n == 0 || uint(n) == id || uncategorize {
			errResult(http.StatusBadRequest, "invalid move_to").write(w)
			return
		}
		t := uint(n)
		moveTo = &t
	}
	res, err := s.txResult(func(tx *gorm.DB) (result, error) {
		c, found, err := loadOwnedCategory(tx, userID, id)
		if err != nil {
			return result{}, err
		}
		if !found {
			return errResult(http.StatusNotFound, "category not found"), nil
		}
		var target dbmodels.Category
		if moveTo != nil {
			t, found, err := loadOwnedCategory(tx, userID, *moveTo)
			if err != nil {
				return result{}, err
			}
			if !found {
				return errResult(http.StatusBadRequest, "invalid move_to"), nil
			}
			target = t
		}
		if err := lockCategoryItems(tx, userID, id); err != nil {
			return result{}, err
		}
		if err := lockCategoryProjects(tx, userID, id); err != nil {
			return result{}, err
		}
		var items int64
		if err := tx.Model(&dbmodels.Item{}).Where("user_id = ? AND category_id = ?", userID, id).Count(&items).Error; err != nil {
			return result{}, err
		}
		var projects int64
		if err := tx.Model(&dbmodels.Project{}).Where("user_id = ? AND category_id = ?", userID, id).Count(&projects).Error; err != nil {
			return result{}, err
		}
		if (items > 0 || projects > 0) && moveTo == nil && !uncategorize {
			return errResult(http.StatusConflict, "category has items; pass move_to or uncategorize=1"), nil
		}
		var affected int64
		// Items stay protected unless they land in a sensitive category.
		if c.IsSensitive && (moveTo == nil || !target.IsSensitive) {
			if affected, err = freezeCategoryItems(tx, userID, id, confirm); err != nil {
				return result{}, err
			}
			if err := freezeCategoryProjects(tx, userID, id, confirm); err != nil {
				return result{}, err
			}
		}
		if items > 0 {
			if err := tx.Model(&dbmodels.Item{}).Where("user_id = ? AND category_id = ?", userID, id).
				Update("category_id", moveTo).Error; err != nil {
				return result{}, err
			}
		}
		if projects > 0 {
			if err := tx.Model(&dbmodels.Project{}).Where("user_id = ? AND category_id = ?", userID, id).
				Update("category_id", moveTo).Error; err != nil {
				return result{}, err
			}
		}
		shareMoveTo := moveTo
		if moveTo != nil && target.IsSensitive {
			shareMoveTo = nil
		}
		if err := tx.Model(&dbmodels.ProjectShare{}).Where("user_id = ? AND category_id = ?", userID, id).
			Update("category_id", shareMoveTo).Error; err != nil {
			return result{}, err
		}
		if moveTo != nil {
			if err := tx.Model(&dbmodels.ApiToken{}).Where("user_id = ? AND category_id = ?", userID, id).
				UpdateColumn("category_id", moveTo).Error; err != nil {
				return result{}, err
			}
		} else {
			// A token scoped to this category must be revoked so it does not
			// unintentionally widen its scope to all categories.
			now := s.now().UTC()
			if err := tx.Model(&dbmodels.ApiToken{}).Where("user_id = ? AND category_id = ? AND revoked_at IS NULL", userID, id).
				UpdateColumn("revoked_at", now).Error; err != nil {
				return result{}, err
			}
		}
		if err := tx.Delete(&dbmodels.Category{}, c.ID).Error; err != nil {
			return result{}, err
		}
		return jsonResult(http.StatusOK, map[string]any{"deleted": true, "moved_items": items, "affected_items": affected}), nil
	})
	if err != nil {
		internalError(w, err, "delete category failed")
		return
	}
	res.write(w)
}

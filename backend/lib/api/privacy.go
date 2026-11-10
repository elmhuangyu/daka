package api

import (
	"net/http"

	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

// privateItemName replaces the names of sensitive items when redact=1 (DESIGN 4.2).
// It is the single place where server-side redaction decisions are made:
// handlers call parseRedact for the flag and sensitiveFn for the effective
// sensitivity (DESIGN 4.1: Item.is_sensitive OR category.is_sensitive).

func parseRedact(w http.ResponseWriter, r *http.Request) (redact, ok bool) {
	switch r.URL.Query().Get("redact") {
	case "", "0", "false":
		return false, true
	case "1", "true":
		return true, true
	}
	errResult(http.StatusBadRequest, "invalid redact").write(w)
	return false, false
}

// sensitiveFn reports whether an item is sensitive, directly or through its
// category or project.
func sensitiveFn(db *gorm.DB, userID string) (func(dbmodels.Item) bool, error) {
	var cats []dbmodels.Category
	if err := db.Where("user_id = ? AND is_sensitive = ?", userID, true).Find(&cats).Error; err != nil {
		return nil, err
	}
	catSet := map[uint]bool{}
	for _, c := range cats {
		catSet[c.ID] = true
	}
	var projs []dbmodels.Project
	if err := db.Where("user_id = ?", userID).Find(&projs).Error; err != nil {
		return nil, err
	}
	projSet := map[uint]bool{}
	for _, p := range projs {
		if p.IsSensitive || (p.CategoryID != nil && catSet[*p.CategoryID]) {
			projSet[p.ID] = true
		}
	}
	return func(it dbmodels.Item) bool {
		return it.IsSensitive || (it.CategoryID != nil && catSet[*it.CategoryID]) || (it.ProjectID != nil && projSet[*it.ProjectID])
	}, nil
}

// readTokenKey marks requests authenticated by a read-scope ApiToken.
type readTokenKey struct{}

// isReadToken reports whether the request came in through a read-scope
// ApiToken. Such requests never see sensitive items (DESIGN 11.6): handlers
// exclude them entirely (no placeholder, no count), whatever redact says.
func isReadToken(r *http.Request) bool {
	v, _ := r.Context().Value(readTokenKey{}).(bool)
	return v
}

// sensitiveItemIDs lists the items that are effectively sensitive (own flag or
// sensitive category), archived ones included.
func sensitiveItemIDs(db *gorm.DB, userID string) ([]uint, error) {
	sens, err := sensitiveFn(db, userID)
	if err != nil {
		return nil, err
	}
	var items []dbmodels.Item
	if err := db.Select("id", "user_id", "category_id", "project_id", "is_sensitive").Where("user_id = ?", userID).Find(&items).Error; err != nil {
		return nil, err
	}
	var ids []uint
	for _, it := range items {
		if sens(it) {
			ids = append(ids, it.ID)
		}
	}
	return ids, nil
}

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
// category.
func sensitiveFn(db *gorm.DB, userID string) (func(dbmodels.Item) bool, error) {
	var cats []dbmodels.Category
	if err := db.Where("user_id = ? AND is_sensitive = ?", userID, true).Find(&cats).Error; err != nil {
		return nil, err
	}
	set := map[uint]bool{}
	for _, c := range cats {
		set[c.ID] = true
	}
	return func(it dbmodels.Item) bool {
		return it.IsSensitive || (it.CategoryID != nil && set[*it.CategoryID])
	}, nil
}

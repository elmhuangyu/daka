package api

import (
	"net/http"

	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/auth"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

// deleteAccountConfirm is the exact value the body's "confirm" must carry.
const deleteAccountConfirm = "DELETE"

type deleteAccountRequest struct {
	Confirm string `json:"confirm"`
}

// DELETE /api/account (behind reauth): permanently deletes the caller's user
// row and every row they own (items and their options / rule versions, check-in
// logs, freezes, categories, settings with encrypted external calendar URLs,
// API tokens, sessions, Telegram link and codes, reminder logs, idempotency
// keys) in one transaction, then clears the session cookie. The body must be
// {"confirm":"DELETE"}.
func (s *server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	var req deleteAccountRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	if req.Confirm != deleteAccountConfirm {
		errResult(http.StatusBadRequest, "confirmation required").write(w)
		return
	}
	var itemIDs []uint
	err := s.db.Write().Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&dbmodels.Item{}).Where("user_id = ?", userID).Pluck("id", &itemIDs).Error; err != nil {
			return err
		}
		return dbmodels.DeleteUserData(tx, userID)
	})
	if err != nil {
		internalError(w, err, "delete account failed")
		return
	}
	for _, id := range itemIDs {
		s.versions.Bump(id)
	}
	auth.SetCookie(w, auth.SessionCookieName, "", -1, s.secureCookie)
	jsonResult(http.StatusOK, map[string]bool{"deleted": true}).write(w)
}

package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

type projectUserInfo struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

type projectShareResponse struct {
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	AvatarURL string    `json:"avatar_url"`
	CreatedAt time.Time `json:"created_at"`
}

// loadAccessibleProject returns a project if userID is the owner or a shared collaborator.
func loadAccessibleProject(tx *gorm.DB, userID string, id uint) (dbmodels.Project, bool, *dbmodels.ProjectShare, bool, error) {
	var p dbmodels.Project
	err := tx.Where("id = ?", id).Take(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return p, false, nil, false, nil
	}
	if err != nil {
		return p, false, nil, false, err
	}
	if p.UserID == userID {
		return p, true, nil, true, nil
	}
	var sh dbmodels.ProjectShare
	err = tx.Where("project_id = ? AND user_id = ?", id, userID).Take(&sh).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return dbmodels.Project{}, false, nil, false, nil
	}
	if err != nil {
		return dbmodels.Project{}, false, nil, false, err
	}
	return p, false, &sh, true, nil
}

// loadAccessibleItem returns an item if userID owns it or has access via a shared project.
func loadAccessibleItem(tx *gorm.DB, userID string, id uint) (dbmodels.Item, bool, bool, error) {
	var it dbmodels.Item
	err := tx.Where("id = ?", id).Take(&it).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return it, false, false, nil
	}
	if err != nil {
		return it, false, false, err
	}
	if it.UserID == userID {
		return it, true, true, nil
	}
	if it.ProjectID != nil {
		var n int64
		if err := tx.Model(&dbmodels.ProjectShare{}).
			Where("project_id = ? AND user_id = ?", *it.ProjectID, userID).
			Count(&n).Error; err != nil {
			return dbmodels.Item{}, false, false, err
		}
		if n > 0 {
			return it, false, true, nil
		}
	}
	return dbmodels.Item{}, false, false, nil
}

// accessibleItemsScope filters an Item query to items owned by userID or in projects shared with userID.
func accessibleItemsScope(tx *gorm.DB, userID string) *gorm.DB {
	sharedProjIDs := tx.Session(&gorm.Session{NewDB: true}).
		Model(&dbmodels.ProjectShare{}).
		Select("project_id").
		Where("user_id = ?", userID)
	return tx.Where("user_id = ? OR project_id IN (?)", userID, sharedProjIDs)
}

func projectShareCount(tx *gorm.DB, projectID uint) (int64, error) {
	var n int64
	err := tx.Model(&dbmodels.ProjectShare{}).Where("project_id = ?", projectID).Count(&n).Error
	return n, err
}

func buildUserInfoMap(db *gorm.DB, userIDs []string) (map[string]projectUserInfo, error) {
	out := make(map[string]projectUserInfo, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	var users []dbmodels.User
	if err := db.Where("id IN ?", userIDs).Find(&users).Error; err != nil {
		return nil, err
	}
	for _, u := range users {
		out[u.ID] = projectUserInfo{
			ID:        u.ID,
			Email:     u.Email,
			Name:      u.Name,
			AvatarURL: u.AvatarURL,
		}
	}
	return out, nil
}

func enrichProjectResponses(
	db *gorm.DB,
	userID string,
	projs []dbmodels.Project,
	myShares map[uint]dbmodels.ProjectShare,
	redact bool,
	isSens func(dbmodels.Project) bool,
) ([]projectResponse, error) {
	out := make([]projectResponse, len(projs))
	if len(projs) == 0 {
		return out, nil
	}

	projIDs := make([]uint, len(projs))
	userIDSet := make(map[string]struct{})
	for i, p := range projs {
		projIDs[i] = p.ID
		userIDSet[p.UserID] = struct{}{}
	}

	var allShares []dbmodels.ProjectShare
	if err := db.Where("project_id IN ?", projIDs).Order("id ASC").Find(&allShares).Error; err != nil {
		return nil, err
	}
	sharesByProj := make(map[uint][]dbmodels.ProjectShare)
	for _, sh := range allShares {
		sharesByProj[sh.ProjectID] = append(sharesByProj[sh.ProjectID], sh)
		userIDSet[sh.UserID] = struct{}{}
	}

	userIDs := make([]string, 0, len(userIDSet))
	for uid := range userIDSet {
		userIDs = append(userIDs, uid)
	}
	usersMap, err := buildUserInfoMap(db, userIDs)
	if err != nil {
		return nil, err
	}

	for i, p := range projs {
		resp := newProjectResponse(p)
		resp.IsOwner = p.UserID == userID
		if !resp.IsOwner {
			if sh, ok := myShares[p.ID]; ok {
				resp.CategoryID = sh.CategoryID
			} else {
				resp.CategoryID = nil
			}
		}
		if u, ok := usersMap[p.UserID]; ok {
			resp.Owner = &u
		} else {
			resp.Owner = &projectUserInfo{ID: p.UserID}
		}

		shList := sharesByProj[p.ID]
		resp.Shares = make([]projectShareResponse, 0, len(shList))
		for _, sh := range shList {
			u := usersMap[sh.UserID]
			resp.Shares = append(resp.Shares, projectShareResponse{
				UserID:    sh.UserID,
				Email:     u.Email,
				Name:      u.Name,
				AvatarURL: u.AvatarURL,
				CreatedAt: sh.CreatedAt,
			})
		}

		if redact && isSens != nil && isSens(p) {
			redactProject(&resp)
		}
		out[i] = resp
	}
	return out, nil
}

type shareProjectRequest struct {
	Email string `json:"email"`
}

// POST /api/projects/{id}/shares
func (s *server) postProjectShare(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req shareProjectRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	email := strings.TrimSpace(req.Email)
	if email == "" {
		errResult(http.StatusBadRequest, "email is required").write(w)
		return
	}

	res, err := s.txResult(func(tx *gorm.DB) (result, error) {
		p, isOwner, _, found, err := loadAccessibleProject(tx, userID, id)
		if err != nil {
			return result{}, err
		}
		if !found {
			return errResult(http.StatusNotFound, "project not found"), nil
		}
		if !isOwner {
			return errResult(http.StatusForbidden, "only the project owner can manage access"), nil
		}

		isSensFn, err := projectSensitiveFn(tx, userID)
		if err != nil {
			return result{}, err
		}
		if isSensFn(p) {
			return errResult(http.StatusBadRequest, "sensitive projects cannot be shared"), nil
		}

		var target dbmodels.User
		err = tx.Where("LOWER(email) = ?", strings.ToLower(email)).Take(&target).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errResult(http.StatusNotFound, "user not found"), nil
		}
		if err != nil {
			return result{}, err
		}
		if target.ID == userID {
			return errResult(http.StatusBadRequest, "cannot share project with yourself"), nil
		}

		var existing dbmodels.ProjectShare
		err = tx.Where("project_id = ? AND user_id = ?", p.ID, target.ID).Take(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return result{}, err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			sh := dbmodels.ProjectShare{
				ProjectID: p.ID,
				UserID:    target.ID,
				CreatedAt: s.now().UTC(),
			}
			if err := tx.Create(&sh).Error; err != nil {
				return result{}, err
			}
		}

		enriched, err := enrichProjectResponses(tx, userID, []dbmodels.Project{p}, nil, false, nil)
		if err != nil {
			return result{}, err
		}
		return jsonResult(http.StatusOK, enriched[0]), nil
	})
	if err != nil {
		internalError(w, err, "share project failed")
		return
	}
	res.write(w)
}

// DELETE /api/projects/{id}/shares/{userId}
func (s *server) deleteProjectShare(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	targetID := r.PathValue("userId")
	if targetID == "" {
		errResult(http.StatusBadRequest, "user_id is required").write(w)
		return
	}
	if targetID == "me" {
		targetID = userID
	}

	res, err := s.txResult(func(tx *gorm.DB) (result, error) {
		p, isOwner, _, found, err := loadAccessibleProject(tx, userID, id)
		if err != nil {
			return result{}, err
		}
		if !found {
			return errResult(http.StatusNotFound, "project not found"), nil
		}
		if !isOwner && targetID != userID {
			return errResult(http.StatusForbidden, "only the project owner can remove other collaborators"), nil
		}
		if targetID == p.UserID {
			return errResult(http.StatusBadRequest, "cannot remove project owner"), nil
		}

		tr := tx.Where("project_id = ? AND user_id = ?", p.ID, targetID).Delete(&dbmodels.ProjectShare{})
		if tr.Error != nil {
			return result{}, tr.Error
		}
		if tr.RowsAffected == 0 {
			return errResult(http.StatusNotFound, "share not found"), nil
		}

		if !isOwner && targetID == userID {
			return jsonResult(http.StatusOK, map[string]bool{"unshared": true}), nil
		}
		enriched, err := enrichProjectResponses(tx, userID, []dbmodels.Project{p}, nil, false, nil)
		if err != nil {
			return result{}, err
		}
		return jsonResult(http.StatusOK, enriched[0]), nil
	})
	if err != nil {
		internalError(w, err, "unshare project failed")
		return
	}
	res.write(w)
}

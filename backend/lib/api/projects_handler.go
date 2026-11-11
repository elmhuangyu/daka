package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

const (
	maxProjectNameLen = 255
	maxProjectDescLen = 2000
)

type projectResponse struct {
	ID          uint                   `json:"id"`
	CategoryID  *uint                  `json:"category_id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Color       string                 `json:"color"`
	Emoji       string                 `json:"emoji"`
	Status      dbmodels.ProjectStatus `json:"status"`
	TargetDate  *string                `json:"target_date"`
	IsSensitive bool                   `json:"is_sensitive"`
	SortOrder   int                    `json:"sort_order"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

func newProjectResponse(p dbmodels.Project) projectResponse {
	return projectResponse{
		ID:          p.ID,
		CategoryID:  p.CategoryID,
		Name:        p.Name,
		Description: p.Description,
		Color:       p.Color,
		Emoji:       p.Emoji,
		Status:      p.Status,
		TargetDate:  p.TargetDate,
		IsSensitive: p.IsSensitive,
		SortOrder:   p.SortOrder,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

func redactProject(resp *projectResponse) {
	resp.Name = "[REDACTED]"
	resp.Emoji = ""
	resp.Description = ""
}

func loadOwnedProject(tx *gorm.DB, userID string, id uint) (dbmodels.Project, bool, error) {
	var p dbmodels.Project
	err := tx.Where("id = ? AND user_id = ?", id, userID).Take(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return p, false, nil
	}
	return p, err == nil, err
}

func lockProjectItems(tx *gorm.DB, userID string, id uint) error {
	var ids []uint
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).Model(&dbmodels.Item{}).
		Where("user_id = ? AND project_id = ?", userID, id).Order("id").Pluck("id", &ids).Error
}

// projectSensitiveFn reports whether a project is effectively sensitive
// (its own is_sensitive flag or belonging to a sensitive category).
func projectSensitiveFn(db *gorm.DB, userID string) (func(dbmodels.Project) bool, error) {
	var cats []dbmodels.Category
	if err := db.Where("user_id = ? AND is_sensitive = ?", userID, true).Find(&cats).Error; err != nil {
		return nil, err
	}
	catSet := map[uint]bool{}
	for _, c := range cats {
		catSet[c.ID] = true
	}
	return func(p dbmodels.Project) bool {
		return p.IsSensitive || (p.CategoryID != nil && catSet[*p.CategoryID])
	}, nil
}

// GET /api/projects
func (s *server) listProjects(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	redact, ok := parseRedact(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	tx := s.db.Read().Where("user_id = ?", userID)

	if st := q.Get("status"); st != "" {
		switch dbmodels.ProjectStatus(st) {
		case dbmodels.ProjectStatusActive, dbmodels.ProjectStatusCompleted, dbmodels.ProjectStatusArchived:
			tx = tx.Where("status = ?", st)
		default:
			errResult(http.StatusBadRequest, "invalid status").write(w)
			return
		}
	}
	if v := q.Get("category_id"); v != "" {
		cid, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			errResult(http.StatusBadRequest, "invalid category_id").write(w)
			return
		}
		tx = tx.Where("category_id = ?", cid)
	}

	var projs []dbmodels.Project
	if err := tx.Order("sort_order ASC, id ASC").Find(&projs).Error; err != nil {
		internalError(w, err, "list projects failed")
		return
	}

	var isSens func(dbmodels.Project) bool
	if redact {
		var err error
		isSens, err = projectSensitiveFn(s.db.Read(), userID)
		if err != nil {
			internalError(w, err, "list projects failed")
			return
		}
	}

	out := make([]projectResponse, len(projs))
	for i, p := range projs {
		out[i] = newProjectResponse(p)
		if redact && isSens(p) {
			redactProject(&out[i])
		}
	}
	jsonResult(http.StatusOK, out).write(w)
}

type createProjectRequest struct {
	Name        *string                 `json:"name"`
	Description *string                 `json:"description"`
	Color       *string                 `json:"color"`
	Emoji       *string                 `json:"emoji"`
	Status      *dbmodels.ProjectStatus `json:"status"`
	TargetDate  *string                 `json:"target_date"`
	CategoryID  *uint                   `json:"category_id"`
	IsSensitive *bool                   `json:"is_sensitive"`
	SortOrder   *int                    `json:"sort_order"`
}

// POST /api/projects
func (s *server) postProject(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	var req createProjectRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	if req.Name == nil {
		errResult(http.StatusBadRequest, "name is required").write(w)
		return
	}
	name := strings.TrimSpace(*req.Name)
	if name == "" {
		errResult(http.StatusBadRequest, "name is required").write(w)
		return
	}
	if utf8.RuneCountInString(name) > maxProjectNameLen {
		errResult(http.StatusBadRequest, "name too long").write(w)
		return
	}
	var desc string
	if req.Description != nil {
		if utf8.RuneCountInString(*req.Description) > maxProjectDescLen {
			errResult(http.StatusBadRequest, "description too long").write(w)
			return
		}
		desc = *req.Description
	}
	var color string
	if req.Color != nil {
		if len(*req.Color) > 32 {
			errResult(http.StatusBadRequest, "color too long").write(w)
			return
		}
		color = *req.Color
	}
	var emoji string
	if req.Emoji != nil {
		if !validEmoji(*req.Emoji) {
			errResult(http.StatusBadRequest, "invalid emoji").write(w)
			return
		}
		emoji = *req.Emoji
	}
	status := dbmodels.ProjectStatusActive
	if req.Status != nil && *req.Status != "" {
		switch *req.Status {
		case dbmodels.ProjectStatusActive, dbmodels.ProjectStatusCompleted, dbmodels.ProjectStatusArchived:
			status = *req.Status
		default:
			errResult(http.StatusBadRequest, "invalid status").write(w)
			return
		}
	}
	var targetDate *string
	if req.TargetDate != nil && *req.TargetDate != "" {
		if _, ok := strictDate(*req.TargetDate); !ok {
			errResult(http.StatusBadRequest, "invalid target_date").write(w)
			return
		}
		td := *req.TargetDate
		targetDate = &td
	}
	var isSensitive bool
	if req.IsSensitive != nil {
		isSensitive = *req.IsSensitive
	}
	if req.CategoryID != nil {
		cat, found, err := loadOwnedCategory(s.db.Read(), userID, *req.CategoryID)
		if err != nil {
			internalError(w, err, "create project failed")
			return
		}
		if !found {
			errResult(http.StatusBadRequest, "invalid category").write(w)
			return
		}
		// DESIGN 4.1 rule 1: sensitivity inheritance
		if cat.IsSensitive {
			isSensitive = true
		}
	}
	var sortOrder int
	if req.SortOrder != nil {
		sortOrder = *req.SortOrder
	}

	now := s.now().UTC()
	p := dbmodels.Project{
		UserID:      userID,
		CategoryID:  req.CategoryID,
		Name:        name,
		Description: desc,
		Color:       color,
		Emoji:       emoji,
		Status:      status,
		TargetDate:  targetDate,
		IsSensitive: isSensitive,
		SortOrder:   sortOrder,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.db.Write().Create(&p).Error; err != nil {
		internalError(w, err, "create project failed")
		return
	}
	jsonResult(http.StatusCreated, newProjectResponse(p)).write(w)
}

// GET /api/projects/{id}
func (s *server) getProject(w http.ResponseWriter, r *http.Request) {
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
	p, found, err := loadOwnedProject(s.db.Read(), userID, id)
	if err != nil {
		internalError(w, err, "get project failed")
		return
	}
	if !found {
		errResult(http.StatusNotFound, "project not found").write(w)
		return
	}

	resp := newProjectResponse(p)
	if redact {
		isSens, err := projectSensitiveFn(s.db.Read(), userID)
		if err != nil {
			internalError(w, err, "get project failed")
			return
		}
		if isSens(p) {
			redactProject(&resp)
		}
	}
	jsonResult(http.StatusOK, resp).write(w)
}

type patchProjectRequest struct {
	Name        *string                 `json:"name"`
	Description *string                 `json:"description"`
	Color       *string                 `json:"color"`
	Emoji       *string                 `json:"emoji"`
	Status      *dbmodels.ProjectStatus `json:"status"`
	TargetDate  nullable[string]        `json:"target_date"`
	CategoryID  nullable[uint]          `json:"category_id"`
	IsSensitive *bool                   `json:"is_sensitive"`
	SortOrder   *int                    `json:"sort_order"`
}

// PATCH /api/projects/{id}
func (s *server) patchProject(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req patchProjectRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	confirm := r.URL.Query().Get("confirm_exposure") == "1"

	res, err := s.txResult(func(tx *gorm.DB) (result, error) {
		p, found, err := loadOwnedProject(tx, userID, id)
		if err != nil {
			return result{}, err
		}
		if !found {
			return errResult(http.StatusNotFound, "project not found"), nil
		}

		// Check sensitivity transition (DESIGN 4.1 rule 3).
		// wasSensitive MUST be captured before mutating p.CategoryID or p.IsSensitive.
		isSensFn, err := projectSensitiveFn(tx, userID)
		if err != nil {
			return result{}, err
		}
		wasSensitive := isSensFn(p)

		if req.Name != nil {
			n := strings.TrimSpace(*req.Name)
			if n == "" {
				return errResult(http.StatusBadRequest, "name is required"), nil
			}
			if utf8.RuneCountInString(n) > maxProjectNameLen {
				return errResult(http.StatusBadRequest, "name too long"), nil
			}
			p.Name = n
		}
		if req.Description != nil {
			if utf8.RuneCountInString(*req.Description) > maxProjectDescLen {
				return errResult(http.StatusBadRequest, "description too long"), nil
			}
			p.Description = *req.Description
		}
		if req.Color != nil {
			if len(*req.Color) > 32 {
				return errResult(http.StatusBadRequest, "color too long"), nil
			}
			p.Color = *req.Color
		}
		if req.Emoji != nil {
			if !validEmoji(*req.Emoji) {
				return errResult(http.StatusBadRequest, "invalid emoji"), nil
			}
			p.Emoji = *req.Emoji
		}
		if req.Status != nil {
			switch *req.Status {
			case dbmodels.ProjectStatusActive, dbmodels.ProjectStatusCompleted, dbmodels.ProjectStatusArchived:
				p.Status = *req.Status
			default:
				return errResult(http.StatusBadRequest, "invalid status"), nil
			}
		}
		if req.TargetDate.Set {
			if req.TargetDate.Null || req.TargetDate.Val == "" {
				p.TargetDate = nil
			} else {
				if _, ok := strictDate(req.TargetDate.Val); !ok {
					return errResult(http.StatusBadRequest, "invalid target_date"), nil
				}
				td := req.TargetDate.Val
				p.TargetDate = &td
			}
		}

		var targetCat *dbmodels.Category
		if req.CategoryID.Set {
			if req.CategoryID.Null {
				p.CategoryID = nil
			} else {
				cat, found, err := loadOwnedCategory(tx, userID, req.CategoryID.Val)
				if err != nil {
					return result{}, err
				}
				if !found {
					return errResult(http.StatusBadRequest, "invalid category"), nil
				}
				cid := req.CategoryID.Val
				p.CategoryID = &cid
				targetCat = &cat
			}
		}

		targetSensitive := p.IsSensitive
		if req.IsSensitive != nil {
			targetSensitive = *req.IsSensitive
		}

		targetCatSensitive := false
		if p.CategoryID != nil {
			if targetCat != nil && targetCat.ID == *p.CategoryID {
				targetCatSensitive = targetCat.IsSensitive
			} else {
				var n int64
				if err := tx.Model(&dbmodels.Category{}).Where("id = ? AND user_id = ? AND is_sensitive = ?", *p.CategoryID, userID, true).Count(&n).Error; err != nil {
					return result{}, err
				}
				targetCatSensitive = n > 0
			}
		}

		// DESIGN 4.1 rule 1: inheriting sensitivity from category
		if targetCatSensitive {
			targetSensitive = true
		}

		willBeSensitive := targetSensitive || targetCatSensitive

		if wasSensitive && !willBeSensitive {
			// Lock items of the project first to maintain lock ordering (items before projects)
			// and prevent deadlocks with category operations that lock items then projects.
			if err := lockProjectItems(tx, userID, p.ID); err != nil {
				return result{}, err
			}
			if !confirm {
				if err := tx.Model(&dbmodels.Item{}).Where("user_id = ? AND project_id = ? AND is_sensitive = ?", userID, p.ID, false).
					Update("is_sensitive", true).Error; err != nil {
					return result{}, err
				}
			}
		}

		p.IsSensitive = targetSensitive
		if req.SortOrder != nil {
			p.SortOrder = *req.SortOrder
		}
		p.UpdatedAt = s.now().UTC()

		if err := tx.Save(&p).Error; err != nil {
			return result{}, err
		}

		return jsonResult(http.StatusOK, newProjectResponse(p)), nil
	})
	if err != nil {
		internalError(w, err, "update project failed")
		return
	}
	res.write(w)
}

// DELETE /api/projects/{id}?cascade=1 | ?unlink=1 [&confirm_exposure=1]
func (s *server) deleteProject(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	q := r.URL.Query()
	cascade := q.Get("cascade") == "1"
	unlink := q.Get("unlink") == "1"
	confirm := q.Get("confirm_exposure") == "1"
	if cascade && unlink {
		errResult(http.StatusBadRequest, "cannot pass both cascade and unlink").write(w)
		return
	}

	var deletedItemIDs []uint
	res, err := s.txResult(func(tx *gorm.DB) (result, error) {
		p, found, err := loadOwnedProject(tx, userID, id)
		if err != nil {
			return result{}, err
		}
		if !found {
			return errResult(http.StatusNotFound, "project not found"), nil
		}

		if err := lockProjectItems(tx, userID, id); err != nil {
			return result{}, err
		}

		var items int64
		if err := tx.Model(&dbmodels.Item{}).Where("user_id = ? AND project_id = ?", userID, id).Count(&items).Error; err != nil {
			return result{}, err
		}
		if items > 0 && !cascade && !unlink {
			return errResult(http.StatusConflict, "project has items; pass cascade=1 or unlink=1"), nil
		}

		if items > 0 && unlink {
			isSensFn, err := projectSensitiveFn(tx, userID)
			if err != nil {
				return result{}, err
			}
			if isSensFn(p) && !confirm {
				if err := tx.Model(&dbmodels.Item{}).Where("user_id = ? AND project_id = ? AND is_sensitive = ?", userID, p.ID, false).
					Update("is_sensitive", true).Error; err != nil {
					return result{}, err
				}
			}
			if err := tx.Model(&dbmodels.Item{}).Where("user_id = ? AND project_id = ?", userID, p.ID).
				Update("project_id", nil).Error; err != nil {
				return result{}, err
			}
			if err := dbmodels.BumpStateVersion(tx, userID); err != nil {
				return result{}, err
			}
		}

		if items > 0 && cascade {
			if err := tx.Model(&dbmodels.Item{}).Where("user_id = ? AND project_id = ?", userID, p.ID).
				Pluck("id", &deletedItemIDs).Error; err != nil {
				return result{}, err
			}
			if len(deletedItemIDs) > 0 {
				if err := tx.Where("user_id = ? AND item_id IN ?", userID, deletedItemIDs).Delete(&dbmodels.CheckinLog{}).Error; err != nil {
					return result{}, err
				}
				if err := tx.Where("item_id IN ?", deletedItemIDs).Delete(&dbmodels.ItemRuleVersion{}).Error; err != nil {
					return result{}, err
				}
				if err := tx.Where("item_id IN ?", deletedItemIDs).Delete(&dbmodels.ItemOption{}).Error; err != nil {
					return result{}, err
				}
				if err := tx.Where("id IN ? AND user_id = ?", deletedItemIDs, userID).Delete(&dbmodels.Item{}).Error; err != nil {
					return result{}, err
				}
				if err := dbmodels.BumpStateVersion(tx, userID); err != nil {
					return result{}, err
				}
			}
		}

		if err := tx.Where("id = ? AND user_id = ?", p.ID, userID).Delete(&dbmodels.Project{}).Error; err != nil {
			return result{}, err
		}

		return jsonResult(http.StatusOK, map[string]bool{"deleted": true}), nil
	})
	if err != nil {
		internalError(w, err, "delete project failed")
		return
	}

	for _, itID := range deletedItemIDs {
		s.versions.Bump(itID)
	}

	res.write(w)
}

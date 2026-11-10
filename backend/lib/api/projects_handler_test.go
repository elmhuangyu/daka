package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

func (e *env) createProject(user string, body any) projectResponse {
	e.t.Helper()
	w := e.do("POST", "/api/projects", user, "", body)
	require.Equal(e.t, http.StatusCreated, w.Code, w.Body.String())
	var resp projectResponse
	require.NoError(e.t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

func (e *env) rawProject(id uint) dbmodels.Project {
	e.t.Helper()
	var p dbmodels.Project
	require.NoError(e.t, e.db.Read().Where("id = ?", id).Take(&p).Error)
	return p
}

func TestProjectCRUD(t *testing.T) {
	e := newEnv(t)

	// Minimal create
	p1 := e.createProject("u1", m{"name": "Project Alpha"})
	require.NotZero(t, p1.ID)
	require.Equal(t, "Project Alpha", p1.Name)
	require.Equal(t, "", p1.Description)
	require.Equal(t, "", p1.Color)
	require.Equal(t, dbmodels.ProjectStatusActive, p1.Status)
	require.Nil(t, p1.TargetDate)
	require.Nil(t, p1.CategoryID)
	require.False(t, p1.IsSensitive)
	require.Equal(t, 0, p1.SortOrder)

	// Full create
	cid := e.category("u1", false)
	td := "2026-12-31"
	p2 := e.createProject("u1", m{
		"name":         "Project Beta",
		"description":  "Beta description",
		"color":        "#ff0000",
		"status":       "completed",
		"target_date":  td,
		"category_id":  cid,
		"is_sensitive": true,
		"sort_order":   5,
	})
	require.Equal(t, "Project Beta", p2.Name)
	require.Equal(t, "Beta description", p2.Description)
	require.Equal(t, "#ff0000", p2.Color)
	require.Equal(t, dbmodels.ProjectStatusCompleted, p2.Status)
	require.Equal(t, &td, p2.TargetDate)
	require.Equal(t, &cid, p2.CategoryID)
	require.True(t, p2.IsSensitive)
	require.Equal(t, 5, p2.SortOrder)

	// Validations on POST
	require.Equal(t, 400, e.do("POST", "/api/projects", "u1", "", m{}).Code)
	require.Equal(t, 400, e.do("POST", "/api/projects", "u1", "", m{"name": "   "}).Code)
	require.Equal(t, 400, e.do("POST", "/api/projects", "u1", "", m{"name": strings.Repeat("x", 256)}).Code)
	require.Equal(t, 400, e.do("POST", "/api/projects", "u1", "", m{"name": "P", "description": strings.Repeat("y", 2001)}).Code)
	require.Equal(t, 400, e.do("POST", "/api/projects", "u1", "", m{"name": "P", "color": strings.Repeat("c", 33)}).Code)
	require.Equal(t, 400, e.do("POST", "/api/projects", "u1", "", m{"name": "P", "status": "unknown"}).Code)
	require.Equal(t, 400, e.do("POST", "/api/projects", "u1", "", m{"name": "P", "target_date": "not-a-date"}).Code)
	require.Equal(t, 400, e.do("POST", "/api/projects", "u1", "", m{"name": "P", "category_id": 9999}).Code)

	// List
	w := e.do("GET", "/api/projects", "u1", "", nil)
	require.Equal(t, 200, w.Code)
	var list []projectResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list, 2)
	require.Equal(t, p1.ID, list[0].ID) // sort_order 0 before sort_order 5
	require.Equal(t, p2.ID, list[1].ID)

	// List filters
	w = e.do("GET", "/api/projects?status=completed", "u1", "", nil)
	require.Equal(t, 200, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list, 1)
	require.Equal(t, p2.ID, list[0].ID)

	w = e.do("GET", fmt.Sprintf("/api/projects?category_id=%d", cid), "u1", "", nil)
	require.Equal(t, 200, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list, 1)
	require.Equal(t, p2.ID, list[0].ID)

	require.Equal(t, 400, e.do("GET", "/api/projects?status=invalid", "u1", "", nil).Code)
	require.Equal(t, 400, e.do("GET", "/api/projects?category_id=abc", "u1", "", nil).Code)

	// Get
	w = e.do("GET", fmt.Sprintf("/api/projects/%d", p1.ID), "u1", "", nil)
	require.Equal(t, 200, w.Code)
	var got projectResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, p1.ID, got.ID)
	require.Equal(t, p1.Name, got.Name)

	require.Equal(t, 404, e.do("GET", "/api/projects/9999", "u1", "", nil).Code)

	// PATCH updates
	newTD := "2027-01-01"
	w = e.do("PATCH", fmt.Sprintf("/api/projects/%d", p1.ID), "u1", "", m{
		"name":        "Project Alpha Updated",
		"description": "New description",
		"color":       "#00ff00",
		"status":      "archived",
		"target_date": newTD,
		"category_id": cid,
		"sort_order":  10,
	})
	require.Equal(t, 200, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, "Project Alpha Updated", got.Name)
	require.Equal(t, "New description", got.Description)
	require.Equal(t, "#00ff00", got.Color)
	require.Equal(t, dbmodels.ProjectStatusArchived, got.Status)
	require.Equal(t, &newTD, got.TargetDate)
	require.Equal(t, &cid, got.CategoryID)
	require.Equal(t, 10, got.SortOrder)

	// Clear target_date and category_id with null
	w = e.do("PATCH", fmt.Sprintf("/api/projects/%d", p1.ID), "u1", "", m{
		"target_date": nil,
		"category_id": nil,
	})
	require.Equal(t, 200, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Nil(t, got.TargetDate)
	require.Nil(t, got.CategoryID)

	// Clear target_date with empty string
	w = e.do("PATCH", fmt.Sprintf("/api/projects/%d", p1.ID), "u1", "", m{
		"target_date": "",
	})
	require.Equal(t, 200, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Nil(t, got.TargetDate)

	// PUT works too
	w = e.do("PUT", fmt.Sprintf("/api/projects/%d", p1.ID), "u1", "", m{
		"name": "Project Alpha Via PUT",
	})
	require.Equal(t, 200, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, "Project Alpha Via PUT", got.Name)

	// Validations on PATCH
	require.Equal(t, 400, e.do("PATCH", fmt.Sprintf("/api/projects/%d", p1.ID), "u1", "", m{"name": ""}).Code)
	require.Equal(t, 400, e.do("PATCH", fmt.Sprintf("/api/projects/%d", p1.ID), "u1", "", m{"status": "bad"}).Code)
	require.Equal(t, 400, e.do("PATCH", fmt.Sprintf("/api/projects/%d", p1.ID), "u1", "", m{"target_date": "not-a-date"}).Code)
	require.Equal(t, 400, e.do("PATCH", fmt.Sprintf("/api/projects/%d", p1.ID), "u1", "", m{"category_id": 9999}).Code)

	// Delete
	w = e.do("DELETE", fmt.Sprintf("/api/projects/%d", p1.ID), "u1", "", nil)
	require.Equal(t, 200, w.Code)
	var delResp map[string]bool
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &delResp))
	require.True(t, delResp["deleted"])

	// GET after delete gives 404
	require.Equal(t, 404, e.do("GET", fmt.Sprintf("/api/projects/%d", p1.ID), "u1", "", nil).Code)
}

func TestProjectUserIsolation(t *testing.T) {
	e := newEnv(t)

	p1 := e.createProject("u1", m{"name": "U1 Project"})
	cat2 := e.category("u2", false)

	// u2 cannot see u1's project in list
	w := e.do("GET", "/api/projects", "u2", "", nil)
	require.Equal(t, 200, w.Code)
	var list []projectResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Empty(t, list)

	// u2 cannot GET, PATCH, DELETE u1's project
	require.Equal(t, 404, e.do("GET", fmt.Sprintf("/api/projects/%d", p1.ID), "u2", "", nil).Code)
	require.Equal(t, 404, e.do("PATCH", fmt.Sprintf("/api/projects/%d", p1.ID), "u2", "", m{"name": "Hacked"}).Code)
	require.Equal(t, 404, e.do("DELETE", fmt.Sprintf("/api/projects/%d", p1.ID), "u2", "", nil).Code)

	// u1 cannot attach project to u2's category
	require.Equal(t, 400, e.do("POST", "/api/projects", "u1", "", m{"name": "P", "category_id": cat2}).Code)
	require.Equal(t, 400, e.do("PATCH", fmt.Sprintf("/api/projects/%d", p1.ID), "u1", "", m{"category_id": cat2}).Code)
}

func TestProjectCategorySensitivityInheritance(t *testing.T) {
	e := newEnv(t)
	sensCat := e.category("u1", true)
	normCat := e.category("u1", false)

	// POST into sensitive category inherits is_sensitive=true even if passed false
	p1 := e.createProject("u1", m{"name": "P1", "category_id": sensCat, "is_sensitive": false})
	require.True(t, p1.IsSensitive)

	// POST into normal category stays is_sensitive=false
	p2 := e.createProject("u1", m{"name": "P2", "category_id": normCat, "is_sensitive": false})
	require.False(t, p2.IsSensitive)

	// PATCH into sensitive category marks is_sensitive=true
	w := e.do("PATCH", fmt.Sprintf("/api/projects/%d", p2.ID), "u1", "", m{"category_id": sensCat})
	require.Equal(t, 200, w.Code)
	var got projectResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.True(t, got.IsSensitive)
}

func TestProjectRedaction(t *testing.T) {
	e := newEnv(t)
	sensCat := e.category("u1", true)

	pSens := e.createProject("u1", m{"name": "Secret Project", "description": "Top secret", "is_sensitive": true})
	pCatSens := e.createProject("u1", m{"name": "Cat Secret", "description": "Inherited", "category_id": sensCat})
	pNorm := e.createProject("u1", m{"name": "Public Project", "description": "Everyone can see"})

	// GET /api/projects?redact=1
	w := e.do("GET", "/api/projects?redact=1", "u1", "", nil)
	require.Equal(t, 200, w.Code)
	var list []projectResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list, 3)

	for _, p := range list {
		switch p.ID {
		case pSens.ID, pCatSens.ID:
			require.Equal(t, "[REDACTED]", p.Name)
			require.Equal(t, "", p.Description)
		case pNorm.ID:
			require.Equal(t, "Public Project", p.Name)
			require.Equal(t, "Everyone can see", p.Description)
		}
	}

	// GET /api/projects without redact reveals plaintext
	w = e.do("GET", "/api/projects", "u1", "", nil)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), "Secret Project")
	require.Contains(t, w.Body.String(), "Top secret")

	// GET /api/projects/{id}?redact=1
	w = e.do("GET", fmt.Sprintf("/api/projects/%d?redact=1", pSens.ID), "u1", "", nil)
	require.Equal(t, 200, w.Code)
	var got projectResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, "[REDACTED]", got.Name)
	require.Equal(t, "", got.Description)

	w = e.do("GET", fmt.Sprintf("/api/projects/%d?redact=1", pNorm.ID), "u1", "", nil)
	require.Equal(t, 200, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, "Public Project", got.Name)
	require.Equal(t, "Everyone can see", got.Description)
}

func TestProjectDeleteConflictAndCascade(t *testing.T) {
	e := newEnv(t)
	p := e.createProject("u1", m{"name": "Proj With Items"})

	// Create an item in this project
	itID := e.item("u1", func(it *dbmodels.Item) {
		it.Name = "Item in Project"
		it.ProjectID = &p.ID
	})
	// Add an option and a checkin log
	opt := dbmodels.ItemOption{ItemID: itID, Name: "Option 1"}
	require.NoError(t, e.db.Write().Create(&opt).Error)
	log := dbmodels.CheckinLog{UserID: "u1", ItemID: itID, CheckinDate: "2026-11-06"}
	require.NoError(t, e.db.Write().Create(&log).Error)

	// DELETE without cascade or unlink -> 409 Conflict
	w := e.do("DELETE", fmt.Sprintf("/api/projects/%d", p.ID), "u1", "", nil)
	require.Equal(t, http.StatusConflict, w.Code)
	require.Contains(t, w.Body.String(), "project has items; pass cascade=1 or unlink=1")

	// DELETE with both cascade and unlink -> 400 Bad Request
	w = e.do("DELETE", fmt.Sprintf("/api/projects/%d?cascade=1&unlink=1", p.ID), "u1", "", nil)
	require.Equal(t, http.StatusBadRequest, w.Code)

	// DELETE with cascade=1
	w = e.do("DELETE", fmt.Sprintf("/api/projects/%d?cascade=1", p.ID), "u1", "", nil)
	require.Equal(t, 200, w.Code)

	// Verify project, item, rule version, option, and checkin log are all gone
	var pCount int64
	require.NoError(t, e.db.Read().Model(&dbmodels.Project{}).Where("id = ?", p.ID).Count(&pCount).Error)
	require.Zero(t, pCount)

	var itCount int64
	require.NoError(t, e.db.Read().Model(&dbmodels.Item{}).Where("id = ?", itID).Count(&itCount).Error)
	require.Zero(t, itCount)

	var verCount int64
	require.NoError(t, e.db.Read().Model(&dbmodels.ItemRuleVersion{}).Where("item_id = ?", itID).Count(&verCount).Error)
	require.Zero(t, verCount)

	var optCount int64
	require.NoError(t, e.db.Read().Model(&dbmodels.ItemOption{}).Where("item_id = ?", itID).Count(&optCount).Error)
	require.Zero(t, optCount)

	var logCount int64
	require.NoError(t, e.db.Read().Model(&dbmodels.CheckinLog{}).Where("item_id = ?", itID).Count(&logCount).Error)
	require.Zero(t, logCount)
}

func TestProjectDeleteUnlink(t *testing.T) {
	e := newEnv(t)

	// Case 1: Sensitive project unlinked without confirm_exposure freezes items to sensitive
	pSens := e.createProject("u1", m{"name": "Sens Project", "is_sensitive": true})
	it1 := e.item("u1", func(it *dbmodels.Item) {
		it.Name = "Item 1"
		it.ProjectID = &pSens.ID
		it.IsSensitive = false
	})

	w := e.do("DELETE", fmt.Sprintf("/api/projects/%d?unlink=1", pSens.ID), "u1", "", nil)
	require.Equal(t, 200, w.Code)

	gotItem1 := e.rawItem(it1)
	require.Nil(t, gotItem1.ProjectID)
	require.True(t, gotItem1.IsSensitive, "item should be frozen sensitive")

	// Case 2: Sensitive project unlinked with confirm_exposure leaves items un-flagged
	pSens2 := e.createProject("u1", m{"name": "Sens Project 2", "is_sensitive": true})
	it2 := e.item("u1", func(it *dbmodels.Item) {
		it.Name = "Item 2"
		it.ProjectID = &pSens2.ID
		it.IsSensitive = false
	})

	w = e.do("DELETE", fmt.Sprintf("/api/projects/%d?unlink=1&confirm_exposure=1", pSens2.ID), "u1", "", nil)
	require.Equal(t, 200, w.Code)

	gotItem2 := e.rawItem(it2)
	require.Nil(t, gotItem2.ProjectID)
	require.False(t, gotItem2.IsSensitive, "item should remain not sensitive")

	// Case 3: Non-sensitive project unlinked leaves items un-flagged
	pNorm := e.createProject("u1", m{"name": "Norm Project", "is_sensitive": false})
	it3 := e.item("u1", func(it *dbmodels.Item) {
		it.Name = "Item 3"
		it.ProjectID = &pNorm.ID
		it.IsSensitive = false
	})

	w = e.do("DELETE", fmt.Sprintf("/api/projects/%d?unlink=1", pNorm.ID), "u1", "", nil)
	require.Equal(t, 200, w.Code)

	gotItem3 := e.rawItem(it3)
	require.Nil(t, gotItem3.ProjectID)
	require.False(t, gotItem3.IsSensitive)
}

func TestProjectPatchDesensitizationFreezing(t *testing.T) {
	e := newEnv(t)

	// Case 1: Sensitive project updated to is_sensitive=false without confirm_exposure
	p1 := e.createProject("u1", m{"name": "Project P1", "is_sensitive": true})
	it1 := e.item("u1", func(it *dbmodels.Item) {
		it.Name = "Item P1"
		it.ProjectID = &p1.ID
		it.IsSensitive = false
	})

	w := e.do("PATCH", fmt.Sprintf("/api/projects/%d", p1.ID), "u1", "", m{"is_sensitive": false})
	require.Equal(t, 200, w.Code)
	gotIt1 := e.rawItem(it1)
	require.True(t, gotIt1.IsSensitive, "item should be frozen sensitive")

	// Case 2: Sensitive project updated to is_sensitive=false with confirm_exposure
	p2 := e.createProject("u1", m{"name": "Project P2", "is_sensitive": true})
	it2 := e.item("u1", func(it *dbmodels.Item) {
		it.Name = "Item P2"
		it.ProjectID = &p2.ID
		it.IsSensitive = false
	})

	w = e.do("PATCH", fmt.Sprintf("/api/projects/%d?confirm_exposure=1", p2.ID), "u1", "", m{"is_sensitive": false})
	require.Equal(t, 200, w.Code)
	gotIt2 := e.rawItem(it2)
	require.False(t, gotIt2.IsSensitive, "item should remain not sensitive with confirm_exposure")

	// Case 3: Project in sensitive category moved to null category without confirm_exposure
	sensCat := e.category("u1", true)
	p3 := e.createProject("u1", m{"name": "Project P3", "category_id": sensCat, "is_sensitive": false})
	// Because of inheritance, p3.IsSensitive is true. Let's create an item in it:
	it3 := e.item("u1", func(it *dbmodels.Item) {
		it.Name = "Item P3"
		it.ProjectID = &p3.ID
		it.IsSensitive = false
	})

	w = e.do("PATCH", fmt.Sprintf("/api/projects/%d", p3.ID), "u1", "", m{
		"category_id":  nil,
		"is_sensitive": false,
	})
	require.Equal(t, 200, w.Code)
	gotIt3 := e.rawItem(it3)
	require.True(t, gotIt3.IsSensitive, "item should be frozen sensitive when project leaves sensitive category")
}

func TestDeleteProjectRequiresReauthWhenConfigured(t *testing.T) {
	e := newEnv(t)
	deny := func(_ http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			errResult(http.StatusForbidden, "reauth_required").write(w)
		})
	}
	e.h = NewRouter(Deps{DB: e.db, Now: func() time.Time { return e.now }, Auth: userHeaderAuth, Reauth: deny})

	p := e.createProject("u1", m{"name": "Protected Project"})
	require.Equal(t, 403, e.do("DELETE", fmt.Sprintf("/api/projects/%d", p.ID), "u1", "", nil).Code)
	require.Equal(t, 200, e.do("GET", fmt.Sprintf("/api/projects/%d", p.ID), "u1", "", nil).Code, "only DELETE is guarded")
}

func TestItemSensitivityThroughProject(t *testing.T) {
	e := newEnv(t)
	p := e.createProject("u1", m{"name": "Sensitive Project", "is_sensitive": true})
	itID := e.item("u1", func(it *dbmodels.Item) {
		it.Name = "Secret Medicine"
		it.ProjectID = &p.ID
		it.IsSensitive = false
	})

	// sensitiveItemIDs should report itID as sensitive
	ids, err := sensitiveItemIDs(e.db.Read(), "u1")
	require.NoError(t, err)
	require.Contains(t, ids, itID)

	// GET /api/items?redact=1 should redact item name
	w := e.do("GET", "/api/items?redact=1", "u1", "", nil)
	require.Equal(t, 200, w.Code)
	require.NotContains(t, w.Body.String(), "Secret Medicine")
	require.Contains(t, w.Body.String(), "Private item")

	// GET /api/items/{id}?redact=1 should redact item name
	w = e.do("GET", fmt.Sprintf("/api/items/%d?redact=1", itID), "u1", "", nil)
	require.Equal(t, 200, w.Code)
	require.NotContains(t, w.Body.String(), "Secret Medicine")
	require.Contains(t, w.Body.String(), "Private item")

	// GET /api/items/{id} without redact returns effective_sensitive = true
	w = e.do("GET", fmt.Sprintf("/api/items/%d", itID), "u1", "", nil)
	require.Equal(t, 200, w.Code)
	var itResp itemResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &itResp))
	require.False(t, itResp.IsSensitive, "own is_sensitive is false")
	require.True(t, itResp.EffectiveSensitive, "effective_sensitive is true via project")
}

func TestProjectDynamicSensitivityCategoryFlipRegression(t *testing.T) {
	e := newEnv(t)

	// Scenario 1: Category flipped sensitive -> project in category has item -> PATCH project with category_id: null without confirm_exposure -> item is frozen sensitive!
	catID := e.category("u1", false)
	p := e.createProject("u1", m{"name": "Dynamic Project", "category_id": catID, "is_sensitive": false})
	rawProj := e.rawProject(p.ID)
	require.False(t, rawProj.IsSensitive, "project row in DB should have is_sensitive=false")

	// Category flipped sensitive via PUT
	w := e.do("PUT", fmt.Sprintf("/api/categories/%d", catID), "u1", "", m{"is_sensitive": true})
	require.Equal(t, 200, w.Code)

	// Verify project row in DB still has is_sensitive=false (dynamically sensitive via category)
	rawProj = e.rawProject(p.ID)
	require.False(t, rawProj.IsSensitive, "project row in DB is still is_sensitive=false")

	itID := e.item("u1", func(it *dbmodels.Item) {
		it.Name = "Dynamic Item"
		it.ProjectID = &p.ID
		it.IsSensitive = false
	})
	require.False(t, e.rawItem(itID).IsSensitive, "item is initially not explicitly sensitive")

	// PATCH project with category_id: null without confirm_exposure
	w = e.do("PATCH", fmt.Sprintf("/api/projects/%d", p.ID), "u1", "", m{
		"category_id": nil,
	})
	require.Equal(t, 200, w.Code)
	require.True(t, e.rawItem(itID).IsSensitive, "item should be frozen sensitive when dynamically sensitive project leaves category without confirm_exposure")

	// Scenario 2: With confirm_exposure=1, item should remain un-frozen
	catFlip2 := e.category("u1", false)
	pConf := e.createProject("u1", m{"name": "Dynamic Project Conf", "category_id": catFlip2, "is_sensitive": false})
	w = e.do("PUT", fmt.Sprintf("/api/categories/%d", catFlip2), "u1", "", m{"is_sensitive": true})
	require.Equal(t, 200, w.Code)

	itConf := e.item("u1", func(it *dbmodels.Item) {
		it.Name = "Dynamic Item Conf"
		it.ProjectID = &pConf.ID
		it.IsSensitive = false
	})
	w = e.do("PATCH", fmt.Sprintf("/api/projects/%d?confirm_exposure=1", pConf.ID), "u1", "", m{
		"category_id": nil,
	})
	require.Equal(t, 200, w.Code)
	require.False(t, e.rawItem(itConf).IsSensitive, "item should remain not sensitive with confirm_exposure")

	// Scenario 3: Category flipped sensitive -> PATCH project moving to an insensitive category without confirm_exposure -> item is frozen sensitive!
	catFlip3 := e.category("u1", false)
	pMove := e.createProject("u1", m{"name": "Dynamic Project Move", "category_id": catFlip3, "is_sensitive": false})
	w = e.do("PUT", fmt.Sprintf("/api/categories/%d", catFlip3), "u1", "", m{"is_sensitive": true})
	require.Equal(t, 200, w.Code)

	itMove := e.item("u1", func(it *dbmodels.Item) {
		it.Name = "Dynamic Item Move"
		it.ProjectID = &pMove.ID
		it.IsSensitive = false
	})
	insensCat := e.category("u1", false)
	w = e.do("PATCH", fmt.Sprintf("/api/projects/%d", pMove.ID), "u1", "", m{
		"category_id": insensCat,
	})
	require.Equal(t, 200, w.Code)
	require.True(t, e.rawItem(itMove).IsSensitive, "item should be frozen sensitive when moved to insensitive category without confirm_exposure")
}

func TestSubItems(t *testing.T) {
	e := newEnv(t)
	p := e.createProject("u1", m{"name": "P"})
	create := func(body m) (int, uint) {
		w := e.do("POST", "/api/items", "u1", "", body)
		var r struct {
			ID uint `json:"id"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &r)
		return w.Code, r.ID
	}
	base := func(extra m) m {
		b := m{"name": "t", "tracker_type": "boolean", "kind": "event", "polarity": "positive",
			"rule": m{"schedule_type": "none", "holiday_mode": "none"}}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	code, parent := create(base(m{"project_id": p.ID}))
	require.Equal(t, http.StatusCreated, code)

	// The sub-item inherits the parent's project even if none is sent.
	code, child := create(base(m{"parent_id": parent}))
	require.Equal(t, http.StatusCreated, code)
	var row dbmodels.Item
	require.NoError(t, e.db.Read().Where("id = ?", child).Take(&row).Error)
	require.NotNil(t, row.ParentID)
	require.Equal(t, parent, *row.ParentID)
	require.NotNil(t, row.ProjectID)
	require.Equal(t, p.ID, *row.ProjectID)

	// Only one level of nesting; unknown parents are rejected.
	code, _ = create(base(m{"parent_id": child}))
	require.Equal(t, http.StatusBadRequest, code)
	code, _ = create(base(m{"parent_id": 99999}))
	require.Equal(t, http.StatusBadRequest, code)

	// A sub-item cannot be moved to another project on its own.
	w0 := e.do("PATCH", fmt.Sprintf("/api/items/%d", child), "u1", "", m{"project_id": nil})
	require.Equal(t, http.StatusBadRequest, w0.Code)

	// A sensitive parent (via its category, flagged after creation) makes new
	// sub-items sensitive too.
	cat := e.category("u1", false)
	_, sp := create(base(m{"category_id": cat}))
	require.NoError(t, e.db.Write().Model(&dbmodels.Category{}).Where("id = ?", cat).Update("is_sensitive", true).Error)
	_, sc := create(base(m{"parent_id": sp}))
	var srow dbmodels.Item
	require.NoError(t, e.db.Read().Where("id = ?", sc).Take(&srow).Error)
	require.True(t, srow.IsSensitive)

	// Deleting the parent deletes the sub-item.
	w := e.do("DELETE", fmt.Sprintf("/api/items/%d", parent), "u1", "", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var n int64
	require.NoError(t, e.db.Read().Model(&dbmodels.Item{}).Where("id IN ?", []uint{parent, child}).Count(&n).Error)
	require.Zero(t, n)
}

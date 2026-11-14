package api

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

func (e *env) seedUser(id, email, name string) {
	e.t.Helper()
	var u dbmodels.User
	err := e.db.Write().Where("id = ?", id).Take(&u).Error
	if err == nil {
		require.NoError(e.t, e.db.Write().Model(&u).Updates(map[string]any{"email": email, "name": name}).Error)
		return
	}
	require.NoError(e.t, e.db.Write().Create(&dbmodels.User{ID: id, Email: email, Name: name}).Error)
}

func TestProjectShareLifecycleAndPermissions(t *testing.T) {
	e := newEnv(t)
	e.seedUser("u1", "owner@example.com", "Owner")
	e.seedUser("u2", "collab@example.com", "Collab")
	e.seedUser("u3", "other@example.com", "Other")

	p := e.createProject("u1", m{"name": "Shared Trip"})

	// Non-existent user or self-share fails.
	w := e.doRaw("POST", fmt.Sprintf("/api/projects/%d/shares", p.ID), "u1", `{"email":"missing@example.com"}`)
	require.Equal(t, 404, w.Code)
	w = e.doRaw("POST", fmt.Sprintf("/api/projects/%d/shares", p.ID), "u1", `{"email":"owner@example.com"}`)
	require.Equal(t, 400, w.Code)

	// Owner shares with u2 (case-insensitive email).
	w = e.doRaw("POST", fmt.Sprintf("/api/projects/%d/shares", p.ID), "u1", `{"email":"COLLAB@Example.com"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	var shared projectResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &shared))
	require.True(t, shared.IsOwner)
	require.Len(t, shared.Shares, 1)
	require.Equal(t, "u2", shared.Shares[0].UserID)
	require.Equal(t, "collab@example.com", shared.Shares[0].Email)

	// u2 sees it in listProjects and getProject with is_owner=false and shares populated.
	w = e.doRaw("GET", "/api/projects", "u2", "")
	require.Equal(t, 200, w.Code)
	var list []projectResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list, 1)
	require.False(t, list[0].IsOwner)
	require.Equal(t, "Owner", list[0].Owner.Name)
	require.Len(t, list[0].Shares, 1)

	// u3 cannot see it.
	w = e.doRaw("GET", fmt.Sprintf("/api/projects/%d", p.ID), "u3", "")
	require.Equal(t, 404, w.Code)

	// u2 cannot manage access, change sensitivity, or delete the project.
	w = e.doRaw("POST", fmt.Sprintf("/api/projects/%d/shares", p.ID), "u2", `{"email":"other@example.com"}`)
	require.Equal(t, 403, w.Code)
	w = e.doRaw("PATCH", fmt.Sprintf("/api/projects/%d", p.ID), "u2", `{"is_sensitive":true}`)
	require.Equal(t, 403, w.Code)
	w = e.doRaw("DELETE", fmt.Sprintf("/api/projects/%d", p.ID), "u2", "")
	require.Equal(t, 403, w.Code)

	// Owner cannot mark a shared project sensitive.
	w = e.doRaw("PATCH", fmt.Sprintf("/api/projects/%d", p.ID), "u1", `{"is_sensitive":true}`)
	require.Equal(t, 400, w.Code)

	// u2 can set their own personal category on the shared project without affecting u1.
	w = e.doRaw("POST", "/api/categories", "u2", `{"name":"Collab Cat"}`)
	require.Equal(t, 201, w.Code)
	var cat2 struct {
		ID uint `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cat2))

	w = e.doRaw("PATCH", fmt.Sprintf("/api/projects/%d", p.ID), "u2", fmt.Sprintf(`{"category_id":%d,"name":"Renamed Trip"}`, cat2.ID))
	require.Equal(t, 200, w.Code, w.Body.String())
	var u2Proj projectResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &u2Proj))
	require.NotNil(t, u2Proj.CategoryID)
	require.Equal(t, cat2.ID, *u2Proj.CategoryID)
	require.Equal(t, "Renamed Trip", u2Proj.Name)

	// Owner sees the renamed title, but their own category_id remains nil.
	w = e.doRaw("GET", fmt.Sprintf("/api/projects/%d", p.ID), "u1", "")
	require.Equal(t, 200, w.Code)
	var u1Proj projectResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &u1Proj))
	require.Nil(t, u1Proj.CategoryID)
	require.Equal(t, "Renamed Trip", u1Proj.Name)

	// u2 can unshare themselves via /shares/me.
	w = e.doRaw("DELETE", fmt.Sprintf("/api/projects/%d/shares/me", p.ID), "u2", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	w = e.doRaw("GET", fmt.Sprintf("/api/projects/%d", p.ID), "u2", "")
	require.Equal(t, 404, w.Code)

	// Re-share and let owner unshare u2.
	w = e.doRaw("POST", fmt.Sprintf("/api/projects/%d/shares", p.ID), "u1", `{"email":"collab@example.com"}`)
	require.Equal(t, 200, w.Code)
	w = e.doRaw("DELETE", fmt.Sprintf("/api/projects/%d/shares/u2", p.ID), "u1", "")
	require.Equal(t, 200, w.Code)
	w = e.doRaw("GET", fmt.Sprintf("/api/projects/%d", p.ID), "u2", "")
	require.Equal(t, 404, w.Code)
}

func TestSensitiveProjectCannotBeShared(t *testing.T) {
	e := newEnv(t)
	e.seedUser("u1", "owner@example.com", "Owner")
	e.seedUser("u2", "collab@example.com", "Collab")

	p := e.createProject("u1", m{"name": "Secret", "is_sensitive": true})
	w := e.doRaw("POST", fmt.Sprintf("/api/projects/%d/shares", p.ID), "u1", `{"email":"collab@example.com"}`)
	require.Equal(t, 400, w.Code)
}

func TestSharedProjectItemsAndCheckinsAndOCC(t *testing.T) {
	e := newEnv(t)
	e.seedUser("u1", "owner@example.com", "Owner")
	e.seedUser("u2", "collab@example.com", "Collab")

	p := e.createProject("u1", m{"name": "Team Project"})
	w := e.doRaw("POST", fmt.Sprintf("/api/projects/%d/shares", p.ID), "u1", `{"email":"collab@example.com"}`)
	require.Equal(t, 200, w.Code)

	// Collaborator creates a task inside the shared project.
	w = e.doRaw("POST", "/api/items", "u2", fmt.Sprintf(`{"name":"Book hotel","tracker_type":"boolean","kind":"event","polarity":"positive","project_id":%d,"rule":{"schedule_type":"none"}}`, p.ID))
	require.Equal(t, 201, w.Code, w.Body.String())
	var createdItem itemResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &createdItem))
	require.Equal(t, int64(1), createdItem.Version)

	// Owner sees the task in /api/items and /api/view/today.
	w = e.doRaw("GET", fmt.Sprintf("/api/items?project_id=%d", p.ID), "u1", "")
	require.Equal(t, 200, w.Code)
	var itemsOut struct {
		Items []itemResponse `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &itemsOut))
	require.Len(t, itemsOut.Items, 1)
	require.Equal(t, "Book hotel", itemsOut.Items[0].Name)

	// Collaborator checks in on the task; owner sees it in checkins and stats.
	w = e.doRaw("POST", "/api/checkins", "u2", fmt.Sprintf(`{"item_id":%d,"mode":"set","date":"2026-01-10"}`, createdItem.ID))
	require.Equal(t, 200, w.Code, w.Body.String())

	w = e.doRaw("GET", fmt.Sprintf("/api/items?project_id=%d&include=stats", p.ID), "u1", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var itemsWithStats struct {
		Items []itemResponse `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &itemsWithStats))
	require.Len(t, itemsWithStats.Items, 1)
	require.NotNil(t, itemsWithStats.Items[0].Stats)
	require.NotNil(t, itemsWithStats.Items[0].Stats.LastDate)
	require.Equal(t, "2026-01-10", *itemsWithStats.Items[0].Stats.LastDate)
	require.NotNil(t, itemsWithStats.Items[0].Stats.LastBy)
	require.Equal(t, "u2", itemsWithStats.Items[0].Stats.LastBy.ID)
	require.Equal(t, "Collab", itemsWithStats.Items[0].Stats.LastBy.Name)

	w = e.doRaw("GET", fmt.Sprintf("/api/checkins?item_id=%d", createdItem.ID), "u1", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "2026-01-10")

	w = e.doRaw("GET", fmt.Sprintf("/api/items/%d/stats", createdItem.ID), "u1", "")
	require.Equal(t, 200, w.Code)

	// OCC: Owner updates item with version=1 -> succeeds (version becomes 2).
	w = e.doRaw("PATCH", fmt.Sprintf("/api/items/%d", createdItem.ID), "u1", `{"name":"Book flight","version":1}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	var patched itemResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &patched))
	require.Equal(t, int64(2), patched.Version)

	// Collaborator still has stale version=1 -> gets 409 Conflict.
	w = e.doRaw("PATCH", fmt.Sprintf("/api/items/%d", createdItem.ID), "u2", `{"name":"Book train","version":1}`)
	require.Equal(t, 409, w.Code)

	// Collaborator leaves; item created by u2 remains in owner's project.
	w = e.doRaw("DELETE", fmt.Sprintf("/api/projects/%d/shares/me", p.ID), "u2", "")
	require.Equal(t, 200, w.Code)
	w = e.doRaw("GET", fmt.Sprintf("/api/items/%d", createdItem.ID), "u1", "")
	require.Equal(t, 200, w.Code)
}

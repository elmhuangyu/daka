package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

func (e *env) category(user string, sensitive bool) uint {
	e.t.Helper()
	c := dbmodels.Category{UserID: user, Name: "Cat", IsSensitive: sensitive}
	require.NoError(e.t, e.db.Write().Create(&c).Error)
	return c.ID
}

func (e *env) rawItem(id uint) dbmodels.Item {
	e.t.Helper()
	var it dbmodels.Item
	require.NoError(e.t, e.db.Write().First(&it, id).Error)
	return it
}

func (e *env) itemsRedacted(user string) (string, string) {
	w := e.do("GET", "/api/items?redact=1", user, "", nil)
	require.Equal(e.t, 200, w.Code)
	t := e.do("GET", "/api/view/today?redact=1", user, "", nil)
	require.Equal(e.t, 200, t.Code)
	return w.Body.String(), t.Body.String()
}

// B4: deleting a sensitive category keeps its items protected.
func TestDeleteSensitiveCategoryKeepsItemsRedacted(t *testing.T) {
	e := newEnv(t)
	cid := e.category("u1", true)
	id := e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Hidden Thing"; it.CategoryID = &cid })

	w := e.do("DELETE", fmt.Sprintf("/api/categories/%d", cid), "", "", nil)
	require.Equal(t, 409, w.Code)

	w = e.do("DELETE", fmt.Sprintf("/api/categories/%d?uncategorize=1", cid), "", "", nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	got := e.rawItem(id)
	require.Nil(t, got.CategoryID)
	require.True(t, got.IsSensitive)

	items, today := e.itemsRedacted("")
	require.NotContains(t, items, "Hidden Thing")
	require.NotContains(t, today, "Hidden Thing")
	w = e.do("GET", fmt.Sprintf("/api/items/%d/stats?redact=1", id), "", "", nil)
	require.NotContains(t, w.Body.String(), "Hidden Thing")
	require.Contains(t, w.Body.String(), `"is_sensitive":true`)
}

func TestDeleteSensitiveCategoryConfirmExposure(t *testing.T) {
	e := newEnv(t)
	cid := e.category("u1", true)
	id := e.dailyItem("u1", func(it *dbmodels.Item) { it.CategoryID = &cid })
	w := e.do("DELETE", fmt.Sprintf("/api/categories/%d?uncategorize=1&confirm_exposure=1", cid), "", "", nil)
	require.Equal(t, 200, w.Code)
	require.JSONEq(t, `{"deleted":true,"moved_items":1,"affected_items":1}`, w.Body.String())
	require.False(t, e.rawItem(id).IsSensitive)
}

func TestDeleteCategoryMoveToSensitive(t *testing.T) {
	e := newEnv(t)
	a := e.category("u1", true)
	b := e.category("u1", true)
	id := e.dailyItem("u1", func(it *dbmodels.Item) { it.CategoryID = &a })
	w := e.do("DELETE", fmt.Sprintf("/api/categories/%d?move_to=%d", a, b), "", "", nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	got := e.rawItem(id)
	require.Equal(t, b, *got.CategoryID)
	require.False(t, got.IsSensitive) // still protected dynamically by b
	require.Equal(t, 400, e.do("DELETE", fmt.Sprintf("/api/categories/%d?move_to=999", b), "", "", nil).Code)
	// Another user's category cannot be touched.
	o := e.category("u2", false)
	require.Equal(t, 404, e.do("DELETE", fmt.Sprintf("/api/categories/%d", o), "", "", nil).Code)
}

// B5: un-flagging a category freezes items unless confirm_exposure=1.
func TestCategoryDesensitizeNeedsConfirm(t *testing.T) {
	e := newEnv(t)
	cid := e.category("u1", true)
	id := e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Hidden Thing"; it.CategoryID = &cid })
	path := fmt.Sprintf("/api/categories/%d", cid)

	w := e.do("PUT", path, "", "", m{"is_sensitive": false})
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"affected_items":1`)
	require.True(t, e.rawItem(id).IsSensitive)
	items, _ := e.itemsRedacted("")
	require.NotContains(t, items, "Hidden Thing")

	// Explicitly exposing needs confirm_exposure.
	cid2 := e.category("u1", true)
	id2 := e.dailyItem("u1", func(it *dbmodels.Item) { it.CategoryID = &cid2 })
	w = e.do("PUT", fmt.Sprintf("/api/categories/%d?confirm_exposure=1", cid2), "", "", m{"is_sensitive": false})
	require.Equal(t, 200, w.Code)
	require.False(t, e.rawItem(id2).IsSensitive)

	// Flagging a category needs no write-back: effective sensitivity is dynamic.
	cid3 := e.category("u1", false)
	id3 := e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Later Secret"; it.CategoryID = &cid3 })
	require.Equal(t, 200, e.do("PUT", fmt.Sprintf("/api/categories/%d", cid3), "", "", m{"is_sensitive": true}).Code)
	require.False(t, e.rawItem(id3).IsSensitive)
	items, _ = e.itemsRedacted("")
	require.NotContains(t, items, "Later Secret")
	w = e.do("GET", fmt.Sprintf("/api/items/%d/stats", id3), "", "", nil)
	require.Contains(t, w.Body.String(), `"is_sensitive":true`)
}

func TestMoveItemOutOfSensitiveCategory(t *testing.T) {
	e := newEnv(t)
	sc := e.category("u1", true)
	pc := e.category("u1", false)
	id := e.dailyItem("u1", func(it *dbmodels.Item) { it.CategoryID = &sc })
	require.Equal(t, 200, e.do("PATCH", itemPath(id), "", "", m{"category_id": pc}).Code)
	require.True(t, e.rawItem(id).IsSensitive)

	id2 := e.dailyItem("u1", func(it *dbmodels.Item) { it.CategoryID = &sc })
	require.Equal(t, 200, e.do("PATCH", itemPath(id2)+"?confirm_exposure=1", "", "", m{"category_id": nil}).Code)
	require.False(t, e.rawItem(id2).IsSensitive)
}

// B6: redact covers items, checkins, today, calendar and stats.
func TestRedactEverywhere(t *testing.T) {
	e := newEnv(t)
	cid := e.category("u1", true)
	id := e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Hidden Thing"; it.CategoryID = &cid })
	require.Equal(t, 200, e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "set", "note": "Hidden Note"}).Code)
	for _, p := range []string{
		"/api/items", fmt.Sprintf("/api/items/%d", id), "/api/checkins", "/api/view/today",
		"/api/view/calendar?start=2026-11-08&end=2026-11-08", fmt.Sprintf("/api/items/%d/stats", id),
	} {
		sep := "?"
		if strings.Contains(p, "?") {
			sep = "&"
		}
		w := e.do("GET", p+sep+"redact=1", "", "", nil)
		require.Equal(t, 200, w.Code, p)
		require.NotContains(t, w.Body.String(), "Hidden Thing", p)
		require.NotContains(t, w.Body.String(), "Hidden Note", p)
	}
}

// Both orders of a category freeze and an item PATCH must end protected.
func TestFreezeAndPatchOrderings(t *testing.T) {
	e := newEnv(t)
	// Freeze first, then an unrelated PATCH (must not clobber is_sensitive).
	c1 := e.category("u1", true)
	id1 := e.dailyItem("u1", func(it *dbmodels.Item) { it.CategoryID = &c1 })
	require.Equal(t, 200, e.do("PUT", fmt.Sprintf("/api/categories/%d", c1), "", "", m{"is_sensitive": false}).Code)
	require.Equal(t, 200, e.do("PATCH", itemPath(id1), "", "", m{"pinned": true}).Code)
	require.True(t, e.rawItem(id1).IsSensitive)

	// PATCH first (rename), then the delete freeze.
	c2 := e.category("u1", true)
	id2 := e.dailyItem("u1", func(it *dbmodels.Item) { it.CategoryID = &c2 })
	require.Equal(t, 200, e.do("PATCH", itemPath(id2), "", "", m{"name": "Renamed"}).Code)
	require.Equal(t, 200, e.do("DELETE", fmt.Sprintf("/api/categories/%d?uncategorize=1", c2), "", "", nil).Code)
	got := e.rawItem(id2)
	require.True(t, got.IsSensitive)
	require.Equal(t, "Renamed", got.Name)

	// PATCH after the category is gone must keep the frozen flag.
	require.Equal(t, 200, e.do("PATCH", itemPath(id2), "", "", m{"category_id": nil, "pinned": true}).Code)
	require.True(t, e.rawItem(id2).IsSensitive)
}

func TestDeleteCategoryMoveToPlain(t *testing.T) {
	e := newEnv(t)
	a := e.category("u1", true)
	b := e.category("u1", false)
	id := e.dailyItem("u1", func(it *dbmodels.Item) { it.CategoryID = &a })
	w := e.do("DELETE", fmt.Sprintf("/api/categories/%d?move_to=%d", a, b), "", "", nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	got := e.rawItem(id)
	require.Equal(t, b, *got.CategoryID)
	require.True(t, got.IsSensitive) // frozen when landing in a plain category
}

func TestDeleteCategoryForeignMoveTo(t *testing.T) {
	e := newEnv(t)
	a := e.category("u1", true)
	foreign := e.category("u2", false)
	id := e.dailyItem("u1", func(it *dbmodels.Item) { it.CategoryID = &a })
	w := e.do("DELETE", fmt.Sprintf("/api/categories/%d?move_to=%d", a, foreign), "", "", nil)
	require.Equal(t, 400, w.Code)
	require.Equal(t, a, *e.rawItem(id).CategoryID)
	var n int64
	require.NoError(t, e.db.Write().Model(&dbmodels.Category{}).Where("id = ?", a).Count(&n).Error)
	require.EqualValues(t, 1, n)
}

func TestRedactHidesUnits(t *testing.T) {
	e := newEnv(t)
	w := e.do("POST", "/api/items", "", "", m{
		"name": "Hidden Thing", "tracker_type": "counter", "is_sensitive": true, "unit": "secretunit",
		"options": []m{{"name": "o1", "unit": "secretunit"}},
		"rule":    m{"schedule_type": "fixed_rrule", "rrule_string": "FREQ=DAILY"},
	})
	require.Equal(t, 201, w.Code, w.Body.String())
	it := decode[itemResponse](t, w)
	for _, p := range []string{
		"/api/items?redact=1", fmt.Sprintf("/api/items/%d?redact=1", it.ID),
		fmt.Sprintf("/api/items/%d/options?redact=1", it.ID), fmt.Sprintf("/api/items/%d/stats?redact=1", it.ID),
	} {
		w := e.do("GET", p, "", "", nil)
		require.Equal(t, 200, w.Code, p)
		require.NotContains(t, w.Body.String(), "secretunit", p)
	}
}

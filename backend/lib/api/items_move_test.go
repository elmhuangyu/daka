package api

import (
	"fmt"
	"net/http/httptest"
	"slices"
	"testing"

	"gorm.io/gorm"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

func (e *env) moveItem(user string, id uint, query string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.do("POST", fmt.Sprintf("/api/items/%d/move%s", id, query), user, "", body)
}

// order returns the ids among want in (sort_order, id) order.
func (e *env) order(want []uint) []uint {
	e.t.Helper()
	var ids []uint
	require.NoError(e.t, e.db.Write().Model(&dbmodels.Item{}).Where("id IN ?", want).Order("sort_order, id").Pluck("id", &ids).Error)
	return ids
}

func (e *env) sortOrders(ids []uint) map[uint]int {
	out := map[uint]int{}
	for _, id := range ids {
		out[id] = e.rawItem(id).SortOrder
	}
	return out
}

func (e *env) stateVer(user string) int64 {
	e.t.Helper()
	var st []dbmodels.UserSettings
	require.NoError(e.t, e.db.Write().Where("user_id = ?", user).Find(&st).Error)
	if len(st) == 0 {
		return 0
	}
	return st[0].StateVersion
}

// kids creates n children of parent (nil: top level in proj) with the given sort orders.
func (e *env) kids(user string, parent, proj *uint, orders ...int) []uint {
	ids := make([]uint, len(orders))
	for i, so := range orders {
		ids[i] = e.item(user, func(it *dbmodels.Item) { it.ParentID = parent; it.ProjectID = proj; it.SortOrder = so })
	}
	return ids
}

func TestMoveReorder(t *testing.T) {
	e := newEnv(t)
	pa := e.createProject("u1", m{"name": "A"})
	par := e.item("u1", nil)
	tests := []struct {
		name   string
		orders []int
		top    bool
		body   func(k []uint) m // k = children, k[0] is the moved item
		want   []int            // expected order as indexes into k
	}{
		{"after, all zero", []int{0, 0, 0, 0}, false, func(k []uint) m { return m{"after_id": k[2]} }, []int{1, 2, 0, 3}},
		{"before, all zero", []int{0, 0, 0, 0}, false, func(k []uint) m { return m{"before_id": k[3]} }, []int{1, 2, 0, 3}},
		{"before first", []int{5, 6, 7}, false, func(k []uint) m { return m{"before_id": k[1]} }, []int{0, 1, 2}},
		{"append", []int{0, 0, 0}, false, func(k []uint) m { return m{} }, []int{1, 2, 0}},
		{"append explicit parent null top level", []int{3, 1, 2}, true, func(k []uint) m { return m{"parent_id": nil} }, []int{1, 2, 0}},
		{"top level after", []int{0, 0, 0}, true, func(k []uint) m { return m{"after_id": k[1]} }, []int{1, 0, 2}},
		{"negative and gaps", []int{-5, 100, 100}, false, func(k []uint) m { return m{"after_id": k[2]} }, []int{1, 2, 0}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var k []uint
			if tc.top {
				k = e.kids("u1", nil, &pa.ID, tc.orders...)
			} else {
				k = e.kids("u1", &par, nil, tc.orders...)
			}
			other := e.kids("u1", nil, nil, 0, 0)
			otherBefore := e.sortOrders(other)
			w := e.moveItem("u1", k[0], "", tc.body(k))
			require.Equal(t, 200, w.Code, w.Body.String())
			var want []uint
			for _, i := range tc.want {
				want = append(want, k[i])
			}
			require.Equal(t, want, e.order(k))
			vals := e.sortOrders(k)
			var sorted []int
			for _, id := range want {
				sorted = append(sorted, vals[id])
			}
			for i := 1; i < len(sorted); i++ {
				require.Greater(t, sorted[i], sorted[i-1], "distinct increasing values")
			}
			require.Equal(t, otherBefore, e.sortOrders(other), "unrelated items untouched")
		})
	}
}

func TestMoveResponseShape(t *testing.T) {
	e := newEnv(t)
	k := e.kids("u1", nil, nil, 0, 0)
	w := e.moveItem("u1", k[0], "", m{"after_id": k[1]})
	require.Equal(t, 200, w.Code)
	g := e.doRaw("GET", fmt.Sprintf("/api/items/%d", k[0]), "u1", "")
	require.Equal(t, 200, g.Code)
	require.JSONEq(t, g.Body.String(), w.Body.String())
}

func TestMoveAcrossParents(t *testing.T) {
	e := newEnv(t)
	pa := e.createProject("u1", m{"name": "A"})
	pb := e.createProject("u1", m{"name": "B"})
	t.Run("into position under another parent, project follows", func(t *testing.T) {
		np := e.item("u1", func(it *dbmodels.Item) { it.ProjectID = &pb.ID })
		sibs := e.kids("u1", &np, &pb.ID, 0, 0, 0)
		mv := e.item("u1", func(it *dbmodels.Item) { it.ProjectID = &pa.ID })
		sub := e.item("u1", func(it *dbmodels.Item) { it.ParentID = &mv; it.ProjectID = &pa.ID })
		w := e.moveItem("u1", mv, "", m{"parent_id": np, "before_id": sibs[1]})
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, []uint{sibs[0], mv, sibs[1], sibs[2]}, e.order(append([]uint{mv}, sibs...)))
		require.Equal(t, np, *e.rawItem(mv).ParentID)
		require.Equal(t, pb.ID, *e.rawItem(mv).ProjectID)
		require.Equal(t, pb.ID, *e.rawItem(sub).ProjectID)
	})
	t.Run("to top level with position, project kept", func(t *testing.T) {
		tops := e.kids("u1", nil, &pa.ID, 0, 0)
		par := e.item("u1", func(it *dbmodels.Item) { it.ProjectID = &pa.ID })
		c := e.kids("u1", &par, &pa.ID, 0)[0]
		w := e.moveItem("u1", c, "", m{"parent_id": nil, "after_id": tops[0]})
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Nil(t, e.rawItem(c).ParentID)
		require.Equal(t, pa.ID, *e.rawItem(c).ProjectID)
		require.Equal(t, []uint{tops[0], c, tops[1]}, e.order([]uint{c, tops[0], tops[1]}))
	})
	t.Run("anchor must be under the new parent", func(t *testing.T) {
		np := e.item("u1", nil)
		mv := e.item("u1", nil)
		other := e.kids("u1", nil, nil, 0)[0] // top-level, not under np
		w := e.moveItem("u1", mv, "", m{"parent_id": np, "after_id": other})
		require.Equal(t, 400, w.Code, w.Body.String())
		require.Nil(t, e.rawItem(mv).ParentID, "reparent rolled back")
	})
}

func TestMoveRejections(t *testing.T) {
	e := newEnv(t)
	pa := e.createProject("u1", m{"name": "A"})
	par := e.item("u1", nil)
	tests := []struct {
		name  string
		setup func() (uint, m)
		code  int
	}{
		{"both anchors", func() (uint, m) {
			k := e.kids("u1", &par, nil, 0, 0, 0)
			return k[0], m{"before_id": k[1], "after_id": k[2]}
		}, 400},
		{"self anchor", func() (uint, m) {
			k := e.kids("u1", &par, nil, 0)
			return k[0], m{"after_id": k[0]}
		}, 400},
		{"zero anchor", func() (uint, m) {
			k := e.kids("u1", &par, nil, 0)
			return k[0], m{"after_id": 0}
		}, 400},
		{"anchor under other parent", func() (uint, m) {
			other := e.item("u1", nil)
			a := e.kids("u1", &par, nil, 0)[0]
			b := e.kids("u1", &other, nil, 0)[0]
			return a, m{"before_id": b}
		}, 400},
		{"anchor top-level but item has parent", func() (uint, m) {
			a := e.kids("u1", &par, nil, 0)[0]
			return a, m{"before_id": e.item("u1", nil)}
		}, 400},
		{"top-level anchor in another project", func() (uint, m) {
			a := e.item("u1", func(it *dbmodels.Item) { it.ProjectID = &pa.ID })
			return a, m{"before_id": e.item("u1", nil)}
		}, 400},
		{"other user's anchor", func() (uint, m) {
			return e.item("u1", nil), m{"before_id": e.item("u2", nil)}
		}, 404},
		{"missing anchor", func() (uint, m) { return e.item("u1", nil), m{"before_id": 99999} }, 404},
		{"other user's item", func() (uint, m) { return e.item("u2", nil), m{} }, 404},
		{"other user's parent", func() (uint, m) { return e.item("u1", nil), m{"parent_id": e.item("u2", nil)} }, 404},
		{"self parent", func() (uint, m) {
			a := e.item("u1", nil)
			return a, m{"parent_id": a}
		}, 400},
		{"cycle", func() (uint, m) {
			c := e.chain("u1", 3, nil)
			return c[0], m{"parent_id": c[2]}
		}, 400},
		{"depth", func() (uint, m) {
			c := e.chain("u1", 5, nil)
			return e.item("u1", nil), m{"parent_id": c[4]}
		}, 400},
		{"unknown field", func() (uint, m) { return e.item("u1", nil), m{"x": 1} }, 400},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, body := tc.setup()
			before := e.rawItem(id)
			ver := e.stateVer("u1")
			w := e.moveItem("u1", id, "", body)
			require.Equal(t, tc.code, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), "Secret Medication")
			after := e.rawItem(id)
			require.Equal(t, before.ParentID, after.ParentID)
			require.Equal(t, before.SortOrder, after.SortOrder)
			require.Equal(t, ver, e.stateVer("u1"))
		})
	}
}

func TestMoveIdempotent(t *testing.T) {
	e := newEnv(t)
	par := e.item("u1", nil)
	k := e.kids("u1", &par, nil, 1, 2, 3)
	ver := e.stateVer("u1")
	before := e.sortOrders(k)
	for _, body := range []m{{"before_id": k[2]}, {"after_id": k[0]}, {"parent_id": par, "after_id": k[0]}} {
		w := e.moveItem("u1", k[1], "", body)
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	w := e.moveItem("u1", k[2], "", m{})
	require.Equal(t, 200, w.Code)
	require.Equal(t, before, e.sortOrders(k))
	require.Equal(t, ver, e.stateVer("u1"), "no version bump")

	// A real move bumps once.
	w = e.moveItem("u1", k[0], "", m{})
	require.Equal(t, 200, w.Code)
	require.Equal(t, ver+1, e.stateVer("u1"))
	// Tied values at the right relative position are still resolved.
	z := e.kids("u1", &par, nil, 0, 0)
	w = e.moveItem("u1", z[0], "", m{"before_id": z[1]})
	require.Equal(t, 200, w.Code)
	require.NotEqual(t, e.rawItem(z[0]).SortOrder, e.rawItem(z[1]).SortOrder)
}

func TestMoveSensitivity(t *testing.T) {
	e := newEnv(t)
	tests := []struct {
		name  string
		setup func() (id uint, body m, q string)
		want  bool
	}{
		{"under sensitive parent", func() (uint, m, string) {
			p := e.item("u1", func(it *dbmodels.Item) { it.IsSensitive = true })
			return e.item("u1", nil), m{"parent_id": p}, ""
		}, true},
		{"leaving sensitive parent freezes", func() (uint, m, string) {
			p := e.item("u1", func(it *dbmodels.Item) { it.IsSensitive = true })
			c := e.kids("u1", &p, nil, 0)[0]
			return c, m{"parent_id": nil}, ""
		}, true},
		{"leaving sensitive parent confirmed", func() (uint, m, string) {
			p := e.item("u1", func(it *dbmodels.Item) { it.IsSensitive = true })
			c := e.kids("u1", &p, nil, 0)[0]
			return c, m{"parent_id": nil}, "?confirm_exposure=1"
		}, false},
		{"pure reorder keeps flags", func() (uint, m, string) {
			p := e.item("u1", func(it *dbmodels.Item) { it.IsSensitive = true })
			k := e.kids("u1", &p, nil, 0, 0)
			return k[0], m{"after_id": k[1]}, ""
		}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, body, q := tc.setup()
			w := e.moveItem("u1", id, q, body)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Equal(t, tc.want, e.rawItem(id).IsSensitive)
		})
	}
}

func TestMoveLeavesHistoryAlone(t *testing.T) {
	e := newEnv(t)
	par := e.item("u1", nil)
	k := e.kids("u1", nil, nil, 0, 0)
	var n int64
	require.NoError(t, e.db.Write().Model(&dbmodels.ItemRuleVersion{}).Count(&n).Error)
	require.Equal(t, 200, e.moveItem("u1", k[0], "", m{"parent_id": par}).Code)
	require.Equal(t, 200, e.moveItem("u1", k[1], "", m{"parent_id": par, "before_id": k[0]}).Code)
	var n2 int64
	require.NoError(t, e.db.Write().Model(&dbmodels.ItemRuleVersion{}).Count(&n2).Error)
	require.Equal(t, n, n2)
}

// A failure after the sort_order writes (an unknown tracker mutation cannot
// happen here, so force one by a rejected reparent) leaves siblings untouched.
func TestMoveAtomicOnFailure(t *testing.T) {
	e := newEnv(t)
	par := e.item("u1", nil)
	k := e.kids("u1", &par, nil, 0, 0, 0)
	before := e.sortOrders(k)
	w := e.moveItem("u1", k[0], "", m{"parent_id": 99999, "after_id": k[1]})
	require.Equal(t, 404, w.Code)
	require.Equal(t, before, e.sortOrders(k))
	w = e.moveItem("u1", k[0], "", m{"after_id": 99999})
	require.Equal(t, 404, w.Code)
	require.Equal(t, before, e.sortOrders(k))
	require.True(t, slices.Equal(k, e.order(k)))
}

// The sibling set joins the single ordered lock statement.
func TestMoveLockSetIncludesSiblings(t *testing.T) {
	e := newEnv(t)
	par := e.item("u1", nil)
	k := e.kids("u1", &par, nil, 0, 0)
	mv := e.item("u1", nil)
	var got []uint
	require.NoError(t, e.db.Write().Transaction(func(tx *gorm.DB) error {
		sibs, err := (&siblingScope{parentSet: true, parent: &par}).siblingIDs(tx, "u1", mv)
		got = sibs
		return err
	}))
	require.Equal(t, k, got)
}

package api

import (
	"context"
	"fmt"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

func (e *env) patchItem(user string, id uint, query string, body m) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.do("PATCH", fmt.Sprintf("/api/items/%d%s", id, query), user, "", body)
}

// chain creates n nested items (index 0 is the top) in project proj.
func (e *env) chain(user string, n int, proj *uint) []uint {
	ids := make([]uint, 0, n)
	var parent *uint
	for i := 0; i < n; i++ {
		id := e.item(user, func(it *dbmodels.Item) { it.Name = "n"; it.ParentID = parent; it.ProjectID = proj })
		ids = append(ids, id)
		p := id
		parent = &p
	}
	return ids
}

func TestReparent(t *testing.T) {
	e := newEnv(t)
	pa := e.createProject("u1", m{"name": "A"})
	pb := e.createProject("u1", m{"name": "B"})

	var (
		parent uint
		sub    []uint
	)
	tests := []struct {
		name  string
		setup func() (id uint, body m, query string)
		code  int
		check func(t *testing.T, id uint)
	}{
		{"self parent", func() (uint, m, string) {
			a := e.item("u1", nil)
			return a, m{"parent_id": a}, ""
		}, 400, nil},
		{"cycle via descendant", func() (uint, m, string) {
			c := e.chain("u1", 3, nil)
			return c[0], m{"parent_id": c[2]}, ""
		}, 400, nil},
		{"other user's parent", func() (uint, m, string) {
			a := e.item("u1", nil)
			o := e.item("u2", nil)
			return a, m{"parent_id": o}, ""
		}, 404, func(t *testing.T, id uint) { require.Nil(t, e.rawItem(id).ParentID) }},
		{"unknown parent", func() (uint, m, string) {
			return e.item("u1", nil), m{"parent_id": 99999}, ""
		}, 404, nil},
		{"depth 5 allowed", func() (uint, m, string) {
			c := e.chain("u1", 4, nil)
			return e.item("u1", nil), m{"parent_id": c[3]}, ""
		}, 200, nil},
		{"depth 6 rejected", func() (uint, m, string) {
			c := e.chain("u1", 5, nil)
			return e.item("u1", nil), m{"parent_id": c[4]}, ""
		}, 400, nil},
		{"subtree height counts", func() (uint, m, string) {
			c := e.chain("u1", 3, nil)
			sub := e.chain("u1", 3, nil)
			return sub[0], m{"parent_id": c[2]}, ""
		}, 400, nil},
		{"subtree height boundary", func() (uint, m, string) {
			c := e.chain("u1", 3, nil)
			sub := e.chain("u1", 2, nil)
			return sub[0], m{"parent_id": c[2]}, ""
		}, 200, nil},
		{"null to top level keeps project", func() (uint, m, string) {
			c := e.chain("u1", 2, &pa.ID)
			return c[1], m{"parent_id": nil}, ""
		}, 200, func(t *testing.T, id uint) {
			r := e.rawItem(id)
			require.Nil(t, r.ParentID)
			require.Equal(t, pa.ID, *r.ProjectID)
		}},
		{"reparent re-projects subtree", func() (uint, m, string) {
			par := e.item("u1", func(it *dbmodels.Item) { it.ProjectID = &pb.ID })
			c := e.chain("u1", 3, &pa.ID)
			parent, sub = par, c
			return c[0], m{"parent_id": par}, ""
		}, 200, func(t *testing.T, id uint) {
			for _, x := range sub {
				require.Equal(t, pb.ID, *e.rawItem(x).ProjectID)
			}
			require.Equal(t, parent, *e.rawItem(id).ParentID)
		}},
		{"reparent under project-less parent clears project", func() (uint, m, string) {
			par := e.item("u1", nil)
			c := e.chain("u1", 2, &pa.ID)
			sub = c
			return c[0], m{"parent_id": par}, ""
		}, 200, func(t *testing.T, id uint) {
			for _, x := range sub {
				require.Nil(t, e.rawItem(x).ProjectID)
			}
		}},
		{"project_id with parent_id rejected", func() (uint, m, string) {
			par := e.item("u1", nil)
			return e.item("u1", nil), m{"parent_id": par, "project_id": pa.ID}, ""
		}, 400, nil},
		{"kind is not restricted", func() (uint, m, string) {
			par := e.item("u1", func(it *dbmodels.Item) { it.Kind = dbmodels.KindEvent })
			return e.item("u1", func(it *dbmodels.Item) { it.Kind = dbmodels.KindHabit }), m{"parent_id": par}, ""
		}, 200, nil},
		{"sensitive parent makes subtree sensitive", func() (uint, m, string) {
			par := e.item("u1", func(it *dbmodels.Item) { it.IsSensitive = true })
			c := e.chain("u1", 3, nil)
			sub = c
			return c[0], m{"parent_id": par}, ""
		}, 200, func(t *testing.T, id uint) {
			for _, x := range sub {
				require.True(t, e.rawItem(x).IsSensitive)
			}
		}},
		{"category-sensitive parent makes subtree sensitive", func() (uint, m, string) {
			cat := e.category("u1", true)
			par := e.item("u1", func(it *dbmodels.Item) { it.CategoryID = &cat })
			c := e.chain("u1", 2, nil)
			sub = c
			return c[0], m{"parent_id": par}, ""
		}, 200, func(t *testing.T, id uint) {
			for _, x := range sub {
				require.True(t, e.rawItem(x).IsSensitive)
			}
		}},
		{"leaving sensitive parent freezes sensitivity", func() (uint, m, string) {
			par := e.item("u1", func(it *dbmodels.Item) { it.IsSensitive = true })
			c := e.chain("u1", 2, nil)
			require.NoError(t, e.db.Write().Model(&dbmodels.Item{}).Where("id = ?", c[0]).Update("parent_id", par).Error)
			sub = c
			return c[0], m{"parent_id": nil}, ""
		}, 200, func(t *testing.T, id uint) {
			for _, x := range sub {
				require.True(t, e.rawItem(x).IsSensitive)
			}
		}},
		{"leaving sensitive parent with confirm keeps stored flags", func() (uint, m, string) {
			par := e.item("u1", func(it *dbmodels.Item) { it.IsSensitive = true })
			c := e.item("u1", func(it *dbmodels.Item) { it.ParentID = &par })
			return c, m{"parent_id": nil}, "?confirm_exposure=1"
		}, 200, func(t *testing.T, id uint) { require.False(t, e.rawItem(id).IsSensitive) }},
		{"stored sensitive flag is never cleared", func() (uint, m, string) {
			par := e.item("u1", nil)
			c := e.item("u1", func(it *dbmodels.Item) { it.ParentID = &par; it.IsSensitive = true })
			return c, m{"parent_id": nil}, "?confirm_exposure=1"
		}, 200, func(t *testing.T, id uint) { require.True(t, e.rawItem(id).IsSensitive) }},
		{"absent parent_id leaves parent untouched", func() (uint, m, string) {
			par := e.item("u1", nil)
			c := e.item("u1", func(it *dbmodels.Item) { it.ParentID = &par })
			parent = par
			return c, m{"name": "renamed"}, ""
		}, 200, func(t *testing.T, id uint) { require.Equal(t, parent, *e.rawItem(id).ParentID) }},
		{"atomic on failure", func() (uint, m, string) {
			a := e.item("u1", nil)
			return a, m{"name": "renamed", "parent_id": 99999}, ""
		}, 404, func(t *testing.T, id uint) { require.Equal(t, "Secret Medication", e.rawItem(id).Name) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parent, sub = 0, nil
			id, body, q := tc.setup()
			w := e.patchItem("u1", id, q, body)
			require.Equal(t, tc.code, w.Code, w.Body.String())
			if w.Code != 200 {
				require.NotContains(t, w.Body.String(), "Secret Medication", "errors carry no item names")
			}
			if tc.check != nil {
				tc.check(t, id)
			}
		})
	}
}

func TestCreateDepthLimit(t *testing.T) {
	e := newEnv(t)
	tests := []struct {
		name string
		n    int // existing chain length; the new item goes under its last item
		code int
	}{
		{"create at depth 5", 4, 200},
		{"create at depth 6 rejected", 5, 400},
		{"create under legacy deep chain rejected", 7, 400},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := e.chain("u1", tc.n, nil)
			w := e.do("POST", "/api/items", "u1", "", m{
				"name": "x", "kind": "event", "tracker_type": "boolean", "parent_id": c[len(c)-1],
				"rule": m{"schedule_type": "fixed_rrule", "rrule_string": "FREQ=DAILY;COUNT=1", "dtstart": "2026-11-05", "holiday_mode": "none"},
			})
			if tc.code == 400 {
				require.Equal(t, 400, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), errItemTooDeep)
			} else {
				require.Equal(t, 201, w.Code, w.Body.String())
			}
		})
	}
}

func TestReparentDepthEdgeCases(t *testing.T) {
	e := newEnv(t)
	tests := []struct {
		name  string
		setup func() (uint, m)
		code  int
		msg   string
	}{
		{"top level of a deeper-than-limit legacy chain", func() (uint, m) {
			c := e.chain("u1", 8, nil)
			return c[3], m{"parent_id": nil}
		}, 200, ""},
		{"parent_id 0 is invalid", func() (uint, m) {
			return e.item("u1", nil), m{"parent_id": 0}
		}, 400, "invalid parent"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, body := tc.setup()
			w := e.patchItem("u1", id, "", body)
			require.Equal(t, tc.code, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), tc.msg)
			if tc.code == 200 {
				require.Nil(t, e.rawItem(id).ParentID)
			}
		})
	}
}

type sqlRecorder struct {
	logger.Interface
	sqls []string
}

func (r *sqlRecorder) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	q, _ := fc()
	r.sqls = append(r.sqls, q)
}

// The lock statement covers the moved item, its subtree and the new parent's
// whole ancestor chain, ordered by id, and the descendants returned come from a
// read that runs after the lock.
func TestLockItemTreeSet(t *testing.T) {
	e := newEnv(t)
	// a > b > c ; x > y > z. Moving a under z locks a, b, c and z, y, x.
	abc := e.chain("u1", 3, nil)
	xyz := e.chain("u1", 3, nil)
	want := append(append([]uint{}, abc...), xyz...)
	slices.Sort(want)
	rec := &sqlRecorder{Interface: logger.Default.LogMode(logger.Silent)}
	require.NoError(t, e.db.Write().Transaction(func(tx *gorm.DB) error {
		tx = tx.Session(&gorm.Session{Logger: rec})
		sub, ok, err := lockItemTree(tx, "u1", abc[0], &xyz[2])
		require.NoError(t, err)
		require.True(t, ok)
		require.ElementsMatch(t, abc[1:], sub)
		return nil
	}))
	lockIdx := -1
	for i, q := range rec.sqls {
		if strings.Contains(q, "id IN") && strings.Contains(q, "ORDER BY id") {
			lockIdx = i
			for _, id := range want {
				require.Contains(t, q, fmt.Sprint(id))
			}
			require.NotContains(t, q, "parent_id IN", "lock statement is not the descendant walk")
			break
		}
	}
	require.GreaterOrEqual(t, lockIdx, 0, "ordered lock statement not found: %v", rec.sqls)
	// A descendant walk (parent_id IN) must follow the lock.
	after := false
	for _, q := range rec.sqls[lockIdx+1:] {
		after = after || strings.Contains(q, "parent_id IN")
	}
	require.True(t, after, "descendants are recomputed after locking")
}

// A commit that lands between computing the lock set and getting the locks
// (simulated by the hook, which runs in the same transaction) must be seen:
// the set is recomputed after locking, so "A under B" then sees B under A.
func TestReparentSeesChangeAfterLock(t *testing.T) {
	e := newEnv(t)
	a := e.item("u1", nil)
	b := e.item("u1", nil)
	reparentRaceHook = func(tx *gorm.DB, round int) {
		if round == 0 {
			require.NoError(t, tx.Model(&dbmodels.Item{}).Where("id = ?", b).Update("parent_id", a).Error)
		}
	}
	t.Cleanup(func() { reparentRaceHook = nil })
	w := e.patchItem("u1", a, "", m{"parent_id": b})
	require.Equal(t, 400, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "descendant")
	require.Nil(t, e.rawItem(a).ParentID)

	// A set that never stabilizes gives up with 409 instead of committing.
	c := e.item("u1", nil)
	d := e.item("u1", nil)
	f := e.item("u1", nil)
	flip := false
	reparentRaceHook = func(tx *gorm.DB, round int) {
		flip = !flip
		var pid any
		if flip {
			pid = c
		}
		require.NoError(t, tx.Model(&dbmodels.Item{}).Where("id = ?", f).Update("parent_id", pid).Error)
	}
	w = e.patchItem("u1", c, "", m{"parent_id": d})
	require.Equal(t, 409, w.Code, w.Body.String())
}

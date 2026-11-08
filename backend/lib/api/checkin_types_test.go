package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
)

func (e *env) option(itemID uint, mut func(*dbmodels.ItemOption)) uint {
	e.t.Helper()
	o := dbmodels.ItemOption{ItemID: itemID, Name: "opt"}
	if mut != nil {
		mut(&o)
	}
	require.NoError(e.t, e.db.Write().Create(&o).Error)
	return o.ID
}

func fp(v float64) *float64 { return &v }

func (e *env) typed(user string, tr dbmodels.TrackerType, mut func(*dbmodels.Item)) uint {
	return e.item(user, func(i *dbmodels.Item) {
		i.TrackerType = tr
		if mut != nil {
			mut(i)
		}
	})
}

func (e *env) sum(itemID uint, date string) float64 {
	var s float64
	for _, l := range e.logs(itemID, date) {
		s += l.Value
	}
	return s
}

func TestModeTypeMatrix(t *testing.T) {
	e := newEnv(t)
	items := map[string]uint{
		"bool":        e.item("u1", nil),
		"boolneg":     e.item("u1", func(i *dbmodels.Item) { i.Polarity = dbmodels.PolarityNegative }),
		"counter":     e.typed("u1", dbmodels.TrackerCounter, nil),
		"maintenance": e.typed("u1", dbmodels.TrackerMaintenance, nil),
		"metric":      e.typed("u1", dbmodels.TrackerMetric, nil),
		"metricneg":   e.typed("u1", dbmodels.TrackerMetric, func(i *dbmodels.Item) { i.Polarity = dbmodels.PolarityNegative }),
	}
	allowed := map[string]string{
		"bool": "set unset", "boolneg": "append", "counter": "delta",
		"maintenance": "set unset", "metric": "append", "metricneg": "append",
	}
	for name, id := range items {
		for _, mode := range []string{"set", "unset", "delta", "append"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				w := e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": mode, "value": 5})
				ok := false
				for _, a := range splitWords(allowed[name]) {
					ok = ok || a == mode
				}
				if ok {
					require.Equal(t, 200, w.Code, w.Body.String())
				} else {
					require.Equal(t, 400, w.Code, w.Body.String())
					require.Contains(t, w.Body.String(), "mode does not match")
				}
			})
		}
	}
}

func splitWords(s string) []string {
	var out []string
	cur := ""
	for _, r := range s + " " {
		if r == ' ' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	return out
}

func TestMaintenanceSetUnset(t *testing.T) {
	e := newEnv(t)
	id := e.typed("u1", dbmodels.TrackerMaintenance, nil)
	for _, note := range []string{"changed filter", "changed filter again"} {
		w := e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "set", "note": note})
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	l := e.logs(id, "2026-11-07")
	require.Len(t, l, 1)
	require.Equal(t, "changed filter again", l[0].Note)
	w := e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "unset"})
	require.Equal(t, 200, w.Code)
	require.Empty(t, e.logs(id, "2026-11-07"))
	// options are not supported on maintenance
	w = e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "set", "option_id": 1})
	require.Equal(t, 400, w.Code)
}

func TestCounterDeltaGuardAndUndo(t *testing.T) {
	e := newEnv(t)
	id := e.typed("u1", dbmodels.TrackerCounter, nil)
	post := func(v float64) *struct {
		code int
		resp checkinResponse
	} {
		w := e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "delta", "value": v})
		r := &struct {
			code int
			resp checkinResponse
		}{code: w.Code}
		_ = json.Unmarshal(w.Body.Bytes(), &r.resp)
		return r
	}
	r := post(250)
	require.Equal(t, 200, r.code)
	require.Equal(t, 250.0, *r.resp.Total)
	require.Equal(t, 200, post(250).code)
	r = post(-100)
	require.Equal(t, 200, r.code)
	require.Equal(t, 400.0, *r.resp.Total)
	require.Equal(t, 400, post(-400.5).code) // would go to -0.5
	require.Equal(t, 200, post(-400).code)   // exactly zero is fine
	require.Equal(t, 400, post(-1).code)
	require.Len(t, e.logs(id, "2026-11-07"), 4)
	require.Zero(t, e.sum(id, "2026-11-07"))
	// another day starts at zero
	w := e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "delta", "value": -1, "date": "2026-11-06"})
	require.Equal(t, 400, w.Code)
	w = e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "delta", "value": 0})
	require.Equal(t, 400, w.Code)
}

func TestCounterDeleteIncrementAndCompletion(t *testing.T) {
	e := newEnv(t)
	id := e.typed("u1", dbmodels.TrackerCounter, nil)
	require.NoError(t, e.db.Write().Model(&dbmodels.ItemRuleVersion{}).Where("item_id = ?", id).
		Update("target_value", 2000).Error)
	var last checkinResponse
	for i := 0; i < 8; i++ {
		w := e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "delta", "value": 250})
		require.Equal(t, 200, w.Code)
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &last))
	}
	done := func() bool {
		v, err := dbmodels.CurrentRuleVersion(e.db.Write(), id, "2026-11-07")
		require.NoError(t, err)
		return schedule.DayCompletion(dbmodels.TrackerCounter, *v, nil, e.logs(id, "2026-11-07"), "").Done
	}
	require.True(t, done())
	w := e.do("DELETE", "/api/checkins/"+itoa(last.ID), "", "", nil)
	require.Equal(t, 200, w.Code)
	require.Equal(t, 1750.0, e.sum(id, "2026-11-07"))
	require.False(t, done())
}

func itoa(n uint) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestDeltaIdempotentReplay(t *testing.T) {
	e := newEnv(t)
	id := e.typed("u1", dbmodels.TrackerCounter, nil)
	body := m{"item_id": id, "mode": "delta", "value": 250}
	w1 := e.do("POST", "/api/checkins", "", "k1", body)
	w2 := e.do("POST", "/api/checkins", "", "k1", body)
	require.Equal(t, 200, w1.Code)
	require.Equal(t, w1.Body.String(), w2.Body.String())
	require.Len(t, e.logs(id, "2026-11-07"), 1)
	require.Equal(t, 250.0, e.sum(id, "2026-11-07"))
	// failed (rejected) responses are not stored: the same key may retry
	bad := m{"item_id": id, "mode": "delta", "value": -9999}
	require.Equal(t, 400, e.do("POST", "/api/checkins", "", "k2", bad).Code)
	require.Equal(t, 200, e.do("POST", "/api/checkins", "", "k2", m{"item_id": id, "mode": "delta", "value": -10}).Code)
}

func TestMetricAppendsAndAggregates(t *testing.T) {
	// A4: same-day two appends both succeed; avg/latest/sum are correct.
	for _, tc := range []struct {
		agg  dbmodels.Aggregate
		want float64
	}{{dbmodels.AggregateLatest, 40}, {dbmodels.AggregateAvg, 50}, {dbmodels.AggregateSum, 100}} {
		t.Run(string(tc.agg), func(t *testing.T) {
			e := newEnv(t)
			id := e.typed("u1", dbmodels.TrackerMetric, func(i *dbmodels.Item) { i.Aggregate = tc.agg })
			for _, v := range []float64{60, 40} {
				w := e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "append", "value": v})
				require.Equal(t, 200, w.Code, w.Body.String())
				e.now = e.now.Add(time.Minute)
			}
			logs := e.logs(id, "2026-11-07")
			require.Len(t, logs, 2)
			ver, err := dbmodels.CurrentRuleVersion(e.db.Write(), id, "2026-11-07")
			require.NoError(t, err)
			c := schedule.DayCompletion(dbmodels.TrackerMetric, *ver, nil, logs, tc.agg)
			require.Equal(t, tc.want, c.Progress[0].Value)
		})
	}
}

func TestNegativeBooleanTwiceIsTwoLogs(t *testing.T) { // A13
	e := newEnv(t)
	id := e.item("u1", func(i *dbmodels.Item) { i.Polarity = dbmodels.PolarityNegative })
	for i := 0; i < 2; i++ {
		w := e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "append", "value": 99, "note": "slipped"})
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	l := e.logs(id, "2026-11-07")
	require.Len(t, l, 2)
	require.Zero(t, l[0].Value) // value ignored
	require.Equal(t, "slipped", l[1].Note)
}

func TestOptionsRules(t *testing.T) {
	e := newEnv(t)
	plain := e.typed("u1", dbmodels.TrackerCounter, nil)
	withOpts := e.typed("u1", dbmodels.TrackerCounter, nil)
	o1 := e.option(withOpts, nil)
	archived := e.option(withOpts, func(o *dbmodels.ItemOption) { o.Archived = true })
	foreign := e.option(e.typed("u1", dbmodels.TrackerCounter, nil), nil)
	otherUser := e.typed("u2", dbmodels.TrackerCounter, nil)
	e.option(otherUser, nil)
	metric := e.typed("u1", dbmodels.TrackerMetric, func(i *dbmodels.Item) { i.MinVal, i.MaxVal = fp(0), fp(100) })
	mo := e.option(metric, func(o *dbmodels.ItemOption) { o.MinVal, o.MaxVal = fp(10), fp(50) })

	tests := []struct {
		name string
		body any
		code int
	}{
		{"option required", m{"item_id": withOpts, "mode": "delta", "value": 1}, 400},
		{"ok", m{"item_id": withOpts, "mode": "delta", "value": 1, "option_id": o1}, 200},
		{"archived option", m{"item_id": withOpts, "mode": "delta", "value": 1, "option_id": archived}, 400},
		{"foreign item's option", m{"item_id": withOpts, "mode": "delta", "value": 1, "option_id": foreign}, 400},
		{"unknown option", m{"item_id": withOpts, "mode": "delta", "value": 1, "option_id": 9999}, 400},
		{"option on item without options", m{"item_id": plain, "mode": "delta", "value": 1, "option_id": foreign}, 400},
		{"adhoc on item without options", m{"item_id": plain, "mode": "delta", "value": 1, "adhoc": true, "note": "x"}, 400},
		{"adhoc ok", m{"item_id": withOpts, "mode": "delta", "value": 1, "adhoc": true, "note": "swim"}, 200},
		{"adhoc without name", m{"item_id": withOpts, "mode": "delta", "value": 1, "adhoc": true}, 400},
		{"adhoc with option_id", m{"item_id": withOpts, "mode": "delta", "value": 1, "adhoc": true, "note": "x", "option_id": o1}, 400},
		{"unknown item", m{"item_id": 9999, "mode": "delta", "value": 1}, 404},
		{"other user's item", m{"item_id": otherUser, "mode": "delta", "value": 1}, 404},
		{"metric missing value", m{"item_id": metric, "mode": "append", "option_id": mo}, 400},
		{"metric below option min", m{"item_id": metric, "mode": "append", "option_id": mo, "value": 5}, 400},
		{"metric above option max", m{"item_id": metric, "mode": "append", "option_id": mo, "value": 51}, 400},
		{"metric within", m{"item_id": metric, "mode": "append", "option_id": mo, "value": 50}, 200},
		{"huge value", m{"item_id": plain, "mode": "delta", "value": 2e9}, 400},
		{"NaN literal", `{"item_id":` + itoa(plain) + `,"mode":"delta","value":NaN}`, 400},
		{"overflow literal", `{"item_id":` + itoa(plain) + `,"mode":"delta","value":1e999}`, 400},
		{"string value", `{"item_id":` + itoa(plain) + `,"mode":"delta","value":"1"}`, 400},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := e.do("POST", "/api/checkins", "", "", tc.body)
			require.Equal(t, tc.code, w.Code, w.Body.String())
		})
	}
}

func TestMetricItemRangeAndNoOptions(t *testing.T) {
	e := newEnv(t)
	id := e.typed("u1", dbmodels.TrackerMetric, func(i *dbmodels.Item) { i.MinVal, i.MaxVal = fp(30), fp(200) })
	for v, code := range map[float64]int{29.9: 400, 30: 200, 200: 200, 200.1: 400, -5: 400} {
		w := e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "append", "value": v})
		require.Equal(t, code, w.Code, "%v %s", v, w.Body.String())
	}
	// negative metric values are allowed when no range forbids them
	free := e.typed("u1", dbmodels.TrackerMetric, nil)
	require.Equal(t, 200, e.do("POST", "/api/checkins", "", "", m{"item_id": free, "mode": "append", "value": -3.5}).Code)
}

func TestOptionTargetsFromRuleSnapshot(t *testing.T) { // A7
	e := newEnv(t)
	id := e.typed("u1", dbmodels.TrackerCounter, nil)
	o := e.option(id, func(o *dbmodels.ItemOption) { o.Aggregate = dbmodels.AggregateSum })
	key := `{"` + itoa(o) + `":10}`
	require.NoError(t, e.db.Write().Model(&dbmodels.ItemRuleVersion{}).Where("item_id = ?", id).
		Update("option_targets", key).Error)
	require.NoError(t, e.db.Write().Create(&dbmodels.ItemRuleVersion{
		ItemID: id, EffectiveFrom: "2026-11-07", ScheduleType: dbmodels.ScheduleNone, HolidayMode: dbmodels.HolidayNone,
		OptionTargets: `{"` + itoa(o) + `":5}`,
	}).Error)
	for _, date := range []string{"2026-11-06", "2026-11-07"} {
		w := e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "delta", "value": 8, "option_id": o, "date": date})
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	var opts []dbmodels.ItemOption
	require.NoError(t, e.db.Write().Where("item_id = ?", id).Find(&opts).Error)
	for date, want := range map[string]bool{"2026-11-06": false, "2026-11-07": true} {
		v, err := dbmodels.CurrentRuleVersion(e.db.Write(), id, date)
		require.NoError(t, err)
		c := schedule.DayCompletion(dbmodels.TrackerCounter, *v, opts, e.logs(id, date), "")
		require.Equal(t, want, c.Done, date)
	}
}

func TestSupplementAndFutureForDelta(t *testing.T) {
	e := newEnv(t)
	id := e.typed("u1", dbmodels.TrackerCounter, nil)
	w := e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "delta", "value": 1, "date": "2026-11-06"})
	require.Equal(t, 200, w.Code)
	require.True(t, e.logs(id, "2026-11-06")[0].IsSupplement)
	w = e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "delta", "value": 1, "date": "2026-11-08"})
	require.Equal(t, 400, w.Code)
}

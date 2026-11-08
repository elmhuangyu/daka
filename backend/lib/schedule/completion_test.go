package schedule

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

var t0 = time.Date(2026, 11, 7, 8, 0, 0, 0, time.UTC)

func lg(id uint, opt uint, val float64, min int) dbmodels.CheckinLog {
	l := dbmodels.CheckinLog{ID: id, Value: val, CheckinTime: t0.Add(time.Duration(min) * time.Minute)}
	if opt != 0 {
		l.OptionID = &opt
	}
	return l
}

func f(v float64) *float64 { return &v }

func TestDayCompletion(t *testing.T) {
	cups := func(n int) []dbmodels.CheckinLog {
		var out []dbmodels.CheckinLog
		for i := 0; i < n; i++ {
			out = append(out, lg(uint(i+1), 0, 250, i))
		}
		return out
	}
	multi := []dbmodels.ItemOption{{ID: 1, Aggregate: dbmodels.AggregateSum}, {ID: 2, Aggregate: dbmodels.AggregateSum}, {ID: 3, Aggregate: dbmodels.AggregateSum}}
	multiV := func(rule dbmodels.CompletionRule, n int) dbmodels.ItemRuleVersion {
		return dbmodels.ItemRuleVersion{CompletionRule: rule, CompletionN: n, OptionTargets: `{"1":10,"2":20,"3":30}`}
	}
	// opt1 met (10), opt2 not (5<20), opt3 met (30).
	multiLogs := []dbmodels.CheckinLog{lg(1, 1, 10, 0), lg(2, 2, 5, 1), lg(3, 3, 30, 2)}
	tg := func(v float64) dbmodels.ItemRuleVersion { return dbmodels.ItemRuleVersion{TargetValue: f(v)} }
	none := dbmodels.ItemRuleVersion{}

	tests := []struct {
		name     string
		tracker  dbmodels.TrackerType
		v        dbmodels.ItemRuleVersion
		opts     []dbmodels.ItemOption
		logs     []dbmodels.CheckinLog
		agg      dbmodels.Aggregate
		done     bool
		met, tot int
		values   []float64
	}{
		{"boolean none", dbmodels.TrackerBoolean, none, nil, nil, "", false, 0, 1, nil},
		{"boolean one", dbmodels.TrackerBoolean, none, nil, cups(1), "", true, 1, 1, nil},
		{"maintenance one", dbmodels.TrackerMaintenance, none, nil, cups(1), "", true, 1, 1, nil},
		{"counter 7 cups of 250 vs 2000", dbmodels.TrackerCounter, tg(2000), nil, cups(7), "", false, 0, 1, []float64{1750}},
		{"counter 8 cups", dbmodels.TrackerCounter, tg(2000), nil, cups(8), "", true, 1, 1, []float64{2000}},
		{"counter undo back to 7", dbmodels.TrackerCounter, tg(2000), nil, append(cups(8), lg(9, 0, -250, 9)), "", false, 0, 1, []float64{1750}},
		{"counter no target any log", dbmodels.TrackerCounter, none, nil, cups(1), "", true, 1, 1, []float64{250}},
		{"counter no logs", dbmodels.TrackerCounter, none, nil, nil, "", false, 0, 1, []float64{0}},
		{"metric latest by time then id", dbmodels.TrackerMetric, tg(50), nil,
			[]dbmodels.CheckinLog{lg(5, 0, 40, 5), lg(2, 0, 60, 1), lg(3, 0, 70, 5)}, dbmodels.AggregateLatest, false, 0, 1, []float64{40}},
		{"metric latest below target", dbmodels.TrackerMetric, tg(50), nil,
			[]dbmodels.CheckinLog{lg(1, 0, 60, 0), lg(2, 0, 40, 1)}, dbmodels.AggregateLatest, false, 0, 1, []float64{40}},
		{"metric avg (60+40)/2=50", dbmodels.TrackerMetric, tg(50), nil,
			[]dbmodels.CheckinLog{lg(1, 0, 60, 0), lg(2, 0, 40, 1)}, dbmodels.AggregateAvg, true, 1, 1, []float64{50}},
		{"metric sum 60+40=100 vs 120", dbmodels.TrackerMetric, tg(120), nil,
			[]dbmodels.CheckinLog{lg(1, 0, 60, 0), lg(2, 0, 40, 1)}, dbmodels.AggregateSum, false, 0, 1, []float64{100}},
		{"metric no target any log", dbmodels.TrackerMetric, none, nil,
			[]dbmodels.CheckinLog{lg(1, 0, 1, 0)}, dbmodels.AggregateLatest, true, 1, 1, []float64{1}},
		{"options any", dbmodels.TrackerCounter, multiV(dbmodels.CompletionAny, 0), multi, multiLogs, "", true, 2, 3, []float64{10, 5, 30}},
		{"options all fails", dbmodels.TrackerCounter, multiV(dbmodels.CompletionAll, 0), multi, multiLogs, "", false, 2, 3, nil},
		{"options all ok", dbmodels.TrackerCounter, multiV(dbmodels.CompletionAll, 0), multi, append(multiLogs, lg(4, 2, 15, 3)), "", true, 3, 3, []float64{10, 20, 30}},
		{"at_least_2 ok", dbmodels.TrackerCounter, multiV(dbmodels.CompletionAtLeast, 2), multi, multiLogs, "", true, 2, 3, nil},
		{"at_least_3 fails", dbmodels.TrackerCounter, multiV(dbmodels.CompletionAtLeast, 3), multi, multiLogs, "", false, 2, 3, nil},
		{"archived option excluded from all", dbmodels.TrackerCounter, multiV(dbmodels.CompletionAll, 0),
			[]dbmodels.ItemOption{multi[0], {ID: 2, Archived: true}, multi[2]}, multiLogs, "", true, 2, 2, []float64{10, 30}},
		{"ad-hoc log (no option) ignored when options exist", dbmodels.TrackerCounter, multiV(dbmodels.CompletionAny, 0), multi,
			[]dbmodels.CheckinLog{lg(1, 0, 999, 0)}, "", false, 0, 3, nil},
		{"option without target met by any log", dbmodels.TrackerMetric, dbmodels.ItemRuleVersion{CompletionRule: dbmodels.CompletionAll, OptionTargets: `{"1":5}`},
			[]dbmodels.ItemOption{{ID: 1, Aggregate: dbmodels.AggregateSum}, {ID: 2, Aggregate: dbmodels.AggregateLatest}},
			[]dbmodels.CheckinLog{lg(1, 1, 5, 0), lg(2, 2, 3, 1)}, "", true, 2, 2, []float64{5, 3}},
		{"metric option avg", dbmodels.TrackerMetric, dbmodels.ItemRuleVersion{OptionTargets: `{"1":5}`},
			[]dbmodels.ItemOption{{ID: 1, Aggregate: dbmodels.AggregateAvg}},
			[]dbmodels.CheckinLog{lg(1, 1, 4, 0), lg(2, 1, 8, 1)}, "", true, 1, 1, []float64{6}},
		{"no active options", dbmodels.TrackerCounter, none, []dbmodels.ItemOption{{ID: 1, Archived: true}}, cups(1), "", false, 0, 0, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := DayCompletion(tc.tracker, tc.v, tc.opts, tc.logs, tc.agg)
			require.Equal(t, tc.done, c.Done)
			require.Equal(t, tc.met, c.OptionsMet)
			require.Equal(t, tc.tot, c.OptionsTotal)
			if tc.values != nil {
				var got []float64
				for _, p := range c.Progress {
					got = append(got, p.Value)
				}
				require.Equal(t, tc.values, got)
			}
		})
	}
}

func TestCompletionSnapshotTargetsAndShade(t *testing.T) {
	opts := []dbmodels.ItemOption{{ID: 1, Aggregate: dbmodels.AggregateSum}}
	logs := []dbmodels.CheckinLog{lg(1, 1, 8, 0)}
	old := dbmodels.ItemRuleVersion{OptionTargets: `{"1":10}`}
	newer := dbmodels.ItemRuleVersion{OptionTargets: `{"1":5}`}
	require.False(t, DayCompletion(dbmodels.TrackerCounter, old, opts, logs, "").Done)
	c := DayCompletion(dbmodels.TrackerCounter, newer, opts, logs, "")
	require.True(t, c.Done)
	require.InDelta(t, 1.6, c.Progress[0].Ratio, 1e-9)
	require.Equal(t, 1.0, c.Shade())
	require.InDelta(t, 2.0/3.0, Completion{OptionsMet: 2, OptionsTotal: 3}.Shade(), 1e-9)
	require.Zero(t, Completion{}.Shade())
}

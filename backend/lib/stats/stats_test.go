package stats

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

func tlog(id uint, date string, opt *uint, val float64, hour int) dbmodels.CheckinLog {
	return dbmodels.CheckinLog{ID: id, ItemID: 1, CheckinDate: date, OptionID: opt, Value: val,
		CheckinTime: time.Date(2026, 11, 1, hour, 0, 0, 0, time.UTC)}
}

func TestComputeStatsCountsAndLast(t *testing.T) {
	in := Input{
		Item: habit(), Versions: []dbmodels.ItemRuleVersion{{ID: 1, ItemID: 1, EffectiveFrom: "2025-01-01", ScheduleType: dbmodels.ScheduleNone, StaleAfterDays: 1}},
		Settings: Settings{Timezone: "Asia/Shanghai", WeekStart: 1}, Today: "2026-11-07",
		Logs: []dbmodels.CheckinLog{
			tlog(1, "2025-12-31", nil, 0, 1),
			tlog(2, "2026-01-01", nil, 0, 1),
			tlog(3, "2026-10-08", nil, 0, 1), // 30 days back is 10-09: outside
			tlog(4, "2026-10-09", nil, 0, 1), // inside (inclusive 30-day window)
			tlog(5, "2026-11-05", nil, 0, 1),
			tlog(6, "2026-11-05", nil, 0, 20), // 2026-11-06 04:00 +08, same day
		},
	}
	s, err := ComputeStats(in)
	require.NoError(t, err)
	require.Equal(t, Counts{Last30Days: 3, ThisYear: 5, Total: 6}, s.Counts)
	require.Equal(t, Counts{Last30Days: 2, ThisYear: 4, Total: 5}, s.ActiveDays)
	require.Equal(t, "2026-11-05", *s.LastDate)
	require.Equal(t, "2026-11-02T04:00:00+08:00", *s.LastAt, "latest log is id 6 at 20:00 UTC")
	require.Equal(t, 2, *s.DaysSinceLast)
	require.Equal(t, "Last: 2 days ago (2026-11-05)", s.Summary)
	require.NotNil(t, s.Stale)
	require.True(t, s.Stale.Stale)
	require.Equal(t, 2, s.Stale.Days)
	require.False(t, s.Streak.Applicable)
	require.Len(t, s.Heatmap, HeatmapDays)
	require.Equal(t, "2026-11-07", s.Heatmap[HeatmapDays-1].Date)
	require.Equal(t, 1.0, s.Heatmap[HeatmapDays-3].Level)
	require.Equal(t, 2.0, s.Heatmap[HeatmapDays-3].Value)
	require.Equal(t, 0.0, s.Heatmap[HeatmapDays-1].Level)

	in.Logs = nil
	s, err = ComputeStats(in)
	require.NoError(t, err)
	require.Nil(t, s.LastAt)
	require.Nil(t, s.DaysSinceLast)
	require.Equal(t, "No records yet", s.Summary)
	require.Equal(t, 0, s.Stale.Days)
}

func TestComputeStatsSummaryWording(t *testing.T) {
	in := Input{Item: habit(), Versions: []dbmodels.ItemRuleVersion{{ID: 1, ItemID: 1, EffectiveFrom: "2025-01-01", ScheduleType: dbmodels.ScheduleNone}},
		Today: "2026-11-07", Logs: []dbmodels.CheckinLog{tlog(1, "2026-11-07", nil, 0, 1)}}
	s, _ := ComputeStats(in)
	require.Equal(t, "Last: today (2026-11-07)", s.Summary)
	in.Logs = []dbmodels.CheckinLog{tlog(1, "2026-11-06", nil, 0, 1)}
	s, _ = ComputeStats(in)
	require.Equal(t, "Last: yesterday (2026-11-06)", s.Summary)
}

func TestComputeStatsOptionsAndHeatmap(t *testing.T) {
	one, two := uint(1), uint(2)
	item := habit()
	item.TrackerType = dbmodels.TrackerCounter
	v := rrv(1, "2026-11-01", daily)
	v.CompletionRule = dbmodels.CompletionAll
	v.OptionTargets = `{"1":10,"2":4}`
	opts := []dbmodels.ItemOption{{ID: 1, ItemID: 1, Name: "run", Unit: "km"}, {ID: 2, ItemID: 1, Name: "lift", Unit: "set"},
		{ID: 3, ItemID: 1, Name: "old", Archived: true}}
	in := Input{Item: item, Versions: []dbmodels.ItemRuleVersion{v}, Options: opts, Today: "2026-11-03",
		Logs: []dbmodels.CheckinLog{
			tlog(1, "2026-11-01", &one, 6, 8), tlog(2, "2026-11-01", &one, 4, 9), tlog(3, "2026-11-01", &two, 4, 9), // done
			tlog(4, "2026-11-02", &one, 10, 8), // 1 of 2 met
		}}
	s, err := ComputeStats(in)
	require.NoError(t, err)
	require.Len(t, s.Options, 2)
	run := s.Options[0]
	require.Equal(t, "run", run.Name)
	require.Equal(t, "km", run.Unit)
	require.Equal(t, 3, run.Count)
	require.Equal(t, 20.0, run.Sum)
	require.InDelta(t, 20.0/3, run.Avg, 1e-9)
	require.Equal(t, 10.0, run.LastValue)
	require.Equal(t, "2026-11-02", run.LastDate)
	require.Equal(t, 1, s.Options[1].Count)

	h := s.Heatmap
	require.Equal(t, 1.0, h[HeatmapDays-3].Level)
	require.Equal(t, 14.0, h[HeatmapDays-3].Value)
	require.Equal(t, 0.5, h[HeatmapDays-2].Level)
	require.Equal(t, 0.0, h[HeatmapDays-1].Level)

	// completion rate: 11-01 done, 11-02 missed, today pending excluded
	require.NotNil(t, s.CompletionRate90)
	require.Equal(t, 0.5, *s.CompletionRate90)
	require.True(t, s.Streak.Applicable)
	require.Equal(t, 0, s.Streak.Current)
	require.Equal(t, 1, s.Streak.Longest)
}

func TestCompletionRateFrozenExcluded(t *testing.T) {
	in := Input{Item: habit(), Versions: []dbmodels.ItemRuleVersion{rrv(1, "2026-11-01", daily)}, Today: "2026-11-05",
		Logs: blogs("2026-11-01", "2026-11-02"), Freezes: []dbmodels.FreezePeriod{freeze("2026-11-03", "2026-11-03")}}
	s, err := ComputeStats(in)
	require.NoError(t, err)
	// due 1..4 (+ today pending): done 1,2; 3 frozen; 4 missed
	require.InDelta(t, 2.0/3, *s.CompletionRate90, 1e-9)

	in.Versions = []dbmodels.ItemRuleVersion{rrv(1, "2026-12-01", daily)}
	s, _ = ComputeStats(in)
	require.Nil(t, s.CompletionRate90, "no due days: null")
}

func TestComputeStatsQuotaAndMaintenance(t *testing.T) {
	it := habit()
	it.Polarity = dbmodels.PolarityNegative
	it.Kind = dbmodels.KindEvent
	q := dbmodels.ItemRuleVersion{ID: 1, ItemID: 1, EffectiveFrom: "2026-01-01", ScheduleType: dbmodels.ScheduleQuota,
		QuotaComparator: dbmodels.QuotaAtMost, QuotaCount: 4, QuotaPeriod: dbmodels.QuotaYear, QuotaWindow: dbmodels.QuotaWindowCalendar}
	s, err := ComputeStats(Input{Item: it, Versions: []dbmodels.ItemRuleVersion{q}, Today: "2026-11-07", Settings: Settings{WeekStart: 1},
		Logs: blogs("2026-02-01", "2026-03-01", "2026-04-01")})
	require.NoError(t, err)
	require.NotNil(t, s.Quota)
	require.Equal(t, 3.0, s.Quota.Used)
	require.Equal(t, 4.0, s.Quota.Limit)
	require.Equal(t, 1.0, s.Quota.Remaining)
	require.Equal(t, "yellow", s.Quota.Traffic)
	require.Equal(t, "2026-01-01", s.Quota.WindowStart)
	require.True(t, s.Streak.Applicable)

	m := habit()
	m.TrackerType = dbmodels.TrackerMaintenance
	m.WarningDays = 5
	rv := dbmodels.ItemRuleVersion{ID: 1, ItemID: 1, EffectiveFrom: "2026-01-01", ScheduleType: dbmodels.ScheduleRolling,
		RollingInterval: 30, RollingUnit: dbmodels.RollingDay}
	s, err = ComputeStats(Input{Item: m, Versions: []dbmodels.ItemRuleVersion{rv}, Today: "2026-11-07", Logs: blogs("2026-10-12")})
	require.NoError(t, err)
	require.NotNil(t, s.Maintenance)
	require.Equal(t, "2026-11-11", s.Maintenance.NextDue)
	require.Equal(t, "warning", s.Maintenance.State)
	require.Equal(t, 4, s.Maintenance.DaysUntilDue)
	require.False(t, s.Streak.Applicable)
}

func TestCacheLRUAndCounters(t *testing.T) {
	c := NewCache(2)
	k := func(i int) Key { return Key{UserID: "u", ItemID: uint(i)} }
	_, ok := c.Get(k(1))
	require.False(t, ok)
	c.Put(k(1), Stats{Summary: "1"})
	c.Put(k(2), Stats{Summary: "2"})
	v, ok := c.Get(k(1))
	require.True(t, ok)
	require.Equal(t, "1", v.Summary)
	c.Put(k(3), Stats{Summary: "3"}) // evicts 2 (1 was just used)
	_, ok = c.Get(k(2))
	require.False(t, ok)
	_, ok = c.Get(k(1))
	require.True(t, ok)
	require.Equal(t, 2, c.Len())
	require.EqualValues(t, 2, c.Hits())
	require.EqualValues(t, 2, c.Misses())
}

func TestCacheKeyFieldsAllMatter(t *testing.T) {
	c := NewCache(100)
	base := Key{UserID: "u", ItemID: 1, LogSig: "a", RuleSig: "r", Today: "2026-11-07", StateVersion: 1, SettingsSig: "s"}
	c.Put(base, Stats{Summary: "x"})
	mut := []func(Key) Key{
		func(k Key) Key { k.UserID = "v"; return k },
		func(k Key) Key { k.ItemID = 2; return k },
		func(k Key) Key { k.LogSig = "b"; return k },
		func(k Key) Key { k.RuleSig = "q"; return k },
		func(k Key) Key { k.Today = "2026-11-08"; return k },
		func(k Key) Key { k.StateVersion = 2; return k },
		func(k Key) Key { k.SettingsSig = "t"; return k },
	}
	for i, f := range mut {
		_, ok := c.Get(f(base))
		require.False(t, ok, "mutation %d must miss", i)
	}
	_, ok := c.Get(base)
	require.True(t, ok)
}

func TestCacheConcurrent(t *testing.T) {
	c := NewCache(10)
	v := NewVersions()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				k := Key{UserID: "u", ItemID: uint(i % 20), LogSig: fmt.Sprint(v.Get(1))}
				if _, ok := c.Get(k); !ok {
					c.Put(k, Stats{})
				}
				v.Bump(uint(i % 3))
			}
		}()
	}
	wg.Wait()
	require.LessOrEqual(t, c.Len(), 10)
}

func TestVersionsNilSafe(t *testing.T) {
	var v *Versions
	v.Bump(1)
	require.EqualValues(t, 0, v.Get(1))
	w := NewVersions()
	w.Bump(1)
	w.Bump(1)
	require.EqualValues(t, 2, w.Get(1))
	require.EqualValues(t, 0, w.Get(2))
}

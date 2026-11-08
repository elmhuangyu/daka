package api

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

func (e *env) ruleVersions(id uint, vs ...dbmodels.ItemRuleVersion) {
	e.t.Helper()
	require.NoError(e.t, e.db.Write().Where("item_id = ?", id).Delete(&dbmodels.ItemRuleVersion{}).Error)
	for _, v := range vs {
		v.ItemID = id
		v.HolidayMode = dbmodels.HolidayNone
		require.NoError(e.t, e.db.Write().Create(&v).Error)
	}
}

func fixedV(from string) dbmodels.ItemRuleVersion {
	return dbmodels.ItemRuleVersion{EffectiveFrom: from, ScheduleType: dbmodels.ScheduleFixedRRule, DtStart: from, RRuleString: "FREQ=DAILY"}
}

func TestCalendarViewMixedScheduleTransitions(t *testing.T) {
	quota := dbmodels.ItemRuleVersion{EffectiveFrom: "2026-11-05", ScheduleType: dbmodels.ScheduleQuota,
		QuotaComparator: dbmodels.QuotaAtMost, QuotaCount: 3, QuotaPeriod: dbmodels.QuotaWeek, QuotaWindow: dbmodels.QuotaWindowCalendar}
	rolling := func(from string) dbmodels.ItemRuleVersion {
		return dbmodels.ItemRuleVersion{EffectiveFrom: from, ScheduleType: dbmodels.ScheduleRolling, DtStart: from,
			RollingInterval: 30, RollingUnit: dbmodels.RollingDay}
	}
	tests := []struct {
		name     string
		versions []dbmodels.ItemRuleVersion
		yes, no  []string
	}{
		{"fixed to quota", []dbmodels.ItemRuleVersion{fixedV("2026-11-01"), quota}, []string{"2026-11-02", "2026-11-04"}, []string{"2026-11-05", "2026-11-06"}},
		{"fixed to rolling", []dbmodels.ItemRuleVersion{fixedV("2026-11-01"), rolling("2026-11-05")}, []string{"2026-11-02", "2026-11-04"}, []string{"2026-11-05", "2026-11-06"}},
		{"rolling to fixed", []dbmodels.ItemRuleVersion{rolling("2026-11-01"), fixedV("2026-11-05")}, []string{"2026-11-05", "2026-11-08"}, []string{"2026-11-02", "2026-11-04"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			id := e.item("u1", nil)
			e.ruleVersions(id, tc.versions...)
			r := e.calView("", "?start=2026-11-01&end=2026-11-09")
			due := func(date string) bool {
				d := r.day(t, date)
				return d.has(id) && d.item(t, id).Due
			}
			for _, d := range tc.yes {
				require.True(t, due(d), d)
			}
			for _, d := range tc.no {
				require.False(t, due(d), d)
			}
		})
	}
}

func TestCalendarViewArchivedItemByID(t *testing.T) {
	e := newEnv(t)
	arch := e.dailyItem("u1", func(it *dbmodels.Item) { it.Archived = true })
	q := fmt.Sprintf("?start=2026-11-08&end=2026-11-08&item=%d", arch)
	require.True(t, e.calView("", q).day(t, "2026-11-08").has(arch))
	require.False(t, e.calView("", "?start=2026-11-08&end=2026-11-08").day(t, "2026-11-08").has(arch))
}

func TestCalendarViewItemOrderDeterministic(t *testing.T) {
	e := newEnv(t)
	var ids []uint
	for i := 0; i < 5; i++ {
		ids = append(ids, e.dailyItem("u1", nil))
	}
	for n := 0; n < 5; n++ {
		d := e.calView("", "?start=2026-11-08&end=2026-11-08").day(t, "2026-11-08")
		require.Len(t, d.Items, 5)
		for i, it := range d.Items {
			require.Equal(t, ids[i], it.ItemID)
		}
	}
}

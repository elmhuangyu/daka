package dbmodels

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/config"
	"github.com/elmhuangyu/daka/backend/lib/db"
)

func openTemp(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(config.DBConfig{Driver: config.DriverSQLite, DSN: config.Secret(filepath.Join(t.TempDir(), "t.db"))})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	require.NoError(t, AutoMigrate(d.Write()))
	return d
}

func columns(t *testing.T, g *gorm.DB, model any) map[string]bool {
	t.Helper()
	cols, err := g.Migrator().ColumnTypes(model)
	require.NoError(t, err)
	m := map[string]bool{}
	for _, c := range cols {
		m[c.Name()] = true
	}
	return m
}

func TestAutoMigrateIdempotent(t *testing.T) {
	d := openTemp(t)
	require.NoError(t, AutoMigrate(d.Write()))
	require.NoError(t, AutoMigrate(d.Write()))
}

func TestItemHasNoRuleColumns(t *testing.T) {
	d := openTemp(t)
	item := columns(t, d.Write(), &Item{})
	ver := columns(t, d.Write(), &ItemRuleVersion{})
	ruleCols := []string{"schedule_type", "dtstart", "until", "rrule_string", "holiday_mode",
		"rolling_interval", "rolling_unit", "quota_comparator", "quota_count", "quota_period",
		"quota_window", "quota_measure", "quota_option_id", "target_value", "completion_rule",
		"completion_n", "stale_after_days", "option_targets"}
	for _, c := range ruleCols {
		require.False(t, item[c], "Item must not have %s", c)
		require.True(t, ver[c], "ItemRuleVersion must have %s", c)
	}
	for name := range item {
		for _, p := range []string{"rolling_", "quota_", "completion_", "rrule", "holiday_mode", "schedule"} {
			require.False(t, strings.HasPrefix(name, p), "Item has rule-like column %s", name)
		}
	}
	for _, c := range []string{"is_sensitive", "kind", "polarity", "tracker_type", "remind_time", "aggregate", "warning_days"} {
		require.True(t, item[c], "Item missing %s", c)
	}
}

func TestCheckinLogIndexes(t *testing.T) {
	d := openTemp(t)
	idx, err := d.Write().Migrator().GetIndexes(&CheckinLog{})
	require.NoError(t, err)
	found := false
	for _, i := range idx {
		u, _ := i.Unique()
		if strings.Join(i.Columns(), ",") == "item_id,checkin_date" {
			found = true
			require.False(t, u, "(item_id, checkin_date) must not be unique")
		}
	}
	require.True(t, found, "missing (item_id, checkin_date) index")

	// Duplicate rows for the same item/date must be allowed.
	now := time.Now().UTC()
	for range 2 {
		require.NoError(t, d.Write().Create(&CheckinLog{UserID: "u", ItemID: 1, CheckinDate: "2026-01-01", CheckinTime: now}).Error)
	}
}

func TestRoundTrip(t *testing.T) {
	d := openTemp(t)
	w, r := d.Write(), d.Read()
	now := time.Now().UTC().Truncate(time.Second)

	u := User{ID: "u1", Email: "a@b.c", Name: "A", AvatarURL: "http://x", CreatedAt: now}
	require.NoError(t, w.Create(&u).Error)
	var gu User
	require.NoError(t, r.First(&gu, "id = ?", "u1").Error)
	require.Equal(t, "a@b.c", gu.Email)
	require.True(t, now.Equal(gu.CreatedAt))

	s := UserSettings{UserID: "u1", HolidayRegion: RegionCN, Timezone: "Asia/Shanghai", WeekStart: 1, StateVersion: 3, ExternalCalendarURLs: "[]", StreakHolidayFreeze: true}
	require.NoError(t, w.Create(&s).Error)
	var gs UserSettings
	require.NoError(t, r.First(&gs, "user_id = ?", "u1").Error)
	require.Equal(t, s, gs)

	c := Category{UserID: "u1", Name: "Health", Color: "red", Icon: "heart", IsSensitive: true, SortOrder: 2}
	require.NoError(t, w.Create(&c).Error)
	var gc Category
	require.NoError(t, r.First(&gc, c.ID).Error)
	require.Equal(t, c, gc)

	rt := "07:30"
	mn, mx := 0.0, 100.0
	it := Item{UserID: "u1", CategoryID: &c.ID, Name: "Run", Description: "d", TrackerType: TrackerMetric,
		Unit: "km", Aggregate: AggregateSum, Step: 0.5, MinVal: &mn, MaxVal: &mx, WarningDays: 3,
		Kind: KindOngoing, Polarity: PolarityNegative, Pinned: true, IsSensitive: true, RemindTime: &rt, SortOrder: 1, Archived: true}
	require.NoError(t, w.Create(&it).Error)
	var gi Item
	require.NoError(t, r.First(&gi, it.ID).Error)
	require.Equal(t, it, gi)

	until, tv, opt := "2026-12-31", 30.0, uint(7)
	v := ItemRuleVersion{ItemID: it.ID, EffectiveFrom: "2026-01-01", ScheduleType: ScheduleFixedRRule, DtStart: "2026-01-01",
		Until: &until, RRuleString: "FREQ=DAILY;INTERVAL=3", HolidayMode: HolidayWorkdaysOnly,
		RollingInterval: 2, RollingUnit: RollingMonth, QuotaComparator: QuotaAtLeast, QuotaCount: 2.5,
		QuotaPeriod: QuotaWeek, QuotaWindow: QuotaWindowCalendar, QuotaMeasure: QuotaMeasureSum, QuotaOptionID: &opt,
		TargetValue: &tv, CompletionRule: CompletionAtLeast, CompletionN: 2, StaleAfterDays: 14, OptionTargets: `{"7":30}`}
	require.NoError(t, w.Create(&v).Error)
	var gv ItemRuleVersion
	require.NoError(t, r.First(&gv, v.ID).Error)
	require.Equal(t, v, gv)

	ref := "note-1"
	l := CheckinLog{UserID: "u1", ItemID: it.ID, CheckinDate: "2026-02-03", CheckinTime: now, OptionID: &opt,
		Value: -1.5, Note: "n", Source: SourceAPI, ExternalRef: &ref, IsSupplement: true}
	require.NoError(t, w.Create(&l).Error)
	var gl CheckinLog
	require.NoError(t, r.First(&gl, l.ID).Error)
	require.True(t, l.CheckinTime.Equal(gl.CheckinTime))
	gl.CheckinTime = l.CheckinTime
	require.Equal(t, l, gl)
}

func TestCurrentRuleVersion(t *testing.T) {
	d := openTemp(t)
	for _, from := range []string{"2026-03-01", "2026-06-15"} {
		require.NoError(t, d.Write().Create(&ItemRuleVersion{ItemID: 1, EffectiveFrom: from, ScheduleType: ScheduleNone, RRuleString: from}).Error)
	}
	require.NoError(t, d.Write().Create(&ItemRuleVersion{ItemID: 2, EffectiveFrom: "2026-01-01", ScheduleType: ScheduleNone, RRuleString: "other"}).Error)

	tests := []struct {
		name    string
		item    uint
		date    string
		want    string
		wantErr error
	}{
		{"before first", 1, "2026-02-28", "", ErrNoRuleVersion},
		{"equal first", 1, "2026-03-01", "2026-03-01", nil},
		{"between", 1, "2026-06-14", "2026-03-01", nil},
		{"equal second", 1, "2026-06-15", "2026-06-15", nil},
		{"after last", 1, "2030-01-01", "2026-06-15", nil},
		{"other item", 2, "2026-07-01", "other", nil},
		{"unknown item", 3, "2026-07-01", "", ErrNoRuleVersion},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CurrentRuleVersion(d.Read(), tc.item, tc.date)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got.RRuleString)
		})
	}
}

func TestRuleVersionSameDayLatestIDWins(t *testing.T) {
	d := openTemp(t)
	for _, tag := range []string{"morning", "evening"} {
		v := ItemRuleVersion{ItemID: 1, EffectiveFrom: "2026-01-01", ScheduleType: ScheduleNone, RRuleString: tag}
		require.NoError(t, d.Write().Create(&v).Error)
	}
	got, err := CurrentRuleVersion(d.Read(), 1, "2026-01-01")
	require.NoError(t, err)
	require.Equal(t, "evening", got.RRuleString)
}

func TestCurrentRuleVersionRejectsMalformedDate(t *testing.T) {
	d := openTemp(t)
	for _, bad := range []string{"2026-1-1", "", "2026/01/01", "not-a-date"} {
		_, err := CurrentRuleVersion(d.Read(), 1, bad)
		require.Error(t, err, bad)
		require.NotErrorIs(t, err, ErrNoRuleVersion, bad)
	}
}

// Every table must have an account-deletion path: a UserID column
// (UserOwnedTables), an item_id column (ItemOwnedTables) or be the User row.
// Adding a model to AllModels without covering it fails here.
func TestEveryModelHasAccountDeletionPath(t *testing.T) {
	covered := map[reflect.Type]bool{reflect.TypeOf(&User{}): true}
	for _, m := range UserOwnedTables() {
		covered[reflect.TypeOf(m)] = true
	}
	for _, m := range ItemOwnedTables() {
		typ := reflect.TypeOf(m)
		_, has := typ.Elem().FieldByName("ItemID")
		require.True(t, has, "%s in ItemOwnedTables needs an ItemID field", typ)
		covered[typ] = true
	}
	for _, m := range AllModels() {
		require.True(t, covered[reflect.TypeOf(m)], "%T has no account deletion path; add it to UserOwnedTables or ItemOwnedTables", m)
	}
}

func TestUserOwnedTablesComplete(t *testing.T) {
	owned := map[reflect.Type]bool{}
	for _, m := range UserOwnedTables() {
		owned[reflect.TypeOf(m)] = true
	}
	all := map[reflect.Type]bool{}
	for _, m := range AllModels() {
		typ := reflect.TypeOf(m)
		all[typ] = true
		_, has := typ.Elem().FieldByName("UserID")
		require.Equal(t, has, owned[typ], "%s: UserID field and UserOwnedTables must agree", typ.Elem().Name())
	}
	for typ := range owned {
		require.True(t, all[typ], "%s is in UserOwnedTables but not AllModels", typ)
	}
}

func TestKindRenameMigration(t *testing.T) {
	d := openTemp(t)
	g := d.Write()
	require.NoError(t, g.Create(&Item{UserID: "u", Name: "old", Kind: "habit"}).Error)
	require.NoError(t, g.Create(&Item{UserID: "u", Name: "todo", Kind: KindEvent}).Error)
	// Simulate a database from before the rename: the migration has not run yet.
	require.NoError(t, g.Exec("DELETE FROM migrations").Error)

	require.NoError(t, AutoMigrate(g))
	var kinds []string
	require.NoError(t, g.Model(&Item{}).Order("id").Pluck("kind", &kinds).Error)
	require.Equal(t, []string{"ongoing", "event"}, kinds)

	// Applied once: a later "habit" row is left alone.
	require.NoError(t, g.Create(&Item{UserID: "u", Name: "x", Kind: "habit"}).Error)
	require.NoError(t, AutoMigrate(g))
	var n int64
	require.NoError(t, g.Model(&Item{}).Where("kind = ?", "habit").Count(&n).Error)
	require.EqualValues(t, 1, n)
}

func TestItemCategoryFromProjectMigration(t *testing.T) {
	d := openTemp(t)
	g := d.Write()
	cat := uint(5)
	withCat := Project{UserID: "u", Name: "A", CategoryID: &cat}
	noCat := Project{UserID: "u", Name: "B"}
	require.NoError(t, g.Create(&withCat).Error)
	require.NoError(t, g.Create(&noCat).Error)
	own := uint(9)
	rows := []Item{
		{UserID: "u", Name: "in A", Kind: KindEvent, ProjectID: &withCat.ID},
		{UserID: "u", Name: "in A own", Kind: KindEvent, ProjectID: &withCat.ID, CategoryID: &own},
		{UserID: "u", Name: "in B", Kind: KindEvent, ProjectID: &noCat.ID},
		{UserID: "u", Name: "loose", Kind: KindEvent},
	}
	require.NoError(t, g.Create(&rows).Error)
	require.NoError(t, g.Exec("DELETE FROM migrations").Error)

	require.NoError(t, AutoMigrate(g))
	var got []Item
	require.NoError(t, g.Order("id").Find(&got).Error)
	ids := func(c *uint) any {
		if c == nil {
			return nil
		}
		return *c
	}
	require.Equal(t, uint(5), ids(got[0].CategoryID))
	require.Equal(t, uint(9), ids(got[1].CategoryID))
	require.Nil(t, ids(got[2].CategoryID))
	require.Nil(t, ids(got[3].CategoryID))
}

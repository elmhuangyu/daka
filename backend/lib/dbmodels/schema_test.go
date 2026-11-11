package dbmodels

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Type classes used to compare against the DESIGN section 10 ER diagram.
const (
	tStr   = "string"
	tInt   = "int"
	tFloat = "float"
	tBool  = "bool"
	tTime  = "time"
)

func typeClass(dbType string) string {
	s := strings.ToLower(dbType)
	switch {
	case strings.HasPrefix(s, "varchar"), strings.HasPrefix(s, "text"), strings.HasPrefix(s, "char"):
		return tStr
	case strings.HasPrefix(s, "bool"), strings.HasPrefix(s, "numeric"): // SQLite stores bool as numeric
		return tBool
	case strings.Contains(s, "int"):
		return tInt
	case strings.HasPrefix(s, "real"), strings.HasPrefix(s, "double"), strings.HasPrefix(s, "float"), strings.HasPrefix(s, "decimal"):
		return tFloat
	case strings.HasPrefix(s, "datetime"), strings.HasPrefix(s, "timestamp"):
		return tTime
	}
	return s
}

// TestSchemaMatchesDesign lists every table of DESIGN section 10 (Telegram
// tables excluded, P3-6) with its columns and type classes.
func TestSchemaMatchesDesign(t *testing.T) {
	d := openTemp(t)
	tests := []struct {
		model any
		table string
		cols  map[string]string
	}{
		{&User{}, "users", map[string]string{"id": tStr, "email": tStr, "name": tStr, "avatar_url": tStr, "created_at": tTime}},
		{&UserSettings{}, "user_settings", map[string]string{"user_id": tStr, "holiday_region": tStr, "timezone": tStr,
			"week_start": tInt, "state_version": tInt, "external_calendar_urls": tStr, "streak_holiday_freeze": tBool}},
		{&Category{}, "categories", map[string]string{"id": tInt, "user_id": tStr, "name": tStr, "color": tStr, "icon": tStr,
			"is_sensitive": tBool, "sort_order": tInt}},
		{&Project{}, "projects", map[string]string{"id": tInt, "user_id": tStr, "category_id": tInt, "name": tStr,
			"description": tStr, "color": tStr, "status": tStr, "target_date": tStr, "is_sensitive": tBool,
			"sort_order": tInt, "created_at": tTime, "updated_at": tTime}},
		{&Item{}, "items", map[string]string{"id": tInt, "user_id": tStr, "category_id": tInt, "project_id": tInt, "parent_id": tInt, "name": tStr, "description": tStr,
			"color": tStr, "tracker_type": tStr, "unit": tStr, "aggregate": tStr, "step": tFloat, "min_val": tFloat, "max_val": tFloat,
			"warning_days": tInt, "kind": tStr, "polarity": tStr, "pinned": tBool, "is_sensitive": tBool, "remind_time": tStr,
			"sort_order": tInt, "archived": tBool}},
		{&CheckinLog{}, "checkin_logs", map[string]string{"id": tInt, "user_id": tStr, "item_id": tInt, "checkin_date": tStr,
			"checkin_time": tTime, "option_id": tInt, "value": tFloat, "note": tStr, "source": tStr, "external_ref": tStr,
			"is_supplement": tBool}},
		{&ItemOption{}, "item_options", map[string]string{"id": tInt, "item_id": tInt, "name": tStr, "unit": tStr, "step": tFloat,
			"min_val": tFloat, "max_val": tFloat, "aggregate": tStr, "sort_order": tInt, "archived": tBool}},
		{&ItemRuleVersion{}, "item_rule_versions", map[string]string{"id": tInt, "item_id": tInt, "effective_from": tStr,
			"schedule_type": tStr, "dtstart": tStr, "until": tStr, "rrule_string": tStr, "holiday_mode": tStr,
			"rolling_interval": tInt, "rolling_unit": tStr, "quota_comparator": tStr, "quota_count": tFloat,
			"quota_period": tStr, "quota_window": tStr, "quota_measure": tStr, "quota_option_id": tInt, "target_value": tFloat,
			"completion_rule": tStr, "completion_n": tInt, "stale_after_days": tInt, "option_targets": tStr}},
		{&FreezePeriod{}, "freeze_periods", map[string]string{"id": tInt, "user_id": tStr, "start_date": tStr, "end_date": tStr,
			"reason": tStr}},
		{&ApiToken{}, "api_tokens", map[string]string{"id": tInt, "user_id": tStr, "token_hash": tStr, "name": tStr,
			"scopes": tStr, "privacy": tStr, "expires_at": tTime, "include_negative": tBool, "category_id": tInt, "revoked_at": tTime,
			"last_used_at": tTime}},
		{&IdempotencyKey{}, "idempotency_keys", map[string]string{"user_id": tStr, "key": tStr, "status_code": tInt,
			"response_body": tStr, "created_at": tTime}},
		{&TelegramLink{}, "telegram_links", map[string]string{"user_id": tStr, "token_enc": tStr, "bot_username": tStr, "chat_id": tInt, "enabled": tBool,
			"remind_sensitive": tBool, "digest_time": tStr, "lang": tStr, "linked_at": tTime}},
		{&TelegramLinkCode{}, "telegram_link_codes", map[string]string{"code_hash": tStr, "user_id": tStr, "token_enc": tStr, "bot_username": tStr, "expires_at": tTime}},
		{&ReminderLog{}, "reminder_logs", map[string]string{"user_id": tStr, "item_id": tInt, "remind_date": tStr, "sent_at": tTime}},
	}
	for _, tc := range tests {
		t.Run(tc.table, func(t *testing.T) {
			require.True(t, d.Write().Migrator().HasTable(tc.table), "missing table %s", tc.table)
			cts, err := d.Write().Migrator().ColumnTypes(tc.model)
			require.NoError(t, err)
			got := map[string]string{}
			for _, c := range cts {
				got[c.Name()] = typeClass(c.DatabaseTypeName())
			}
			require.Equal(t, tc.cols, got)
		})
	}
}

func TestItemOptionHasNoTargetValue(t *testing.T) {
	d := openTemp(t)
	require.False(t, columns(t, d.Write(), &ItemOption{})["target_value"])
}

func indexMap(t *testing.T, g *gorm.DB, model any) map[string]bool {
	t.Helper()
	idx, err := g.Migrator().GetIndexes(model)
	require.NoError(t, err)
	m := map[string]bool{}
	for _, i := range idx {
		u, _ := i.Unique()
		m[strings.Join(i.Columns(), ",")] = u
	}
	return m
}

func TestNewIndexes(t *testing.T) {
	d := openTemp(t)
	g := d.Write()

	u, ok := indexMap(t, g, &ApiToken{})["token_hash"]
	require.True(t, ok)
	require.True(t, u, "token_hash must be unique")
	_, ok = indexMap(t, g, &ApiToken{})["user_id"]
	require.True(t, ok, "api_tokens(user_id)")

	u, ok = indexMap(t, g, &FreezePeriod{})["user_id,start_date,end_date"]
	require.True(t, ok, "freeze_periods(user_id,start_date,end_date)")
	require.False(t, u)

	_, ok = indexMap(t, g, &CheckinLog{})["option_id"]
	require.True(t, ok, "checkin_logs(option_id)")

	_, ok = indexMap(t, g, &ItemOption{})["item_id"]
	require.True(t, ok, "item_options(item_id)")
}

func TestNoForeignKeys(t *testing.T) {
	d := openTemp(t)
	var n int64
	require.NoError(t, d.Write().Raw("SELECT count(*) FROM pragma_foreign_key_list('item_options')").Scan(&n).Error)
	require.Zero(t, n)
}

func TestNewModelsRoundTrip(t *testing.T) {
	d := openTemp(t)
	w, r := d.Write(), d.Read()
	now := time.Now().UTC().Truncate(time.Second)

	mn, mx := 1.0, 99.0
	o := ItemOption{ItemID: 5, Name: "Running", Unit: "km", Step: 0.5, MinVal: &mn, MaxVal: &mx,
		Aggregate: AggregateLatest, SortOrder: 2, Archived: true}
	require.NoError(t, w.Create(&o).Error)
	var go_ ItemOption
	require.NoError(t, r.First(&go_, o.ID).Error)
	require.Equal(t, o, go_)

	f := FreezePeriod{UserID: "u1", StartDate: "2026-03-01", EndDate: "2026-03-07", Reason: "sick"}
	require.NoError(t, w.Create(&f).Error)
	var gf FreezePeriod
	require.NoError(t, r.First(&gf, f.ID).Error)
	require.Equal(t, f, gf)

	rev, used := now.Add(time.Hour), now.Add(2*time.Hour)
	tok := ApiToken{UserID: "u1", TokenHash: "h1", Name: "laptop", Scopes: string(ScopeFeedRead) + "," + string(ScopeRead),
		Privacy: PrivacyAnonymizeSensitive, ExpiresAt: now.Add(24 * time.Hour), IncludeNegative: true,
		RevokedAt: &rev, LastUsedAt: &used}
	require.NoError(t, w.Create(&tok).Error)
	var gt ApiToken
	require.NoError(t, r.First(&gt, tok.ID).Error)
	require.True(t, tok.ExpiresAt.Equal(gt.ExpiresAt))
	require.True(t, rev.Equal(*gt.RevokedAt))
	require.True(t, used.Equal(*gt.LastUsedAt))
	gt.ExpiresAt, gt.RevokedAt, gt.LastUsedAt = tok.ExpiresAt, tok.RevokedAt, tok.LastUsedAt
	require.Equal(t, tok, gt)

	// Nullable timestamps stay NULL and defaults apply.
	tok2 := ApiToken{UserID: "u1", TokenHash: "h2", Name: "x", Scopes: "read", ExpiresAt: now.Add(time.Hour)}
	require.NoError(t, w.Create(&tok2).Error)
	var g2 ApiToken
	require.NoError(t, r.First(&g2, tok2.ID).Error)
	require.Nil(t, g2.RevokedAt)
	require.Nil(t, g2.LastUsedAt)
	require.Equal(t, PrivacyExcludeSensitive, g2.Privacy)
	require.False(t, g2.IncludeNegative)

	k := IdempotencyKey{UserID: "u1", Key: "k", StatusCode: 201, ResponseBody: "{}"}
	require.NoError(t, w.Create(&k).Error)

	td := "2026-12-31"
	p := Project{UserID: "u1", Name: "Project A", Description: "desc", Color: "blue", Status: ProjectStatusActive,
		TargetDate: &td, IsSensitive: true, SortOrder: 1, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, w.Create(&p).Error)
	var gp Project
	require.NoError(t, r.First(&gp, p.ID).Error)
	require.True(t, p.CreatedAt.Equal(gp.CreatedAt))
	require.True(t, p.UpdatedAt.Equal(gp.UpdatedAt))
	gp.CreatedAt, gp.UpdatedAt = p.CreatedAt, p.UpdatedAt
	require.Equal(t, p, gp)
}

func TestApiTokenExpiresAtRequired(t *testing.T) {
	d := openTemp(t)
	w := d.Write()

	// Zero value rejected by the model hook.
	err := w.Create(&ApiToken{UserID: "u", TokenHash: "a", Name: "n", Scopes: "read"}).Error
	require.ErrorIs(t, err, ErrTokenExpiryRequired)

	// Omitting the column entirely is rejected by the NOT NULL constraint.
	err = w.Omit("ExpiresAt").Session(&gorm.Session{SkipHooks: true}).
		Create(&ApiToken{UserID: "u", TokenHash: "b", Name: "n", Scopes: "read"}).Error
	require.Error(t, err)

	// Explicit NULL via raw SQL is rejected too.
	err = w.Exec("INSERT INTO api_tokens (user_id, token_hash, name, scopes, expires_at) VALUES ('u','c','n','read',NULL)").Error
	require.Error(t, err)
}

func TestApiTokenHashUnique(t *testing.T) {
	d := openTemp(t)
	exp := time.Now().Add(time.Hour)
	require.NoError(t, d.Write().Create(&ApiToken{UserID: "u", TokenHash: "same", Name: "a", Scopes: "read", ExpiresAt: exp}).Error)
	err := d.Write().Create(&ApiToken{UserID: "v", TokenHash: "same", Name: "b", Scopes: "read", ExpiresAt: exp}).Error
	require.Error(t, err)
}

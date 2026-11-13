package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

// dataEnv is the env used by the export, import and account tests.
func dataEnv(t *testing.T) *env { return newEnv(t) }

func (e *env) doRaw(method, path, user, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if user != "" {
		r.Header.Set("X-User", user)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func (e *env) export(user string) importDoc {
	e.t.Helper()
	w := e.doRaw("GET", "/api/export", user, "")
	require.Equal(e.t, 200, w.Code, w.Body.String())
	var d importDoc
	require.NoError(e.t, json.Unmarshal(w.Body.Bytes(), &d), w.Body.String())
	return d
}

// seedRich builds a user with a sensitive category, a counter with an archived
// option and several rule versions (same-day edits and a retroactive one), a
// quota rule, an event, check-ins and a freeze.
func (e *env) seedRich(user string) {
	e.t.Helper()
	w := e.do("POST", "/api/categories", user, "", m{"name": "Health", "color": "red", "icon": "heart", "is_sensitive": true, "sort_order": 2})
	require.Equal(e.t, 201, w.Code, w.Body.String())
	cat := decode[categoryResponse](e.t, w)

	body := m{
		"name": "Workout", "tracker_type": "counter", "category_id": cat.ID,
		"options": []m{{"name": "run", "unit": "km", "target_value": 2}, {"name": "pushups", "unit": "reps", "target_value": 30}},
		"rule":    m{"schedule_type": "fixed_rrule", "rrule_string": "FREQ=DAILY", "completion_rule": "all"},
	}
	it := e.mustCreate(user, body)
	td := "2026-12-31"
	proj := dbmodels.Project{
		UserID:      user,
		CategoryID:  &cat.ID,
		Name:        "Fitness Goal",
		Description: "Get fit",
		Color:       "blue",
		Status:      dbmodels.ProjectStatusActive,
		TargetDate:  &td,
		IsSensitive: false,
		SortOrder:   1,
		CreatedAt:   e.now.UTC(),
		UpdatedAt:   e.now.UTC(),
	}
	require.NoError(e.t, e.db.Write().Create(&proj).Error)
	require.NoError(e.t, e.db.Write().Model(&dbmodels.Item{}).Where("id = ?", it.ID).Update("project_id", proj.ID).Error)
	run, push := it.Options[0].ID, it.Options[1].ID
	patch := func(rule m) {
		w := e.do("PATCH", itemPath(it.ID), user, "", m{"rule": rule})
		require.Equal(e.t, 200, w.Code, w.Body.String())
	}
	patch(m{"option_targets": m{strconv.Itoa(int(run)): 5}})
	patch(m{"option_targets": m{strconv.Itoa(int(run)): 6}})
	patch(m{"option_targets": m{strconv.Itoa(int(run)): 3}, "effective_from": "2026-01-01"})
	require.Equal(e.t, 200, e.do("DELETE", itemPath(it.ID)+"/options/"+strconv.Itoa(int(push)), user, "", nil).Code)
	patch(m{"schedule_type": "quota", "quota_comparator": "at_least", "quota_count": 100, "quota_period": "week",
		"quota_measure": "sum", "quota_option_id": run})
	require.Equal(e.t, 200, e.do("PATCH", itemPath(it.ID), user, "", m{"remind_time": "07:30", "pinned": true, "description": "d"}).Code)

	ev := e.mustCreate(user, m{"name": "Dentist", "tracker_type": "boolean", "kind": "event", "rule": m{}})
	e.mustCreate(user, rruleItem("Read", "FREQ=WEEKLY;BYDAY=MO"))

	for i, l := range []dbmodels.CheckinLog{
		{ItemID: it.ID, OptionID: &run, CheckinDate: "2026-11-05", Value: 3.5, Note: "n1", Source: dbmodels.SourceWeb},
		{ItemID: it.ID, OptionID: &push, CheckinDate: "2026-11-05", Value: 20, Source: dbmodels.SourceAPI, IsSupplement: true},
		{ItemID: ev.ID, CheckinDate: "2026-11-06", Value: 1, Source: dbmodels.SourceTelegram, ExternalRef: ptr("ext-1")},
	} {
		l.UserID = user
		l.CheckinTime = time.Date(2026, 11, 6, 10, i, 0, 123000000, time.UTC)
		require.NoError(e.t, e.db.Write().Create(&l).Error)
	}
	require.Equal(e.t, 201, e.do("POST", "/api/freezes", user, "", m{"start_date": "2026-10-01", "end_date": "2026-10-05", "reason": "trip"}).Code)
}

func ptr[T any](v T) *T { return &v }

// normalize replaces all ids by document positions so two exports of equal
// data compare equal even though import assigned fresh ids.
func normalize(d importDoc) importDoc {
	d.ExportedAt = ""
	cat := map[uint]uint{}
	for i := range d.Categories {
		cat[d.Categories[i].ID] = uint(i + 1)
		d.Categories[i].ID = uint(i + 1)
	}
	proj := map[uint]uint{}
	for i := range d.Projects {
		p := &d.Projects[i]
		proj[p.ID] = uint(i + 1)
		p.ID = uint(i + 1)
		if p.CategoryID != nil {
			c := cat[*p.CategoryID]
			p.CategoryID = &c
		}
	}
	item := map[uint]uint{}
	opt := map[uint]map[uint]uint{}
	origIDs := make([]uint, len(d.Items))
	for i := range d.Items {
		it := &d.Items[i]
		origIDs[i] = it.ID
		item[it.ID] = uint(i + 1)
		opt[it.ID] = map[uint]uint{}
		it.ID = uint(i + 1)
		if it.CategoryID != nil {
			c := cat[*it.CategoryID]
			it.CategoryID = &c
		}
		if it.ProjectID != nil {
			p := proj[*it.ProjectID]
			it.ProjectID = &p
		}
	}
	for i := range d.Items {
		it := &d.Items[i]
		orig := origIDs[i]
		for j := range it.Options {
			opt[orig][it.Options[j].ID] = uint(j + 1)
			it.Options[j].ID = uint(j + 1)
		}
		for j := range it.RuleVersions {
			rv := &it.RuleVersions[j]
			if rv.QuotaOptionID != nil {
				o := opt[orig][*rv.QuotaOptionID]
				rv.QuotaOptionID = &o
			}
			tg := map[string]float64{}
			for k, v := range rv.OptionTargets {
				id, _ := strconv.Atoi(k)
				tg[strconv.Itoa(int(opt[orig][uint(id)]))] = v
			}
			rv.OptionTargets = tg
		}
	}
	for i := range d.Checkins {
		c := &d.Checkins[i]
		if c.OptionID != nil {
			o := opt[c.ItemID][*c.OptionID]
			c.OptionID = &o
		}
		c.ID = uint(i + 1)
		c.ItemID = item[c.ItemID]
	}
	return d
}

func TestExportImportRoundTripPreservesHistory(t *testing.T) {
	e := dataEnv(t)
	e.seedRich("u1")
	orig := e.export("u1")
	require.Len(t, orig.Projects, 1)
	require.Len(t, orig.Items, 3)
	require.Equal(t, &orig.Projects[0].ID, orig.Items[0].ProjectID)
	require.GreaterOrEqual(t, len(orig.Items[0].RuleVersions), 5, "all rule versions are exported")
	require.Len(t, orig.Items[0].Options, 2)
	require.True(t, orig.Items[0].Options[1].Archived)
	require.Len(t, orig.Checkins, 3)
	require.Len(t, orig.Freezes, 1)

	raw := e.doRaw("GET", "/api/export", "u1", "")
	require.Equal(t, "no-store", raw.Header().Get("Cache-Control"))
	require.Contains(t, raw.Header().Get("Content-Disposition"), "attachment")

	// dry run reports the counts and writes nothing
	w := e.doRaw("POST", "/api/import?dry_run=1", "u2", raw.Body.String())
	require.Equal(t, 200, w.Code, w.Body.String())
	sum := decode[importSummary](t, w)
	require.True(t, sum.DryRun)
	require.Equal(t, 1, sum.Projects)
	require.Equal(t, 3, sum.Items)
	require.Equal(t, 3, sum.Checkins)
	require.Zero(t, e.count(&dbmodels.Item{}, "u2"))
	require.Zero(t, e.count(&dbmodels.Project{}, "u2"))

	var verBefore int64
	require.NoError(t, e.db.Read().Model(&dbmodels.UserSettings{}).Where("user_id = ?", "u2").Select("state_version").Scan(&verBefore).Error)
	w = e.doRaw("POST", "/api/import", "u2", raw.Body.String())
	require.Equal(t, 200, w.Code, w.Body.String())
	var verAfter int64
	require.NoError(t, e.db.Read().Model(&dbmodels.UserSettings{}).Where("user_id = ?", "u2").Select("state_version").Scan(&verAfter).Error)
	require.Greater(t, verAfter, verBefore, "import bumps state_version")

	got := e.export("u2")
	var maxU1 uint
	for _, it := range orig.Items {
		if it.ID > maxU1 {
			maxU1 = it.ID
		}
	}
	for _, it := range got.Items {
		require.Greater(t, it.ID, maxU1)
	}
	require.Equal(t, normalize(orig), normalize(got))
	require.Equal(t, "Asia/Shanghai", got.Settings.Timezone, "settings untouched without apply_settings")

	// u1 is untouched, and ids are fresh (not reused from the file)
	require.Equal(t, 1, e.count(&dbmodels.Project{}, "u1"))
	require.Equal(t, 3, e.count(&dbmodels.Item{}, "u1"))

	// additive: importing again into the same user doubles the data
	w = e.doRaw("POST", "/api/import", "u2", raw.Body.String())
	require.Equal(t, 200, w.Code)
	require.Equal(t, 2, e.count(&dbmodels.Project{}, "u2"))
	require.Equal(t, 6, e.count(&dbmodels.Item{}, "u2"))
	require.Equal(t, 6, e.count(&dbmodels.CheckinLog{}, "u2"))
	// the current rule resolves identically for the imported copy
	items := decode[struct {
		Items []itemResponse `json:"items"`
	}](t, e.do("GET", "/api/items", "u2", "", nil)).Items
	require.Len(t, items, 6)
}

func (e *env) count(model any, user string) int {
	e.t.Helper()
	var n int64
	require.NoError(e.t, e.db.Read().Model(model).Where("user_id = ?", user).Count(&n).Error)
	return int(n)
}

func TestImportApplySettings(t *testing.T) {
	e := dataEnv(t)
	d := e.export("u1")
	d.Settings = &exportSettings{Timezone: "Europe/Paris", WeekStart: 0, HolidayRegion: "US", StreakHolidayFreeze: true}
	b, _ := json.Marshal(d)
	require.Equal(t, 200, e.doRaw("POST", "/api/import?apply_settings=1", "u2", string(b)).Code)
	st := decode[calendarSettings](t, e.do("GET", "/api/settings/calendar", "u2", "", nil))
	require.Equal(t, "Europe/Paris", st.Timezone)
	require.Equal(t, 0, st.WeekStart)
	require.True(t, st.StreakHolidayFreeze)

	d.Settings.Timezone = "Not/AZone"
	b, _ = json.Marshal(d)
	require.Equal(t, 400, e.doRaw("POST", "/api/import?apply_settings=1", "u2", string(b)).Code)
}

func TestExportIsolatedAndHidesSecrets(t *testing.T) {
	e := dataEnv(t)
	e.seedRich("u1")
	e.mustCreate("u2", rruleItem("OtherUsersItem", "FREQ=DAILY"))
	require.NoError(t, e.db.Write().Model(&dbmodels.UserSettings{}).Where("user_id = ?", "u1").
		Update("external_calendar_urls", `[{"id":"a","name":"Work","enc":"CIPHERTEXT-SECRET"}]`).Error)
	require.NoError(t, e.db.Write().Create(&dbmodels.ApiToken{UserID: "u1", Name: "tok", TokenHash: "HASH-SECRET", Scopes: "feed:read", ExpiresAt: fixedNow.Add(time.Hour)}).Error)

	w := e.doRaw("GET", "/api/export", "u1", "")
	body := w.Body.String()
	require.NotContains(t, body, "OtherUsersItem")
	require.NotContains(t, body, "CIPHERTEXT-SECRET")
	require.NotContains(t, body, "HASH-SECRET")
	require.NotContains(t, body, "user_id")
	require.Contains(t, body, `"external_calendars":[{"id":"a","name":"Work","url_hint":""}]`)

	w = e.doRaw("GET", "/api/export", "u2", "")
	require.NotContains(t, w.Body.String(), "Workout")
	require.Contains(t, w.Body.String(), "OtherUsersItem")
}

func minimalDoc() m {
	return m{
		"schema": "daka.export", "version": 1,
		"categories": []m{{"id": 1, "name": "C"}},
		"items": []m{{
			"id": 7, "category_id": 1, "name": "SecretItem", "tracker_type": "boolean", "kind": "ongoing", "polarity": "positive",
			"options": []m{},
			"rule_versions": []m{{"effective_from": "2026-01-01", "schedule_type": "fixed_rrule", "dtstart": "2026-01-01",
				"rrule_string": "FREQ=DAILY", "holiday_mode": "none"}},
		}},
		"checkins": []m{{"id": 1, "item_id": 7, "checkin_date": "2026-01-02", "checkin_time": "2026-01-02T10:00:00Z", "value": 1, "source": "web"}},
		"freezes":  []m{},
	}
}

func TestImportRejectsInvalidPayloads(t *testing.T) {
	mutate := func(f func(d m)) string {
		d := minimalDoc()
		f(d)
		b, _ := json.Marshal(d)
		return string(b)
	}
	item := func(d m) m { return d["items"].([]m)[0] }
	rule := func(d m) m { return item(d)["rule_versions"].([]m)[0] }
	chk := func(d m) m { return d["checkins"].([]m)[0] }
	cases := map[string]string{
		"not json":           `nope`,
		"trailing data":      mutate(func(d m) {}) + `{}`,
		"wrong schema":       mutate(func(d m) { d["schema"] = "other" }),
		"future version":     mutate(func(d m) { d["version"] = 2 }),
		"unknown field":      mutate(func(d m) { d["user_id"] = "u2" }),
		"nested unknown":     mutate(func(d m) { item(d)["user_id"] = "u2" }),
		"empty name":         mutate(func(d m) { item(d)["name"] = "  " }),
		"unknown tracker":    mutate(func(d m) { item(d)["tracker_type"] = "x" }),
		"no rule versions":   mutate(func(d m) { item(d)["rule_versions"] = []m{} }),
		"bad schedule":       mutate(func(d m) { rule(d)["schedule_type"] = "weird" }),
		"bad rrule":          mutate(func(d m) { rule(d)["rrule_string"] = "FREQ=NOPE" }),
		"bad effective date": mutate(func(d m) { rule(d)["effective_from"] = "2026-13-40" }),
		"bad remind time":    mutate(func(d m) { item(d)["remind_time"] = "25:00" }),
		"unknown category":   mutate(func(d m) { item(d)["category_id"] = 99 }),
		"dup item id":        mutate(func(d m) { d["items"] = []m{item(d), item(d)} }),
		"foreign item":       mutate(func(d m) { chk(d)["item_id"] = 1234 }),
		"foreign option":     mutate(func(d m) { chk(d)["option_id"] = 5 }),
		"bad checkin date":   mutate(func(d m) { chk(d)["checkin_date"] = "yesterday" }),
		"bad checkin time":   mutate(func(d m) { chk(d)["checkin_time"] = "noon" }),
		"huge value":         mutate(func(d m) { chk(d)["value"] = 1e300 }),
		"bad source":         mutate(func(d m) { chk(d)["source"] = "evil" }),
		"long note":          mutate(func(d m) { chk(d)["note"] = strings.Repeat("x", maxNoteBytes+1) }),
		"bad freeze":         mutate(func(d m) { d["freezes"] = []m{{"start_date": "2026-02-01", "end_date": "2026-01-01"}} }),
		"unknown option tgt": mutate(func(d m) { rule(d)["option_targets"] = m{"42": 1} }),
		"category long name": mutate(func(d m) { d["categories"].([]m)[0]["name"] = strings.Repeat("x", 300) }),
		"unknown project":    mutate(func(d m) { item(d)["project_id"] = 99 }),
		"dup project id":     mutate(func(d m) { d["projects"] = []m{{"id": 1, "name": "P"}, {"id": 1, "name": "P2"}} }),
		"project bad cat":    mutate(func(d m) { d["projects"] = []m{{"id": 1, "name": "P", "category_id": 99}} }),
		"project bad status": mutate(func(d m) { d["projects"] = []m{{"id": 1, "name": "P", "status": "unknown"}} }),
		"project bad date":   mutate(func(d m) { d["projects"] = []m{{"id": 1, "name": "P", "target_date": "bad-date"}} }),
	}
	for name, body := range cases {
		e := dataEnv(t)
		w := e.doRaw("POST", "/api/import", "u1", body)
		require.Equal(t, 400, w.Code, name+": "+w.Body.String())
		require.NotContains(t, w.Body.String(), "SecretItem", name)
		require.Zero(t, e.count(&dbmodels.Item{}, "u1"), name)
		require.Zero(t, e.count(&dbmodels.Project{}, "u1"), name)
		require.Zero(t, e.count(&dbmodels.Category{}, "u1"), name)
		require.Zero(t, e.count(&dbmodels.CheckinLog{}, "u1"), name)
	}

	// the base document is itself valid
	e := dataEnv(t)
	b, _ := json.Marshal(minimalDoc())
	require.Equal(t, 200, e.doRaw("POST", "/api/import", "u1", string(b)).Code)
}

func TestImportLimits(t *testing.T) {
	e := dataEnv(t)
	// body larger than the cap
	big := `{"schema":"` + strings.Repeat("a", maxImportBytes+10) + `"}`
	w := e.doRaw("POST", "/api/import", "u1", big)
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)

	// too many items
	d := minimalDoc()
	items := make([]m, maxImportItems+1)
	for i := range items {
		items[i] = m{"id": i + 1}
	}
	d["items"] = items
	b, _ := json.Marshal(d)
	w = e.doRaw("POST", "/api/import", "u1", string(b))
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code, w.Body.String())

	// too many options on one item
	d = minimalDoc()
	opts := make([]m, maxOptions+1)
	for i := range opts {
		opts[i] = m{"id": i + 1, "name": "o"}
	}
	d["items"].([]m)[0]["options"] = opts
	b, _ = json.Marshal(d)
	require.Equal(t, 400, e.doRaw("POST", "/api/import", "u1", string(b)).Code)
}

func TestImportRollbackOnLateFailure(t *testing.T) {
	// A database failure after the first writes must leave nothing behind.
	e := dataEnv(t)
	d := minimalDoc()
	require.NoError(t, e.db.Write().Exec("DROP TABLE freeze_periods").Error)
	d["freezes"] = []m{{"start_date": "2026-01-01", "end_date": "2026-01-02"}}
	b, _ := json.Marshal(d)
	w := e.doRaw("POST", "/api/import", "u1", string(b))
	require.Equal(t, 500, w.Code)
	require.NotContains(t, w.Body.String(), "SecretItem")
	require.Zero(t, e.count(&dbmodels.Item{}, "u1"))
	require.Zero(t, e.count(&dbmodels.Category{}, "u1"))
	require.Zero(t, e.count(&dbmodels.CheckinLog{}, "u1"))
	var n int64
	require.NoError(t, e.db.Read().Model(&dbmodels.ItemRuleVersion{}).Count(&n).Error)
	require.Zero(t, n)
}

// seedEveryTable puts one row of every model in AllModels for the user.
func (e *env) seedEveryTable(user string) uint {
	e.t.Helper()
	w := e.db.Write()
	cat := dbmodels.Category{UserID: user, Name: "c"}
	require.NoError(e.t, w.Create(&cat).Error)
	proj := dbmodels.Project{UserID: user, Name: "p"}
	require.NoError(e.t, w.Create(&proj).Error)
	id := e.item(user, nil)
	require.NoError(e.t, w.Create(&dbmodels.ItemOption{ItemID: id, Name: "o"}).Error)
	require.NoError(e.t, w.Create(&dbmodels.CheckinLog{UserID: user, ItemID: id, CheckinDate: "2026-01-01", CheckinTime: fixedNow}).Error)
	require.NoError(e.t, w.Create(&dbmodels.FreezePeriod{UserID: user, StartDate: "2026-01-01", EndDate: "2026-01-02"}).Error)
	require.NoError(e.t, w.Create(&dbmodels.ApiToken{UserID: user, Name: "t", TokenHash: "h" + user, Scopes: "feed:read", ExpiresAt: fixedNow}).Error)
	require.NoError(e.t, w.Create(&dbmodels.IdempotencyKey{UserID: user, Key: "k", StatusCode: 200}).Error)
	require.NoError(e.t, w.Create(&dbmodels.Session{ID: "sess-" + user, UserID: user, ExpiresAt: fixedNow.Add(time.Hour)}).Error)
	require.NoError(e.t, w.Create(&dbmodels.TelegramLink{UserID: user, ChatID: int64(user[len(user)-1])}).Error)
	require.NoError(e.t, w.Create(&dbmodels.TelegramLinkCode{CodeHash: "code-" + user, UserID: user, ExpiresAt: fixedNow}).Error)
	require.NoError(e.t, w.Create(&dbmodels.ReminderLog{UserID: user, ItemID: id, RemindDate: "2026-01-01"}).Error)
	return id
}

func (e *env) rowsOf(model any, filter string, args ...any) int64 {
	e.t.Helper()
	var n int64
	require.NoError(e.t, e.db.Read().Model(model).Where(filter, args...).Count(&n).Error)
	return n
}

func TestDeleteAccountRemovesEverythingOfTheUserOnly(t *testing.T) {
	e := dataEnv(t)
	id1 := e.seedEveryTable("u1")
	id2 := e.seedEveryTable("u2")

	// wrong / missing confirmation deletes nothing
	for _, body := range []string{``, `{}`, `{"confirm":"delete"}`, `{"confirm":"DELETE","x":1}`} {
		w := e.doRaw("DELETE", "/api/account", "u1", body)
		require.Equal(t, 400, w.Code, body)
	}
	require.Equal(t, int64(1), e.rowsOf(&dbmodels.User{}, "id = ?", "u1"))

	w := e.doRaw("DELETE", "/api/account", "u1", `{"confirm":"DELETE"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	var cleared bool
	for _, c := range w.Result().Cookies() {
		if c.Name == "daka_session" || strings.Contains(c.Name, "session") {
			cleared = c.MaxAge < 0 && c.Value == ""
		}
	}
	require.True(t, cleared, "session cookie is cleared: %v", w.Result().Header["Set-Cookie"])

	for _, m := range dbmodels.AllModels() {
		switch m.(type) {
		case *dbmodels.User:
			require.Zero(t, e.rowsOf(m, "id = ?", "u1"), "%T", m)
			require.Equal(t, int64(1), e.rowsOf(m, "id = ?", "u2"), "%T", m)
		case *dbmodels.ItemOption, *dbmodels.ItemRuleVersion:
			require.Zero(t, e.rowsOf(m, "item_id = ?", id1), "%T", m)
			require.Equal(t, int64(1), e.rowsOf(m, "item_id = ?", id2), "%T of other user", m)
		default:
			require.Zero(t, e.rowsOf(m, "user_id = ?", "u1"), "%T", m)
			require.Equal(t, int64(1), e.rowsOf(m, "user_id = ?", "u2"), "%T of other user", m)
		}
	}
}

func TestImportKindLegacyName(t *testing.T) {
	require.Equal(t, dbmodels.KindOngoing, importKind("habit"))
	require.Equal(t, dbmodels.KindOngoing, importKind("ongoing"))
	require.Equal(t, dbmodels.KindEvent, importKind("event"))
}

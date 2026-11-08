package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &v), w.Body.String())
	return v
}

func (e *env) mustCreate(user string, body m) itemResponse {
	e.t.Helper()
	w := e.do("POST", "/api/items", user, "", body)
	require.Equal(e.t, 201, w.Code, w.Body.String())
	return decode[itemResponse](e.t, w)
}

func itemPath(id uint) string { return "/api/items/" + strconv.Itoa(int(id)) }

func (e *env) versionsOf(id uint) []dbmodels.ItemRuleVersion {
	e.t.Helper()
	var vs []dbmodels.ItemRuleVersion
	require.NoError(e.t, e.db.Write().Where("item_id = ?", id).Order("id").Find(&vs).Error)
	return vs
}

func counterWithOptions(e *env) itemResponse {
	return e.mustCreate("", m{
		"name": "Workout", "tracker_type": "counter",
		"options": []m{
			{"name": "run", "unit": "km", "target_value": 2},
			{"name": "pushups", "unit": "reps", "target_value": 30},
		},
		"rule": m{"schedule_type": "fixed_rrule", "rrule_string": "FREQ=DAILY", "completion_rule": "all"},
	})
}

func TestGetAndListItems(t *testing.T) {
	e := newEnv(t)
	a := e.mustCreate("", rruleItem("Alpha_one", "FREQ=DAILY"))
	e.mustCreate("", m{"name": "Beta", "tracker_type": "boolean", "kind": "event", "rule": m{}})
	e.mustCreate("u2", rruleItem("Alpha_other_user", "FREQ=DAILY"))

	w := e.do("GET", itemPath(a.ID), "", "", nil)
	require.Equal(t, 200, w.Code)
	got := decode[itemResponse](t, w)
	require.Equal(t, "Alpha_one", got.Name)
	require.Equal(t, "fixed_rrule", got.CurrentRule.ScheduleType)
	require.Equal(t, 404, e.do("GET", itemPath(a.ID), "u2", "", nil).Code)
	require.Equal(t, 400, e.do("GET", "/api/items/abc", "", "", nil).Code)

	list := func(q string) []itemResponse {
		w := e.do("GET", "/api/items"+q, "", "", nil)
		require.Equal(t, 200, w.Code, w.Body.String())
		return decode[struct {
			Items []itemResponse `json:"items"`
		}](t, w).Items
	}
	require.Len(t, list(""), 2)
	require.Len(t, list("?q=alp"), 1)
	require.Len(t, list("?q=ALPHA_"), 1)
	require.Empty(t, list("?q=%25"), "wildcards are literal")
	require.Empty(t, list("?q=a_p"), "underscore is literal")
	require.Len(t, list("?kind=event"), 1)
	require.Len(t, list("?polarity=negative"), 0)
	for _, bad := range []string{"?kind=x", "?polarity=x", "?category_id=x", "?archived=x", "?pinned=x", "?include=x", "?redact=x"} {
		require.Equal(t, 400, e.do("GET", "/api/items"+bad, "", "", nil).Code, bad)
	}

	// archive hides by default
	require.Equal(t, 200, e.do("PATCH", itemPath(a.ID), "", "", m{"archived": true}).Code)
	require.Len(t, list(""), 1)
	require.Len(t, list("?archived=true"), 1)
	require.Len(t, list("?archived=all"), 2)
	require.Equal(t, 200, e.do("PATCH", itemPath(a.ID), "", "", m{"archived": false, "pinned": true}).Code)
	items := list("?pinned=true")
	require.Len(t, items, 1)
	require.Equal(t, a.ID, items[0].ID)
	require.Equal(t, a.ID, list("")[0].ID, "pinned first")
}

func TestListItemsIncludeStats(t *testing.T) {
	e := newEnv(t)
	a := e.mustCreate("", rruleItem("Read", "FREQ=DAILY"))
	e.mustCreate("", rruleItem("Idle", "FREQ=DAILY"))
	require.Equal(t, 200, e.do("POST", "/api/checkins", "", "", m{"item_id": a.ID, "mode": "set", "date": "2026-11-05"}).Code)
	require.Equal(t, 200, e.do("POST", "/api/checkins", "", "", m{"item_id": a.ID, "mode": "set"}).Code)

	w := e.do("GET", "/api/items?include=stats", "", "", nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	items := decode[struct {
		Items []itemResponse `json:"items"`
	}](t, w).Items
	require.Len(t, items, 2)
	require.NotNil(t, items[0].Stats)
	require.Equal(t, 2, items[0].Stats.Counts.Total)
	require.NotNil(t, items[0].Stats.DaysSinceLast)
	require.Equal(t, 0, *items[0].Stats.DaysSinceLast)
	require.Nil(t, items[1].Stats.LastAt)
	// without include there is no stats key
	require.NotContains(t, e.do("GET", "/api/items", "", "", nil).Body.String(), `"stats"`)
}

func TestItemsRedact(t *testing.T) {
	e := newEnv(t)
	it := e.mustCreate("", m{"name": "Secret Medication", "tracker_type": "counter", "is_sensitive": true,
		"options": []m{{"name": "Pill X"}}, "rule": m{"schedule_type": "none"}})
	e.mustCreate("", rruleItem("Visible", "FREQ=DAILY"))
	body := e.do("GET", "/api/items?redact=1", "", "", nil).Body.String()
	require.NotContains(t, body, "Secret Medication")
	require.NotContains(t, body, "Pill X")
	require.Contains(t, body, "Visible")
	body = e.do("GET", itemPath(it.ID)+"?redact=1", "", "", nil).Body.String()
	require.NotContains(t, body, "Secret")
	require.NotContains(t, body, "Pill X")
	body = e.do("GET", itemPath(it.ID)+"/options?redact=1", "", "", nil).Body.String()
	require.NotContains(t, body, "Pill X")
}

func TestPatchDisplayFieldsInPlace(t *testing.T) {
	e := newEnv(t)
	it := e.mustCreate("", rruleItem("Old", "FREQ=DAILY"))
	cat := dbmodels.Category{UserID: "u1", Name: "c", IsSensitive: true}
	require.NoError(t, e.db.Write().Create(&cat).Error)

	w := e.do("PATCH", itemPath(it.ID), "", "", m{"name": " New ", "description": "d", "pinned": true,
		"sort_order": 3, "remind_time": "07:30", "category_id": cat.ID})
	require.Equal(t, 200, w.Code, w.Body.String())
	got := decode[itemResponse](t, w)
	require.Equal(t, "New", got.Name)
	require.Equal(t, "d", got.Description)
	require.True(t, got.Pinned)
	require.Equal(t, 3, got.SortOrder)
	require.Equal(t, "07:30", *got.RemindTime)
	require.True(t, got.IsSensitive, "sensitive category makes the item sensitive")
	require.Len(t, e.versionsOf(it.ID), 1, "display edits create no rule version")

	w = e.do("PATCH", itemPath(it.ID), "", "", m{"remind_time": nil, "category_id": nil})
	require.Equal(t, 200, w.Code)
	got = decode[itemResponse](t, w)
	require.Nil(t, got.RemindTime)
	require.Nil(t, got.CategoryID)

	for name, body := range map[string]m{
		"empty name":      {"name": "  "},
		"bad remind":      {"remind_time": "25:00"},
		"foreign cat":     {"category_id": 9999},
		"tracker change":  {"tracker_type": "counter"},
		"polarity change": {"polarity": "negative"},
		"unknown field":   {"nope": 1},
	} {
		require.Equal(t, 400, e.do("PATCH", itemPath(it.ID), "", "", body).Code, name)
	}
	require.Equal(t, 200, e.do("PATCH", itemPath(it.ID), "", "", m{"tracker_type": "boolean"}).Code, "same value is allowed")
	require.Equal(t, 404, e.do("PATCH", itemPath(it.ID), "u2", "", m{"name": "x"}).Code)
	require.Equal(t, 400, e.do("PATCH", itemPath(it.ID), "", "", "{").Code)
}

func TestPatchRuleCreatesVersion(t *testing.T) {
	e := newEnv(t)
	it := e.mustCreate("", rruleItem("Water", "FREQ=DAILY"))
	require.Len(t, e.versionsOf(it.ID), 1)

	// no-op rule patch makes no version
	require.Equal(t, 200, e.do("PATCH", itemPath(it.ID), "", "", m{"rule": m{"rrule_string": "FREQ=DAILY"}}).Code)
	require.Len(t, e.versionsOf(it.ID), 1)

	w := e.do("PATCH", itemPath(it.ID), "", "", m{"rule": m{"rrule_string": "FREQ=WEEKLY;BYDAY=MO", "holiday_mode": "skip_holidays"}})
	require.Equal(t, 200, w.Code, w.Body.String())
	got := decode[itemResponse](t, w)
	require.Equal(t, "FREQ=WEEKLY;BYDAY=MO", got.CurrentRule.RRuleString)
	require.Equal(t, "skip_holidays", got.CurrentRule.HolidayMode)
	require.Equal(t, "2026-11-07", got.CurrentRule.EffectiveFrom)
	vs := e.versionsOf(it.ID)
	require.Len(t, vs, 2)
	require.Equal(t, "FREQ=DAILY", vs[0].RRuleString, "history is immutable")

	// retroactive edit
	w = e.do("PATCH", itemPath(it.ID), "", "", m{"rule": m{"rrule_string": "FREQ=DAILY", "effective_from": "2026-01-01"}})
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Len(t, e.versionsOf(it.ID), 3)
	// the current rule on today is still the highest effective_from
	require.Equal(t, "FREQ=WEEKLY;BYDAY=MO", decode[itemResponse](t, w).CurrentRule.RRuleString)

	// invalid combos and bad dates
	require.Equal(t, 400, e.do("PATCH", itemPath(it.ID), "", "", m{"rule": m{"effective_from": "nope"}}).Code)
	w = e.do("PATCH", itemPath(it.ID), "", "", m{"rule": m{"schedule_type": "rolling", "rolling_interval": 2, "rolling_unit": "day", "holiday_mode": "skip_holidays"}})
	require.Equal(t, 400, w.Code)
	require.Contains(t, w.Body.String(), "holiday_mode_not_allowed")
	require.Len(t, e.versionsOf(it.ID), 3, "rejected patch writes nothing")
	require.Equal(t, 400, e.do("PATCH", itemPath(it.ID), "", "", m{"rule": m{"rrule_string": "NOT AN RRULE"}}).Code)
}

func TestPatchScheduleTypeSwitch(t *testing.T) {
	e := newEnv(t)
	it := e.mustCreate("", rruleItem("Gym", "FREQ=WEEKLY;BYDAY=MO"))
	w := e.do("PATCH", itemPath(it.ID), "", "", m{"rule": m{"schedule_type": "quota", "quota_comparator": "at_least", "quota_count": 3, "quota_period": "week"}})
	require.Equal(t, 200, w.Code, w.Body.String())
	got := decode[itemResponse](t, w)
	require.Equal(t, "quota", got.CurrentRule.ScheduleType)
	require.Empty(t, got.CurrentRule.RRuleString, "rrule does not carry over")
	require.Equal(t, "days", got.CurrentRule.QuotaMeasure)
	require.Equal(t, "calendar", got.CurrentRule.QuotaWindow)

	w = e.do("PATCH", itemPath(it.ID), "", "", m{"rule": m{"schedule_type": "none"}})
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Empty(t, decode[itemResponse](t, w).CurrentRule.QuotaComparator)
}

func TestPatchOptionTargetsAndArchivedExclusion(t *testing.T) {
	e := newEnv(t)
	it := counterWithOptions(e)
	runID, pushID := it.Options[0].ID, it.Options[1].ID
	require.Equal(t, 2.0, *it.Options[0].TargetValue)

	w := e.do("PATCH", itemPath(it.ID), "", "", m{"rule": m{"option_targets": m{idStr(runID): 5, idStr(pushID): nil}}})
	require.Equal(t, 200, w.Code, w.Body.String())
	got := decode[itemResponse](t, w)
	require.Equal(t, 5.0, *got.Options[0].TargetValue)
	require.Nil(t, got.Options[1].TargetValue)
	require.Len(t, e.versionsOf(it.ID), 2)

	// unknown option id in targets
	require.Equal(t, 400, e.do("PATCH", itemPath(it.ID), "", "", m{"rule": m{"option_targets": m{"99999": 1}}}).Code)

	// archived options are excluded when validating quota_option_id
	require.Equal(t, 200, e.do("DELETE", itemPath(it.ID)+"/options/"+idStr(pushID), "", "", nil).Code)
	w = e.do("PATCH", itemPath(it.ID), "", "", m{"rule": m{"schedule_type": "quota", "quota_comparator": "at_least",
		"quota_count": 100, "quota_period": "week", "quota_measure": "sum", "quota_option_id": pushID}})
	require.Equal(t, 400, w.Code)
	require.Contains(t, w.Body.String(), "quota_option_unknown")
	// a target for the archived option is rejected too
	require.Equal(t, 400, e.do("PATCH", itemPath(it.ID), "", "", m{"rule": m{"option_targets": m{idStr(pushID): 3}}}).Code)
	// the live option is accepted
	w = e.do("PATCH", itemPath(it.ID), "", "", m{"rule": m{"schedule_type": "quota", "quota_comparator": "at_least",
		"quota_count": 100, "quota_period": "week", "quota_measure": "sum", "quota_option_id": runID}})
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, runID, *decode[itemResponse](t, w).CurrentRule.QuotaOptionID)
	// quota_option_id is rejected for non-sum measures
	w = e.do("PATCH", itemPath(it.ID), "", "", m{"rule": m{"quota_measure": "occurrences"}})
	require.Equal(t, 400, w.Code)
	require.Contains(t, w.Body.String(), "quota_option_not_applicable")
}

func TestCreateQuotaOptionGuards(t *testing.T) {
	e := newEnv(t)
	base := func(idx any, measure string) m {
		return m{"name": "q", "tracker_type": "counter", "options": []m{{"name": "a", "unit": "x"}},
			"rule": m{"schedule_type": "quota", "quota_comparator": "at_least", "quota_count": 3, "quota_period": "week",
				"quota_measure": measure, "quota_option_index": idx}}
	}
	require.Equal(t, 201, e.do("POST", "/api/items", "", "", base(0, "sum")).Code)
	for _, idx := range []any{5, -1} {
		require.Equal(t, 400, e.do("POST", "/api/items", "", "", base(idx, "sum")).Code)
	}
	require.Equal(t, 400, e.do("POST", "/api/items", "", "", base(0, "days")).Code)
	require.Equal(t, 400, e.do("POST", "/api/items", "", "", m{"name": "x", "tracker_type": "boolean", "bogus": 1, "rule": m{}}).Code, "unknown fields rejected")
}

func TestOptionsCRUD(t *testing.T) {
	e := newEnv(t)
	it := counterWithOptions(e)
	base := itemPath(it.ID) + "/options"

	w := e.do("POST", base, "", "", m{"name": "swim", "unit": "m", "target_value": 400})
	require.Equal(t, 201, w.Code, w.Body.String())
	swim := decode[optionResponse](t, w)
	require.Equal(t, 400.0, *swim.TargetValue)
	require.Len(t, e.versionsOf(it.ID), 2, "target change appends a version")

	// no target: no new version
	require.Equal(t, 201, e.do("POST", base, "", "", m{"name": "yoga"}).Code)
	require.Len(t, e.versionsOf(it.ID), 2)

	list := decode[struct {
		Options []optionResponse `json:"options"`
	}](t, e.do("GET", base, "", "", nil))
	require.Len(t, list.Options, 4)

	// PUT replaces, changing the target appends a version, removing it too
	w = e.do("PUT", base+"/"+idStr(swim.ID), "", "", m{"name": "swim2", "unit": "m", "target_value": 500})
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "swim2", decode[optionResponse](t, w).Name)
	require.Len(t, e.versionsOf(it.ID), 3)
	w = e.do("PUT", base+"/"+idStr(swim.ID), "", "", m{"name": "swim2", "unit": "m"})
	require.Equal(t, 200, w.Code)
	require.Nil(t, decode[optionResponse](t, w).TargetValue)
	require.Len(t, e.versionsOf(it.ID), 4)
	e.do("PUT", base+"/"+idStr(swim.ID), "", "", m{"name": "swim2", "unit": "m"})
	require.Len(t, e.versionsOf(it.ID), 4, "unchanged target adds no version")

	// DELETE archives; logs keep their option id
	require.Equal(t, 200, e.do("POST", "/api/checkins", "", "", m{"item_id": it.ID, "option_id": swim.ID, "mode": "delta", "value": 1}).Code)
	require.Equal(t, 200, e.do("DELETE", base+"/"+idStr(swim.ID), "", "", nil).Code)
	var opt dbmodels.ItemOption
	require.NoError(t, e.db.Write().First(&opt, swim.ID).Error)
	require.True(t, opt.Archived)
	require.Len(t, e.logs(it.ID, "2026-11-07"), 1)
	require.Len(t, decode[struct {
		Options []optionResponse `json:"options"`
	}](t, e.do("GET", base, "", "", nil)).Options, 3)
	require.Len(t, decode[struct {
		Options []optionResponse `json:"options"`
	}](t, e.do("GET", base+"?include_archived=1", "", "", nil)).Options, 4)
	// archived options cannot be used for check-ins or edited again
	require.Equal(t, 400, e.do("POST", "/api/checkins", "", "", m{"item_id": it.ID, "option_id": swim.ID, "mode": "delta", "value": 1}).Code)
	require.Equal(t, 404, e.do("PUT", base+"/"+idStr(swim.ID), "", "", m{"name": "z"}).Code)
	require.Equal(t, 404, e.do("DELETE", base+"/"+idStr(swim.ID), "", "", nil).Code)

	// validation and isolation
	require.Equal(t, 400, e.do("POST", base, "", "", m{"name": " "}).Code)
	require.Equal(t, 400, e.do("POST", base, "", "", m{"name": "x", "step": -1}).Code)
	require.Equal(t, 404, e.do("POST", base, "u2", "", m{"name": "x"}).Code)
	require.Equal(t, 404, e.do("GET", base, "u2", "", nil).Code)
	b := e.do("POST", itemPath(e.mustCreate("", rruleItem("plain", "FREQ=DAILY")).ID)+"/options", "", "", m{"name": "x"})
	require.Equal(t, 400, b.Code, "boolean items cannot have options")
	require.Contains(t, b.Body.String(), "options_not_allowed")
}

func TestOptionDeleteBlockedByQuotaAndCompletion(t *testing.T) {
	e := newEnv(t)
	it := e.mustCreate("", m{"name": "w", "tracker_type": "counter",
		"options": []m{{"name": "a", "unit": "x"}, {"name": "b", "unit": "y"}},
		"rule": m{"schedule_type": "quota", "quota_comparator": "at_least", "quota_count": 5, "quota_period": "week",
			"quota_measure": "sum", "quota_option_index": 0}})
	a, b := it.Options[0].ID, it.Options[1].ID
	w := e.do("DELETE", itemPath(it.ID)+"/options/"+idStr(a), "", "", nil)
	require.Equal(t, 400, w.Code, "the quota option cannot be archived")
	var opt dbmodels.ItemOption
	require.NoError(t, e.db.Write().First(&opt, a).Error)
	require.False(t, opt.Archived, "failed delete rolls back")
	require.Equal(t, 200, e.do("DELETE", itemPath(it.ID)+"/options/"+idStr(b), "", "", nil).Code)

	// completion_n cannot exceed the remaining options
	c := e.mustCreate("", m{"name": "c", "tracker_type": "counter",
		"options": []m{{"name": "a"}, {"name": "b"}},
		"rule":    m{"schedule_type": "fixed_rrule", "rrule_string": "FREQ=DAILY", "completion_rule": "at_least_n", "completion_n": 2}})
	require.Equal(t, 400, e.do("DELETE", itemPath(c.ID)+"/options/"+idStr(c.Options[0].ID), "", "", nil).Code)
}

func TestDeleteItemCascades(t *testing.T) {
	e := newEnv(t)
	it := counterWithOptions(e)
	other := e.mustCreate("", rruleItem("keep", "FREQ=DAILY"))
	require.Equal(t, 200, e.do("POST", "/api/checkins", "", "", m{"item_id": it.ID, "option_id": it.Options[0].ID, "mode": "delta", "value": 1}).Code)
	require.Equal(t, 200, e.do("POST", "/api/checkins", "", "", m{"item_id": other.ID, "mode": "set"}).Code)

	require.Equal(t, 404, e.do("DELETE", itemPath(it.ID), "u2", "", nil).Code)
	require.Equal(t, 200, e.do("DELETE", itemPath(it.ID), "", "", nil).Code)
	for _, mdl := range []any{&dbmodels.CheckinLog{}, &dbmodels.ItemRuleVersion{}, &dbmodels.ItemOption{}} {
		var n int64
		require.NoError(t, e.db.Write().Model(mdl).Where("item_id = ?", it.ID).Count(&n).Error)
		require.Zero(t, n)
	}
	var n int64
	require.NoError(t, e.db.Write().Model(&dbmodels.CheckinLog{}).Where("item_id = ?", other.ID).Count(&n).Error)
	require.Equal(t, int64(1), n, "other items are untouched")
	require.Equal(t, 404, e.do("GET", itemPath(it.ID), "", "", nil).Code)
	require.Equal(t, 404, e.do("DELETE", itemPath(it.ID), "", "", nil).Code)
}

func TestCheckinsListCursorPagination(t *testing.T) {
	e := newEnv(t)
	a := e.mustCreate("", m{"name": "N", "tracker_type": "metric", "unit": "kg", "rule": m{"schedule_type": "none"}})
	b := e.mustCreate("", rruleItem("B", "FREQ=DAILY"))
	for i := 0; i < 7; i++ {
		require.Equal(t, 200, e.do("POST", "/api/checkins", "", "", m{"item_id": a.ID, "mode": "append", "value": 60 + i, "date": "2026-11-0" + strconv.Itoa(1+i/2)}).Code)
	}
	require.Equal(t, 200, e.do("POST", "/api/checkins", "", "", m{"item_id": b.ID, "mode": "set", "note": "n"}).Code)
	u2 := e.mustCreate("u2", rruleItem("other", "FREQ=DAILY"))
	require.Equal(t, 200, e.do("POST", "/api/checkins", "u2", "", m{"item_id": u2.ID, "mode": "set"}).Code)

	type page struct {
		Checkins   []checkinLogResponse `json:"checkins"`
		NextCursor *string              `json:"next_cursor"`
	}
	get := func(q string) page {
		w := e.do("GET", "/api/checkins"+q, "", "", nil)
		require.Equal(t, 200, w.Code, w.Body.String())
		return decode[page](t, w)
	}
	// walk all pages of item a, 3 at a time: newest first, no gaps, no dupes
	var seen []uint
	cursor := ""
	pages := 0
	for {
		q := "?item_id=" + strconv.Itoa(int(a.ID)) + "&limit=3"
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		p := get(q)
		pages++
		for _, l := range p.Checkins {
			seen = append(seen, l.ID)
			require.Equal(t, a.ID, l.ItemID)
		}
		if p.NextCursor == nil {
			break
		}
		cursor = *p.NextCursor
	}
	require.Equal(t, 3, pages)
	require.Len(t, seen, 7)
	for i := 1; i < len(seen); i++ {
		require.Greater(t, seen[i-1], seen[i], "id desc within descending dates")
	}

	all := get("")
	require.Len(t, all.Checkins, 8, "only the caller's logs")
	require.Nil(t, all.NextCursor)
	require.Equal(t, "2026-11-07", all.Checkins[0].Date)
	require.Equal(t, "n", all.Checkins[0].Note)
	require.Equal(t, "2026-11-06T16:30:00Z", all.Checkins[0].Time)

	require.Len(t, get("?item_id="+strconv.Itoa(int(a.ID))+"&from=2026-11-02&to=2026-11-03").Checkins, 4)
	require.Empty(t, get("?from=2030-01-01").Checkins)
	exact := get("?item_id=" + strconv.Itoa(int(a.ID)) + "&limit=7")
	require.Len(t, exact.Checkins, 7)
	require.Nil(t, exact.NextCursor, "exactly a full page has no next cursor")
	require.Contains(t, e.do("GET", "/api/checkins?to=2026-01-01", "", "", nil).Body.String(), `"checkins":[]`)

	for _, q := range []string{"?item_id=x", "?from=2026-13-01", "?to=nope", "?from=2026-02-02&to=2026-01-01", "?limit=0", "?limit=201", "?limit=x", "?cursor=!!", "?cursor=" + encodeCursor("bad", 1), "?redact=2"} {
		require.Equal(t, 400, e.do("GET", "/api/checkins"+q, "", "", nil).Code, q)
	}
	require.Equal(t, 404, e.do("GET", "/api/checkins?item_id="+strconv.Itoa(int(u2.ID)), "", "", nil).Code)
}

func TestCheckinsListRedact(t *testing.T) {
	e := newEnv(t)
	it := e.mustCreate("", m{"name": "Secret", "tracker_type": "boolean", "is_sensitive": true, "rule": m{"schedule_type": "fixed_rrule", "rrule_string": "FREQ=DAILY"}})
	require.Equal(t, 200, e.do("POST", "/api/checkins", "", "", m{"item_id": it.ID, "mode": "set", "note": "private note"}).Code)
	require.Contains(t, e.do("GET", "/api/checkins", "", "", nil).Body.String(), "private note")
	require.NotContains(t, e.do("GET", "/api/checkins?redact=1", "", "", nil).Body.String(), "private note")
}

func TestDeleteCounterIncrementGuard(t *testing.T) {
	e := newEnv(t)
	it := e.mustCreate("", m{"name": "cups", "tracker_type": "counter", "unit": "cup", "rule": m{"schedule_type": "fixed_rrule", "rrule_string": "FREQ=DAILY"}})
	post := func(v float64) uint {
		w := e.do("POST", "/api/checkins", "", "", m{"item_id": it.ID, "mode": "delta", "value": v})
		require.Equal(t, 200, w.Code, w.Body.String())
		return decode[checkinResponse](t, w).ID
	}
	plus2 := post(2)
	minus1 := post(-1)
	// removing +2 would leave -1
	w := e.do("DELETE", "/api/checkins/"+strconv.Itoa(int(plus2)), "", "", nil)
	require.Equal(t, 400, w.Code, w.Body.String())
	require.Len(t, e.logs(it.ID, "2026-11-07"), 2)
	// removing the -1 is fine, then +2 can go
	require.Equal(t, 200, e.do("DELETE", "/api/checkins/"+strconv.Itoa(int(minus1)), "", "", nil).Code)
	require.Equal(t, 200, e.do("DELETE", "/api/checkins/"+strconv.Itoa(int(plus2)), "", "", nil).Code)
	require.Equal(t, 404, e.do("DELETE", "/api/checkins/"+strconv.Itoa(int(plus2)), "", "", nil).Code)
	require.Equal(t, 404, e.do("DELETE", "/api/checkins/"+strconv.Itoa(int(plus2)), "u2", "", nil).Code)
}

func TestCounterNegativeRejected(t *testing.T) {
	e := newEnv(t)
	w := e.do("POST", "/api/items", "", "", m{"name": "c", "tracker_type": "counter", "polarity": "negative",
		"rule": m{"schedule_type": "none"}})
	require.Equal(t, 400, w.Code)
	require.Contains(t, w.Body.String(), "negative_tracker_not_allowed")
	// legacy row: the check-in matrix refuses it as well
	id := e.item("u1", func(it *dbmodels.Item) {
		it.TrackerType, it.Polarity = dbmodels.TrackerCounter, dbmodels.PolarityNegative
	})
	require.Equal(t, 400, e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "delta", "value": 1}).Code)
}

func TestErrorsNeverEchoItemName(t *testing.T) {
	e := newEnv(t)
	it := e.mustCreate("", rruleItem("Highly Sensitive Name", "FREQ=DAILY"))
	for _, w := range []*httptest.ResponseRecorder{
		e.do("PATCH", itemPath(it.ID), "", "", m{"rule": m{"schedule_type": "bogus"}}),
		e.do("PATCH", itemPath(it.ID), "", "", m{"name": ""}),
		e.do("POST", itemPath(it.ID)+"/options", "", "", m{"name": "Highly Sensitive Option"}),
	} {
		require.GreaterOrEqual(t, w.Code, 400)
		require.NotContains(t, w.Body.String(), "Highly Sensitive")
	}
}

func TestDeleteItemRequiresReauthWhenConfigured(t *testing.T) {
	e := newEnv(t)
	deny := func(_ http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			errResult(http.StatusForbidden, "reauth_required").write(w)
		})
	}
	e.h = NewRouter(Deps{DB: e.db, Now: func() time.Time { return e.now }, Auth: userHeaderAuth, Reauth: deny})
	it := e.mustCreate("", rruleItem("x", "FREQ=DAILY"))
	require.Equal(t, 403, e.do("DELETE", itemPath(it.ID), "", "", nil).Code)
	require.Equal(t, 200, e.do("GET", itemPath(it.ID), "", "", nil).Code, "only DELETE is guarded")
}

func TestTodayUsesArchivedOptionExclusion(t *testing.T) {
	e := newEnv(t)
	it := counterWithOptions(e)
	run, push := it.Options[0].ID, it.Options[1].ID
	require.Equal(t, 200, e.do("POST", "/api/checkins", "", "", m{"item_id": it.ID, "option_id": run, "mode": "delta", "value": 2}).Code)
	require.False(t, e.today("", "").Items[0].Done, "completion_rule all needs both options")
	require.Equal(t, 200, e.do("DELETE", itemPath(it.ID)+"/options/"+idStr(push), "", "", nil).Code)
	require.True(t, e.today("", "").Items[0].Done, "archived option no longer required")
}

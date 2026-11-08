package api

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

func idStr(id uint) string { return strconv.FormatUint(uint64(id), 10) }

func TestCreateItemValidCombos(t *testing.T) {
	quota := func(extra m) m {
		r := m{"schedule_type": "quota", "quota_comparator": "at_least", "quota_count": 3, "quota_period": "week"}
		for k, v := range extra {
			r[k] = v
		}
		return r
	}
	tests := []struct {
		name string
		body m
	}{
		{"boolean rolling", m{"name": "a", "tracker_type": "boolean", "rule": m{"schedule_type": "rolling", "rolling_interval": 2, "rolling_unit": "day"}}},
		{"boolean none", m{"name": "a", "tracker_type": "boolean", "rule": m{"schedule_type": "none", "stale_after_days": 30}}},
		{"boolean quota", m{"name": "a", "tracker_type": "boolean", "rule": quota(nil)}},
		{"boolean rrule workdays", m{"name": "a", "tracker_type": "boolean", "rule": m{"schedule_type": "fixed_rrule", "rrule_string": "FREQ=DAILY", "holiday_mode": "workdays_only"}}},
		{"event default none", m{"name": "a", "tracker_type": "boolean", "kind": "event", "rule": m{}}},
		{"maintenance default rolling", m{"name": "a", "tracker_type": "maintenance", "warning_days": 7, "rule": m{"rolling_interval": 3, "rolling_unit": "month"}}},
		{"counter rrule target", m{"name": "a", "tracker_type": "counter", "unit": "cup", "step": 1, "rule": m{"schedule_type": "fixed_rrule", "rrule_string": "FREQ=DAILY", "target_value": 8}}},
		{"counter quota sum", m{"name": "a", "tracker_type": "counter", "rule": quota(m{"quota_measure": "sum", "quota_count": 200})}},
		{"metric none", m{"name": "a", "tracker_type": "metric", "unit": "kg", "rule": m{"schedule_type": "none"}}},
		{"negative boolean quota", m{"name": "a", "tracker_type": "boolean", "polarity": "negative", "rule": quota(m{"quota_comparator": "at_most"})}},
		{"negative metric sum", m{"name": "a", "tracker_type": "metric", "polarity": "negative", "rule": quota(m{"quota_comparator": "at_most", "quota_measure": "sum", "quota_period": "year"})}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			w := e.do("POST", "/api/items", "", "", tc.body)
			require.Equal(t, 201, w.Code, w.Body.String())
		})
	}
}

func TestCreateItemDefaultsStored(t *testing.T) {
	e := newEnv(t)
	w := e.do("POST", "/api/items", "", "", m{"name": "cola", "tracker_type": "boolean", "polarity": "negative",
		"rule": m{"schedule_type": "quota", "quota_comparator": "at_most", "quota_count": 4, "quota_period": "year"}})
	require.Equal(t, 201, w.Code, w.Body.String())
	var it itemResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &it))
	require.Equal(t, "occurrences", it.CurrentRule.QuotaMeasure)
	require.Equal(t, "calendar", it.CurrentRule.QuotaWindow)
	require.Empty(t, it.Options)

	w = e.do("POST", "/api/items", "", "", m{"name": "run", "tracker_type": "boolean",
		"rule": m{"schedule_type": "quota", "quota_comparator": "at_least", "quota_count": 2, "quota_period": "week"}})
	require.Equal(t, 201, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &it))
	require.Equal(t, "days", it.CurrentRule.QuotaMeasure)
}

func TestCreateItemWithOptions(t *testing.T) {
	e := newEnv(t)
	w := e.do("POST", "/api/items", "", "", m{
		"name": "Exercise", "tracker_type": "counter",
		"options": []m{
			{"name": "Cardio", "unit": "min", "step": 5, "target_value": 20, "sort_order": 1},
			{"name": "Run", "unit": "km", "step": 0.5, "min_val": 0, "max_val": 50, "target_value": 2, "sort_order": 2},
			{"name": "Strength", "unit": "sets", "sort_order": 3},
		},
		"rule": m{"schedule_type": "fixed_rrule", "rrule_string": "FREQ=DAILY", "completion_rule": "at_least_n", "completion_n": 2},
	})
	require.Equal(t, 201, w.Code, w.Body.String())
	var it itemResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &it))
	require.Len(t, it.Options, 3)
	require.Equal(t, map[string]float64{
		idStr(it.Options[0].ID): 20, idStr(it.Options[1].ID): 2,
	}, it.CurrentRule.OptionTargets)
	require.Equal(t, "at_least_n", it.CurrentRule.CompletionRule)
	require.Equal(t, 2, it.CurrentRule.CompletionN)

	var opts []dbmodels.ItemOption
	require.NoError(t, e.db.Write().Where("item_id = ?", it.ID).Order("sort_order").Find(&opts).Error)
	require.Len(t, opts, 3)
	require.Equal(t, "Run", opts[1].Name)
	var ver dbmodels.ItemRuleVersion
	require.NoError(t, e.db.Write().Where("item_id = ?", it.ID).Take(&ver).Error)
	var snap map[string]float64
	require.NoError(t, json.Unmarshal([]byte(ver.OptionTargets), &snap))
	require.Equal(t, 20.0, snap[idStr(opts[0].ID)])
	require.Equal(t, 2.0, snap[idStr(opts[1].ID)])
	require.NotContains(t, snap, idStr(opts[2].ID))
}

func TestCreateItemQuotaOptionIndex(t *testing.T) {
	e := newEnv(t)
	body := func(idx any) m {
		r := m{"schedule_type": "quota", "quota_comparator": "at_least", "quota_count": 10, "quota_period": "week", "quota_measure": "sum"}
		if idx != nil {
			r["quota_option_index"] = idx
		}
		return m{"name": "Sport", "tracker_type": "metric",
			"options": []m{{"name": "Cardio", "unit": "min"}, {"name": "Run", "unit": "km"}}, "rule": r}
	}
	// Different units without quota_option_index -> 400.
	w := e.do("POST", "/api/items", "", "", body(nil))
	require.Equal(t, 400, w.Code)
	require.JSONEq(t, `{"error":"invalid combination","reason":"quota_option_required"}`, w.Body.String())
	w = e.do("POST", "/api/items", "", "", body(5))
	require.Equal(t, 400, w.Code)
	require.JSONEq(t, `{"error":"invalid combination","reason":"quota_option_unknown"}`, w.Body.String())

	w = e.do("POST", "/api/items", "", "", body(1))
	require.Equal(t, 201, w.Code, w.Body.String())
	var it itemResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &it))
	require.NotNil(t, it.CurrentRule.QuotaOptionID)
	require.Equal(t, it.Options[1].ID, *it.CurrentRule.QuotaOptionID)
}

func TestCreateItemInvalidCombos(t *testing.T) {
	quota := m{"schedule_type": "quota", "quota_comparator": "at_least", "quota_count": 3, "quota_period": "week"}
	tests := []struct {
		name   string
		body   m
		reason string
	}{
		{"counter rolling", m{"tracker_type": "counter", "rule": m{"schedule_type": "rolling", "rolling_interval": 1, "rolling_unit": "day"}}, "schedule_not_allowed_for_tracker"},
		{"metric rolling", m{"tracker_type": "metric", "rule": m{"schedule_type": "rolling", "rolling_interval": 1, "rolling_unit": "day"}}, "schedule_not_allowed_for_tracker"},
		{"maintenance quota", m{"tracker_type": "maintenance", "rule": quota}, "schedule_not_allowed_for_tracker"},
		{"negative rrule", m{"tracker_type": "boolean", "polarity": "negative", "rule": m{"schedule_type": "fixed_rrule", "rrule_string": "FREQ=DAILY"}}, "negative_schedule_not_allowed"},
		{"negative counter", m{"tracker_type": "counter", "polarity": "negative", "rule": m{"schedule_type": "none"}}, "negative_tracker_not_allowed"},
		{"negative maintenance", m{"tracker_type": "maintenance", "polarity": "negative", "rule": m{"schedule_type": "none"}}, "negative_tracker_not_allowed"},
		{"unknown schedule", m{"tracker_type": "boolean", "rule": m{"schedule_type": "weekly"}}, "unknown_schedule_type"},
		{"missing schedule", m{"tracker_type": "boolean", "rule": m{}}, "unknown_schedule_type"},
		{"unknown tracker", m{"tracker_type": "x", "rule": m{"schedule_type": "none"}}, "unknown_tracker_type"},
		{"foreign rrule on none", m{"tracker_type": "boolean", "rule": m{"schedule_type": "none", "rrule_string": "FREQ=DAILY"}}, "foreign_rrule_fields"},
		{"options on boolean", m{"tracker_type": "boolean", "options": []m{{"name": "x"}}, "rule": m{"schedule_type": "none"}}, "options_not_allowed"},
		{"negative target", m{"tracker_type": "counter", "rule": m{"schedule_type": "none", "target_value": -1}}, "invalid_target_value"},
		{"negative option target", m{"tracker_type": "counter", "options": []m{{"name": "x", "target_value": -2}}, "rule": m{"schedule_type": "none"}}, "invalid_option_targets"},
		{"at_least_n too big", m{"tracker_type": "counter", "options": []m{{"name": "x"}}, "rule": m{"schedule_type": "none", "completion_rule": "at_least_n", "completion_n": 2}}, "invalid_completion_rule"},
		{"sum on boolean", m{"tracker_type": "boolean", "rule": m{"schedule_type": "quota", "quota_comparator": "at_least", "quota_count": 3, "quota_period": "week", "quota_measure": "sum"}}, "invalid_quota_measure"},
		{"unnamed option", m{"tracker_type": "counter", "options": []m{{"name": " "}}, "rule": m{"schedule_type": "none"}}, "invalid_option"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			tc.body["name"] = "Secret Name"
			w := e.do("POST", "/api/items", "", "", tc.body)
			require.Equal(t, 400, w.Code, w.Body.String())
			require.JSONEq(t, `{"error":"invalid combination","reason":"`+tc.reason+`"}`, w.Body.String())
			var n int64
			require.NoError(t, e.db.Write().Model(&dbmodels.Item{}).Count(&n).Error)
			require.Zero(t, n)
			require.NoError(t, e.db.Write().Model(&dbmodels.ItemOption{}).Count(&n).Error)
			require.Zero(t, n)
		})
	}
}

func TestTodayIgnoresNonRRuleItems(t *testing.T) {
	e := newEnv(t)
	for _, b := range []m{
		{"name": "r", "tracker_type": "maintenance", "rule": m{"rolling_interval": 1, "rolling_unit": "month"}},
		{"name": "q", "tracker_type": "metric", "rule": m{"schedule_type": "quota", "quota_comparator": "at_least", "quota_count": 1, "quota_period": "week"}},
		{"name": "n", "tracker_type": "counter", "rule": m{"schedule_type": "none"}},
	} {
		code, _ := e.createItem("", b)
		require.Equal(t, 201, code)
	}
	r := e.today("", "")
	require.Empty(t, r.Items)
}

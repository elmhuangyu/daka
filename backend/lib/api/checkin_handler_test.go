package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/auth"
	"github.com/elmhuangyu/daka/backend/lib/config"
	"github.com/elmhuangyu/daka/backend/lib/db"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

// 2026-11-06 16:30 UTC is 2026-11-07 00:30 in Asia/Shanghai.
var fixedNow = time.Date(2026, 11, 6, 16, 30, 0, 0, time.UTC)

type env struct {
	t   *testing.T
	db  *db.DB
	h   http.Handler
	now time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d, err := db.Open(config.DBConfig{Driver: config.DriverSQLite, DSN: config.Secret(filepath.Join(t.TempDir(), "t.db"))})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	require.NoError(t, dbmodels.AutoMigrate(d.Write()))
	e := &env{t: t, db: d, now: fixedNow}
	e.h = NewRouter(Deps{DB: d, Now: func() time.Time { return e.now }, Auth: userHeaderAuth})
	for _, id := range []string{"u1", "u2"} {
		require.NoError(t, d.Write().Create(&dbmodels.User{ID: id}).Error)
		require.NoError(t, d.Write().Create(&dbmodels.UserSettings{UserID: id, Timezone: "Asia/Shanghai"}).Error)
	}
	return e
}

// userHeaderAuth is a test-only auth middleware: X-User selects the user (default u1).
func userHeaderAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-User")
		if id == "" {
			id = "u1"
		}
		next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), auth.User{ID: id})))
	})
}

func (e *env) item(userID string, mut func(*dbmodels.Item)) uint {
	e.t.Helper()
	it := dbmodels.Item{UserID: userID, Name: "Secret Medication", TrackerType: dbmodels.TrackerBoolean,
		Kind: dbmodels.KindOngoing, Polarity: dbmodels.PolarityPositive}
	if mut != nil {
		mut(&it)
	}
	require.NoError(e.t, e.db.Write().Create(&it).Error)
	require.NoError(e.t, e.db.Write().Create(&dbmodels.ItemRuleVersion{
		ItemID: it.ID, EffectiveFrom: "2026-01-01", ScheduleType: dbmodels.ScheduleNone, HolidayMode: dbmodels.HolidayNone,
	}).Error)
	return it.ID
}

func (e *env) do(method, path, user, key string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	var buf bytes.Buffer
	switch b := body.(type) {
	case string:
		buf.WriteString(b)
	case nil:
	default:
		require.NoError(e.t, json.NewEncoder(&buf).Encode(b))
	}
	r := httptest.NewRequest(method, path, &buf)
	if user != "" {
		r.Header.Set("X-User", user)
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func (e *env) logs(itemID uint, date string) []dbmodels.CheckinLog {
	e.t.Helper()
	var l []dbmodels.CheckinLog
	require.NoError(e.t, e.db.Write().Where("item_id = ? AND checkin_date = ?", itemID, date).Find(&l).Error)
	return l
}

type m = map[string]any

func TestSetSupplementAndTimezone(t *testing.T) {
	tests := []struct {
		name       string
		date       string
		wantDate   string
		supplement bool
	}{
		{"default date is user today", "", "2026-11-07", false},
		{"explicit today", "2026-11-07", "2026-11-07", false},
		{"yesterday in user tz is supplement", "2026-11-06", "2026-11-06", true},
		{"older is supplement", "2026-10-01", "2026-10-01", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			id := e.item("u1", nil)
			body := m{"item_id": id, "mode": "set", "note": "n", "is_supplement": !tc.supplement}
			if tc.date != "" {
				body["date"] = tc.date
			}
			w := e.do("POST", "/api/checkins", "", "", body)
			require.Equal(t, 200, w.Code, w.Body.String())
			var resp checkinResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			require.Equal(t, tc.supplement, resp.IsSupplement)
			require.Equal(t, tc.wantDate, resp.Date)
			logs := e.logs(id, tc.wantDate)
			require.Len(t, logs, 1)
			require.Equal(t, tc.supplement, logs[0].IsSupplement)
			require.Equal(t, dbmodels.SourceWeb, logs[0].Source)
			require.Equal(t, "u1", logs[0].UserID)
			require.True(t, logs[0].CheckinTime.Equal(fixedNow))
		})
	}
}

func TestRejections(t *testing.T) {
	e := newEnv(t)
	good := e.item("u1", nil)
	other := e.item("u2", nil)
	neg := e.item("u1", func(i *dbmodels.Item) { i.Polarity = dbmodels.PolarityNegative })
	arch := e.item("u1", func(i *dbmodels.Item) { i.Archived = true })
	opt := uint(1)

	tests := []struct {
		name string
		body any
		code int
	}{
		{"invalid json", "{nope", 400},
		{"invalid mode", m{"item_id": good, "mode": "zap"}, 400},
		{"empty mode", m{"item_id": good}, 400},
		{"invalid date format", m{"item_id": good, "mode": "set", "date": "2026-1-5"}, 400},
		{"invalid date garbage", m{"item_id": good, "mode": "set", "date": "tomorrow"}, 400},
		{"impossible date", m{"item_id": good, "mode": "set", "date": "2026-02-30"}, 400},
		{"trailing json", `{"item_id":1,"mode":"set"}{"x":1}`, 400},
		{"trailing garbage", `{"item_id":1,"mode":"set"} x`, 400},
		{"note too long", m{"item_id": good, "mode": "set", "note": strings.Repeat("a", 2001)}, 400},
		{"leap day invalid year", m{"item_id": good, "mode": "set", "date": "2027-02-29"}, 400},
		{"future date", m{"item_id": good, "mode": "set", "date": "2026-11-08"}, 400},
		{"future date unset", m{"item_id": good, "mode": "unset", "date": "2026-11-08"}, 400},
		{"missing item", m{"item_id": 9999, "mode": "set"}, 404},
		{"zero item", m{"mode": "set"}, 404},
		{"other user's item", m{"item_id": other, "mode": "set"}, 404},
		{"other user's item unset", m{"item_id": other, "mode": "unset"}, 404},
		{"archived", m{"item_id": arch, "mode": "set"}, 400},
		{"negative set", m{"item_id": neg, "mode": "set"}, 400},
		{"negative unset", m{"item_id": neg, "mode": "unset"}, 400},
		{"boolean delta mismatch", m{"item_id": good, "mode": "delta", "value": 1}, 400},
		{"boolean append mismatch", m{"item_id": good, "mode": "append"}, 400},
		{"option not supported", m{"item_id": good, "mode": "set", "option_id": opt}, 400},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := e.do("POST", "/api/checkins", "", "", tc.body)
			require.Equal(t, tc.code, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"error"`)
			require.NotContains(t, w.Body.String(), "Secret Medication")
		})
	}
	var n int64
	require.NoError(t, e.db.Write().Model(&dbmodels.CheckinLog{}).Count(&n).Error)
	require.Zero(t, n)
}

func TestSetIdempotentAndNoteOverwrite(t *testing.T) {
	e := newEnv(t)
	id := e.item("u1", nil)
	var first checkinResponse
	for i, note := range []string{"a", "a", "b"} {
		w := e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "set", "date": "2026-11-07", "note": note})
		require.Equal(t, 200, w.Code)
		var resp checkinResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		if i == 0 {
			first = resp
		}
		require.Equal(t, first.ID, resp.ID)
		require.Equal(t, note, resp.Note)
	}
	logs := e.logs(id, "2026-11-07")
	require.Len(t, logs, 1)
	require.Equal(t, "b", logs[0].Note)
}

func TestSetHealsDuplicates(t *testing.T) {
	e := newEnv(t)
	id := e.item("u1", nil)
	for range 3 {
		require.NoError(t, e.db.Write().Create(&dbmodels.CheckinLog{UserID: "u1", ItemID: id, CheckinDate: "2026-11-07", CheckinTime: fixedNow}).Error)
	}
	w := e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "set"})
	require.Equal(t, 200, w.Code)
	require.Len(t, e.logs(id, "2026-11-07"), 1)
}

func TestUnset(t *testing.T) {
	e := newEnv(t)
	id := e.item("u1", nil)
	// unset with nothing there is ok
	w := e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "unset"})
	require.Equal(t, 200, w.Code)

	require.Equal(t, 200, e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "set"}).Code)
	require.Equal(t, 200, e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "set", "date": "2026-11-05"}).Code)
	require.Len(t, e.logs(id, "2026-11-07"), 1)

	w = e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "unset"})
	require.Equal(t, 200, w.Code)
	require.Empty(t, e.logs(id, "2026-11-07"))
	require.Len(t, e.logs(id, "2026-11-05"), 1, "other days untouched")
}

func TestDeleteCheckin(t *testing.T) {
	e := newEnv(t)
	id := e.item("u1", nil)
	var resp checkinResponse
	w := e.do("POST", "/api/checkins", "", "", m{"item_id": id, "mode": "set"})
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	path := fmt.Sprintf("/api/checkins/%d", resp.ID)

	require.Equal(t, 404, e.do("DELETE", path, "u2", "", nil).Code, "other user")
	require.Len(t, e.logs(id, "2026-11-07"), 1)
	require.Equal(t, 400, e.do("DELETE", "/api/checkins/abc", "", "", nil).Code)
	require.Equal(t, 200, e.do("DELETE", path, "", "", nil).Code)
	require.Empty(t, e.logs(id, "2026-11-07"))
	require.Equal(t, 404, e.do("DELETE", path, "", "", nil).Code)
}

func TestIdempotencyReplay(t *testing.T) {
	e := newEnv(t)
	id := e.item("u1", nil)
	body := m{"item_id": id, "mode": "set", "note": "x"}
	w1 := e.do("POST", "/api/checkins", "", "k1", body)
	require.Equal(t, 200, w1.Code)

	// Undo the effect, then replay: stored response comes back, nothing re-applied.
	require.NoError(t, e.db.Write().Where("item_id = ?", id).Delete(&dbmodels.CheckinLog{}).Error)
	e.now = e.now.Add(48 * time.Hour)
	w2 := e.do("POST", "/api/checkins", "", "k1", body)
	require.Equal(t, w1.Code, w2.Code)
	require.Equal(t, w1.Body.String(), w2.Body.String())
	require.Empty(t, e.logs(id, "2026-11-07"))

	// Errors are not stored: the same key can be retried after a 4xx.
	bad := m{"item_id": 9999, "mode": "set"}
	require.Equal(t, 404, e.do("POST", "/api/checkins", "", "kbad", bad).Code)
	require.Equal(t, 200, e.do("POST", "/api/checkins", "", "kbad", m{"item_id": id, "mode": "set"}).Code)

	require.Equal(t, 400, e.do("POST", "/api/checkins", "", string(bytes.Repeat([]byte("k"), 256)), body).Code)
}

func TestIdempotencyPerUser(t *testing.T) {
	e := newEnv(t)
	a := e.item("u1", nil)
	b := e.item("u2", nil)
	w1 := e.do("POST", "/api/checkins", "u1", "same", m{"item_id": a, "mode": "set"})
	require.Equal(t, 200, w1.Code)
	w2 := e.do("POST", "/api/checkins", "u2", "same", m{"item_id": b, "mode": "set"})
	require.Equal(t, 200, w2.Code)
	require.NotEqual(t, w1.Body.String(), w2.Body.String())
	require.Len(t, e.logs(a, "2026-11-07"), 1)
	require.Len(t, e.logs(b, "2026-11-07"), 1)
}

func TestIdempotencyConcurrent(t *testing.T) {
	e := newEnv(t)
	id := e.item("u1", nil)
	const n = 20
	bodies := make([]string, n)
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Different notes: without idempotency the last writer would win.
			w := e.do("POST", "/api/checkins", "", "race", m{"item_id": id, "mode": "set", "note": fmt.Sprint("n", i)})
			codes[i], bodies[i] = w.Code, w.Body.String()
		}()
	}
	wg.Wait()
	for i := range n {
		require.Equal(t, 200, codes[i])
		require.Equal(t, bodies[0], bodies[i])
	}
	logs := e.logs(id, "2026-11-07")
	require.Len(t, logs, 1)
	var keys int64
	require.NoError(t, e.db.Write().Model(&dbmodels.IdempotencyKey{}).Count(&keys).Error)
	require.EqualValues(t, 1, keys)
	var resp checkinResponse
	require.NoError(t, json.Unmarshal([]byte(bodies[0]), &resp))
	require.Equal(t, resp.Note, logs[0].Note)
}

func TestPurgeIdempotencyKeys(t *testing.T) {
	e := newEnv(t)
	now := fixedNow
	for _, k := range []struct {
		key string
		age time.Duration
	}{{"old", 8 * 24 * time.Hour}, {"edge", 7*24*time.Hour + time.Minute}, {"fresh", 6 * 24 * time.Hour}} {
		require.NoError(t, e.db.Write().Create(&dbmodels.IdempotencyKey{UserID: "u1", Key: k.key, StatusCode: 200, CreatedAt: now.Add(-k.age)}).Error)
	}
	n, err := PurgeIdempotencyKeys(e.db, now)
	require.NoError(t, err)
	require.EqualValues(t, 2, n)
	var left []dbmodels.IdempotencyKey
	require.NoError(t, e.db.Write().Find(&left).Error)
	require.Len(t, left, 1)
	require.Equal(t, "fresh", left[0].Key)
}

func TestPatchCheckin(t *testing.T) {
	e := newEnv(t)

	// 1. Updating note on boolean, counter, metric, and event check-in logs.
	types := []struct {
		name       string
		createItem func() uint
		postBody   func(id uint) m
	}{
		{
			name: "boolean",
			createItem: func() uint {
				return e.item("u1", nil)
			},
			postBody: func(id uint) m {
				return m{"item_id": id, "mode": "set", "note": "initial bool note"}
			},
		},
		{
			name: "counter",
			createItem: func() uint {
				return e.typed("u1", dbmodels.TrackerCounter, nil)
			},
			postBody: func(id uint) m {
				return m{"item_id": id, "mode": "delta", "value": 2, "note": "initial counter note"}
			},
		},
		{
			name: "metric",
			createItem: func() uint {
				return e.typed("u1", dbmodels.TrackerMetric, nil)
			},
			postBody: func(id uint) m {
				return m{"item_id": id, "mode": "append", "value": 42.5, "note": "initial metric note"}
			},
		},
		{
			name: "event",
			createItem: func() uint {
				return e.item("u1", func(i *dbmodels.Item) { i.Kind = dbmodels.KindEvent })
			},
			postBody: func(id uint) m {
				return m{"item_id": id, "mode": "set", "note": "initial event note"}
			},
		},
	}

	for _, tc := range types {
		t.Run(tc.name, func(t *testing.T) {
			itID := tc.createItem()
			w := e.do("POST", "/api/checkins", "", "", tc.postBody(itID))
			require.Equal(t, 200, w.Code)
			var posted checkinResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &posted))

			var stBefore dbmodels.UserSettings
			require.NoError(t, e.db.Write().Where("user_id = ?", "u1").Take(&stBefore).Error)

			newNote := fmt.Sprintf("updated note for %s", tc.name)
			patchPath := fmt.Sprintf("/api/checkins/%d", posted.ID)
			w = e.do("PATCH", patchPath, "", "", m{"note": newNote})
			require.Equal(t, 200, w.Code, w.Body.String())

			var patched patchCheckinResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &patched))
			require.Equal(t, posted.ID, patched.ID)
			require.Equal(t, itID, patched.ItemID)
			require.Equal(t, posted.Date, patched.Date)
			require.Equal(t, newNote, patched.Note)

			var row dbmodels.CheckinLog
			require.NoError(t, e.db.Write().Where("id = ?", posted.ID).Take(&row).Error)
			require.Equal(t, newNote, row.Note)

			var stAfter dbmodels.UserSettings
			require.NoError(t, e.db.Write().Where("user_id = ?", "u1").Take(&stAfter).Error)
			require.Greater(t, stAfter.StateVersion, stBefore.StateVersion, "state_version bumped")
		})
	}

	// 2. Clearing note to ""
	t.Run("clear note to empty string", func(t *testing.T) {
		itID := e.item("u1", nil)
		w := e.do("POST", "/api/checkins", "", "", m{"item_id": itID, "mode": "set", "note": "to be cleared"})
		require.Equal(t, 200, w.Code)
		var posted checkinResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &posted))

		patchPath := fmt.Sprintf("/api/checkins/%d", posted.ID)
		w = e.do("PATCH", patchPath, "", "", m{"note": ""})
		require.Equal(t, 200, w.Code)
		var patched patchCheckinResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &patched))
		require.Equal(t, "", patched.Note)

		var row dbmodels.CheckinLog
		require.NoError(t, e.db.Write().Where("id = ?", posted.ID).Take(&row).Error)
		require.Equal(t, "", row.Note)
	})

	// 3. Missing note field or invalid JSON -> 400
	t.Run("validation rejections", func(t *testing.T) {
		itID := e.item("u1", nil)
		w := e.do("POST", "/api/checkins", "", "", m{"item_id": itID, "mode": "set", "note": "orig"})
		var posted checkinResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &posted))
		patchPath := fmt.Sprintf("/api/checkins/%d", posted.ID)

		for name, body := range map[string]any{
			"invalid json":         "{broken json",
			"empty object":         m{},
			"wrong type":           m{"note": 123},
			"unknown fields":       m{"note": "valid", "extra": 42},
			"trailing json":        `{"note":"a"}{"note":"b"}`,
			"note too long (2001)": m{"note": strings.Repeat("x", 2001)},
		} {
			w := e.do("PATCH", patchPath, "", "", body)
			require.Equal(t, 400, w.Code, name)
		}

		// Boundary check: note exactly 2000 bytes succeeds
		w = e.do("PATCH", patchPath, "", "", m{"note": strings.Repeat("x", 2000)})
		require.Equal(t, 200, w.Code)

		// Invalid path ID
		require.Equal(t, 400, e.do("PATCH", "/api/checkins/abc", "", "", m{"note": "valid"}).Code)
	})

	// 4. Non-existent id or another user's check-in id -> 404
	t.Run("not found and user isolation", func(t *testing.T) {
		itID := e.item("u1", nil)
		w := e.do("POST", "/api/checkins", "", "", m{"item_id": itID, "mode": "set", "note": "orig"})
		var posted checkinResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &posted))
		patchPath := fmt.Sprintf("/api/checkins/%d", posted.ID)

		// Other user
		w = e.do("PATCH", patchPath, "u2", "", m{"note": "hacked"})
		require.Equal(t, 404, w.Code)
		require.JSONEq(t, `{"error":"checkin not found"}`, w.Body.String())

		// Non-existent ID
		w = e.do("PATCH", "/api/checkins/9999999", "", "", m{"note": "ghost"})
		require.Equal(t, 404, w.Code)
		require.JSONEq(t, `{"error":"checkin not found"}`, w.Body.String())
	})

	// 5. Verify GET /api/checkins reflects updated note, and ?redact=1 on sensitive item hides note
	t.Run("list checkins and redaction", func(t *testing.T) {
		sensID := e.item("u1", func(i *dbmodels.Item) { i.IsSensitive = true })
		w := e.do("POST", "/api/checkins", "", "", m{"item_id": sensID, "mode": "set", "note": "secret 1"})
		var posted checkinResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &posted))

		patchPath := fmt.Sprintf("/api/checkins/%d", posted.ID)
		w = e.do("PATCH", patchPath, "", "", m{"note": "updated secret"})
		require.Equal(t, 200, w.Code)

		// Normal GET reveals updated note
		w = e.do("GET", fmt.Sprintf("/api/checkins?item_id=%d", sensID), "", "", nil)
		require.Equal(t, 200, w.Code)
		var listResp struct {
			Checkins []checkinLogResponse `json:"checkins"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listResp))
		require.Len(t, listResp.Checkins, 1)
		require.Equal(t, "updated secret", listResp.Checkins[0].Note)

		// Redacted GET masks note
		w = e.do("GET", fmt.Sprintf("/api/checkins?item_id=%d&redact=1", sensID), "", "", nil)
		require.Equal(t, 200, w.Code)
		var redactedResp struct {
			Checkins []checkinLogResponse `json:"checkins"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &redactedResp))
		require.Len(t, redactedResp.Checkins, 1)
		require.Equal(t, "", redactedResp.Checkins[0].Note)
	})

	// 6. Idempotency replay
	t.Run("idempotency replay", func(t *testing.T) {
		itID := e.item("u1", nil)
		w := e.do("POST", "/api/checkins", "", "", m{"item_id": itID, "mode": "set", "note": "v1"})
		var posted checkinResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &posted))

		patchPath := fmt.Sprintf("/api/checkins/%d", posted.ID)
		w1 := e.do("PATCH", patchPath, "", "patch-key-1", m{"note": "v2"})
		require.Equal(t, 200, w1.Code)

		w2 := e.do("PATCH", patchPath, "", "patch-key-1", m{"note": "v2"})
		require.Equal(t, 200, w2.Code)
		require.Equal(t, w1.Body.String(), w2.Body.String())
	})
}

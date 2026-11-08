package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/notify"
)

type fakeBot struct{ on bool }

func (f fakeBot) Configured() bool                   { return f.on }
func (f fakeBot) BotUsername(context.Context) string { return "daka_bot" }

func tgEnv(t *testing.T, configured bool) *env {
	e := newEnv(t)
	e.h = NewRouter(Deps{DB: e.db, Now: func() time.Time { return e.now }, Auth: userHeaderAuth, Telegram: fakeBot{on: configured},
		Reauth: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Reauth") == "" {
					errResult(http.StatusForbidden, "reauth_required").write(w)
					return
				}
				next.ServeHTTP(w, r)
			})
		}})
	return e
}

func (e *env) doReauth(method, path, user string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(e.t, json.NewEncoder(&buf).Encode(body))
	}
	r := httptest.NewRequest(method, path, &buf)
	if user != "" {
		r.Header.Set("X-User", user)
	}
	r.Header.Set("X-Reauth", "1")
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func TestTelegramLinkLifecycle(t *testing.T) {
	e := tgEnv(t, true)

	w := e.do("GET", "/api/telegram", "", "", nil)
	require.Equal(t, 200, w.Code)
	var st map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &st))
	require.Equal(t, true, st["configured"])
	require.Equal(t, false, st["linked"])

	// link code and unlink need reauth
	require.Equal(t, 403, e.do("POST", "/api/telegram/link-code", "", "", nil).Code)
	require.Equal(t, 403, e.do("DELETE", "/api/telegram", "", "", nil).Code)

	w = e.doReauth("POST", "/api/telegram/link-code", "", nil)
	require.Equal(t, 201, w.Code, w.Body.String())
	var lc struct {
		Code     string `json:"code"`
		DeepLink string `json:"deep_link"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &lc))
	require.NotEmpty(t, lc.Code)
	require.Equal(t, "https://t.me/daka_bot?start="+lc.Code, lc.DeepLink)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))

	// settings before linking
	require.Equal(t, 404, e.do("PATCH", "/api/telegram/settings", "", "", m{"enabled": false}).Code)

	_, err := notify.Bind(e.db.Write(), lc.Code, 999, e.now)
	require.NoError(t, err)

	w = e.do("GET", "/api/telegram", "", "", nil)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &st))
	require.Equal(t, true, st["linked"])
	require.Equal(t, "08:00", st["digest_time"])
	require.NotContains(t, w.Body.String(), "999", "chat id is not exposed")

	w = e.do("PATCH", "/api/telegram/settings", "", "", m{"digest_time": "06:30", "remind_sensitive": false})
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &st))
	require.Equal(t, "06:30", st["digest_time"])
	require.Equal(t, false, st["remind_sensitive"])
	require.Equal(t, 400, e.do("PATCH", "/api/telegram/settings", "", "", m{"digest_time": "25:00"}).Code)
	require.Equal(t, 400, e.do("PATCH", "/api/telegram/settings", "", "", m{"chat_id": 5}).Code)

	// another user sees no link and cannot touch it
	w = e.do("GET", "/api/telegram", "u2", "", nil)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &st))
	require.Equal(t, false, st["linked"])
	require.Equal(t, 404, e.do("PATCH", "/api/telegram/settings", "u2", "", m{"enabled": false}).Code)

	require.Equal(t, 200, e.doReauth("DELETE", "/api/telegram", "", nil).Code)
	var n int64
	require.NoError(t, e.db.Write().Model(&dbmodels.TelegramLink{}).Count(&n).Error)
	require.Zero(t, n)
}

func TestTelegramNotConfigured(t *testing.T) {
	e := tgEnv(t, false)
	w := e.do("GET", "/api/telegram", "", "", nil)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"configured":false`)
	require.Equal(t, 503, e.doReauth("POST", "/api/telegram/link-code", "", nil).Code)

	e.h = NewRouter(Deps{DB: e.db, Auth: userHeaderAuth}) // nil Telegram dep
	require.Contains(t, e.do("GET", "/api/telegram", "", "", nil).Body.String(), `"configured":false`)
}

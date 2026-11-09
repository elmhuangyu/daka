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
	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/notify"
	"github.com/elmhuangyu/daka/backend/lib/secret"
)

type fakeBot struct {
	on bool
	db *gorm.DB
}

func (f fakeBot) Configured() bool                   { return f.on }
func (f fakeBot) BotUsername(context.Context) string { return "daka_bot" }

func (f fakeBot) GetStatus(ctx context.Context, userID string) (notify.TelegramStatus, error) {
	st := notify.TelegramStatus{
		Configured:      f.on,
		DigestTime:      dbmodels.DefaultDigestTime,
		RemindSensitive: true,
		BotUsername:     "daka_bot",
	}
	if !f.on {
		return st, nil
	}
	l, err := notify.GetLink(f.db, userID)
	if err != nil {
		return st, err
	}
	if l != nil {
		st.Linked = l.ChatID != 0
		st.Enabled = l.Enabled
		st.RemindSensitive = l.RemindSensitive
		st.DigestTime = l.DigestTime
		t := l.LinkedAt.UTC()
		st.LinkedAt = &t
		st.HasToken = true
	}
	return st, nil
}

func (f fakeBot) SetBot(ctx context.Context, userID, token string, chatID int64) (*dbmodels.TelegramLink, error) {
	if token == "bad" {
		return nil, notify.ErrInvalidToken
	}
	if token == "nokey" {
		return nil, secret.ErrNoKey
	}
	if chatID == 88888 {
		return nil, notify.ErrChatTaken
	}
	l, err := notify.GetLink(f.db, userID)
	if err != nil {
		return nil, err
	}
	if token == "" && l == nil {
		return nil, notify.ErrBotTokenRequired
	}
	if l == nil {
		l = &dbmodels.TelegramLink{
			UserID:          userID,
			TokenEnc:        "enc:" + token,
			BotUsername:     "daka_bot",
			ChatID:          chatID,
			Enabled:         chatID != 0,
			RemindSensitive: true,
			DigestTime:      dbmodels.DefaultDigestTime,
			LinkedAt:        time.Now().UTC(),
		}
		if err := f.db.Create(l).Error; err != nil {
			return nil, err
		}
	} else {
		if token != "" {
			l.TokenEnc = "enc:" + token
		}
		if chatID != 0 {
			l.ChatID = chatID
			l.Enabled = true
		}
		if err := f.db.Save(l).Error; err != nil {
			return nil, err
		}
	}
	return l, nil
}

func (f fakeBot) CreateLinkCode(ctx context.Context, userID string) (string, time.Time, string, error) {
	code, exp, err := notify.CreateLinkCode(f.db, userID, time.Now())
	if err != nil {
		return "", time.Time{}, "", err
	}
	return code, exp, "https://t.me/daka_bot?start=" + code, nil
}

func (f fakeBot) Unlink(ctx context.Context, userID string) error {
	return notify.Unlink(f.db, userID)
}

func (f fakeBot) UpdateSettings(ctx context.Context, userID string, p notify.SettingsPatch) (*dbmodels.TelegramLink, error) {
	return notify.UpdateSettings(f.db, userID, p)
}

func (f fakeBot) SendTestMessage(ctx context.Context, userID string) error {
	if userID == "u_nokey" {
		return secret.ErrNoKey
	}
	l, err := notify.GetLink(f.db, userID)
	if err != nil {
		return err
	}
	if l == nil || l.ChatID == 0 {
		return notify.ErrNotLinked
	}
	return nil
}

func tgEnv(t *testing.T, configured bool) *env {
	e := newEnv(t)
	e.h = NewRouter(Deps{DB: e.db, Now: func() time.Time { return e.now }, Auth: userHeaderAuth, Telegram: fakeBot{on: configured, db: e.db.Write()},
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

	// mutating endpoints need reauth
	require.Equal(t, 403, e.do("POST", "/api/telegram/link-code", "", "", nil).Code)
	require.Equal(t, 403, e.do("DELETE", "/api/telegram", "", "", nil).Code)
	require.Equal(t, 403, e.do("PUT", "/api/telegram", "", "", m{"bot_token": "token"}).Code)

	// test message before linking answers 404
	require.Equal(t, 404, e.do("POST", "/api/telegram/test", "", "", nil).Code)

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

	// test message succeeds when linked
	require.Equal(t, 200, e.do("POST", "/api/telegram/test", "", "", nil).Code)

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

func TestTelegramPutBot(t *testing.T) {
	e := tgEnv(t, true)

	// Bad token answers 400
	w := e.doReauth("PUT", "/api/telegram", "", m{"bot_token": "bad"})
	require.Equal(t, 400, w.Code)

	// Missing token answers 400
	w = e.doReauth("PUT", "/api/telegram", "", m{"bot_token": ""})
	require.Equal(t, 400, w.Code)

	// Missing encryption key answers 503
	w = e.doReauth("PUT", "/api/telegram", "", m{"bot_token": "nokey"})
	require.Equal(t, 503, w.Code)

	// Chat ID taken answers 409
	w = e.doReauth("PUT", "/api/telegram", "", m{"bot_token": "valid_token", "chat_id": 88888})
	require.Equal(t, 409, w.Code)

	// Test message with missing key answers 503
	w = e.do("POST", "/api/telegram/test", "u_nokey", "", nil)
	require.Equal(t, 503, w.Code)

	// Valid setup with token and chat_id
	w = e.doReauth("PUT", "/api/telegram", "", m{"bot_token": "valid_token", "chat_id": 12345})
	require.Equal(t, 200, w.Code, w.Body.String())
	var st map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &st))
	require.Equal(t, true, st["linked"])
	require.Equal(t, "daka_bot", st["bot_username"])
}

func TestTelegramNotConfigured(t *testing.T) {
	e := tgEnv(t, false)
	w := e.do("GET", "/api/telegram", "", "", nil)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"configured":false`)
	require.Equal(t, 503, e.doReauth("POST", "/api/telegram/link-code", "", nil).Code)
	require.Equal(t, 503, e.doReauth("PUT", "/api/telegram", "", m{"bot_token": "abc"}).Code)
	require.Equal(t, 503, e.do("POST", "/api/telegram/test", "", "", nil).Code)

	e.h = NewRouter(Deps{DB: e.db, Auth: userHeaderAuth}) // nil Telegram dep
	require.Contains(t, e.do("GET", "/api/telegram", "", "", nil).Body.String(), `"configured":false`)
}

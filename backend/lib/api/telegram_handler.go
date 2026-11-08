package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/notify"
)

// TelegramBot is what the API needs from the notify service.
type TelegramBot interface {
	Configured() bool
	BotUsername(ctx context.Context) string
}

type telegramStatus struct {
	Configured      bool       `json:"configured"`
	Linked          bool       `json:"linked"`
	Enabled         bool       `json:"enabled"`
	RemindSensitive bool       `json:"remind_sensitive"`
	DigestTime      string     `json:"digest_time"`
	LinkedAt        *time.Time `json:"linked_at"`
	BotUsername     string     `json:"bot_username,omitempty"`
}

func (s *server) telegramConfigured() bool { return s.telegram != nil && s.telegram.Configured() }

func (s *server) telegramStatusFor(r *http.Request, l *dbmodels.TelegramLink) telegramStatus {
	st := telegramStatus{Configured: s.telegramConfigured(), DigestTime: dbmodels.DefaultDigestTime, RemindSensitive: true}
	if st.Configured {
		st.BotUsername = s.telegram.BotUsername(r.Context())
	}
	if l != nil {
		st.Linked, st.Enabled, st.RemindSensitive, st.DigestTime = true, l.Enabled, l.RemindSensitive, l.DigestTime
		t := l.LinkedAt.UTC()
		st.LinkedAt = &t
	}
	return st
}

// GET /api/telegram: link status and preferences (never the chat id or token).
func (s *server) getTelegram(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	l, err := notify.GetLink(s.db.Read(), userID)
	if err != nil {
		internalError(w, err, "load telegram link failed")
		return
	}
	jsonResult(http.StatusOK, s.telegramStatusFor(r, l)).write(w)
}

// POST /api/telegram/link-code (behind reauth): a one-time code valid 10 minutes.
func (s *server) postTelegramLinkCode(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	if !s.telegramConfigured() {
		errResult(http.StatusServiceUnavailable, "telegram is not configured").write(w)
		return
	}
	code, exp, err := notify.CreateLinkCode(s.db.Write(), userID, s.now())
	if err != nil {
		internalError(w, err, "create telegram link code failed")
		return
	}
	resp := map[string]any{"code": code, "expires_at": exp}
	if name := s.telegram.BotUsername(r.Context()); name != "" {
		resp["deep_link"] = "https://t.me/" + name + "?start=" + url.QueryEscape(code)
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResult(http.StatusCreated, resp).write(w)
}

// DELETE /api/telegram (behind reauth): unlink; idempotent.
func (s *server) deleteTelegram(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	if err := notify.Unlink(s.db.Write(), userID); err != nil {
		internalError(w, err, "unlink telegram failed")
		return
	}
	jsonResult(http.StatusOK, map[string]any{"unlinked": true}).write(w)
}

type telegramPatch struct {
	Enabled         *bool   `json:"enabled"`
	RemindSensitive *bool   `json:"remind_sensitive"`
	DigestTime      *string `json:"digest_time"`
}

// PATCH /api/telegram/settings: enabled, remind_sensitive, digest_time ("HH:MM"
// in the user's time zone, "" disables the digest).
func (s *server) patchTelegramSettings(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	var req telegramPatch
	if !decodeStrict(w, r, &req) {
		return
	}
	l, err := notify.UpdateSettings(s.db.Write(), userID, notify.SettingsPatch{
		Enabled: req.Enabled, RemindSensitive: req.RemindSensitive, DigestTime: req.DigestTime})
	switch {
	case errors.Is(err, notify.ErrInvalidTime):
		errResult(http.StatusBadRequest, "digest_time must be HH:MM or empty").write(w)
	case errors.Is(err, gorm.ErrRecordNotFound):
		errResult(http.StatusNotFound, "telegram is not linked").write(w)
	case err != nil:
		internalError(w, err, "update telegram settings failed")
	default:
		jsonResult(http.StatusOK, s.telegramStatusFor(r, l)).write(w)
	}
}

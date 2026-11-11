package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/notify"
	"github.com/elmhuangyu/daka/backend/lib/secret"
)

// TelegramBot is what the API needs from the notify service.
type TelegramBot interface {
	Configured() bool
	BotUsername(ctx context.Context) string
	GetStatus(ctx context.Context, userID string) (notify.TelegramStatus, error)
	SetBot(ctx context.Context, userID, token string, chatID int64) (*dbmodels.TelegramLink, error)
	CreateLinkCode(ctx context.Context, userID string) (code string, exp time.Time, deepLink string, err error)
	Unlink(ctx context.Context, userID string) error
	UpdateSettings(ctx context.Context, userID string, p notify.SettingsPatch) (*dbmodels.TelegramLink, error)
	SendTestMessage(ctx context.Context, userID string) error
}

func (s *server) telegramConfigured() bool { return s.telegram != nil && s.telegram.Configured() }

// GET /api/telegram: link status and preferences (never the chat id or token).
func (s *server) getTelegram(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	if s.telegram == nil {
		jsonResult(http.StatusOK, notify.TelegramStatus{
			Configured:      false,
			DigestTime:      dbmodels.DefaultDigestTime,
			RemindSensitive: true,
		}).write(w)
		return
	}
	st, err := s.telegram.GetStatus(r.Context(), userID)
	if err != nil {
		internalError(w, err, "load telegram link failed")
		return
	}
	jsonResult(http.StatusOK, st).write(w)
}

type telegramBotReq struct {
	BotToken string `json:"bot_token"`
	ChatID   *int64 `json:"chat_id"`
}

// PUT /api/telegram (behind reauth): configure or update personal bot token and/or chat ID.
func (s *server) putTelegram(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	if !s.telegramConfigured() {
		errResult(http.StatusServiceUnavailable, "telegram is not configured").write(w)
		return
	}
	var req telegramBotReq
	if !decodeStrict(w, r, &req) {
		return
	}
	chatID := int64(0)
	if req.ChatID != nil {
		chatID = *req.ChatID
	}
	_, err := s.telegram.SetBot(r.Context(), userID, req.BotToken, chatID)
	switch {
	case errors.Is(err, notify.ErrInvalidToken):
		errResult(http.StatusBadRequest, "invalid bot token").write(w)
	case errors.Is(err, notify.ErrBotTokenRequired):
		errResult(http.StatusBadRequest, "bot token is required").write(w)
	case errors.Is(err, notify.ErrChatTaken):
		errResult(http.StatusConflict, "chat already linked to another user").write(w)
	case errors.Is(err, secret.ErrNoKey):
		errResult(http.StatusServiceUnavailable, "encryption key not configured").write(w)
	case err != nil:
		internalError(w, err, "set telegram bot failed")
	default:
		st, _ := s.telegram.GetStatus(r.Context(), userID)
		jsonResult(http.StatusOK, st).write(w)
	}
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
	code, exp, deepLink, err := s.telegram.CreateLinkCode(r.Context(), userID)
	if err != nil {
		if errors.Is(err, notify.ErrNotConfigured) {
			errResult(http.StatusBadRequest, "bot token not configured").write(w)
			return
		}
		internalError(w, err, "create telegram link code failed")
		return
	}
	resp := map[string]any{"code": code, "expires_at": exp}
	if deepLink != "" {
		resp["deep_link"] = deepLink
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResult(http.StatusCreated, resp).write(w)
}

// POST /api/telegram/test: send a test notification to the user's linked chat.
func (s *server) postTelegramTest(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	if !s.telegramConfigured() {
		errResult(http.StatusServiceUnavailable, "telegram is not configured").write(w)
		return
	}
	err := s.telegram.SendTestMessage(r.Context(), userID)
	switch {
	case errors.Is(err, notify.ErrNotLinked):
		errResult(http.StatusNotFound, "telegram is not linked").write(w)
	case errors.Is(err, secret.ErrNoKey):
		errResult(http.StatusServiceUnavailable, "encryption key not configured").write(w)
	case err != nil:
		errResult(http.StatusBadGateway, "telegram send failed").write(w)
	default:
		jsonResult(http.StatusOK, map[string]any{"sent": true, "ok": true}).write(w)
	}
}

// DELETE /api/telegram (behind reauth): unlink; idempotent.
func (s *server) deleteTelegram(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	if s.telegram != nil {
		if err := s.telegram.Unlink(r.Context(), userID); err != nil {
			internalError(w, err, "unlink telegram failed")
			return
		}
	} else {
		if err := notify.Unlink(s.db.Write(), userID); err != nil {
			internalError(w, err, "unlink telegram failed")
			return
		}
	}
	jsonResult(http.StatusOK, map[string]any{"unlinked": true}).write(w)
}

type telegramPatch struct {
	Enabled         *bool   `json:"enabled"`
	RemindSensitive *bool   `json:"remind_sensitive"`
	DigestTime      *string `json:"digest_time"`
	Lang            *string `json:"lang"`
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
	patch := notify.SettingsPatch{
		Enabled: req.Enabled, RemindSensitive: req.RemindSensitive, DigestTime: req.DigestTime, Lang: req.Lang,
	}
	var err error
	if s.telegram != nil {
		_, err = s.telegram.UpdateSettings(r.Context(), userID, patch)
	} else {
		_, err = notify.UpdateSettings(s.db.Write(), userID, patch)
	}
	switch {
	case errors.Is(err, notify.ErrInvalidTime):
		errResult(http.StatusBadRequest, "digest_time must be HH:MM or empty").write(w)
	case errors.Is(err, notify.ErrInvalidLang):
		errResult(http.StatusBadRequest, "unsupported lang").write(w)
	case errors.Is(err, gorm.ErrRecordNotFound):
		errResult(http.StatusNotFound, "telegram is not linked").write(w)
	case err != nil:
		internalError(w, err, "update telegram settings failed")
	default:
		st, _ := s.telegram.GetStatus(r.Context(), userID)
		jsonResult(http.StatusOK, st).write(w)
	}
}

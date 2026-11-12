package notify

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
	"github.com/elmhuangyu/daka/backend/lib/secret"
)

const (
	pollTimeout    = 25 * time.Second
	maxBadAttempts = 5
	badWindow      = 15 * time.Minute
)

// TelegramStatus is the user-facing status of their Telegram bot configuration.
type TelegramStatus struct {
	Configured      bool   `json:"configured"`
	HasToken        bool   `json:"has_token"`
	Linked          bool   `json:"linked"`
	Enabled         bool   `json:"enabled"`
	RemindSensitive bool   `json:"remind_sensitive"`
	DigestTime      string `json:"digest_time"`
	// Timezone is the IANA zone digest_time is interpreted in (the user's Daka time zone).
	Timezone string `json:"timezone"`
	// Lang is the language of bot messages.
	Lang        string     `json:"lang"`
	LinkedAt    *time.Time `json:"linked_at"`
	BotUsername string     `json:"bot_username,omitempty"`
}

// Options configure a Service.
type Options struct {
	DB   *gorm.DB
	Keys *secret.Keyring
	// Token is the optional fallback bot token (tests only; the server does not set it, bots are per-user).
	Token string
	// BaseURL overrides the Telegram API (tests only).
	BaseURL  string
	Now      func() time.Time
	Calendar func(dbmodels.HolidayRegion) schedule.DayCalendar
	// Gap between sends; zero means the scheduler default.
	Gap time.Duration
	// NoPollWait disables the sleep between failed polls (tests).
	NoPollWait bool
}

// Service runs the reminder scheduler and the long-poll link listener.
type Service struct {
	opts   Options
	client *Client
	sched  *Scheduler

	mu       sync.Mutex
	username string
	bad      map[int64]badState
	offset   int64

	syncMu   sync.Mutex
	lastSync map[string]time.Time
}

type badState struct {
	n     int
	until time.Time
}

// New returns a Service; without keys or a token it is inert (Configured false).
func New(o Options) *Service {
	s := &Service{opts: o, bad: map[int64]badState{}}
	if o.Token != "" {
		s.client = NewClient(o.Token, o.BaseURL)
	}
	gap := o.Gap
	if gap == 0 {
		gap = 100 * time.Millisecond
	}
	s.sched = &Scheduler{
		DB:           o.DB,
		Keys:         o.Keys,
		BaseURL:      o.BaseURL,
		DefaultToken: o.Token,
		Now:          o.Now,
		Calendar:     o.Calendar,
		Gap:          gap,
	}
	return s
}

// Configured reports whether Telegram is configured (keys available or token set).
func (s *Service) Configured() bool { return s != nil && (s.opts.Keys != nil || s.client != nil) }

// Scheduler exposes the scheduler (tests).
func (s *Service) Scheduler() *Scheduler { return s.sched }

// BotUsername returns the bot's username for the deep link ("" when unknown).
func (s *Service) BotUsername(ctx context.Context) string {
	if !s.Configured() || s.client == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.username == "" {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		name, err := s.client.BotUsername(cctx)
		if err != nil {
			log.Warn().Err(err).Msg("notify: getMe failed")
			return ""
		}
		s.username = name
	}
	return s.username
}

// Run starts the scheduler and the link listener and blocks until ctx ends.
func (s *Service) Run(ctx context.Context) {
	if !s.Configured() {
		return
	}
	if s.client != nil {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); s.sched.Run(ctx) }()
		go func() { defer wg.Done(); s.pollLoop(ctx) }()
		wg.Wait()
		return
	}
	s.sched.Run(ctx)
}

func (s *Service) pollLoop(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		if err := s.PollOnce(ctx, pollTimeout); err != nil {
			if ctx.Err() != nil {
				return
			}
			wait := backoff
			var ae *APIError
			if errors.As(err, &ae) && ae.RetryAfter > 0 {
				wait = min(ae.RetryAfter, time.Hour)
			}
			log.Warn().Err(err).Msg("notify: getUpdates failed")
			if !s.opts.NoPollWait {
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
			}
			backoff = min(backoff*2, time.Minute)
			continue
		}
		backoff = time.Second
	}
}

func (s *Service) now() time.Time {
	if s.opts.Now != nil {
		return s.opts.Now()
	}
	return time.Now()
}

// PollOnce fetches one batch of updates and handles them.
func (s *Service) PollOnce(ctx context.Context, timeout time.Duration) error {
	ups, err := s.client.GetUpdates(ctx, s.offset, timeout)
	if err != nil {
		return err
	}
	for _, u := range ups {
		if u.UpdateID >= s.offset {
			s.offset = u.UpdateID + 1
		}
		s.handle(ctx, u)
	}
	return nil
}

// handle accepts only private-chat text: "/start <code>" or a bare code.
func (s *Service) handle(ctx context.Context, u Update) {
	if u.Message == nil || u.Message.Chat.Type != "private" {
		return
	}
	chat := u.Message.Chat.ID
	text := strings.TrimSpace(u.Message.Text)
	code := text
	if f := strings.Fields(text); len(f) > 0 && (f[0] == "/start" || strings.HasPrefix(f[0], "/start@")) {
		code = ""
		if len(f) > 1 {
			code = f[1]
		}
	}
	reply := func(msg string) { _ = s.client.SendMessage(ctx, chat, msg) }
	if code == "" {
		if strings.HasPrefix(text, "/start") {
			reply("Send the link code from Daka settings to connect this chat.")
		}
		return
	}
	if len(code) > 128 || strings.HasPrefix(code, "/") {
		return
	}
	now := s.now()
	s.mu.Lock()
	b := s.bad[chat]
	if now.Before(b.until) {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	link, err := Bind(s.opts.DB, code, chat, now)
	switch {
	case err == nil:
		s.mu.Lock()
		delete(s.bad, chat)
		s.mu.Unlock()
		reply(msgsFor(link.Lang).linkedText(link.DigestTime))
	case errors.Is(err, ErrChatTaken):
		reply(msgsFor("").chatTaken)
	case errors.Is(err, ErrInvalidCode):
		s.mu.Lock()
		b.n++
		if b.n >= maxBadAttempts {
			b = badState{until: now.Add(badWindow)}
		}
		s.bad[chat] = b
		s.mu.Unlock()
		reply(msgsFor("").invalidCode)
	default:
		log.Error().Err(err).Msg("notify: bind failed")
	}
}

func (s *Service) decryptToken(userID, enc string) (string, error) {
	if enc == "" {
		if s.opts.Token != "" {
			return s.opts.Token, nil
		}
		return "", ErrNotConfigured
	}
	if s.opts.Keys == nil {
		return "", secret.ErrNoKey
	}
	b, err := s.opts.Keys.Decrypt(enc, TokenAAD(userID))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// SetBot configures or updates the bot token and/or chat ID for a user.
func (s *Service) SetBot(ctx context.Context, userID, token string, chatID int64) (*dbmodels.TelegramLink, error) {
	token = strings.TrimSpace(token)
	var existing dbmodels.TelegramLink
	hasExisting := false
	err := s.opts.DB.Where("user_id = ?", userID).Take(&existing).Error
	if err == nil {
		hasExisting = true
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	if token == "" {
		if !hasExisting || (existing.TokenEnc == "" && s.opts.Token == "") {
			return nil, ErrBotTokenRequired
		}
		if chatID != 0 {
			var n int64
			if err := s.opts.DB.Model(&dbmodels.TelegramLink{}).Where("chat_id = ? AND user_id <> ?", chatID, userID).Count(&n).Error; err != nil {
				return nil, err
			}
			if n > 0 {
				return nil, ErrChatTaken
			}
			existing.ChatID = chatID
			existing.Enabled = true
			existing.LinkedAt = s.now().UTC()
			if err := s.opts.DB.Save(&existing).Error; err != nil {
				return nil, mapUnique(err)
			}
		}
		return &existing, nil
	}

	if s.opts.Keys == nil {
		return nil, secret.ErrNoKey
	}

	client := NewClient(token, s.opts.BaseURL)
	botUsername, err := client.BotUsername(ctx)
	if err != nil {
		var ae *APIError
		if errors.As(err, &ae) && (ae.Code == 401 || ae.Code == 404) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}

	enc, err := s.opts.Keys.Encrypt([]byte(token), TokenAAD(userID))
	if err != nil {
		return nil, err
	}

	now := s.now().UTC()
	if chatID == 0 {
		if hasExisting {
			existing.TokenEnc = enc
			existing.BotUsername = botUsername
			if err := s.opts.DB.Save(&existing).Error; err != nil {
				return nil, mapUnique(err)
			}
			return &existing, nil
		}
		_, _, err := CreateLinkCodeWithToken(s.opts.DB, userID, enc, botUsername, now)
		if err != nil {
			return nil, err
		}
		return nil, nil
	}

	var out dbmodels.TelegramLink
	err = s.opts.DB.Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&dbmodels.TelegramLink{}).Where("chat_id = ? AND user_id <> ?", chatID, userID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return ErrChatTaken
		}
		if hasExisting {
			out = existing
			out.TokenEnc = enc
			out.BotUsername = botUsername
			out.ChatID = chatID
			out.Enabled = true
			out.LinkedAt = now
			return mapUnique(tx.Save(&out).Error)
		}
		out = dbmodels.TelegramLink{
			UserID:          userID,
			TokenEnc:        enc,
			BotUsername:     botUsername,
			ChatID:          chatID,
			Enabled:         true,
			RemindSensitive: true,
			DigestTime:      dbmodels.DefaultDigestTime,
			LinkedAt:        now,
		}
		return mapUnique(tx.Create(&out).Error)
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateLinkCode generates a one-time link code and deep link for the user.
func (s *Service) CreateLinkCode(ctx context.Context, userID string) (string, time.Time, string, error) {
	if !s.Configured() {
		return "", time.Time{}, "", ErrNotConfigured
	}
	l, err := GetLink(s.opts.DB, userID)
	if err != nil {
		return "", time.Time{}, "", err
	}
	botName := ""
	tokenEnc := ""
	if l != nil {
		botName = l.BotUsername
		tokenEnc = l.TokenEnc
	}
	if botName == "" && s.client != nil {
		botName = s.BotUsername(ctx)
	}
	if tokenEnc == "" {
		var existingCode dbmodels.TelegramLinkCode
		if s.opts.DB.Where("user_id = ? AND expires_at > ?", userID, s.now().UTC()).Take(&existingCode).Error == nil {
			tokenEnc = existingCode.TokenEnc
			if botName == "" {
				botName = existingCode.BotUsername
			}
		}
	}
	if tokenEnc == "" && s.opts.Token == "" {
		return "", time.Time{}, "", ErrNotConfigured
	}
	code, exp, err := CreateLinkCodeWithToken(s.opts.DB, userID, tokenEnc, botName, s.now())
	if err != nil {
		return "", time.Time{}, "", err
	}
	deepLink := ""
	if botName != "" {
		deepLink = "https://t.me/" + botName + "?start=" + url.QueryEscape(code)
	}
	return code, exp, deepLink, nil
}

// SyncLink checks getUpdates for this user's bot to detect if the user sent /start with their link code.
func (s *Service) SyncLink(ctx context.Context, userID string) (*dbmodels.TelegramLink, error) {
	if s.client != nil {
		// When a global bot poller is active, it runs pollLoop and binds incoming updates.
		l, _ := GetLink(s.opts.DB, userID)
		return l, nil
	}
	s.syncMu.Lock()
	if s.lastSync == nil {
		s.lastSync = make(map[string]time.Time)
	}
	last := s.lastSync[userID]
	if !last.IsZero() && s.now().Sub(last) < 3*time.Second {
		s.syncMu.Unlock()
		l, _ := GetLink(s.opts.DB, userID)
		return l, nil
	}
	s.lastSync[userID] = s.now()
	s.syncMu.Unlock()

	l, _ := GetLink(s.opts.DB, userID)
	if l != nil && l.ChatID != 0 {
		return l, nil
	}
	var codes []dbmodels.TelegramLinkCode
	now := s.now().UTC()
	if err := s.opts.DB.Where("user_id = ? AND expires_at > ?", userID, now).Find(&codes).Error; err != nil {
		return nil, err
	}
	if len(codes) == 0 {
		return l, nil
	}
	tokenEnc := ""
	if l != nil {
		tokenEnc = l.TokenEnc
	}
	if tokenEnc == "" {
		tokenEnc = codes[0].TokenEnc
	}
	token, err := s.decryptToken(userID, tokenEnc)
	if err != nil {
		return nil, err
	}
	client := NewClient(token, s.opts.BaseURL)
	ups, err := client.GetUpdates(ctx, 0, 0)
	if err != nil {
		return nil, err
	}
	var matchedChatID int64
	var matchedCode string
	for _, u := range ups {
		if u.Message == nil || u.Message.Chat.Type != "private" {
			continue
		}
		text := strings.TrimSpace(u.Message.Text)
		code := text
		if f := strings.Fields(text); len(f) > 0 && (f[0] == "/start" || strings.HasPrefix(f[0], "/start@")) {
			code = ""
			if len(f) > 1 {
				code = f[1]
			}
		}
		if code != "" {
			for _, c := range codes {
				if hashCode(code) == c.CodeHash {
					matchedChatID = u.Message.Chat.ID
					matchedCode = code
					break
				}
			}
		}
		if matchedChatID != 0 {
			break
		}
	}
	if matchedChatID == 0 || matchedCode == "" {
		return l, nil
	}
	bound, err := Bind(s.opts.DB, matchedCode, matchedChatID, s.now())
	if err != nil {
		return nil, err
	}
	_ = client.SendMessage(ctx, bound.ChatID, msgsFor(bound.Lang).linkedText(bound.DigestTime))
	return bound, nil
}

// GetStatus returns the TelegramStatus for a user, syncing pending codes if unlinked.
func (s *Service) GetStatus(ctx context.Context, userID string) (TelegramStatus, error) {
	st := TelegramStatus{
		Configured:      s.Configured(),
		DigestTime:      dbmodels.DefaultDigestTime,
		RemindSensitive: true,
		Timezone:        userTimezone(s.opts.DB, userID),
	}
	if !st.Configured {
		return st, nil
	}
	l, err := GetLink(s.opts.DB, userID)
	if err != nil {
		return st, err
	}
	var code dbmodels.TelegramLinkCode
	hasCode := s.opts.DB.Where("user_id = ? AND expires_at > ?", userID, s.now().UTC()).Take(&code).Error == nil
	if l != nil && l.ChatID != 0 {
		st.HasToken = l.TokenEnc != ""
		st.BotUsername = l.BotUsername
		st.Linked = true
		st.Enabled = l.Enabled
		st.RemindSensitive = l.RemindSensitive
		st.DigestTime = l.DigestTime
		st.Lang = l.Lang
		t := l.LinkedAt.UTC()
		st.LinkedAt = &t
	} else if hasCode {
		st.HasToken = code.TokenEnc != ""
		st.BotUsername = code.BotUsername
		if s.client == nil {
			if synced, err := s.SyncLink(ctx, userID); err == nil && synced != nil && synced.ChatID != 0 {
				st.HasToken = synced.TokenEnc != ""
				st.BotUsername = synced.BotUsername
				st.Linked = true
				st.Enabled = synced.Enabled
				st.RemindSensitive = synced.RemindSensitive
				st.DigestTime = synced.DigestTime
				st.Lang = synced.Lang
				t := synced.LinkedAt.UTC()
				st.LinkedAt = &t
			}
		}
	} else if l != nil {
		st.HasToken = l.TokenEnc != ""
		st.BotUsername = l.BotUsername
	}
	if st.BotUsername == "" && s.client != nil {
		st.BotUsername = s.BotUsername(ctx)
	}
	return st, nil
}

// Unlink unlinks the user and deletes their bot config.
func (s *Service) Unlink(ctx context.Context, userID string) error {
	return Unlink(s.opts.DB, userID)
}

// UpdateSettings updates preferences for the user.
func (s *Service) UpdateSettings(ctx context.Context, userID string, p SettingsPatch) (*dbmodels.TelegramLink, error) {
	return UpdateSettings(s.opts.DB, userID, p)
}

// SendTestMessage sends a test notification to the user's linked chat.
func (s *Service) SendTestMessage(ctx context.Context, userID string) error {
	l, err := GetLink(s.opts.DB, userID)
	if err != nil {
		return err
	}
	if l == nil || l.ChatID == 0 {
		return ErrNotLinked
	}
	token, err := s.decryptToken(userID, l.TokenEnc)
	if err != nil {
		return err
	}
	client := NewClient(token, s.opts.BaseURL)
	return client.SendMessage(ctx, l.ChatID, msgsFor(l.Lang).test)
}

// userTimezone returns the user's IANA time zone (UTC when unset).
func userTimezone(g *gorm.DB, userID string) string {
	var st dbmodels.UserSettings
	if err := g.Where("user_id = ?", userID).Take(&st).Error; err != nil || st.Timezone == "" {
		return "UTC"
	}
	return st.Timezone
}

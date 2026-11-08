package notify

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
)

const (
	pollTimeout    = 25 * time.Second
	maxBadAttempts = 5
	badWindow      = 15 * time.Minute
)

// Options configure a Service.
type Options struct {
	DB *gorm.DB
	// Token is the bot token (from DAKA_TELEGRAM_BOT_TOKEN only). Empty
	// disables everything.
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
}

type badState struct {
	n     int
	until time.Time
}

// New returns a Service; with an empty token it is inert (Configured false).
func New(o Options) *Service {
	s := &Service{opts: o, bad: map[int64]badState{}}
	if o.Token == "" {
		return s
	}
	s.client = NewClient(o.Token, o.BaseURL)
	gap := o.Gap
	if gap == 0 {
		gap = 100 * time.Millisecond
	}
	s.sched = &Scheduler{DB: o.DB, Sender: s.client, Now: o.Now, Calendar: o.Calendar, Gap: gap}
	return s
}

// Configured reports whether a bot token is set.
func (s *Service) Configured() bool { return s != nil && s.client != nil }

// Scheduler exposes the scheduler (tests).
func (s *Service) Scheduler() *Scheduler { return s.sched }

// BotUsername returns the bot's username for the deep link ("" when unknown).
func (s *Service) BotUsername(ctx context.Context) string {
	if !s.Configured() {
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
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); s.sched.Run(ctx) }()
	go func() { defer wg.Done(); s.pollLoop(ctx) }()
	wg.Wait()
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
		msg := "Linked to Daka. You will get a daily digest"
		if link.DigestTime != "" {
			msg += " at " + link.DigestTime
		}
		reply(msg + " (your Daka time zone).")
	case errors.Is(err, ErrChatTaken):
		reply("This chat is already linked to a Daka account.")
	case errors.Is(err, ErrInvalidCode):
		s.mu.Lock()
		b.n++
		if b.n >= maxBadAttempts {
			b = badState{until: now.Add(badWindow)}
		}
		s.bad[chat] = b
		s.mu.Unlock()
		reply("That link code is invalid or expired. Create a new one in Daka settings.")
	default:
		log.Error().Err(err).Msg("notify: bind failed")
	}
}

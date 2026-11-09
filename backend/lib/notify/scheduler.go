package notify

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
	"github.com/elmhuangyu/daka/backend/lib/secret"
)

// PrivateItemText replaces the name of a sensitive item in every message.
const PrivateItemText = "a private item"

const (
	tickInterval = 30 * time.Second
	// itemCatchUp is how late a per-item reminder may still be sent (e.g.
	// after a restart); a digest may be sent any time later the same day.
	itemCatchUp = 3 * time.Hour
	maxListed   = 30
	maxBackoff  = 30 * time.Minute
	logKeepDays = 14
)

// Sender delivers a text message to a chat.
type Sender interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
}

// Scheduler sends the daily digest and per-item reminders. Run it in-process;
// tests call Tick with an injected clock.
type Scheduler struct {
	DB           *gorm.DB
	Keys         *secret.Keyring
	BaseURL      string
	DefaultToken string
	Sender       Sender
	// Now defaults to time.Now.
	Now func() time.Time
	// Calendar maps UserSettings.holiday_region to a holiday calendar; may be nil.
	Calendar func(dbmodels.HolidayRegion) schedule.DayCalendar
	// Gap is the pause between two sends (Telegram flood limits); default 100ms.
	Gap time.Duration

	mu           sync.Mutex
	blockedUntil time.Time
	retry        map[string]retryState
}

type retryState struct {
	fails int
	next  time.Time
}

func (s *Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Run ticks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		s.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick processes every enabled link once.
func (s *Scheduler) Tick(ctx context.Context) {
	if s.blocked() {
		return
	}
	var links []dbmodels.TelegramLink
	if err := s.DB.Where("enabled = ? AND chat_id <> 0", true).Find(&links).Error; err != nil {
		log.Error().Err(err).Msg("notify: list links failed")
		return
	}
	for _, l := range links {
		if ctx.Err() != nil || s.blocked() {
			return
		}
		if err := s.processUser(ctx, l); err != nil {
			log.Error().Err(err).Str("user_id", l.UserID).Msg("notify: user failed")
		}
	}
	cutoff := s.now().AddDate(0, 0, -logKeepDays).Format(dbmodels.DateLayout)
	_ = s.DB.Where("remind_date < ?", cutoff).Delete(&dbmodels.ReminderLog{}).Error
}

func (s *Scheduler) blocked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now().Before(s.blockedUntil)
}

func (s *Scheduler) processUser(ctx context.Context, l dbmodels.TelegramLink) error {
	var st dbmodels.UserSettings
	tz := "UTC"
	var region dbmodels.HolidayRegion
	if err := s.DB.Where("user_id = ?", l.UserID).Take(&st).Error; err == nil {
		region = st.HolidayRegion
		if st.Timezone != "" {
			tz = st.Timezone
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	local := s.now().In(loc)
	date := local.Format(dbmodels.DateLayout)
	hm := local.Format("15:04")

	digestDue := l.DigestTime != "" && hm >= l.DigestTime && !s.sent(l.UserID, 0, date)
	// Candidate item reminders need the pending list, so load lazily.
	if !digestDue && !s.anyRemindTime(l.UserID) {
		return nil
	}
	frozen, err := frozenOn(s.DB, l.UserID, date)
	if err != nil || frozen {
		return err
	}
	var cal schedule.DayCalendar
	if s.Calendar != nil {
		cal = s.Calendar(region)
	}
	pending, err := pendingToday(s.DB, l.UserID, date, cal)
	if err != nil {
		return err
	}
	visible := pending[:0:0]
	for _, e := range pending {
		if e.Sensitive && !l.RemindSensitive {
			continue
		}
		visible = append(visible, e)
	}

	if digestDue {
		s.deliver(ctx, l, 0, date, digestText(local, visible))
	}
	for _, e := range visible {
		if e.RemindTime == "" || hm < e.RemindTime || s.sent(l.UserID, e.ID, date) {
			continue
		}
		if late, ok := lateness(local, e.RemindTime); !ok || late > itemCatchUp {
			continue
		}
		if ctx.Err() != nil || s.blocked() {
			return nil
		}
		s.deliver(ctx, l, e.ID, date, itemText(e))
	}
	return nil
}

func (s *Scheduler) anyRemindTime(userID string) bool {
	var n int64
	return s.DB.Model(&dbmodels.Item{}).Where("user_id = ? AND archived = ? AND remind_time IS NOT NULL AND remind_time <> ''", userID, false).
		Limit(1).Count(&n).Error != nil || n > 0
}

// lateness is how long after the local wall-clock time hm the moment local is.
func lateness(local time.Time, hm string) (time.Duration, bool) {
	t, err := time.Parse("15:04", hm)
	if err != nil {
		return 0, false
	}
	at := time.Date(local.Year(), local.Month(), local.Day(), t.Hour(), t.Minute(), 0, 0, local.Location())
	return local.Sub(at), true
}

func (s *Scheduler) sent(userID string, itemID uint, date string) bool {
	var n int64
	err := s.DB.Model(&dbmodels.ReminderLog{}).
		Where("user_id = ? AND item_id = ? AND remind_date = ?", userID, itemID, date).Count(&n).Error
	return err != nil || n > 0
}

// deliver claims the ReminderLog row first (so a crash or concurrent tick never
// double-sends), sends, and releases the claim with a backoff on failure.
func (s *Scheduler) deliver(ctx context.Context, l dbmodels.TelegramLink, itemID uint, date, text string) {
	key := fmt.Sprintf("%s/%d/%s", l.UserID, itemID, date)
	s.mu.Lock()
	rs := s.retry[key]
	s.mu.Unlock()
	if s.now().Before(rs.next) {
		return
	}
	row := dbmodels.ReminderLog{UserID: l.UserID, ItemID: itemID, RemindDate: date, SentAt: s.now().UTC()}
	res := s.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	if res.Error != nil || res.RowsAffected == 0 {
		return
	}
	var snd Sender
	if l.TokenEnc != "" && s.Keys != nil {
		dec, decErr := s.Keys.Decrypt(l.TokenEnc, TokenAAD(l.UserID))
		if decErr != nil {
			_ = s.DB.Where("user_id = ? AND item_id = ? AND remind_date = ?", l.UserID, itemID, date).Delete(&dbmodels.ReminderLog{}).Error
			log.Error().Err(decErr).Str("user_id", l.UserID).Msg("notify: decrypt bot token failed")
			s.mu.Lock()
			if s.retry == nil {
				s.retry = map[string]retryState{}
			}
			rs.fails++
			rs.next = s.now().Add(min(time.Minute<<min(rs.fails-1, 5), maxBackoff))
			s.retry[key] = rs
			s.mu.Unlock()
			return
		}
		snd = NewClient(string(dec), s.BaseURL)
	} else if s.Sender != nil {
		snd = s.Sender
	} else if s.DefaultToken != "" {
		snd = NewClient(s.DefaultToken, s.BaseURL)
	} else {
		_ = s.DB.Where("user_id = ? AND item_id = ? AND remind_date = ?", l.UserID, itemID, date).Delete(&dbmodels.ReminderLog{}).Error
		s.mu.Lock()
		if s.retry == nil {
			s.retry = map[string]retryState{}
		}
		rs.fails++
		rs.next = s.now().Add(min(time.Minute<<min(rs.fails-1, 5), maxBackoff))
		s.retry[key] = rs
		s.mu.Unlock()
		return
	}
	err := snd.SendMessage(ctx, l.ChatID, text)
	if err == nil {
		s.mu.Lock()
		delete(s.retry, key)
		s.mu.Unlock()
		if s.Gap > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(s.Gap):
			}
		}
		return
	}
	_ = s.DB.Where("user_id = ? AND item_id = ? AND remind_date = ?", l.UserID, itemID, date).Delete(&dbmodels.ReminderLog{}).Error
	var ae *APIError
	if errors.As(err, &ae) {
		if ae.Forbidden() { // blocked the bot / chat gone: stop messaging until re-enabled
			_ = s.DB.Model(&dbmodels.TelegramLink{}).Where("user_id = ?", l.UserID).UpdateColumn("enabled", false).Error
			log.Warn().Str("user_id", l.UserID).Msg("notify: chat refused the bot; reminders disabled")
			return
		}
		if ae.RetryAfter > 0 {
			wait := min(ae.RetryAfter, time.Hour)
			s.mu.Lock()
			s.blockedUntil = s.now().Add(wait)
			s.mu.Unlock()
		}
	}
	s.mu.Lock()
	if s.retry == nil {
		s.retry = map[string]retryState{}
	}
	rs.fails++
	rs.next = s.now().Add(min(time.Minute<<min(rs.fails-1, 5), maxBackoff))
	s.retry[key] = rs
	s.mu.Unlock()
	log.Warn().Err(err).Str("user_id", l.UserID).Int("fails", rs.fails).Msg("notify: send failed")
}

func itemName(e entry) string {
	if e.Sensitive {
		return PrivateItemText
	}
	return e.Name
}

func itemText(e entry) string {
	if e.OverdueDays > 0 {
		return fmt.Sprintf("Reminder: %s is overdue by %d day(s).", itemName(e), e.OverdueDays)
	}
	return "Reminder: " + itemName(e)
}

func digestText(local time.Time, pending []entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Daka - %s\n", local.Format("Monday 2006-01-02"))
	var due, overdue []entry
	for _, e := range pending {
		if e.OverdueDays > 0 {
			overdue = append(overdue, e)
		} else {
			due = append(due, e)
		}
	}
	if len(pending) == 0 {
		b.WriteString("Nothing due today.")
		return b.String()
	}
	listed := 0
	section := func(title string, es []entry) {
		if len(es) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s (%d):\n", title, len(es))
		for _, e := range es {
			if listed >= maxListed || b.Len() > 3500 {
				b.WriteString("- ...and more\n")
				return
			}
			listed++
			if e.OverdueDays > 0 {
				fmt.Fprintf(&b, "- %s (%d day(s) overdue)\n", itemName(e), e.OverdueDays)
			} else {
				fmt.Fprintf(&b, "- %s\n", itemName(e))
			}
		}
	}
	section("Due today", due)
	section("Maintenance overdue", overdue)
	return strings.TrimRight(b.String(), "\n")
}

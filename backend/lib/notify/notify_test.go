package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/config"
	"github.com/elmhuangyu/daka/backend/lib/db"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
	"github.com/elmhuangyu/daka/backend/lib/secret"
)

const testToken = "123456:SECRET-TOKEN-VALUE"

type sentMsg struct {
	Chat int64
	Text string
}

// fakeTelegram is an in-process Telegram Bot API; the real API is never called.
type fakeTelegram struct {
	*httptest.Server
	mu      sync.Mutex
	sent    []sentMsg
	updates []Update
	sendErr func(n int) (int, string) // returns HTTP/error code + body for the nth send, 0 = ok
	paths   []string
}

func newFake(t *testing.T) *fakeTelegram {
	f := &fakeTelegram{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.paths = append(f.paths, r.URL.Path)
		if !strings.HasPrefix(r.URL.Path, "/bot"+testToken+"/") {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		method := strings.TrimPrefix(r.URL.Path, "/bot"+testToken+"/")
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "getMe":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"username":"daka_bot"}}`))
		case "getUpdates":
			b, _ := json.Marshal(f.updates)
			f.updates = nil
			_, _ = w.Write([]byte(`{"ok":true,"result":` + string(b) + `}`))
		case "sendMessage":
			var m struct {
				ChatID int64  `json:"chat_id"`
				Text   string `json:"text"`
			}
			_ = json.Unmarshal(body, &m)
			if f.sendErr != nil {
				if code, resp := f.sendErr(len(f.sent) + 1); code != 0 {
					w.WriteHeader(code)
					_, _ = w.Write([]byte(resp))
					return
				}
			}
			f.sent = append(f.sent, sentMsg{m.ChatID, m.Text})
			_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeTelegram) messages() []sentMsg {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentMsg(nil), f.sent...)
}

func (f *fakeTelegram) reset() {
	f.mu.Lock()
	f.sent = nil
	f.mu.Unlock()
}

type fakeCal struct{ holidays map[string]bool }

func (c fakeCal) IsStatutoryHoliday(d string) bool { return c.holidays[d] }
func (c fakeCal) IsWorkday(d string) bool          { return !c.holidays[d] }
func (c fakeCal) IsMakeupWorkday(string) bool      { return false }

type env struct {
	t   *testing.T
	g   *gorm.DB
	tg  *fakeTelegram
	now time.Time
	s   *Scheduler
}

func newEnv(t *testing.T, tz string, now time.Time) *env {
	t.Helper()
	d, err := db.Open(config.DBConfig{Driver: config.DriverSQLite, DSN: config.Secret(filepath.Join(t.TempDir(), "t.db"))})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	require.NoError(t, dbmodels.AutoMigrate(d.Write()))
	e := &env{t: t, g: d.Write(), tg: newFake(t), now: now}
	require.NoError(t, e.g.Create(&dbmodels.User{ID: "u1"}).Error)
	require.NoError(t, e.g.Create(&dbmodels.UserSettings{UserID: "u1", Timezone: tz, HolidayRegion: dbmodels.RegionCN}).Error)
	require.NoError(t, e.g.Create(&dbmodels.TelegramLink{UserID: "u1", ChatID: 42, Enabled: true, RemindSensitive: true,
		DigestTime: "08:00", LinkedAt: now}).Error)
	e.s = e.newScheduler()
	return e
}

func (e *env) newScheduler() *Scheduler {
	return &Scheduler{DB: e.g, Sender: NewClient(testToken, e.tg.URL), Now: func() time.Time { return e.now },
		Calendar: func(dbmodels.HolidayRegion) schedule.DayCalendar {
			return fakeCal{holidays: map[string]bool{"2026-11-10": true}}
		}}
}

func (e *env) tick() { e.s.Tick(context.Background()) }

func (e *env) at(s string) { e.now = mustTime(e.t, s) }

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err)
	return v
}

func (e *env) item(name string, mut func(*dbmodels.Item), rule dbmodels.ItemRuleVersion) uint {
	e.t.Helper()
	it := dbmodels.Item{UserID: "u1", Name: name, TrackerType: dbmodels.TrackerBoolean, Kind: dbmodels.KindHabit,
		Polarity: dbmodels.PolarityPositive}
	if mut != nil {
		mut(&it)
	}
	require.NoError(e.t, e.g.Create(&it).Error)
	rule.ItemID = it.ID
	if rule.EffectiveFrom == "" {
		rule.EffectiveFrom = "2026-01-01"
	}
	if rule.ScheduleType == "" {
		rule.ScheduleType = dbmodels.ScheduleFixedRRule
		rule.RRuleString = "FREQ=DAILY"
		rule.DtStart = "2026-01-01"
	}
	if rule.HolidayMode == "" {
		rule.HolidayMode = dbmodels.HolidayNone
	}
	require.NoError(e.t, e.g.Create(&rule).Error)
	return it.ID
}

func ptr[T any](v T) *T { return &v }

func (e *env) checkin(id uint, date string) {
	require.NoError(e.t, e.g.Create(&dbmodels.CheckinLog{UserID: "u1", ItemID: id, CheckinDate: date, CheckinTime: e.now}).Error)
}

func TestDigestOncePerDayAtLocalTime(t *testing.T) {
	// 2026-11-08 08:00 in Asia/Shanghai is 00:00Z.
	e := newEnv(t, "Asia/Shanghai", mustTime(t, "2026-11-07T23:59:00Z"))
	e.item("Water plants", nil, dbmodels.ItemRuleVersion{})
	e.item("Read", nil, dbmodels.ItemRuleVersion{})
	e.tick()
	require.Empty(t, e.tg.messages(), "before the digest time")

	e.at("2026-11-08T00:00:00Z")
	e.tick()
	msgs := e.tg.messages()
	require.Len(t, msgs, 1)
	require.EqualValues(t, 42, msgs[0].Chat)
	require.Contains(t, msgs[0].Text, "Sunday 2026-11-08")
	require.Contains(t, msgs[0].Text, "- Water plants")
	require.Contains(t, msgs[0].Text, "Due today (2)")

	e.at("2026-11-08T05:00:00Z")
	e.tick()
	require.Len(t, e.tg.messages(), 1, "deduped within the day")

	// A restart (fresh scheduler, same DB) must not re-send.
	e.s = e.newScheduler()
	e.tick()
	require.Len(t, e.tg.messages(), 1, "deduped across restarts")

	// Next local day sends again.
	e.at("2026-11-09T00:01:00Z")
	e.tick()
	require.Len(t, e.tg.messages(), 2)
}

func TestDigestSkipsDoneAndNonDue(t *testing.T) {
	e := newEnv(t, "UTC", mustTime(t, "2026-11-08T08:00:00Z"))
	done := e.item("Done thing", nil, dbmodels.ItemRuleVersion{})
	e.checkin(done, "2026-11-08")
	e.item("Weekdays only", nil, dbmodels.ItemRuleVersion{RRuleString: "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR", ScheduleType: dbmodels.ScheduleFixedRRule, DtStart: "2026-01-01"})
	e.item("Quota", nil, dbmodels.ItemRuleVersion{ScheduleType: dbmodels.ScheduleQuota})
	e.item("None", nil, dbmodels.ItemRuleVersion{ScheduleType: dbmodels.ScheduleNone})
	e.item("Negative", func(i *dbmodels.Item) { i.Polarity = dbmodels.PolarityNegative }, dbmodels.ItemRuleVersion{})
	e.item("Archived", func(i *dbmodels.Item) { i.Archived = true }, dbmodels.ItemRuleVersion{})
	e.item("Due", nil, dbmodels.ItemRuleVersion{})
	e.tick() // 2026-11-08 is a Sunday
	msgs := e.tg.messages()
	require.Len(t, msgs, 1)
	require.Equal(t, "Daka - Sunday 2026-11-08\n\nDue today (1):\n- Due", msgs[0].Text)
}

func TestDigestEmptyStillSent(t *testing.T) {
	e := newEnv(t, "UTC", mustTime(t, "2026-11-08T08:00:00Z"))
	e.tick()
	msgs := e.tg.messages()
	require.Len(t, msgs, 1)
	require.Contains(t, msgs[0].Text, "Nothing due today.")
}

func TestDigestHolidayAndFreeze(t *testing.T) {
	e := newEnv(t, "UTC", mustTime(t, "2026-11-10T08:00:00Z"))
	e.item("Skips holiday", nil, dbmodels.ItemRuleVersion{HolidayMode: dbmodels.HolidaySkipHolidays})
	e.item("Always", nil, dbmodels.ItemRuleVersion{})
	e.tick()
	msgs := e.tg.messages()
	require.Len(t, msgs, 1)
	require.NotContains(t, msgs[0].Text, "Skips holiday")
	require.Contains(t, msgs[0].Text, "- Always")

	// Frozen days send nothing at all.
	require.NoError(t, e.g.Create(&dbmodels.FreezePeriod{UserID: "u1", StartDate: "2026-11-11", EndDate: "2026-11-12"}).Error)
	e.reset()
	e.at("2026-11-11T09:00:00Z")
	e.tick()
	require.Empty(t, e.tg.messages())
}

func (e *env) reset() { e.tg.reset() }

func TestRollingDueAndOverdue(t *testing.T) {
	e := newEnv(t, "UTC", mustTime(t, "2026-11-08T08:00:00Z"))
	rule := dbmodels.ItemRuleVersion{ScheduleType: dbmodels.ScheduleRolling, RollingInterval: 30, RollingUnit: dbmodels.RollingDay,
		DtStart: "2026-01-01", EffectiveFrom: "2026-01-01"}
	over := e.item("Filter", func(i *dbmodels.Item) { i.TrackerType = dbmodels.TrackerMaintenance }, rule)
	e.checkin(over, "2026-10-01") // next due 2026-10-31 -> 8 days overdue
	fresh := e.item("Smoke alarm", func(i *dbmodels.Item) { i.TrackerType = dbmodels.TrackerMaintenance }, rule)
	e.checkin(fresh, "2026-10-20") // next due 2026-11-19: not yet
	e.tick()
	msgs := e.tg.messages()
	require.Len(t, msgs, 1)
	require.Contains(t, msgs[0].Text, "Maintenance overdue (1):\n- Filter (8 day(s) overdue)")
	require.NotContains(t, msgs[0].Text, "Smoke alarm")
}

func TestRedaction(t *testing.T) {
	e := newEnv(t, "UTC", mustTime(t, "2026-11-08T08:00:00Z"))
	cat := dbmodels.Category{UserID: "u1", Name: "Health", IsSensitive: true}
	require.NoError(t, e.g.Create(&cat).Error)
	proj := dbmodels.Project{UserID: "u1", Name: "Secret Project", IsSensitive: true}
	require.NoError(t, e.g.Create(&proj).Error)
	e.item("Therapy Appointment", func(i *dbmodels.Item) { i.IsSensitive = true; i.RemindTime = ptr("08:00") }, dbmodels.ItemRuleVersion{})
	e.item("Pills", func(i *dbmodels.Item) { i.CategoryID = &cat.ID; i.Description = "private note" }, dbmodels.ItemRuleVersion{})
	e.item("Classified Task", func(i *dbmodels.Item) { i.ProjectID = &proj.ID }, dbmodels.ItemRuleVersion{})
	e.item("Public", nil, dbmodels.ItemRuleVersion{})
	e.tick()
	msgs := e.tg.messages()
	require.Len(t, msgs, 2) // digest + the item reminder
	for _, m := range msgs {
		require.NotContains(t, m.Text, "Therapy")
		require.NotContains(t, m.Text, "Pills")
		require.NotContains(t, m.Text, "Classified")
		require.NotContains(t, m.Text, "private note")
	}
	require.Contains(t, msgs[0].Text, "- "+PrivateItemText)
	require.Contains(t, msgs[0].Text, "- Public")
	require.Equal(t, "Reminder: "+PrivateItemText, msgs[1].Text)

	// remind_sensitive=false: sensitive items disappear entirely.
	e.reset()
	require.NoError(t, e.g.Where("1=1").Delete(&dbmodels.ReminderLog{}).Error)
	require.NoError(t, e.g.Model(&dbmodels.TelegramLink{}).Where("user_id = ?", "u1").Update("remind_sensitive", false).Error)
	e.tick()
	msgs = e.tg.messages()
	require.Len(t, msgs, 1)
	require.NotContains(t, msgs[0].Text, PrivateItemText)
	require.Contains(t, msgs[0].Text, "Due today (1)")
}

func TestItemReminderOnlyWithRemindTime(t *testing.T) {
	e := newEnv(t, "UTC", mustTime(t, "2026-11-08T07:00:00Z"))
	require.NoError(t, e.g.Model(&dbmodels.TelegramLink{}).Where("user_id = ?", "u1").Update("digest_time", "").Error)
	e.item("No time", nil, dbmodels.ItemRuleVersion{})
	id := e.item("Stretch", func(i *dbmodels.Item) { i.RemindTime = ptr("09:30") }, dbmodels.ItemRuleVersion{})
	e.tick()
	require.Empty(t, e.tg.messages(), "digest off, reminder not yet due")

	e.at("2026-11-08T09:30:00Z")
	e.tick()
	e.tick()
	msgs := e.tg.messages()
	require.Len(t, msgs, 1)
	require.Equal(t, "Reminder: Stretch", msgs[0].Text)

	// Done items are not reminded.
	e.reset()
	e.at("2026-11-09T09:30:00Z")
	e.checkin(id, "2026-11-09")
	e.tick()
	require.Empty(t, e.tg.messages())

	// Too late (catch-up window passed) is skipped.
	e.at("2026-11-10T20:00:00Z")
	e.tick()
	require.Empty(t, e.tg.messages())
}

func TestDSTTransitions(t *testing.T) {
	// Fall back: 2026-11-01 08:00 America/New_York (EST) is 13:00Z.
	e := newEnv(t, "America/New_York", mustTime(t, "2026-11-01T12:59:00Z"))
	e.tick()
	require.Empty(t, e.tg.messages())
	e.at("2026-11-01T13:00:00Z")
	e.tick()
	require.Len(t, e.tg.messages(), 1)
	require.Contains(t, e.tg.messages()[0].Text, "2026-11-01")

	// Spring forward: 02:30 does not exist on 2026-03-08; it fires at 03:00 EDT (07:00Z), not before.
	require.NoError(t, e.g.Model(&dbmodels.TelegramLink{}).Where("user_id = ?", "u1").Update("digest_time", "02:30").Error)
	e.reset()
	e.at("2026-03-08T06:59:00Z") // 01:59 EST
	e.tick()
	require.Empty(t, e.tg.messages())
	e.at("2026-03-08T07:00:00Z") // 03:00 EDT
	e.tick()
	require.Len(t, e.tg.messages(), 1)
	require.Contains(t, e.tg.messages()[0].Text, "2026-03-08")
}

func TestUsersAreIndependentAndDisabledSkipped(t *testing.T) {
	e := newEnv(t, "UTC", mustTime(t, "2026-11-08T08:00:00Z"))
	require.NoError(t, e.g.Create(&dbmodels.User{ID: "u2"}).Error)
	require.NoError(t, e.g.Create(&dbmodels.TelegramLink{UserID: "u2", ChatID: 43, Enabled: false, RemindSensitive: true, DigestTime: "08:00"}).Error)
	e.tick()
	msgs := e.tg.messages()
	require.Len(t, msgs, 1)
	require.EqualValues(t, 42, msgs[0].Chat)
}

func TestSendFailureReleasesClaimWithBackoff(t *testing.T) {
	e := newEnv(t, "UTC", mustTime(t, "2026-11-08T08:00:00Z"))
	fail := true
	e.tg.sendErr = func(int) (int, string) {
		if fail {
			return 500, `{"ok":false,"error_code":500,"description":"boom"}`
		}
		return 0, ""
	}
	e.tick()
	require.Empty(t, e.tg.messages())
	var n int64
	require.NoError(t, e.g.Model(&dbmodels.ReminderLog{}).Count(&n).Error)
	require.Zero(t, n, "claim released after failure")

	fail = false
	e.tick()
	require.Empty(t, e.tg.messages(), "backoff holds the retry")
	e.at("2026-11-08T08:01:01Z")
	e.tick()
	require.Len(t, e.tg.messages(), 1)
}

func TestRateLimitBlocksAndForbiddenDisables(t *testing.T) {
	e := newEnv(t, "UTC", mustTime(t, "2026-11-08T08:00:00Z"))
	e.tg.sendErr = func(int) (int, string) {
		return 429, `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":120}}`
	}
	e.tick()
	calls := len(e.tg.paths)
	e.at("2026-11-08T08:01:00Z")
	e.tick()
	require.Equal(t, calls, len(e.tg.paths), "no Telegram call while blocked by retry_after")
	e.tg.sendErr = nil
	e.at("2026-11-08T08:03:00Z")
	e.tick()
	require.Len(t, e.tg.messages(), 1)

	e2 := newEnv(t, "UTC", mustTime(t, "2026-11-08T08:00:00Z"))
	e2.tg.sendErr = func(int) (int, string) {
		return 403, `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`
	}
	e2.tick()
	l, err := GetLink(e2.g, "u1")
	require.NoError(t, err)
	require.False(t, l.Enabled)
}

func TestLongMessageTruncated(t *testing.T) {
	e := newEnv(t, "UTC", mustTime(t, "2026-11-08T08:00:00Z"))
	for i := 0; i < 40; i++ {
		e.item(strings.Repeat("n", 200), nil, dbmodels.ItemRuleVersion{})
	}
	e.tick()
	msgs := e.tg.messages()
	require.Len(t, msgs, 1)
	require.LessOrEqual(t, len([]rune(msgs[0].Text)), MaxMessageRunes)
	require.Contains(t, msgs[0].Text, "...and more")
}

// ---- binding ----

func TestBindingFlow(t *testing.T) {
	e := newEnv(t, "UTC", mustTime(t, "2026-11-08T08:00:00Z"))
	require.NoError(t, Unlink(e.g, "u1"))
	require.NoError(t, e.g.Create(&dbmodels.User{ID: "u2"}).Error)

	code, exp, err := CreateLinkCode(e.g, "u1", e.now)
	require.NoError(t, err)
	require.Equal(t, e.now.Add(LinkCodeTTL).UTC(), exp)
	var rows []dbmodels.TelegramLinkCode
	require.NoError(t, e.g.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.NotContains(t, rows[0].CodeHash, code, "only the hash is stored")

	// A new code invalidates the old one.
	code2, _, err := CreateLinkCode(e.g, "u1", e.now)
	require.NoError(t, err)
	_, err = Bind(e.g, code, 100, e.now)
	require.ErrorIs(t, err, ErrInvalidCode)

	l, err := Bind(e.g, code2, 100, e.now)
	require.NoError(t, err)
	require.Equal(t, "u1", l.UserID)
	require.True(t, l.Enabled)
	require.Equal(t, dbmodels.DefaultDigestTime, l.DigestTime)
	_, err = Bind(e.g, code2, 100, e.now)
	require.ErrorIs(t, err, ErrInvalidCode, "single use")

	// Another user cannot bind the same chat.
	c3, _, err := CreateLinkCode(e.g, "u2", e.now)
	require.NoError(t, err)
	_, err = Bind(e.g, c3, 100, e.now)
	require.ErrorIs(t, err, ErrChatTaken)
	got, err := GetLink(e.g, "u2")
	require.NoError(t, err)
	require.Nil(t, got)
	// ...but can bind another chat with the same (kept) code.
	_, err = Bind(e.g, c3, 101, e.now)
	require.NoError(t, err)

	// Relinking replaces the chat and keeps settings.
	_, err = UpdateSettings(e.g, "u1", SettingsPatch{DigestTime: ptr("06:45")})
	require.NoError(t, err)
	c4, _, _ := CreateLinkCode(e.g, "u1", e.now)
	l, err = Bind(e.g, c4, 200, e.now)
	require.NoError(t, err)
	require.EqualValues(t, 200, l.ChatID)
	require.Equal(t, "06:45", l.DigestTime)

	// Expired code.
	c5, _, _ := CreateLinkCode(e.g, "u1", e.now)
	_, err = Bind(e.g, c5, 300, e.now.Add(LinkCodeTTL+time.Second))
	require.ErrorIs(t, err, ErrInvalidCode)

	require.NoError(t, Unlink(e.g, "u1"))
	got, err = GetLink(e.g, "u1")
	require.NoError(t, err)
	require.Nil(t, got)
	require.NoError(t, Unlink(e.g, "u1"), "idempotent")
}

func TestChatIDUniqueInDB(t *testing.T) {
	e := newEnv(t, "UTC", mustTime(t, "2026-11-08T08:00:00Z"))
	require.NoError(t, e.g.Create(&dbmodels.User{ID: "u2"}).Error)
	err := e.g.Create(&dbmodels.TelegramLink{UserID: "u2", ChatID: 42, Enabled: true}).Error
	require.Error(t, err)
}

func TestUpdateSettingsValidation(t *testing.T) {
	e := newEnv(t, "UTC", mustTime(t, "2026-11-08T08:00:00Z"))
	for _, bad := range []string{"8:00", "24:00", "08:60", "abcde", "08-00"} {
		_, err := UpdateSettings(e.g, "u1", SettingsPatch{DigestTime: ptr(bad)})
		require.ErrorIs(t, err, ErrInvalidTime, bad)
	}
	l, err := UpdateSettings(e.g, "u1", SettingsPatch{DigestTime: ptr(""), Enabled: ptr(false)})
	require.NoError(t, err)
	require.Empty(t, l.DigestTime)
	require.False(t, l.Enabled)
	_, err = UpdateSettings(e.g, "nobody", SettingsPatch{})
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func (e *env) service() *Service {
	return New(Options{DB: e.g, Token: testToken, BaseURL: e.tg.URL, Now: func() time.Time { return e.now }, NoPollWait: true})
}

func (e *env) say(chat int64, typ, text string) {
	u := Update{UpdateID: int64(len(e.tg.updates) + 1)}
	u.Message = &struct {
		Text string `json:"text"`
		Chat struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
	}{Text: text}
	u.Message.Chat.ID, u.Message.Chat.Type = chat, typ
	e.tg.mu.Lock()
	e.tg.updates = append(e.tg.updates, u)
	e.tg.mu.Unlock()
}

func TestPollerLinksChat(t *testing.T) {
	e := newEnv(t, "UTC", mustTime(t, "2026-11-08T08:00:00Z"))
	require.NoError(t, Unlink(e.g, "u1"))
	svc := e.service()
	code, _, err := CreateLinkCode(e.g, "u1", e.now)
	require.NoError(t, err)

	e.say(500, "group", "/start "+code) // groups are ignored
	require.NoError(t, svc.PollOnce(context.Background(), time.Second))
	l, _ := GetLink(e.g, "u1")
	require.Nil(t, l)

	e.say(500, "private", "/start "+code)
	require.NoError(t, svc.PollOnce(context.Background(), time.Second))
	l, _ = GetLink(e.g, "u1")
	require.NotNil(t, l)
	require.EqualValues(t, 500, l.ChatID)
	msgs := e.tg.messages()
	require.Len(t, msgs, 1)
	require.Contains(t, msgs[0].Text, "Linked to Daka")
	require.EqualValues(t, 500, msgs[0].Chat)
}

func TestPollerBareCodeAndBruteForceLimit(t *testing.T) {
	e := newEnv(t, "UTC", mustTime(t, "2026-11-08T08:00:00Z"))
	require.NoError(t, Unlink(e.g, "u1"))
	svc := e.service()
	for i := 0; i < maxBadAttempts+3; i++ {
		e.say(600, "private", "wrongcode")
	}
	require.NoError(t, svc.PollOnce(context.Background(), time.Second))
	require.Len(t, e.tg.messages(), maxBadAttempts, "replies stop once the chat is locked out")

	code, _, _ := CreateLinkCode(e.g, "u1", e.now)
	e.say(600, "private", code)
	require.NoError(t, svc.PollOnce(context.Background(), time.Second))
	l, _ := GetLink(e.g, "u1")
	require.Nil(t, l, "locked-out chat cannot bind")

	e.now = e.now.Add(badWindow + time.Second)
	code, _, _ = CreateLinkCode(e.g, "u1", e.now)
	e.say(600, "private", code)
	require.NoError(t, svc.PollOnce(context.Background(), time.Second))
	l, _ = GetLink(e.g, "u1")
	require.NotNil(t, l)
}

func TestPollerChatTakenAndStartWithoutCode(t *testing.T) {
	e := newEnv(t, "UTC", mustTime(t, "2026-11-08T08:00:00Z")) // u1 linked to chat 42
	require.NoError(t, e.g.Create(&dbmodels.User{ID: "u2"}).Error)
	svc := e.service()
	code, _, _ := CreateLinkCode(e.g, "u2", e.now)
	e.say(42, "private", "/start "+code)
	e.say(77, "private", "/start")
	require.NoError(t, svc.PollOnce(context.Background(), time.Second))
	msgs := e.tg.messages()
	require.Len(t, msgs, 2)
	require.Contains(t, msgs[0].Text, "already linked")
	require.Contains(t, msgs[1].Text, "Send the link code")
	l, _ := GetLink(e.g, "u2")
	require.Nil(t, l)
}

func TestServiceInertWithoutToken(t *testing.T) {
	e := newEnv(t, "UTC", mustTime(t, "2026-11-08T08:00:00Z"))
	svc := New(Options{DB: e.g})
	require.False(t, svc.Configured())
	require.Empty(t, svc.BotUsername(context.Background()))
	svc.Run(context.Background()) // returns immediately
	var nilSvc *Service
	require.False(t, nilSvc.Configured())
}

func TestBotUsernameCached(t *testing.T) {
	e := newEnv(t, "UTC", mustTime(t, "2026-11-08T08:00:00Z"))
	svc := e.service()
	require.Equal(t, "daka_bot", svc.BotUsername(context.Background()))
	require.Equal(t, "daka_bot", svc.BotUsername(context.Background()))
	n := 0
	for _, p := range e.tg.paths {
		if strings.HasSuffix(p, "/getMe") {
			n++
		}
	}
	require.Equal(t, 1, n)
}

// ---- token hygiene ----

func TestClientErrorsNeverContainToken(t *testing.T) {
	// Unreachable host: net/http embeds the full URL (with token) in its error.
	c := NewClient(testToken, "http://127.0.0.1:1")
	err := c.SendMessage(context.Background(), 1, "x")
	require.Error(t, err)
	require.NotContains(t, err.Error(), testToken)
	require.NotContains(t, err.Error(), "SECRET-TOKEN")

	// An API error description that echoes the token is scrubbed too.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"bad ` + testToken + `"}`))
	}))
	defer srv.Close()
	err = NewClient(testToken, srv.URL).SendMessage(context.Background(), 1, "x")
	require.Error(t, err)
	require.NotContains(t, err.Error(), testToken)
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	hit := false
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	err := NewClient(testToken, srv.URL).SendMessage(context.Background(), 1, "x")
	require.Error(t, err)
	require.False(t, hit, "redirect target must never be contacted")
}

func TestTokenNotStoredInDB(t *testing.T) {
	e := newEnv(t, "UTC", mustTime(t, "2026-11-08T08:00:00Z"))
	e.item("Thing", nil, dbmodels.ItemRuleVersion{})
	e.tick()
	var logs []dbmodels.ReminderLog
	require.NoError(t, e.g.Find(&logs).Error)
	require.NotEmpty(t, logs)
	b, _ := json.Marshal(logs)
	require.NotContains(t, string(b), testToken)
}

func TestPerUserBotSetupAndDelivery(t *testing.T) {
	ring, err := secret.NewKeyring("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.NoError(t, err)

	e := newEnv(t, "UTC", mustTime(t, "2026-11-08T08:00:00Z"))
	require.NoError(t, Unlink(e.g, "u1"))

	svc := New(Options{
		DB:      e.g,
		Keys:    ring,
		BaseURL: e.tg.URL,
		Now:     func() time.Time { return e.now },
	})
	require.True(t, svc.Configured())

	// 1. Initial status: unconfigured and unlinked
	st, err := svc.GetStatus(context.Background(), "u1")
	require.NoError(t, err)
	require.True(t, st.Configured)
	require.False(t, st.HasToken)
	require.False(t, st.Linked)

	// 2. SetBot without chat ID: creates a link code with the token
	_, err = svc.SetBot(context.Background(), "u1", testToken, 0)
	require.NoError(t, err)

	code, _, _, err := svc.CreateLinkCode(context.Background(), "u1")
	require.NoError(t, err)
	require.NotEmpty(t, code)

	st, err = svc.GetStatus(context.Background(), "u1")
	require.NoError(t, err)
	require.True(t, st.HasToken)
	require.Equal(t, "daka_bot", st.BotUsername)
	require.False(t, st.Linked)

	// 3. Bare /start does NOT link (security check: requires code)
	e.say(888, "private", "/start")
	st, err = svc.GetStatus(context.Background(), "u1")
	require.NoError(t, err)
	require.False(t, st.Linked)

	// Sending /start <code> successfully binds! Advance clock past debounce.
	e.at("2026-11-08T08:00:10Z")
	e.say(888, "private", "/start "+code)
	st, err = svc.GetStatus(context.Background(), "u1")
	require.NoError(t, err)
	require.True(t, st.Linked)
	require.True(t, st.Enabled)
	require.Equal(t, "daka_bot", st.BotUsername)

	// Confirmation message was sent
	msgs := e.tg.messages()
	require.NotEmpty(t, msgs)
	require.EqualValues(t, 888, msgs[len(msgs)-1].Chat)

	// 4. SendTestMessage
	require.NoError(t, svc.SendTestMessage(context.Background(), "u1"))
	msgs = e.tg.messages()
	require.Contains(t, msgs[len(msgs)-1].Text, "working")

	// 5. Scheduler delivery with decrypted token in mixed-mode (DefaultToken set)
	sched := &Scheduler{
		DB:           e.g,
		Keys:         ring,
		DefaultToken: "global-fallback-token",
		BaseURL:      e.tg.URL,
		Now:          func() time.Time { return e.now },
	}
	e.tg.reset()
	e.item("Habit", nil, dbmodels.ItemRuleVersion{})
	sched.Tick(context.Background())
	msgs = e.tg.messages()
	require.NotEmpty(t, msgs)
	require.EqualValues(t, 888, msgs[0].Chat)

	// 6. Unlink
	require.NoError(t, svc.Unlink(context.Background(), "u1"))
	st, err = svc.GetStatus(context.Background(), "u1")
	require.NoError(t, err)
	require.False(t, st.Linked)
	require.False(t, st.HasToken)
}

func TestDigestLanguage(t *testing.T) {
	// 2026-11-08 08:00 in Asia/Shanghai is 00:00Z; the digest follows the user's language.
	e := newEnv(t, "Asia/Shanghai", mustTime(t, "2026-11-08T00:00:00Z"))
	e.item("浇花", nil, dbmodels.ItemRuleVersion{})
	_, err := UpdateSettings(e.g, "u1", SettingsPatch{Lang: ptr(LangZhCN)})
	require.NoError(t, err)
	e.tick()
	msgs := e.tg.messages()
	require.Len(t, msgs, 1)
	require.Equal(t, "Daka - 2026-11-08 星期日\n\n今日待办（1）：\n- 浇花", msgs[0].Text)

	_, err = UpdateSettings(e.g, "u1", SettingsPatch{Lang: ptr("fr")})
	require.ErrorIs(t, err, ErrInvalidLang)
}

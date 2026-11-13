package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/auth"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/ratelimit"
)

type createdToken struct {
	tokenResponse
}

func (e *env) mkToken(user string, body any) createdToken {
	e.t.Helper()
	w := e.do("POST", "/api/tokens", user, "", body)
	require.Equal(e.t, 201, w.Code, w.Body.String())
	var c createdToken
	require.NoError(e.t, json.Unmarshal(w.Body.Bytes(), &c))
	require.NotEmpty(e.t, c.Token)
	return c
}

func (e *env) feed(h http.Handler, method, target string, hdr map[string]string) *httptest.ResponseRecorder {
	e.t.Helper()
	if h == nil {
		h = e.h
	}
	r := httptest.NewRequest(method, target, nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func (e *env) getFeedBody(token string) string {
	e.t.Helper()
	w := e.feed(nil, "GET", feedPath+"?token="+token, nil)
	require.Equal(e.t, 200, w.Code, w.Body.String())
	return w.Body.String()
}

func TestTokenLifecycle(t *testing.T) {
	e := newEnv(t)
	c := e.mkToken("", m{"name": "Laptop"})
	require.True(t, strings.HasPrefix(c.Token, "dk_"))
	require.Equal(t, []string{"feed:read"}, c.Scopes)
	require.Equal(t, "exclude_sensitive", c.Privacy)
	require.NotNil(t, c.ExpiresAt)
	require.Equal(t, fixedNow.Add(defaultTokenDays*24*time.Hour), *c.ExpiresAt)
	require.Equal(t, feedPath+"?token="+c.Token, c.FeedPath)

	// Only the hash is stored.
	var row dbmodels.ApiToken
	require.NoError(t, e.db.Write().First(&row, c.ID).Error)
	require.Equal(t, hashToken(c.Token), row.TokenHash)
	require.NotContains(t, row.TokenHash, c.Token)

	// The listing never exposes the token or its hash.
	w := e.do("GET", "/api/tokens", "", "", nil)
	require.Equal(t, 200, w.Code)
	require.NotContains(t, w.Body.String(), c.Token)
	require.NotContains(t, w.Body.String(), row.TokenHash)
	require.Contains(t, w.Body.String(), `"name":"Laptop"`)
	// Other users do not see it.
	require.JSONEq(t, `{"tokens":[]}`, e.do("GET", "/api/tokens", "u2", "", nil).Body.String())

	require.Equal(t, 200, e.feed(nil, "GET", feedPath+"?token="+c.Token, nil).Code)
	e.now = e.now.Add(2 * time.Minute) // past the last_used_at granularity
	require.Equal(t, 200, e.feed(nil, "GET", feedPath, map[string]string{"Authorization": "Bearer " + c.Token}).Code)
	require.NoError(t, e.db.Write().First(&row, c.ID).Error)
	require.NotNil(t, row.LastUsedAt)

	// Another user cannot revoke it.
	require.Equal(t, 404, e.do("DELETE", fmt.Sprintf("/api/tokens/%d", c.ID), "u2", "", nil).Code)
	require.Equal(t, 200, e.feed(nil, "GET", feedPath+"?token="+c.Token, nil).Code)

	require.Equal(t, 200, e.do("DELETE", fmt.Sprintf("/api/tokens/%d", c.ID), "", "", nil).Code)
	require.Equal(t, 200, e.do("DELETE", fmt.Sprintf("/api/tokens/%d", c.ID), "", "", nil).Code) // idempotent
	require.Equal(t, 401, e.feed(nil, "GET", feedPath+"?token="+c.Token, nil).Code)
	require.Equal(t, 404, e.do("DELETE", "/api/tokens/9999", "", "", nil).Code)
	require.Contains(t, e.do("GET", "/api/tokens", "", "", nil).Body.String(), `"revoked_at":"`)

	// Revoked token can be permanently deleted with ?delete=1.
	require.Equal(t, 200, e.do("DELETE", fmt.Sprintf("/api/tokens/%d?delete=1", c.ID), "", "", nil).Code)
	require.Equal(t, 404, e.do("DELETE", fmt.Sprintf("/api/tokens/%d", c.ID), "", "", nil).Code)
	require.NotContains(t, e.do("GET", "/api/tokens", "", "", nil).Body.String(), `"name":"Laptop"`)
}

func TestTokenNeverExpiresAndCategoryFilter(t *testing.T) {
	e := newEnv(t)
	catA := dbmodels.Category{UserID: "u1", Name: "Work"}
	require.NoError(t, e.db.Write().Create(&catA).Error)
	catB := dbmodels.Category{UserID: "u1", Name: "Personal"}
	require.NoError(t, e.db.Write().Create(&catB).Error)

	e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Deep Work"; it.CategoryID = &catA.ID })
	e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Therapy Notes"; it.CategoryID = &catA.ID; it.IsSensitive = true })
	e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Workout"; it.CategoryID = &catB.ID })

	// Token with never_expires: true
	cNever := e.mkToken("u1", m{"name": "Never", "never_expires": true, "category_id": catA.ID})
	require.Nil(t, cNever.ExpiresAt)
	require.False(t, cNever.Expired)
	require.Equal(t, &catA.ID, cNever.CategoryID)

	// Feed query 5 years in the future still works.
	e.now = e.now.Add(5 * 365 * 24 * time.Hour)
	resp := e.feed(nil, "GET", feedPath+"?token="+cNever.Token, nil)
	require.Equal(t, 200, resp.Code)
	body := resp.Body.String()
	require.Contains(t, body, "SUMMARY:Deep Work")
	require.NotContains(t, body, "SUMMARY:Workout")
	require.NotContains(t, body, "Therapy Notes")

	// When catA is deleted with uncategorize=1, token scoped to catA is revoked to prevent scope widening.
	require.Equal(t, 200, e.do("DELETE", fmt.Sprintf("/api/categories/%d?uncategorize=1", catA.ID), "u1", "", nil).Code)
	var row dbmodels.ApiToken
	require.NoError(t, e.db.Write().First(&row, cNever.ID).Error)
	require.NotNil(t, row.RevokedAt)
	require.Equal(t, 401, e.feed(nil, "GET", feedPath+"?token="+cNever.Token, nil).Code)
}

func TestTokenCreateValidation(t *testing.T) {
	e := newEnv(t)
	for name, body := range map[string]any{
		"no name":        m{},
		"blank name":     m{"name": "  "},
		"bad privacy":    m{"name": "x", "privacy": "everything"},
		"zero expiry":    m{"name": "x", "expires_in_days": 0},
		"huge expiry":    m{"name": "x", "expires_in_days": 366},
		"unknown field":  m{"name": "x", "scopes": "read"},
		"negative days":  m{"name": "x", "expires_in_days": -3},
		"too long name":  m{"name": strings.Repeat("a", 300)},
		"malformed body": "{",
	} {
		require.Equal(t, 400, e.do("POST", "/api/tokens", "", "", body).Code, name)
	}
	c := e.mkToken("", m{"name": "x", "expires_in_days": 1, "privacy": "all"})
	require.NotNil(t, c.ExpiresAt)
	require.Equal(t, fixedNow.Add(24*time.Hour), *c.ExpiresAt)
	require.Equal(t, "all", c.Privacy)
}

// denyAll is a login-mode verifier that accepts nothing.
type denyAll struct{}

func (denyAll) Verify(*http.Request) (auth.User, bool) { return auth.User{}, false }

func TestTokenIsRejectedOnEveryOtherEndpoint(t *testing.T) {
	e := newEnv(t)
	c := e.mkToken("", m{"name": "x", "privacy": "all"})
	loginMode := NewRouter(Deps{DB: e.db, Now: func() time.Time { return e.now }, Auth: auth.Middleware(denyAll{})})
	openMode := NewRouter(Deps{DB: e.db, Now: func() time.Time { return e.now }, Auth: userHeaderAuth})
	paths := []string{"/api/items", "/api/view/today", "/api/tokens", "/api/checkins", "/api/categories", "/api/settings/calendar", "/api/nope"}
	for _, h := range []http.Handler{loginMode, openMode} {
		for _, p := range paths {
			for _, method := range []string{"GET", "POST", "DELETE", "PUT", "PATCH"} {
				for name, hdr := range map[string]map[string]string{
					"bearer": {"Authorization": "Bearer " + c.Token},
				} {
					w := e.feed(h, method, p, hdr)
					require.Equal(t, 401, w.Code, "%s %s %s", name, method, p)
				}
			}
		}
	}
	// ?token= is ignored by every other endpoint (login mode: no credentials).
	for _, p := range paths {
		require.Equal(t, 401, e.feed(loginMode, "GET", p+"?token="+c.Token, nil).Code, p)
	}
	// The feed itself never writes.
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		w := e.feed(nil, method, feedPath+"?token="+c.Token, nil)
		require.Equal(t, 405, w.Code, method)
	}
	var cnt int64
	require.NoError(t, e.db.Write().Model(&dbmodels.CheckinLog{}).Count(&cnt).Error)
	require.Zero(t, cnt)
}

func TestFeedBasicsAndWindow(t *testing.T) {
	e := newEnv(t)
	id := e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Water, 8 cups; daily" })
	c := e.mkToken("", m{"name": "x"})
	w := e.feed(nil, "GET", feedPath+"?token="+c.Token, nil)
	require.Equal(t, 200, w.Code)
	require.Equal(t, "text/calendar; charset=utf-8", w.Header().Get("Content-Type"))
	require.Equal(t, "no-referrer", w.Header().Get("Referrer-Policy"))
	b := w.Body.String()
	require.True(t, strings.HasPrefix(b, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\n"))
	require.True(t, strings.HasSuffix(b, "END:VCALENDAR\r\n"))
	require.NotContains(t, strings.ReplaceAll(b, "\r\n", ""), "\n")
	require.Contains(t, b, `SUMMARY:Water\, 8 cups\; daily`)
	require.Contains(t, b, fmt.Sprintf("UID:daka-%d-20261101@daka", id))
	// Today is 2026-11-07 (Asia/Shanghai): window 2026-10-08 .. 2027-05-06, item starts 2026-11-01.
	require.Contains(t, b, "DTSTART;VALUE=DATE:20261101\r\n")
	require.Contains(t, b, "DTEND;VALUE=DATE:20261102\r\n")
	require.Contains(t, b, "DTSTART;VALUE=DATE:20270506\r\n")
	require.NotContains(t, b, "DTSTART;VALUE=DATE:20270507")
	require.NotContains(t, b, "DTSTART;VALUE=DATE:20261031")
	require.Contains(t, b, "SEQUENCE:0\r\n")
	// Other users' items never appear.
	e.dailyItem("u2", func(it *dbmodels.Item) { it.Name = "Other Users Thing" })
	require.NotContains(t, e.getFeedBody(c.Token), "Other Users Thing")
}

func TestFeedTimedEventWithAlarm(t *testing.T) {
	e := newEnv(t)
	rt := "08:30"
	e.dailyItem("u1", func(it *dbmodels.Item) { it.RemindTime = &rt })
	c := e.mkToken("", m{"name": "x", "privacy": "all"})
	b := e.getFeedBody(c.Token)
	// 08:30 Asia/Shanghai == 00:30Z.
	require.Contains(t, b, "DTSTART:20261107T003000Z\r\n")
	require.Contains(t, b, "DTEND:20261107T010000Z\r\n")
	require.Contains(t, b, "BEGIN:VALARM\r\n")
	require.Contains(t, b, "TRIGGER:PT0S\r\n")
}

func TestFeedSequenceBumpsOnRuleChangeUIDStable(t *testing.T) {
	e := newEnv(t)
	id := e.dailyItem("u1", nil)
	c := e.mkToken("", m{"name": "x", "privacy": "all"})
	before := e.getFeedBody(c.Token)
	uid := fmt.Sprintf("UID:daka-%d-20261105@daka", id)
	require.Contains(t, before, uid)
	require.NoError(t, e.db.Write().Create(&dbmodels.ItemRuleVersion{ItemID: id, EffectiveFrom: "2026-11-07",
		ScheduleType: dbmodels.ScheduleFixedRRule, DtStart: "2026-11-01", RRuleString: "FREQ=DAILY", HolidayMode: dbmodels.HolidayNone}).Error)
	after := e.getFeedBody(c.Token)
	require.Contains(t, after, uid)
	require.Contains(t, after, "SEQUENCE:1\r\n")
	require.NotContains(t, after, "SEQUENCE:0\r\n")
}

func TestFeedETag(t *testing.T) {
	e := newEnv(t)
	e.dailyItem("u1", nil)
	c := e.mkToken("", m{"name": "x"})
	url := feedPath + "?token=" + c.Token
	w := e.feed(nil, "GET", url, nil)
	etag := w.Header().Get("ETag")
	require.NotEmpty(t, etag)
	require.Equal(t, etag, e.feed(nil, "GET", url, nil).Header().Get("ETag"))

	for _, inm := range []string{etag, "W/" + etag, `"zzz", ` + etag, "*"} {
		n := e.feed(nil, "GET", url, map[string]string{"If-None-Match": inm})
		require.Equal(t, 304, n.Code, inm)
		require.Empty(t, n.Body.String())
		require.Equal(t, etag, n.Header().Get("ETag"))
	}
	require.Equal(t, 200, e.feed(nil, "GET", url, map[string]string{"If-None-Match": `"other"`}).Code)

	// A change invalidates the ETag.
	e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Another" })
	n := e.feed(nil, "GET", url, map[string]string{"If-None-Match": etag})
	require.Equal(t, 200, n.Code)
	require.NotEqual(t, etag, n.Header().Get("ETag"))
}

func TestFeedWithdrawsArchivedItems(t *testing.T) {
	e := newEnv(t)
	id := e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Soon Archived" })
	c := e.mkToken("", m{"name": "x", "privacy": "all"})
	require.Contains(t, e.getFeedBody(c.Token), "Soon Archived")
	require.NoError(t, e.db.Write().Model(&dbmodels.Item{}).Where("id = ?", id).Update("archived", true).Error)
	require.NotContains(t, e.getFeedBody(c.Token), "Soon Archived")
}

func TestFeedSensitiveExclusionAndAnonymize(t *testing.T) {
	e := newEnv(t)
	cat := e.category("u1", true)
	e.dailyItem("u1", func(it *dbmodels.Item) {
		it.Name = "Direct Secret"
		it.Description = "direct note"
		it.IsSensitive = true
	})
	e.dailyItem("u1", func(it *dbmodels.Item) {
		it.Name = "Inherited Secret"
		it.Description = "inherited note"
		it.CategoryID = &cat
	})
	e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Public Thing"; it.Description = "public note" })

	excl := e.mkToken("", m{"name": "work"}) // default exclude_sensitive
	b := e.getFeedBody(excl.Token)
	require.Contains(t, b, "Public Thing")
	require.Contains(t, b, "public note")
	for _, leak := range []string{"Direct Secret", "Inherited Secret", "direct note", "inherited note", "Private"} {
		require.NotContains(t, b, leak)
	}

	anon := e.mkToken("", m{"name": "anon", "privacy": "anonymize_sensitive"})
	b = e.getFeedBody(anon.Token)
	require.Contains(t, b, anonymizedName)
	for _, leak := range []string{"Direct Secret", "Inherited Secret", "direct note", "inherited note"} {
		require.NotContains(t, b, leak)
	}
	require.Contains(t, b, "Public Thing")

	all := e.mkToken("", m{"name": "all", "privacy": "all"})
	b = e.getFeedBody(all.Token)
	require.Contains(t, b, "Direct Secret")
	require.Contains(t, b, "Inherited Secret")

	// Category made sensitive later is picked up immediately.
	require.NoError(t, e.db.Write().Model(&dbmodels.Category{}).Where("id = ?", cat).Update("is_sensitive", false).Error)
	require.Contains(t, e.getFeedBody(excl.Token), "Inherited Secret")
}

func TestFeedNegativeItemsOptIn(t *testing.T) {
	e := newEnv(t)
	e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Quit Sugar"; it.Polarity = dbmodels.PolarityNegative })
	plain := e.mkToken("", m{"name": "a"})
	require.NotContains(t, e.getFeedBody(plain.Token), "Quit Sugar")
	opt := e.mkToken("", m{"name": "b", "include_negative": true})
	require.Contains(t, e.getFeedBody(opt.Token), "Quit Sugar")
}

func TestFeedNonFixedSchedulesProduceNoEvents(t *testing.T) {
	e := newEnv(t)
	id := e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Filter" })
	require.NoError(t, e.db.Write().Where("item_id = ?", id).Delete(&dbmodels.ItemRuleVersion{}).Error)
	require.NoError(t, e.db.Write().Create(&dbmodels.ItemRuleVersion{ItemID: id, EffectiveFrom: "2026-01-01",
		ScheduleType: dbmodels.ScheduleRolling, RollingInterval: 3, RollingUnit: dbmodels.RollingMonth, HolidayMode: dbmodels.HolidayNone}).Error)
	c := e.mkToken("", m{"name": "x"})
	require.NotContains(t, e.getFeedBody(c.Token), "Filter")
}

func TestFeedHolidayModeUsesCalendar(t *testing.T) {
	e := newEnv(t)
	cal := cnCalendars(t)
	e.h = NewRouter(Deps{DB: e.db, Now: func() time.Time { return e.now }, Auth: userHeaderAuth, Calendar: cal})
	require.NoError(t, e.db.Write().Model(&dbmodels.UserSettings{}).Where("user_id = ?", "u1").Update("holiday_region", dbmodels.RegionCN).Error)
	id := e.dailyItem("u1", nil)
	require.NoError(t, e.db.Write().Model(&dbmodels.ItemRuleVersion{}).Where("item_id = ?", id).Update("holiday_mode", dbmodels.HolidayWorkdaysOnly).Error)
	c := e.mkToken("", m{"name": "x"})
	b := e.getFeedBody(c.Token)
	// workdays_only drops weekends: 2026-11-07 is a Saturday, 2026-11-06 a Friday.
	require.NotContains(t, b, "DTSTART;VALUE=DATE:20261107")
	require.Contains(t, b, "DTSTART;VALUE=DATE:20261106")
}

func TestFeedAuthFailures(t *testing.T) {
	e := newEnv(t)
	e.dailyItem("u1", nil)
	c := e.mkToken("", m{"name": "live"})

	expired := e.mkToken("", m{"name": "short", "expires_in_days": 1})
	e.now = e.now.Add(23 * time.Hour)
	require.Equal(t, 200, e.feed(nil, "GET", feedPath+"?token="+expired.Token, nil).Code)
	e.now = e.now.Add(2 * time.Hour)
	require.Equal(t, 401, e.feed(nil, "GET", feedPath+"?token="+expired.Token, nil).Code)
	require.Equal(t, 200, e.feed(nil, "GET", feedPath+"?token="+c.Token, nil).Code)

	// Wrong scope in the database is refused even with a valid hash.
	require.NoError(t, e.db.Write().Model(&dbmodels.ApiToken{}).Where("id = ?", c.ID).UpdateColumn("scopes", "read").Error)
	require.Equal(t, 401, e.feed(nil, "GET", feedPath+"?token="+c.Token, nil).Code)

	bodies := map[string]bool{}
	for _, tok := range []string{"", "dk_bogus", expired.Token, c.Token} {
		w := e.feed(nil, "GET", feedPath+"?token="+tok, nil)
		require.Equal(t, 401, w.Code)
		bodies[w.Body.String()] = true
		require.NotContains(t, w.Body.String(), "BEGIN:VCALENDAR")
	}
	require.Len(t, bodies, 1, "all failures must be indistinguishable")
}

func TestFeedFailureRateLimit(t *testing.T) {
	e := newEnv(t)
	clock := fixedNow
	cfg := func(th int) *ratelimit.AuthFailureLimiter {
		return ratelimit.NewAuthFailureLimiter(ratelimit.AuthFailureConfig{Threshold: th, Now: func() time.Time { return clock }})
	}
	h := NewRouter(Deps{DB: e.db, Now: func() time.Time { return e.now }, Auth: userHeaderAuth,
		FeedFailures: cfg(3), FeedIPFailures: cfg(6)})
	c := e.mkToken("", m{"name": "x"})
	get := func(tok string) *httptest.ResponseRecorder {
		return e.feed(h, "GET", feedPath+"?token="+tok, nil)
	}
	// Same prefix: locks after 3 failures, even the real token is refused for that pair... but a
	// different prefix is separate until the per-IP aggregate trips.
	for i := 0; i < 3; i++ {
		require.Equal(t, 401, get("dk_aaaaaaaa"+fmt.Sprint(i)).Code)
	}
	w := get("dk_aaaaaaaa9")
	require.Equal(t, 429, w.Code)
	require.NotEmpty(t, w.Header().Get("Retry-After"))
	// Varying the prefix is capped by the per-IP aggregate (6 failures in total).
	require.Equal(t, 401, get("dk_bbbbbbbb").Code)
	require.Equal(t, 401, get("dk_cccccccc").Code)
	require.Equal(t, 401, get("dk_dddddddd").Code)
	require.Equal(t, 429, get(c.Token).Code, "aggregate lockout refuses even a valid token")
	require.Equal(t, 429, get("dk_eeeeeeee").Code)
	// Lockout expires.
	clock = clock.Add(2 * time.Minute)
	require.Equal(t, 200, get(c.Token).Code)
}

func TestFeedLogsContainNoItemNames(t *testing.T) {
	// Handler errors never embed names: errResult bodies are fixed strings.
	e := newEnv(t)
	e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Sensitive Name" })
	w := e.feed(nil, "GET", feedPath+"?token=dk_x", nil)
	require.NotContains(t, w.Body.String(), "Sensitive Name")
}

func TestICSEscapingAndFolding(t *testing.T) {
	require.Equal(t, `a\\b\;c\,d\ne`, icsText("a\\b;c,d\ne"))
	require.Equal(t, `x\ny\nz`, icsText("x\r\ny\rz"))
	require.Equal(t, "ab", icsText("a\x00\x07b"))
	// Injection of a fake property via newline is neutralized.
	require.NotContains(t, icsText("x\r\nATTENDEE:evil"), "\n")

	long := "SUMMARY:" + strings.Repeat("打", 60) // 3 bytes per rune
	w := &icsWriter{}
	w.line(long)
	out := w.String()
	require.True(t, strings.HasSuffix(out, "\r\n"))
	lines := strings.Split(strings.TrimSuffix(out, "\r\n"), "\r\n")
	require.Greater(t, len(lines), 1)
	var unfolded strings.Builder
	for i, l := range lines {
		require.LessOrEqual(t, len(l), 75, l)
		require.True(t, utf8Valid(l))
		if i > 0 {
			require.True(t, strings.HasPrefix(l, " "))
			l = l[1:]
		}
		unfolded.WriteString(l)
	}
	require.Equal(t, long, unfolded.String())

	// Short lines are untouched.
	w = &icsWriter{}
	w.line("A:b")
	require.Equal(t, "A:b\r\n", w.String())
}

func utf8Valid(s string) bool { return strings.ToValidUTF8(s, "") == s }

func TestFeedEscapesNameInOutput(t *testing.T) {
	e := newEnv(t)
	e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "A\r\nATTENDEE:evil;x,y\\" })
	c := e.mkToken("", m{"name": "x", "privacy": "all"})
	b := e.getFeedBody(c.Token)
	require.NotContains(t, b, "\r\nATTENDEE:")
	require.Contains(t, b, `SUMMARY:A\nATTENDEE:evil\;x\,y\\`)
}

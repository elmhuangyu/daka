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

func (e *env) bearer(h http.Handler, method, target, tok string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.feed(h, method, target, map[string]string{"Authorization": "Bearer " + tok})
}

func (e *env) readToken() string {
	e.t.Helper()
	return e.mkToken("", m{"name": "ai", "scope": "read"}).Token
}

func TestReadTokenCreation(t *testing.T) {
	e := newEnv(t)
	c := e.mkToken("", m{"name": "ai", "scope": "read", "expires_in_days": 30})
	require.Equal(t, []string{"read"}, c.Scopes)
	require.Equal(t, "exclude_sensitive", c.Privacy)
	require.Empty(t, c.FeedPath)
	require.Equal(t, fixedNow.Add(30*24*time.Hour), c.ExpiresAt)
	var row dbmodels.ApiToken
	require.NoError(t, e.db.Write().First(&row, c.ID).Error)
	require.Equal(t, hashToken(c.Token), row.TokenHash)
	require.NotContains(t, row.TokenHash, c.Token)
	for name, body := range map[string]m{
		"bad scope":  {"name": "x", "scope": "write"},
		"all scopes": {"name": "x", "scope": "read,feed:read"},
		"privacy":    {"name": "x", "scope": "read", "privacy": "all"},
		"negative":   {"name": "x", "scope": "read", "include_negative": true},
		"zero days":  {"name": "x", "scope": "read", "expires_in_days": 0},
	} {
		require.Equal(t, 400, e.do("POST", "/api/tokens", "", "", body).Code, name)
	}
	// The default scope stays feed:read.
	require.Equal(t, []string{"feed:read"}, e.mkToken("", m{"name": "f"}).Scopes)
}

func TestReadTokenAllowlist(t *testing.T) {
	e := newEnv(t)
	id := e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Water" })
	e.checkin(id, "2026-11-05")
	tok := e.readToken()
	for _, p := range []string{
		"/api/items", "/api/items?q=wat&include=stats", fmt.Sprintf("/api/items/%d", id), fmt.Sprintf("/api/items/%d/stats", id),
		"/api/checkins?item_id=" + fmt.Sprint(id), "/api/view/today", "/api/view/calendar?start=2026-11-01&end=2026-11-07",
		"/api/calendar/holidays?year=2026",
	} {
		w := e.bearer(nil, "GET", p, tok)
		require.Equal(t, 200, w.Code, "%s %s", p, w.Body.String())
		require.Equal(t, "private, no-store", w.Header().Get("Cache-Control"))
	}
	for _, p := range []string{
		"/api/tokens", "/api/telegram", "/api/settings/calendar", "/api/calendar/external", "/api/categories", "/api/freezes",
		fmt.Sprintf("/api/items/%d/options", id), "/api/nope", "/api/items/1/stats/x",
	} {
		require.Equal(t, 401, e.bearer(nil, "GET", p, tok).Code, p)
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		for _, p := range []string{"/api/checkins", "/api/items", fmt.Sprintf("/api/items/%d", id), "/api/tokens", "/api/checkins/1", "/api/categories"} {
			require.Equal(t, 401, e.bearer(nil, method, p, tok).Code, method+" "+p)
		}
	}
	var n int64
	require.NoError(t, e.db.Write().Model(&dbmodels.CheckinLog{}).Count(&n).Error)
	require.EqualValues(t, 1, n, "no write went through")
}

func TestReadTokenQueryParamIsNotACredential(t *testing.T) {
	e := newEnv(t)
	tok := e.readToken()
	loginMode := NewRouter(Deps{DB: e.db, Now: func() time.Time { return e.now }, Auth: auth.Middleware(denyAll{})})
	require.Equal(t, 401, e.feed(loginMode, "GET", "/api/items?token="+tok, nil).Code)
	require.Equal(t, 401, e.bearer(loginMode, "GET", "/api/tokens", tok).Code)
}

func TestScopeIsolation(t *testing.T) {
	e := newEnv(t)
	e.dailyItem("u1", nil)
	feedTok := e.mkToken("", m{"name": "f"}).Token
	readTok := e.readToken()
	require.Equal(t, 401, e.bearer(nil, "GET", "/api/items", feedTok).Code)
	require.Equal(t, 401, e.bearer(nil, "GET", feedPath, readTok).Code)
	require.Equal(t, 401, e.feed(nil, "GET", feedPath+"?token="+readTok, nil).Code)
	require.Equal(t, 200, e.feed(nil, "GET", feedPath+"?token="+feedTok, nil).Code)
	require.Equal(t, 200, e.bearer(nil, "GET", "/api/items", readTok).Code)
}

func TestReadTokenExcludesSensitive(t *testing.T) {
	e := newEnv(t)
	cid := e.category("u1", true)
	own := e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Zeta Own"; it.IsSensitive = true })
	viaCat := e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Zeta ViaCat"; it.CategoryID = &cid })
	pub := e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Zeta Public" })
	for _, id := range []uint{own, viaCat, pub} {
		e.checkin(id, "2026-11-05")
		require.NoError(t, e.db.Write().Model(&dbmodels.CheckinLog{}).Where("item_id = ?", id).
			UpdateColumn("note", fmt.Sprintf("note-of-%d", id)).Error)
	}
	require.NoError(t, e.db.Write().Create(&dbmodels.FreezePeriod{UserID: "u1", StartDate: "2026-11-02", EndDate: "2026-11-03", Reason: "private reason"}).Error)
	tok := e.readToken()
	get := func(p string) *httptest.ResponseRecorder { return e.bearer(nil, "GET", p, tok) }

	for _, p := range []string{
		"/api/items?archived=all&redact=0", "/api/items?q=zeta&include=stats", fmt.Sprintf("/api/items?category_id=%d", cid),
		"/api/checkins", "/api/checkins?redact=0&limit=200", "/api/view/today?redact=0",
		"/api/view/calendar?start=2026-11-01&end=2026-11-07&redact=0",
		fmt.Sprintf("/api/view/calendar?start=2026-11-01&end=2026-11-07&item=%d", own),
	} {
		w := get(p)
		require.Equal(t, 200, w.Code, p)
		b := w.Body.String()
		for _, secret := range []string{"Zeta Own", "Zeta ViaCat", "private reason", fmt.Sprintf("note-of-%d", own), fmt.Sprintf("note-of-%d", viaCat)} {
			require.NotContains(t, b, secret, p)
		}
	}
	require.Contains(t, get("/api/items?q=zeta").Body.String(), "Zeta Public")
	require.Contains(t, get("/api/checkins").Body.String(), fmt.Sprintf("note-of-%d", pub))
	for _, id := range []uint{own, viaCat} {
		require.Equal(t, 404, get(fmt.Sprintf("/api/items/%d", id)).Code)
		require.Equal(t, 404, get(fmt.Sprintf("/api/items/%d/stats", id)).Code)
		require.Equal(t, 404, get(fmt.Sprintf("/api/checkins?item_id=%d", id)).Code)
	}
	require.Equal(t, 200, get(fmt.Sprintf("/api/items/%d", pub)).Code)
	// Pagination stays exact when sensitive rows are interleaved.
	var page struct {
		Checkins   []checkinLogResponse `json:"checkins"`
		NextCursor *string              `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(get("/api/checkins?limit=1").Body.Bytes(), &page))
	require.Len(t, page.Checkins, 1)
	require.Nil(t, page.NextCursor)
	// A session user still sees everything.
	require.Contains(t, e.do("GET", "/api/items?q=zeta", "", "", nil).Body.String(), "Zeta Own")
}

func TestReadTokenExpiryAndRevocation(t *testing.T) {
	e := newEnv(t)
	c := e.mkToken("", m{"name": "ai", "scope": "read", "expires_in_days": 1})
	require.Equal(t, 200, e.bearer(nil, "GET", "/api/items", c.Token).Code)
	require.Equal(t, 200, e.do("DELETE", fmt.Sprintf("/api/tokens/%d", c.ID), "", "", nil).Code)
	require.Equal(t, 401, e.bearer(nil, "GET", "/api/items", c.Token).Code)

	c2 := e.mkToken("", m{"name": "ai2", "scope": "read", "expires_in_days": 1})
	require.Equal(t, 200, e.bearer(nil, "GET", "/api/items", c2.Token).Code)
	e.now = e.now.Add(25 * time.Hour)
	require.Equal(t, 401, e.bearer(nil, "GET", "/api/items", c2.Token).Code)
	var row dbmodels.ApiToken
	require.NoError(t, e.db.Write().First(&row, c2.ID).Error)
	require.NotNil(t, row.LastUsedAt)
}

func TestReadTokenFailureRateLimit(t *testing.T) {
	e := newEnv(t)
	clock := fixedNow
	cfg := func(th int) *ratelimit.AuthFailureLimiter {
		return ratelimit.NewAuthFailureLimiter(ratelimit.AuthFailureConfig{Threshold: th, Now: func() time.Time { return clock }})
	}
	h := NewRouter(Deps{DB: e.db, Now: func() time.Time { return e.now }, Auth: userHeaderAuth,
		FeedFailures: cfg(3), FeedIPFailures: cfg(4)})
	tok := e.readToken()
	// Rejected (non-allowlisted) requests never count as failures.
	for i := 0; i < 10; i++ {
		require.Equal(t, 401, e.bearer(h, "POST", "/api/checkins", tok).Code)
	}
	for i := 0; i < 3; i++ {
		require.Equal(t, 401, e.bearer(h, "GET", "/api/items", "dk_aaaaaaaa"+fmt.Sprint(i)).Code)
	}
	require.Equal(t, 429, e.bearer(h, "GET", "/api/items", "dk_aaaaaaaa9").Code)
	require.Equal(t, 401, e.bearer(h, "GET", "/api/items", "dk_bbbbbbbb").Code)
	// The shared per-IP aggregate also blocks the valid token, and the feed.
	w := e.bearer(h, "GET", "/api/items", tok)
	require.Equal(t, 429, w.Code)
	require.NotEmpty(t, w.Header().Get("Retry-After"))
	require.Equal(t, 429, e.feed(h, "GET", feedPath+"?token=dk_x", nil).Code)
	clock = clock.Add(2 * time.Minute)
	require.Equal(t, 200, e.bearer(h, "GET", "/api/items", tok).Code)
}

func TestItemSearchTerms(t *testing.T) {
	e := newEnv(t)
	e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Change Water Filter" })
	e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "Drink water" })
	e.dailyItem("u1", func(it *dbmodels.Item) { it.Name = "100% done" })
	count := func(q string) int {
		w := e.do("GET", "/api/items?q="+q, "", "", nil)
		require.Equal(t, 200, w.Code, q)
		var r struct{ Items []itemResponse }
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &r))
		return len(r.Items)
	}
	require.Equal(t, 2, count("water"))
	require.Equal(t, 1, count("filter+WATER"))
	require.Equal(t, 1, count("water%20change"))
	require.Equal(t, 1, count("100%25"))
	require.Equal(t, 0, count("%25x"))
	require.Equal(t, 3, count("%20%20"))
	require.Equal(t, 400, e.do("GET", "/api/items?q="+strings.Repeat("a+", 9), "", "", nil).Code)
	require.Equal(t, 400, e.do("GET", "/api/items?q="+strings.Repeat("a", 65), "", "", nil).Code)
}

func TestOpenAPI(t *testing.T) {
	e := newEnv(t)
	w := e.feed(nil, "GET", "/api/openapi.json", nil)
	require.Equal(t, 200, w.Code)
	var doc struct {
		OpenAPI string                    `json:"openapi"`
		Paths   map[string]map[string]any `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &doc))
	require.Equal(t, "3.0.3", doc.OpenAPI)
	seen, ids := map[string]bool{}, map[string]bool{}
	for _, op := range openAPIOps() {
		key := "GET " + op.Path
		require.False(t, seen[key], "duplicate route %s", key)
		require.False(t, ids[op.ID], "duplicate operationId %s", op.ID)
		seen[key], ids[op.ID] = true, true
		require.True(t, readAllowed(strings.ReplaceAll(op.Path, "{id}", "1")), key)
		require.Contains(t, doc.Paths, op.Path)
		require.Len(t, doc.Paths[op.Path], 1, "read-only: GET only")
		require.Contains(t, doc.Paths[op.Path], "get")
	}
	require.Len(t, doc.Paths, len(seen))
	for _, p := range []string{"/api/tokens", "/api/telegram", "/api/settings", "/api/calendar/external", "/api/auth"} {
		require.NotContains(t, w.Body.String(), p)
	}
	require.Equal(t, 405, e.feed(nil, "POST", "/api/openapi.json", nil).Code)
	// No token needed even when logins are enforced.
	h := NewRouter(Deps{DB: e.db, Now: func() time.Time { return e.now }, Auth: auth.Middleware(denyAll{})})
	require.Equal(t, 200, e.feed(h, "GET", "/api/openapi.json", nil).Code)
}

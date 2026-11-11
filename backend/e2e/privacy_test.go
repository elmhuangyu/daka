package e2e

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/config"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

// Strings that must never leave the server for a sensitive item.
const (
	secName = "SECRETNAME-9f3a"
	secDesc = "SECRETDESC-4b21"
	secCat  = "SECRETCAT-77c0"
	secProj = "SECRETPROJ-8c12"
	secNote = "SECRETNOTE-e5d8"
	secOpt  = "SECRETOPTION-31aa"
	pubName = "PUBLICNAME-0001"
)

var secrets = []string{secName, secDesc, secCat, secProj, secNote, secOpt}

func (c *client) category(name string, sensitive bool) uint {
	c.t.Helper()
	r := c.ok(http.StatusCreated, "POST", "/api/categories", obj{"name": name, "is_sensitive": sensitive})
	return uint(jsonOf[obj](c.t, r)["id"].(float64))
}

func (c *client) project(name string, sensitive bool) uint {
	c.t.Helper()
	r := c.ok(http.StatusCreated, "POST", "/api/projects", obj{"name": name, "is_sensitive": sensitive})
	return uint(jsonOf[obj](c.t, r)["id"].(float64))
}

// token creates an API token and returns its plaintext.
func (c *client) token(scope, privacy string) string {
	c.t.Helper()
	body := obj{"name": "t-" + scope + "-" + privacy, "scope": scope}
	if privacy != "" {
		body["privacy"] = privacy
	}
	r := c.ok(http.StatusCreated, "POST", "/api/tokens", body)
	return jsonOf[obj](c.t, r)["token"].(string)
}

func bearer(tok string) map[string]string { return map[string]string{"Authorization": "Bearer " + tok} }

// noLeak asserts that none of the secrets appears in body.
func noLeak(t *testing.T, where, body string) {
	t.Helper()
	for _, s := range secrets {
		require.NotContains(t, body, s, "%s leaks %s", where, s)
	}
}

// leakSurface lists the read endpoints that must be covered for an item.
func leakSurface(id uint) []string {
	return []string{
		"/api/items", fmt.Sprintf("/api/items/%d", id), "/api/checkins", "/api/view/today",
		"/api/view/calendar?start=2025-09-01&end=2025-09-30", fmt.Sprintf("/api/items/%d/stats", id),
		fmt.Sprintf("/api/items?q=%s", "SECRETNAME"), fmt.Sprintf("/api/items?include=stats&redact=%d", 1),
		"/api/projects", "/api/checkins?q=" + url.QueryEscape(secNote),
	}
}

func withRedact(p string) string {
	if strings.Contains(p, "redact=") {
		return p
	}
	if strings.Contains(p, "?") {
		return p + "&redact=1"
	}
	return p + "?redact=1"
}

// assertProtected checks every outbound channel for an item that must be
// treated as sensitive right now.
func assertProtected(t *testing.T, s *stack, c *client, id uint) {
	t.Helper()
	// 1. Session API with redact=1 (privacy guard on).
	for _, p := range leakSurface(id) {
		r := c.do("GET", withRedact(p), nil)
		noLeak(t, "redact GET "+p, r.Body)
	}
	// Control: without redact the owner session sees plaintext by design, and
	// the response flags the item as sensitive.
	plain := c.ok(200, "GET", fmt.Sprintf("/api/items/%d", id), nil)
	require.Contains(t, plain.Body, secName)
	require.Contains(t, plain.Body, `"effective_sensitive":true`)

	// 2. Read-scope token: the item does not exist for it, redact or not.
	rt := c.token("read", "")
	rc := s.newClient() // cookie-less: only the bearer identifies the caller
	for _, p := range append(leakSurface(id), withRedact("/api/items")) {
		r := rc.doHdr("GET", p, nil, bearer(rt))
		noLeak(t, "read token GET "+p, r.Body)
		if strings.HasPrefix(p, "/api/items/") && !strings.Contains(p, "?") {
			require.Equal(t, 404, r.Status, p)
		}
	}
	r := rc.doHdr("GET", "/api/items", nil, bearer(rt))
	require.Equal(t, 200, r.Status)
	require.Contains(t, r.Body, pubName, "the control item stays visible")
	for _, p := range []string{"/api/categories", "/api/export", "/api/tokens", "/api/settings/calendar"} {
		r := rc.doHdr("GET", p, nil, bearer(rt))
		require.Equal(t, 401, r.Status, "read token must not reach %s", p)
		noLeak(t, "read token "+p, r.Body)
	}

	// 3. ICS feed, all three privacy levels (as designed), both by query and header.
	excl := c.token("feed:read", "exclude_sensitive")
	anon := c.token("feed:read", "anonymize_sensitive")
	all := c.token("feed:read", "all")
	feed := func(tok string) string {
		r := rc.do("GET", "/api/calendar/feed.ics?token="+tok, nil)
		require.Equal(t, 200, r.Status, r.Body)
		require.Contains(t, r.Body, pubName)
		return r.Body
	}
	b := feed(excl)
	noLeak(t, "feed exclude_sensitive", b)
	// B1: the privacy level cannot be raised from the URL.
	for _, extra := range []string{"&privacy=all", "&privacy=anonymize_sensitive", "&redact=0"} {
		r := rc.do("GET", "/api/calendar/feed.ics?token="+excl+extra, nil)
		noLeak(t, "feed exclude_sensitive"+extra, r.Body)
	}
	hr := rc.doHdr("GET", "/api/calendar/feed.ics", nil, bearer(excl))
	noLeak(t, "feed exclude_sensitive (bearer)", hr.Body)
	b = feed(anon)
	noLeak(t, "feed anonymize_sensitive", b)
	require.Contains(t, b, "[Private] Check-in")
	b = feed(all)
	require.Contains(t, b, secName, "privacy=all exposes the name by design")
	require.NotContains(t, b, secCat, "the category name is never part of the feed")
	require.NotContains(t, b, secProj, "the project name is never part of the feed")
	require.NotContains(t, b, secNote, "check-in notes are never part of the feed")
	// Feed tokens are not API credentials, and read tokens are not feed credentials.
	require.Equal(t, 401, rc.doHdr("GET", "/api/items", nil, bearer(all)).Status)
	require.Equal(t, 401, rc.do("GET", "/api/calendar/feed.ics?token="+rt, nil).Status)

	// 4. Error bodies never echo names.
	for _, e := range []resp{
		c.do("GET", fmt.Sprintf("/api/items/%d/stats?redact=maybe", id), nil),
		c.do("GET", "/api/items?redact=maybe&q="+secName, nil),
		c.do("POST", "/api/checkins", obj{"item_id": id, "mode": "bogus", "note": secNote}),
		c.do("PATCH", fmt.Sprintf("/api/items/%d", id), obj{"name": "", "description": secDesc}),
		c.do("POST", "/api/checkins", obj{"item_id": id, "date": "not-a-date", "mode": "set"}),
		rc.doHdr("GET", fmt.Sprintf("/api/items/%d", id), nil, bearer(rt)),
		rc.do("GET", "/api/calendar/feed.ics?token=dk_bogus"+secName, nil),
	} {
		require.GreaterOrEqual(t, e.Status, 400)
		noLeak(t, "error body", e.Body)
	}
}

// F4 / B1 / B4 / B5 / B6: sensitive items must not leak through any channel,
// whether the sensitivity comes from the item, its category, or survived a
// category change.
func TestSensitiveLeakageMatrix(t *testing.T) {
	type scenario struct {
		name string
		// setup creates the sensitive item (and may mutate categories) and
		// returns its id; all shared state lives on c.
		setup func(t *testing.T, c *client) uint
	}
	newItem := func(c *client, mut func(obj)) uint {
		body := obj{
			"name": secName, "tracker_type": "boolean",
			"rule": fixedRule("FREQ=DAILY", "none"),
		}
		mut(body)
		id := c.createItem(body)
		c.ok(200, "PATCH", fmt.Sprintf("/api/items/%d", id), obj{"remind_time": "09:00", "description": secDesc})
		c.ok(200, "POST", "/api/checkins", obj{"item_id": id, "mode": "set", "note": secNote})
		return id
	}
	scenarios := []scenario{
		{"own flag", func(t *testing.T, c *client) uint {
			return newItem(c, func(b obj) { b["is_sensitive"] = true })
		}},
		{"sensitive category at creation", func(t *testing.T, c *client) uint {
			cat := c.category(secCat, true)
			return newItem(c, func(b obj) { b["category_id"] = cat })
		}},
		{"category flagged afterwards", func(t *testing.T, c *client) uint {
			cat := c.category(secCat, false)
			id := newItem(c, func(b obj) { b["category_id"] = cat })
			c.ok(200, "PUT", fmt.Sprintf("/api/categories/%d", cat), obj{"is_sensitive": true})
			return id
		}},
		{"category un-flagged without confirm (B5)", func(t *testing.T, c *client) uint {
			cat := c.category(secCat, true)
			id := newItem(c, func(b obj) { b["category_id"] = cat })
			c.ok(200, "PUT", fmt.Sprintf("/api/categories/%d", cat), obj{"is_sensitive": false})
			return id
		}},
		{"category deleted with uncategorize (B4)", func(t *testing.T, c *client) uint {
			cat := c.category(secCat, true)
			id := newItem(c, func(b obj) { b["category_id"] = cat })
			c.ok(409, "DELETE", fmt.Sprintf("/api/categories/%d", cat), nil)
			c.ok(200, "DELETE", fmt.Sprintf("/api/categories/%d?uncategorize=1", cat), nil)
			return id
		}},
		{"category deleted with move to a public category", func(t *testing.T, c *client) uint {
			cat := c.category(secCat, true)
			pub := c.category("Public category", false)
			id := newItem(c, func(b obj) { b["category_id"] = cat })
			c.ok(200, "DELETE", fmt.Sprintf("/api/categories/%d?move_to=%d", cat, pub), nil)
			return id
		}},
		{"item moved out of the sensitive category", func(t *testing.T, c *client) uint {
			cat := c.category(secCat, true)
			pub := c.category("Public category", false)
			id := newItem(c, func(b obj) { b["category_id"] = cat })
			c.ok(200, "PATCH", fmt.Sprintf("/api/items/%d", id), obj{"category_id": pub})
			return id
		}},
		{"item moved to no category", func(t *testing.T, c *client) uint {
			cat := c.category(secCat, true)
			id := newItem(c, func(b obj) { b["category_id"] = cat })
			c.ok(200, "PATCH", fmt.Sprintf("/api/items/%d", id), obj{"category_id": nil})
			return id
		}},
		{"sensitive project at creation", func(t *testing.T, c *client) uint {
			proj := c.project(secProj, true)
			id := newItem(c, func(b obj) {})
			require.NoError(t, c.s.db.Write().Model(&dbmodels.Item{}).Where("id = ?", id).Update("project_id", proj).Error)
			return id
		}},
		{"project flagged afterwards", func(t *testing.T, c *client) uint {
			proj := c.project(secProj, false)
			id := newItem(c, func(b obj) {})
			require.NoError(t, c.s.db.Write().Model(&dbmodels.Item{}).Where("id = ?", id).Update("project_id", proj).Error)
			c.ok(200, "PATCH", fmt.Sprintf("/api/projects/%d", proj), obj{"is_sensitive": true})
			return id
		}},
		{"project inherits sensitivity from category", func(t *testing.T, c *client) uint {
			cat := c.category(secCat, true)
			r := c.ok(http.StatusCreated, "POST", "/api/projects", obj{"name": secProj, "category_id": cat, "is_sensitive": false})
			proj := uint(jsonOf[obj](c.t, r)["id"].(float64))
			id := newItem(c, func(b obj) {})
			require.NoError(t, c.s.db.Write().Model(&dbmodels.Item{}).Where("id = ?", id).Update("project_id", proj).Error)
			return id
		}},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			s := startStack(t, "2025-09-10", stackOpts{})
			c := s.newClient()
			c.createItem(obj{"name": pubName, "tracker_type": "boolean", "rule": fixedRule("FREQ=DAILY", "none")})
			id := sc.setup(t, c)
			assertProtected(t, s, c, id)
			// Server logs never contain item data either.
			noLeak(t, "server logs", s.logs.String())
		})
	}
}

// Options of a sensitive counter are redacted too. Making a protected item
// public takes an explicit act: confirm_exposure for items protected only
// through their category, the item's own setting for items flagged at creation.
func TestSensitiveOptionsAndExplicitExposure(t *testing.T) {
	s := startStack(t, "2025-09-10", stackOpts{})
	c := s.newClient()
	cat := c.category(secCat, true)
	id := c.createItem(obj{"name": secName, "tracker_type": "counter", "category_id": cat,
		"options": []obj{{"name": secOpt, "unit": "km", "target_value": 2}},
		"rule":    obj{"schedule_type": "fixed_rrule", "rrule_string": "FREQ=DAILY", "dtstart": "2025-09-01", "completion_rule": "all"}})
	for _, p := range []string{"/api/items", fmt.Sprintf("/api/items/%d", id), fmt.Sprintf("/api/items/%d/stats", id), "/api/view/today"} {
		noLeak(t, p, c.ok(200, "GET", withRedact(p), nil).Body)
	}
	rt := c.token("read", "")
	public := func(id uint) bool {
		return strings.Contains(s.newClient().doHdr("GET", fmt.Sprintf("/api/items/%d", id), nil, bearer(rt)).Body, secName)
	}

	// Created inside a sensitive category: the item carries its own flag, so
	// removing the category with confirm_exposure=1 does not publish it.
	c.ok(200, "DELETE", fmt.Sprintf("/api/categories/%d?uncategorize=1&confirm_exposure=1", cat), nil)
	require.False(t, public(id))
	c.ok(200, "PATCH", fmt.Sprintf("/api/items/%d", id), obj{"is_sensitive": false})
	require.True(t, public(id), "turning the item's own flag off is the explicit act")

	// Protected only dynamically through the category: confirm_exposure publishes.
	cat2 := c.category(secCat, false)
	id2 := c.createItem(obj{"name": secName + "-2", "tracker_type": "boolean", "category_id": cat2, "rule": fixedRule("FREQ=DAILY", "none")})
	c.ok(200, "PUT", fmt.Sprintf("/api/categories/%d", cat2), obj{"is_sensitive": true})
	require.False(t, public(id2))
	r := c.ok(200, "DELETE", fmt.Sprintf("/api/categories/%d?uncategorize=1&confirm_exposure=1", cat2), nil)
	require.Contains(t, r.Body, `"affected_items":1`)
	require.True(t, public(id2))
}

// Export is a plaintext backup by design: it must demand re-authentication
// when logins are on, and must not be reachable with API tokens.
func TestExportIsPlaintextAndGuarded(t *testing.T) {
	s := startStack(t, "2025-09-10", stackOpts{mut: func(cfg *config.Config) { cfg.Auth.SharedSecret = "correct horse battery" }})
	c := s.newClient()
	c.login("correct horse battery")
	s.clk.Advance(10 * time.Minute) // the login's re-authentication window has passed
	c.createItem(obj{"name": secName, "tracker_type": "boolean", "is_sensitive": true, "rule": fixedRule("FREQ=DAILY", "none")})
	r := c.do("GET", "/api/export", nil)
	require.Equal(t, 403, r.Status)
	require.Contains(t, r.Body, "reauth_required")
	noLeak(t, "export without reauth", r.Body)
	c.reauth("correct horse battery")
	r = c.ok(200, "GET", "/api/export", nil)
	require.Contains(t, r.Body, secName, "export is a plaintext backup by design")
	// Tokens never reach export.
	tok := c.token("read", "")
	require.Equal(t, 401, s.newClient().doHdr("GET", "/api/export", nil, bearer(tok)).Status)
	noLeak(t, "server logs", s.logs.String())
}

// Sub-items of a sensitive parent are sensitive too, including when the
// parent's sensitivity is dynamic (category flagged after creation) or arrives
// after the sub-item exists.
func TestSubItemsInheritSensitivity(t *testing.T) {
	s := startStack(t, "2025-09-10", stackOpts{})
	c := s.newClient()
	c.createItem(obj{"name": pubName, "tracker_type": "boolean", "rule": fixedRule("FREQ=DAILY", "none")})

	cat := c.category(secCat, false)
	parent := c.createItem(obj{"name": "parent", "tracker_type": "boolean", "kind": "event", "category_id": cat,
		"rule": obj{"schedule_type": "none", "holiday_mode": "none"}})
	c.ok(200, "PUT", fmt.Sprintf("/api/categories/%d", cat), obj{"is_sensitive": true})
	child := c.createItem(obj{"name": secName, "tracker_type": "boolean", "kind": "event",
		"parent_id": parent, "rule": fixedRule("FREQ=DAILY", "none")})
	assertProtected(t, s, c, child)

	// Sensitivity arriving after the sub-item exists propagates to it.
	p2 := c.createItem(obj{"name": "parent2", "tracker_type": "boolean", "kind": "event",
		"rule": obj{"schedule_type": "none", "holiday_mode": "none"}})
	child2 := c.createItem(obj{"name": secName, "tracker_type": "boolean", "kind": "event",
		"parent_id": p2, "rule": fixedRule("FREQ=DAILY", "none")})
	c.ok(200, "PATCH", fmt.Sprintf("/api/items/%d", p2), obj{"is_sensitive": true})
	assertProtected(t, s, c, child2)
}

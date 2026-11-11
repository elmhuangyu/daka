package api

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidEmoji(t *testing.T) {
	for s, want := range map[string]bool{
		"": true, "🥤": true, "👨‍👩‍👧‍👦": true, "1️⃣": true, "🇨🇦": true,
		"a": false, "abc": false, " 🥤": false, "🥤\n": false, "🥤🥤🥤🥤🥤🥤🥤🥤🥤": false,
	} {
		if got := validEmoji(s); got != want {
			t.Errorf("validEmoji(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestEmojiOnItemsAndProjects(t *testing.T) {
	e := newEnv(t)

	// Optional: omitted means none.
	code, plain := e.createItem("u1", rruleItem("Plain", "FREQ=DAILY"))
	require.Equal(t, 201, code)
	require.Empty(t, plain.Emoji)

	body := rruleItem("Cola", "FREQ=DAILY")
	body["emoji"] = "🥤"
	code, it := e.createItem("u1", body)
	require.Equal(t, 201, code)
	require.Equal(t, "🥤", it.Emoji)

	bad := rruleItem("Bad", "FREQ=DAILY")
	bad["emoji"] = "cola"
	code, _ = e.createItem("u1", bad)
	require.Equal(t, 400, code)

	// Change and clear.
	w := e.do("PATCH", fmt.Sprintf("/api/items/%d", it.ID), "u1", "", m{"emoji": "🍺"})
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "🍺", e.rawItem(it.ID).Emoji)
	w = e.do("PATCH", fmt.Sprintf("/api/items/%d", it.ID), "u1", "", m{"emoji": ""})
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Empty(t, e.rawItem(it.ID).Emoji)
	w = e.do("PATCH", fmt.Sprintf("/api/items/%d", it.ID), "u1", "", m{"emoji": "no"})
	require.Equal(t, 400, w.Code)

	p := e.createProject("u1", m{"name": "Trip", "emoji": "✈️"})
	require.Equal(t, "✈️", p.Emoji)
	w = e.do("PATCH", fmt.Sprintf("/api/projects/%d", p.ID), "u1", "", m{"emoji": ""})
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Empty(t, e.rawProject(p.ID).Emoji)
	w = e.do("POST", "/api/projects", "u1", "", m{"name": "X", "emoji": "x y"})
	require.Equal(t, 400, w.Code)
}

func TestEmojiIsRedactedForSensitive(t *testing.T) {
	e := newEnv(t)
	body := rruleItem("Secret", "FREQ=DAILY")
	body["emoji"] = "🍺"
	body["is_sensitive"] = true
	code, it := e.createItem("u1", body)
	require.Equal(t, 201, code)
	p := e.createProject("u1", m{"name": "Hidden", "emoji": "🎁", "is_sensitive": true})

	for _, url := range []string{"/api/items?redact=1", fmt.Sprintf("/api/items/%d?redact=1", it.ID), "/api/projects?redact=1", fmt.Sprintf("/api/projects/%d?redact=1", p.ID)} {
		w := e.do("GET", url, "u1", "", nil)
		require.Equal(t, 200, w.Code, url)
		require.NotContains(t, w.Body.String(), "🍺", url)
		require.NotContains(t, w.Body.String(), "🎁", url)
	}
	w := e.do("GET", "/api/items", "u1", "", nil)
	require.Contains(t, w.Body.String(), "🍺")
}

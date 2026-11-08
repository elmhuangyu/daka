package e2e

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// F1 / C1 (automatable part): documents the current VALARM behaviour of the
// feed. DESIGN 3.4 and 6.1 *require* a VALARM on timed events (items with
// remind_time) for clients that honour it (Apple Calendar); Google Calendar
// ignores VALARM in subscribed feeds, which is why Telegram is the primary
// reminder channel. All-day events carry no alarm. Whether Google really shows
// no reminder can only be verified manually (README, "Manual verification").
func TestFeedVAlarmStructure(t *testing.T) {
	s := startStack(t, "2025-09-10", stackOpts{})
	c := s.newClient()
	allDay := c.createItem(obj{"name": "All day", "tracker_type": "boolean", "rule": fixedRule("FREQ=DAILY", "none")})
	timed := c.createItem(obj{"name": "Timed", "tracker_type": "boolean", "rule": fixedRule("FREQ=DAILY", "none")})
	c.ok(200, "PATCH", fmt.Sprintf("/api/items/%d", timed), obj{"remind_time": "21:30"})
	tok := c.token("feed:read", "exclude_sensitive")

	r := s.newClient().ok(200, "GET", "/api/calendar/feed.ics?token="+tok, nil)
	require.Equal(t, "text/calendar; charset=utf-8", r.Header.Get("Content-Type"))
	events := strings.Split(r.Body, "BEGIN:VEVENT\r\n")[1:]
	require.NotEmpty(t, events)
	var nTimed, nAllDay int
	for _, ev := range events {
		ev = strings.SplitN(ev, "END:VEVENT", 2)[0]
		switch {
		case strings.Contains(ev, "SUMMARY:Timed"):
			nTimed++
			require.Contains(t, ev, "T133000Z")
			require.Equal(t, 1, strings.Count(ev, "BEGIN:VALARM\r\n"), "timed events carry exactly one alarm")
			require.Contains(t, ev, "ACTION:DISPLAY\r\n")
			require.Contains(t, ev, "TRIGGER:PT0S\r\n")
			require.Contains(t, ev, fmt.Sprintf("UID:daka-%d-", timed))
		case strings.Contains(ev, "SUMMARY:All day"):
			nAllDay++
			require.Contains(t, ev, "DTSTART;VALUE=DATE:")
			require.NotContains(t, ev, "VALARM", "all-day events have no alarm")
			require.Contains(t, ev, fmt.Sprintf("UID:daka-%d-", allDay))
		}
	}
	require.Equal(t, 181, nTimed, "from the rule start (today) to 180 days ahead")
	require.Equal(t, 181, nAllDay)
	// 21:30 Asia/Shanghai is 13:30 UTC.
	require.Contains(t, r.Body, "DTSTART:20250910T133000Z")

	// ETag revalidation.
	etag := r.Header.Get("ETag")
	require.NotEmpty(t, etag)
	nm := s.newClient().doHdr("GET", "/api/calendar/feed.ics?token="+tok, nil, map[string]string{"If-None-Match": etag})
	require.Equal(t, 304, nm.Status)
	// The feed is read-only and uniformly refuses bad credentials.
	require.Equal(t, 405, s.newClient().do("POST", "/api/calendar/feed.ics?token="+tok, obj{}).Status)
	bad := s.newClient().do("GET", "/api/calendar/feed.ics?token=dk_nope", nil)
	require.Equal(t, 401, bad.Status)
	require.Equal(t, "no-referrer", s.newClient().do("GET", "/api/calendar/feed.ics?token="+tok, nil).Header.Get("Referrer-Policy"))
}

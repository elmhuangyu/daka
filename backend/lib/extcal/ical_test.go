package extcal

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	l, err := time.LoadLocation(name)
	require.NoError(t, err)
	return l
}

func wrap(events ...string) []byte {
	return []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\n" + strings.Join(events, "") + "END:VCALENDAR\r\n")
}

func vevent(lines ...string) string {
	return "BEGIN:VEVENT\r\n" + strings.Join(lines, "\r\n") + "\r\nEND:VEVENT\r\n"
}

// parseNov parses with the user in Asia/Shanghai (UTC+8, no DST) over
// [2026-11-01, 2026-12-01).
func parseNov(t *testing.T, data []byte) ParseResult {
	t.Helper()
	loc := mustLoc(t, "Asia/Shanghai")
	res, err := Parse(data, ParseOptions{
		Loc:  loc,
		From: time.Date(2026, 11, 1, 0, 0, 0, 0, loc),
		To:   time.Date(2026, 12, 1, 0, 0, 0, 0, loc),
	})
	require.NoError(t, err)
	return res
}

func at(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.ParseInLocation("2006-01-02 15:04", s, mustLoc(t, "Asia/Shanghai"))
	require.NoError(t, err)
	return tm
}

func requireEvent(t *testing.T, e ExtEvent, title, start, end string, allDay bool) {
	t.Helper()
	require.Equal(t, title, e.Title)
	require.True(t, e.Start.Equal(at(t, start)), "start %v want %s", e.Start, start)
	require.True(t, e.End.Equal(at(t, end)), "end %v want %s", e.End, end)
	require.Equal(t, allDay, e.AllDay)
	require.Equal(t, "Asia/Shanghai", e.Start.Location().String())
}

func TestParseTimedUTCAndTZIDAndFloating(t *testing.T) {
	res := parseNov(t, wrap(
		// 02:00Z-03:30Z == 10:00-11:30 in Shanghai.
		vevent("UID:a", "SUMMARY:Standup", "DTSTART:20261110T020000Z", "DTEND:20261110T033000Z"),
		// 09:00 New York (EST, UTC-5 on Nov 10) == 14:00Z == 22:00 Shanghai.
		vevent("UID:b", "SUMMARY:NY call", "DTSTART;TZID=America/New_York:20261110T090000", "DURATION:PT1H"),
		// Floating time is read in the user's zone.
		vevent("UID:c", "SUMMARY:Floating", "DTSTART:20261112T180000", "DTEND:20261112T190000"),
	))
	require.Zero(t, res.Skipped)
	require.Len(t, res.Events, 3)
	requireEvent(t, res.Events[0], "Standup", "2026-11-10 10:00", "2026-11-10 11:30", false)
	requireEvent(t, res.Events[1], "NY call", "2026-11-10 22:00", "2026-11-10 23:00", false)
	requireEvent(t, res.Events[2], "Floating", "2026-11-12 18:00", "2026-11-12 19:00", false)
}

func TestParseAllDay(t *testing.T) {
	res := parseNov(t, wrap(
		vevent("UID:a", "SUMMARY:Trip", "DTSTART;VALUE=DATE:20261105", "DTEND;VALUE=DATE:20261107"),
		vevent("UID:b", "SUMMARY:Holiday", "DTSTART;VALUE=DATE:20261120"), // no DTEND: one day
		vevent("UID:c", "SUMMARY:Outside", "DTSTART;VALUE=DATE:20270101"),
		vevent("UID:d", "SUMMARY:Ends at window start", "DTSTART;VALUE=DATE:20261030", "DTEND;VALUE=DATE:20261101"),
		vevent("UID:e", "SUMMARY:Started before", "DTSTART;VALUE=DATE:20261030", "DTEND;VALUE=DATE:20261102"),
	))
	require.Len(t, res.Events, 3)
	requireEvent(t, res.Events[0], "Started before", "2026-10-30 00:00", "2026-11-02 00:00", true)
	requireEvent(t, res.Events[1], "Trip", "2026-11-05 00:00", "2026-11-07 00:00", true)
	requireEvent(t, res.Events[2], "Holiday", "2026-11-20 00:00", "2026-11-21 00:00", true)
}

func TestParseWeeklyRRuleWithExdate(t *testing.T) {
	// Mondays 01:00Z == 09:00 Shanghai: Nov 2, 9, 16 (COUNT=3); Nov 9 excluded.
	res := parseNov(t, wrap(vevent(
		"UID:w", "SUMMARY:Weekly", "DTSTART:20261102T010000Z", "DTEND:20261102T013000Z",
		"RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=3", "EXDATE:20261109T010000Z",
	)))
	require.Len(t, res.Events, 2)
	requireEvent(t, res.Events[0], "Weekly", "2026-11-02 09:00", "2026-11-02 09:30", false)
	requireEvent(t, res.Events[1], "Weekly", "2026-11-16 09:00", "2026-11-16 09:30", false)
}

func TestParseRRuleUnboundedOnlyWindow(t *testing.T) {
	// Started in 2020; every Friday 12:00Z == 20:00 Shanghai. Fridays in Nov 2026: 6, 13, 20, 27.
	res := parseNov(t, wrap(vevent(
		"UID:w", "SUMMARY:Fri", "DTSTART:20200103T120000Z", "DTEND:20200103T130000Z", "RRULE:FREQ=WEEKLY;BYDAY=FR",
	)))
	require.Len(t, res.Events, 4)
	for i, d := range []string{"06", "13", "20", "27"} {
		requireEvent(t, res.Events[i], "Fri", "2026-11-"+d+" 20:00", "2026-11-"+d+" 21:00", false)
	}
}

func TestParseAllDayRRuleAndWindowEdge(t *testing.T) {
	// 3 daily all-day occurrences from Nov 30; only Nov 30 is inside the window.
	res := parseNov(t, wrap(vevent(
		"UID:d", "SUMMARY:Daily", "DTSTART;VALUE=DATE:20261130", "RRULE:FREQ=DAILY;COUNT=3",
	)))
	require.Len(t, res.Events, 1)
	requireEvent(t, res.Events[0], "Daily", "2026-11-30 00:00", "2026-12-01 00:00", true)

	// Multi-day all-day recurrence that starts before the window and overlaps it.
	res = parseNov(t, wrap(vevent(
		"UID:m", "SUMMARY:Camp", "DTSTART;VALUE=DATE:20261028", "DTEND;VALUE=DATE:20261102", "RRULE:FREQ=MONTHLY;COUNT=2",
	)))
	require.Len(t, res.Events, 2) // Oct 28-Nov 2 (overlaps) and Nov 28-Dec 3
	requireEvent(t, res.Events[0], "Camp", "2026-10-28 00:00", "2026-11-02 00:00", true)
	requireEvent(t, res.Events[1], "Camp", "2026-11-28 00:00", "2026-12-03 00:00", true)
}

func TestParseRRuleAcrossDST(t *testing.T) {
	// Weekly 09:00 New York wall-clock. Oct 31 is EDT (UTC-4) = 13:00Z; after the
	// Nov 1 fall-back, Nov 7 is EST (UTC-5) = 14:00Z = 22:00 Shanghai.
	res := parseNov(t, wrap(vevent(
		"UID:d", "SUMMARY:NY weekly", "DTSTART;TZID=America/New_York:20261031T090000", "DTEND;TZID=America/New_York:20261031T100000",
		"RRULE:FREQ=WEEKLY;COUNT=2",
	)))
	// The Oct 31 occurrence (21:00 Shanghai) is before the window; only Nov 7 remains.
	require.Len(t, res.Events, 1)
	requireEvent(t, res.Events[0], "NY weekly", "2026-11-07 22:00", "2026-11-07 23:00", false)
}

func TestParseFoldedEscapedAndLineEndings(t *testing.T) {
	data := "BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:f\nSUMMARY:Lunch\\, with \\nthe te\n am \\\\ ok\nDTSTART;TZID=\"Asia/Shanghai\":20261103T120000\nDTEND;TZID=Asia/Shanghai:20261103T130000\nEND:VEVENT\nEND:VCALENDAR\n"
	res := parseNov(t, []byte(data))
	require.Len(t, res.Events, 1)
	requireEvent(t, res.Events[0], "Lunch, with \nthe team \\ ok", "2026-11-03 12:00", "2026-11-03 13:00", false)

	// Folded continuation with a tab, CRLF, and a property value containing a colon.
	data = "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nSUMMARY:Meet at: Room\r\n\t 5\r\nDTSTART:20261103T040000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	res = parseNov(t, []byte(data))
	require.Len(t, res.Events, 1)
	require.Equal(t, "Meet at: Room 5", res.Events[0].Title)
}

func TestParseMalformedEventsSkipped(t *testing.T) {
	res := parseNov(t, wrap(
		vevent("UID:1", "SUMMARY:No start"),
		vevent("UID:2", "SUMMARY:Bad date", "DTSTART:notadate"),
		vevent("UID:3", "SUMMARY:Bad rrule", "DTSTART:20261103T040000Z", "RRULE:FREQ=NEVER"),
		vevent("UID:4", "SUMMARY:Secondly", "DTSTART:20261103T040000Z", "RRULE:FREQ=SECONDLY"),
		vevent("UID:5", "SUMMARY:Bad duration", "DTSTART:20261103T040000Z", "DURATION:garbage"),
		vevent("UID:6", "SUMMARY:Good", "DTSTART:20261103T040000Z", "DTEND:20261103T050000Z"),
		"BEGIN:VEVENT\r\nSUMMARY:Unterminated\r\n",
	))
	require.Equal(t, 5, res.Skipped)
	require.Len(t, res.Events, 1)
	require.Equal(t, "Good", res.Events[0].Title)
}

func TestParseUnknownTZIDTreatedAsUTC(t *testing.T) {
	res := parseNov(t, wrap(vevent("UID:z", "SUMMARY:Win", "DTSTART;TZID=China Standard Time:20261103T040000", "DTEND;TZID=China Standard Time:20261103T050000")))
	require.Len(t, res.Events, 1)
	requireEvent(t, res.Events[0], "Win", "2026-11-03 12:00", "2026-11-03 13:00", false) // 04:00 UTC
	require.Equal(t, []string{"unknown TZID treated as UTC"}, res.Warnings)
}

func TestParseCancelledAndRecurrenceOverrides(t *testing.T) {
	res := parseNov(t, wrap(
		vevent("UID:x", "SUMMARY:Cancelled", "STATUS:CANCELLED", "DTSTART:20261103T040000Z"),
		vevent("UID:s", "SUMMARY:Series", "DTSTART:20261102T010000Z", "DTEND:20261102T020000Z", "RRULE:FREQ=WEEKLY;COUNT=3"),
		// The Nov 9 instance moved to Nov 10 03:00Z.
		vevent("UID:s", "SUMMARY:Series moved", "RECURRENCE-ID:20261109T010000Z", "DTSTART:20261110T030000Z", "DTEND:20261110T040000Z"),
	))
	require.Len(t, res.Events, 3)
	requireEvent(t, res.Events[0], "Series", "2026-11-02 09:00", "2026-11-02 10:00", false)
	requireEvent(t, res.Events[1], "Series moved", "2026-11-10 11:00", "2026-11-10 12:00", false)
	requireEvent(t, res.Events[2], "Series", "2026-11-16 09:00", "2026-11-16 10:00", false)
}

func TestParseIgnoresAlarmsAndTimezoneBlocks(t *testing.T) {
	data := "BEGIN:VCALENDAR\r\nBEGIN:VTIMEZONE\r\nTZID:X\r\nBEGIN:STANDARD\r\nDTSTART:19700101T000000\r\nEND:STANDARD\r\nEND:VTIMEZONE\r\n" +
		"BEGIN:VEVENT\r\nSUMMARY:Real\r\nDTSTART:20261103T040000Z\r\nBEGIN:VALARM\r\nSUMMARY:alarm\r\nDTSTART:20261104T040000Z\r\nEND:VALARM\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	res := parseNov(t, []byte(data))
	require.Len(t, res.Events, 1)
	require.Equal(t, "Real", res.Events[0].Title)
}

func TestParseCapsEventsAndWindow(t *testing.T) {
	loc := time.UTC
	daily := wrap(vevent("UID:d", "SUMMARY:Daily", "DTSTART;VALUE=DATE:20260101", "RRULE:FREQ=DAILY"))
	// Window of 1000 days is clamped to 366 days.
	res, err := Parse(daily, ParseOptions{Loc: loc, From: time.Date(2026, 1, 1, 0, 0, 0, 0, loc), To: time.Date(2028, 10, 1, 0, 0, 0, 0, loc)})
	require.NoError(t, err)
	require.Len(t, res.Events, 366)
	require.False(t, res.Truncated)

	res, err = Parse(daily, ParseOptions{Loc: loc, From: time.Date(2026, 1, 1, 0, 0, 0, 0, loc), To: time.Date(2026, 12, 1, 0, 0, 0, 0, loc), MaxEvents: 10})
	require.NoError(t, err)
	require.Len(t, res.Events, 10)
	require.True(t, res.Truncated)

	now := time.Now()
	_, err = Parse(daily, ParseOptions{From: now, To: now})
	require.ErrorIs(t, err, ErrEmptyWindow)
}

func TestParseDuration(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"PT1H":    time.Hour,
		"PT1H30M": 90 * time.Minute,
		"P1D":     24 * time.Hour,
		"P1W":     7 * 24 * time.Hour,
		"P1DT2H":  26 * time.Hour,
		"PT45S":   45 * time.Second,
		"-PT15M":  -15 * time.Minute,
	} {
		got, ok := parseDuration(in)
		require.True(t, ok, in)
		require.Equal(t, want, got, in)
	}
	for _, bad := range []string{"", "P", "1H", "PT", "PTH", "P1X", "PT1"} {
		_, ok := parseDuration(bad)
		require.False(t, ok, bad)
	}
}

func TestParseMaliciousLongEventsFeedIsBounded(t *testing.T) {
	var evs []string
	for i := 0; i < 167; i++ {
		evs = append(evs, vevent("UID:m"+strconv.Itoa(i), "SUMMARY:x", "DTSTART:20000101T000000Z", "DTEND:99991231T235959Z", "RRULE:FREQ=DAILY"))
	}
	evs = append(evs, vevent("UID:p", "SUMMARY:P", "DTSTART:20261103T040000Z", "DURATION:P1000000W"))
	data := wrap(evs...)
	start := time.Now()
	res := parseNov(t, data)
	require.Less(t, time.Since(start), 300*time.Millisecond)
	require.LessOrEqual(t, len(res.Events), MaxEvents)
}

func TestParseHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	loc := time.UTC
	data := wrap(vevent("UID:d", "SUMMARY:D", "DTSTART:20000101T000000Z", "RRULE:FREQ=DAILY"))
	_, err := Parse(data, ParseOptions{Loc: loc, Context: ctx, From: time.Date(2026, 11, 1, 0, 0, 0, 0, loc), To: time.Date(2026, 12, 1, 0, 0, 0, 0, loc)})
	require.ErrorIs(t, err, context.Canceled)
}

func TestParseOldDailyRuleStillExpands(t *testing.T) {
	res := parseNov(t, wrap(vevent("UID:d", "SUMMARY:D", "DTSTART:20000101T000000Z", "DTEND:20000101T010000Z", "RRULE:FREQ=DAILY")))
	require.Len(t, res.Events, 30)
	require.Zero(t, res.Skipped)
}

func TestUnfoldIsLinear(t *testing.T) {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nSUMMARY:a\r\n")
	for b.Len() < 5<<20 {
		b.WriteString(" x\r\n")
	}
	b.WriteString("DTSTART:20261103T040000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	start := time.Now()
	res := parseNov(t, []byte(b.String()))
	require.Less(t, time.Since(start), time.Second)
	require.Len(t, res.Events, 1)
	require.LessOrEqual(t, len(res.Events[0].Title), maxTitleRunes)
}

func TestParseNeverMatchingRulesAreBounded(t *testing.T) {
	rules := []string{
		"FREQ=DAILY;BYMONTH=2;BYMONTHDAY=30",
		"FREQ=DAILY;BYYEARDAY=366;BYMONTH=2",
		"FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=30",
	}
	evs := []string{vevent("UID:ok", "SUMMARY:Legit", "DTSTART:20261103T040000Z", "DTEND:20261103T050000Z")}
	for i := 0; i < 30000; i++ {
		evs = append(evs, vevent("UID:n"+strconv.Itoa(i), "SUMMARY:x", "DTSTART:20000101T000000Z", "RRULE:"+rules[i%3]))
	}
	data := wrap(evs...)
	t.Logf("feed size %d bytes", len(data))
	start := time.Now()
	res := parseNov(t, data)
	require.Less(t, time.Since(start), 3*time.Second)
	require.NotEmpty(t, res.Events)
	require.Equal(t, "Legit", res.Events[0].Title)
	require.Positive(t, res.Skipped)
}

func TestParseAncientDtstartSkipped(t *testing.T) {
	res := parseNov(t, wrap(vevent("UID:a", "SUMMARY:Old", "DTSTART:18500101T000000Z", "RRULE:FREQ=YEARLY")))
	require.Empty(t, res.Events)
	require.Equal(t, 1, res.Skipped)
}

package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/extcal"
	"github.com/elmhuangyu/daka/backend/lib/secret"
)

const testSecretKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type hostMap map[string]string

func (h hostMap) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if ip, ok := h[host]; ok {
		return []netip.Addr{netip.MustParseAddr(ip)}, nil
	}
	return nil, net.UnknownNetworkError("no such host")
}

// calEnv is an env whose router has an external calendar service backed by a
// fake resolver and a local TLS server (no real DNS or network).
type calEnv struct {
	*env
	port string
}

const extICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\n" +
	"BEGIN:VEVENT\r\nUID:1\r\nSUMMARY:Dentist <script>\r\nDTSTART:20261110T020000Z\r\nDTEND:20261110T030000Z\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:2\r\nSUMMARY:Holiday\r\nDTSTART;VALUE=DATE:20261120\r\nDTEND;VALUE=DATE:20261122\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func newCalEnv(t *testing.T, withKey bool) *calEnv {
	t.Helper()
	e := newEnv(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/slow") {
			select {
			case <-time.After(3 * time.Second):
			case <-r.Context().Done():
			}
			return
		}
		_, _ = w.Write([]byte(extICS))
	}))
	t.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	f := extcal.NewFetcher(extcal.FetcherOptions{
		Resolver: hostMap{"example.com": "8.8.8.8", "internal.test": "10.0.0.7"},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, srv.Listener.Addr().String())
		},
		TLSConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		AnyPort:   true,
		Timeout:   300 * time.Millisecond,
	})
	opts := extcal.ServiceOptions{Fetcher: f}
	if withKey {
		k, err := secret.NewKeyring(testSecretKey)
		require.NoError(t, err)
		opts.Keys = k
	}
	e.h = NewRouter(Deps{DB: e.db, Now: func() time.Time { return e.now }, Auth: userHeaderAuth, ExtCal: extcal.NewService(opts)})
	return &calEnv{env: e, port: port}
}

func (e *calEnv) url(path string) string { return "https://example.com:" + e.port + path }

func (e *env) settings(user string) map[string]any {
	e.t.Helper()
	w := e.do("GET", "/api/settings/calendar", user, "", nil)
	require.Equal(e.t, 200, w.Code, w.Body.String())
	var out map[string]any
	require.NoError(e.t, json.Unmarshal(w.Body.Bytes(), &out))
	return out
}

func (e *env) rawColumn(user string) string {
	e.t.Helper()
	var st dbmodels.UserSettings
	require.NoError(e.t, e.db.Write().Where("user_id = ?", user).Take(&st).Error)
	return st.ExternalCalendarURLs
}

func (e *env) enc(user string) string {
	e.t.Helper()
	es, err := extcal.Entries(e.rawColumn(user))
	require.NoError(e.t, err)
	require.Len(e.t, es, 1)
	return es[0].Enc
}

func (e *env) stateVersion(user string) int64 {
	e.t.Helper()
	var st dbmodels.UserSettings
	require.NoError(e.t, e.db.Write().Where("user_id = ?", user).Take(&st).Error)
	return st.StateVersion
}

func TestSettingsCalendarDefaultsAndPartialUpdate(t *testing.T) {
	e := newCalEnv(t, true)
	got := e.settings("")
	require.Equal(t, "Asia/Shanghai", got["timezone"])
	require.Equal(t, []any{}, got["external_calendars"])
	require.Equal(t, false, got["streak_holiday_freeze"])

	v0 := e.stateVersion("u1")
	w := e.do("PUT", "/api/settings/calendar", "", "", m{"timezone": "America/New_York", "week_start": 0, "holiday_region": "US", "streak_holiday_freeze": true})
	require.Equal(t, 200, w.Code, w.Body.String())
	got = e.settings("")
	require.Equal(t, "America/New_York", got["timezone"])
	require.EqualValues(t, 0, got["week_start"])
	require.Equal(t, "US", got["holiday_region"])
	require.Equal(t, true, got["streak_holiday_freeze"])
	require.Equal(t, v0+1, e.stateVersion("u1"))

	// Unchanged values and external-only changes do not bump state_version.
	w = e.do("PUT", "/api/settings/calendar", "", "", m{"timezone": "America/New_York", "week_start": 0})
	require.Equal(t, 200, w.Code)
	w = e.do("PUT", "/api/settings/calendar", "", "", m{"external_calendars": []m{{"name": "A", "url": e.url("/a.ics")}}})
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, v0+1, e.stateVersion("u1"))
	// Other users are untouched.
	require.Equal(t, "Asia/Shanghai", e.settings("u2")["timezone"])
	require.Equal(t, []any{}, e.settings("u2")["external_calendars"])
}

func TestSettingsCalendarValidation(t *testing.T) {
	e := newCalEnv(t, true)
	for _, body := range []any{
		m{"timezone": "Mars/Base"}, m{"timezone": ""}, m{"week_start": 2}, m{"week_start": -1},
		m{"holiday_region": "FR"}, `{"bogus":1}`, `not json`,
		m{"external_calendars": []m{{"name": "A"}}}, // new source without url
		m{"external_calendars": []m{{"name": "A", "url": "http://example.com/x"}}},
		m{"external_calendars": []m{{"name": "A", "url": "https://127.0.0.1/x"}}},
	} {
		w := e.do("PUT", "/api/settings/calendar", "", "", body)
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	require.Equal(t, "Asia/Shanghai", e.settings("")["timezone"])
	// Four sources are too many.
	var cals []m
	for _, n := range []string{"a", "b", "c", "d"} {
		cals = append(cals, m{"id": n, "name": n, "url": e.url("/" + n + ".ics")})
	}
	require.Equal(t, 400, e.do("PUT", "/api/settings/calendar", "", "", m{"external_calendars": cals}).Code)
	require.Equal(t, 200, e.do("PUT", "/api/settings/calendar", "", "", m{"external_calendars": cals[:3]}).Code)
}

func TestSettingsCalendarNeverExposesURLAndDBIsEncrypted(t *testing.T) {
	e := newCalEnv(t, true)
	secretURL := e.url("/private/feed/abc123/basic.ics?key=TOPSECRETKEY")
	w := e.do("PUT", "/api/settings/calendar", "", "", m{"external_calendars": []m{{"id": "work", "name": "Work", "url": secretURL}}})
	require.Equal(t, 200, w.Code, w.Body.String())
	for _, body := range []string{w.Body.String(), e.do("GET", "/api/settings/calendar", "", "", nil).Body.String()} {
		require.NotContains(t, body, "TOPSECRETKEY")
		require.NotContains(t, body, "abc123")
		require.Contains(t, body, `"url_hint":"https://example.com:`+e.port+`/…"`)
		require.NotContains(t, body, `"url":`)
	}
	raw := e.rawColumn("u1")
	for _, plain := range []string{"TOPSECRETKEY", "example.com", "abc123", "basic.ics", "private"} {
		require.NotContains(t, raw, plain, "plaintext leaked into the DB column")
	}
	require.Contains(t, raw, "v1:")
}

func TestSettingsCalendarMaskedPlaceholderDoesNotOverwrite(t *testing.T) { // B11
	e := newCalEnv(t, true)
	require.Equal(t, 200, e.do("PUT", "/api/settings/calendar", "", "", m{"external_calendars": []m{{"id": "work", "name": "Work", "url": e.url("/p/feed.ics?key=K")}}}).Code)
	before := e.enc("u1")
	hint := e.settings("")["external_calendars"].([]any)[0].(map[string]any)["url_hint"].(string)

	// Replaying the GET response verbatim (url_hint included, no url) keeps the ciphertext.
	body := e.settings("")
	w := e.do("PUT", "/api/settings/calendar", "", "", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, before, e.enc("u1"))

	// Explicit null and a rename also keep it.
	w = e.do("PUT", "/api/settings/calendar", "", "", `{"external_calendars":[{"id":"work","name":"Renamed","url":null}]}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "Renamed")
	require.Equal(t, before, e.enc("u1"))

	// Sending the masked placeholder as the url is rejected and changes nothing.
	for _, bad := range []string{hint, "https://example.com/…", "https://example.com/***", ""} {
		w = e.do("PUT", "/api/settings/calendar", "", "", m{"external_calendars": []m{{"id": "work", "name": "Work", "url": bad}}})
		require.Equal(t, 400, w.Code, bad)
		require.Equal(t, before, e.enc("u1"))
	}
	// A genuinely new url replaces it.
	w = e.do("PUT", "/api/settings/calendar", "", "", m{"external_calendars": []m{{"id": "work", "name": "Work", "url": e.url("/other.ics")}}})
	require.Equal(t, 200, w.Code)
	require.NotEqual(t, before, e.enc("u1"))
	// Omitting the list entirely leaves the sources alone; an empty list removes them.
	after := e.enc("u1")
	require.Equal(t, 200, e.do("PUT", "/api/settings/calendar", "", "", m{"week_start": 0}).Code)
	require.Equal(t, after, e.enc("u1"))
	require.Equal(t, 200, e.do("PUT", "/api/settings/calendar", "", "", m{"external_calendars": []m{}}).Code)
	require.Empty(t, e.rawColumn("u1"))
}

func TestSettingsCalendarWithoutEncryptionKey(t *testing.T) {
	e := newCalEnv(t, false)
	w := e.do("PUT", "/api/settings/calendar", "", "", m{"external_calendars": []m{{"name": "A", "url": e.url("/a.ics")}}})
	require.Equal(t, 503, w.Code)
	require.JSONEq(t, `{"error":"encryption key not configured"}`, w.Body.String())
	require.Empty(t, e.rawColumn("u1"))
	// Everything else still works.
	require.Equal(t, 200, e.do("PUT", "/api/settings/calendar", "", "", m{"week_start": 0}).Code)
	require.Equal(t, 200, e.do("PUT", "/api/settings/calendar", "", "", m{"external_calendars": []m{}}).Code)
	// A default router (no ExtCal dep) behaves the same.
	d := newEnv(t)
	w = d.do("PUT", "/api/settings/calendar", "", "", m{"external_calendars": []m{{"name": "A", "url": "https://example.com/a.ics"}}})
	require.Equal(t, 503, w.Code)
}

type extResp struct {
	Sources []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		OK     bool   `json:"ok"`
		Error  string `json:"error"`
		Events []struct {
			Title  string `json:"title"`
			Start  string `json:"start"`
			End    string `json:"end"`
			AllDay bool   `json:"all_day"`
		} `json:"events"`
	} `json:"sources"`
}

func TestExternalCalendarPartialFailureAndIndependence(t *testing.T) { // D5, B2
	e := newCalEnv(t, true)
	secretQ := "?key=TOPSECRETKEY"
	w := e.do("PUT", "/api/settings/calendar", "", "", m{"external_calendars": []m{
		{"id": "good", "name": "Good", "url": e.url("/good.ics" + secretQ)},
		{"id": "slow", "name": "Slow", "url": e.url("/slow.ics" + secretQ)},
		{"id": "evil", "name": "Evil", "url": "https://internal.test:" + e.port + "/x.ics" + secretQ},
	}})
	require.Equal(t, 200, w.Code, w.Body.String())

	start := time.Now()
	w = e.do("GET", "/api/calendar/external?start=2026-11-01&end=2026-11-30", "", "", nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Less(t, time.Since(start), 2*time.Second)
	require.NotContains(t, w.Body.String(), "TOPSECRETKEY")
	require.NotContains(t, w.Body.String(), "internal.test")
	var r extResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &r))
	require.Len(t, r.Sources, 3)
	good, slow, evil := r.Sources[0], r.Sources[1], r.Sources[2]
	require.True(t, good.OK)
	require.Len(t, good.Events, 2)
	// 02:00Z on Nov 10 is 10:00 in Asia/Shanghai; the title is returned as-is (JSON-escaped on the wire).
	require.Equal(t, "Dentist <script>", good.Events[0].Title)
	require.Equal(t, "2026-11-10T10:00:00+08:00", good.Events[0].Start)
	require.Equal(t, "2026-11-10T11:00:00+08:00", good.Events[0].End)
	require.False(t, good.Events[0].AllDay)
	require.Equal(t, "2026-11-20", good.Events[1].Start)
	require.Equal(t, "2026-11-21", good.Events[1].End) // inclusive last day of a 2-day event
	require.True(t, good.Events[1].AllDay)
	require.False(t, slow.OK)
	require.Equal(t, "fetch_failed", slow.Error)
	require.Empty(t, slow.Events)
	require.False(t, evil.OK)
	require.Equal(t, "blocked_address", evil.Error)

	// source_id restricts to one source; the slow one does not affect the good one.
	w = e.do("GET", "/api/calendar/external?start=2026-11-01&end=2026-11-30&source_id=good", "", "", nil)
	require.Equal(t, 200, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &r))
	require.Len(t, r.Sources, 1)
	require.True(t, r.Sources[0].OK)
	require.Equal(t, 404, e.do("GET", "/api/calendar/external?start=2026-11-01&end=2026-11-30&source_id=nope", "", "", nil).Code)

	// Window outside the events: ok with no events. Other users see nothing.
	w = e.do("GET", "/api/calendar/external?start=2026-12-01&end=2026-12-05&source_id=good", "", "", nil)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &r))
	require.True(t, r.Sources[0].OK)
	require.Empty(t, r.Sources[0].Events)
	w = e.do("GET", "/api/calendar/external?start=2026-11-01&end=2026-11-30", "u2", "", nil)
	require.JSONEq(t, `{"start":"2026-11-01","end":"2026-11-30","timezone":"Asia/Shanghai","sources":[]}`, w.Body.String())
}

func TestExternalCalendarWindowValidation(t *testing.T) {
	e := newCalEnv(t, true)
	get := func(q string) int { return e.do("GET", "/api/calendar/external"+q, "", "", nil).Code }
	require.Equal(t, 200, get("?start=2026-11-01&end=2026-11-01"))
	require.Equal(t, 200, get("?start=2026-11-01&end=2026-12-31")) // 61 days
	require.Equal(t, 200, get("?start=2026-11-01&end=2027-01-01")) // 62 days
	require.Equal(t, 400, get("?start=2026-11-01&end=2027-01-02")) // 63 days
	require.Equal(t, 400, get("?start=2026-11-05&end=2026-11-01"))
	require.Equal(t, 400, get(""))
	require.Equal(t, 400, get("?start=2026-11-01"))
	require.Equal(t, 400, get("?start=nope&end=2026-11-02"))
}

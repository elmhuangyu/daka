package holiday

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newCal(t *testing.T, opts ...Option) (*Calendar, *Provider) {
	t.Helper()
	p, err := NewProvider(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return NewCalendar(p, RegionCN), p
}

func TestDayTable(t *testing.T) {
	c, _ := newCal(t)
	tests := []struct {
		date      string
		kind      Kind
		off       bool
		workday   bool
		statutory bool
	}{
		{"2025-10-01", Holiday, true, false, true},
		{"2025-10-08", Holiday, true, false, true},
		{"2025-10-09", Normal, false, true, false},  // Thursday after the span
		{"2025-09-28", Workday, false, true, false}, // Sunday makeup
		{"2025-10-11", Workday, false, true, false}, // Saturday makeup
		{"2025-01-26", Workday, false, true, false}, // Sunday makeup
		{"2025-01-28", Holiday, true, false, true},
		{"2025-02-04", Holiday, true, false, true},
		{"2025-02-08", Workday, false, true, false}, // Saturday makeup
		{"2025-03-08", Normal, true, false, false},  // plain Saturday
		{"2025-03-10", Normal, false, true, false},  // plain Monday
		{"2025-01-29", Holiday, true, false, true},  // Wednesday holiday
		{"garbage", Normal, false, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.date, func(t *testing.T) {
			d := c.Day(tc.date)
			if d.Kind != tc.kind || d.IsOffDay != tc.off {
				t.Errorf("Day = %+v, want kind=%s off=%v", d, tc.kind, tc.off)
			}
			if got := c.IsWorkday(tc.date); got != tc.workday {
				t.Errorf("IsWorkday = %v", got)
			}
			if got := c.IsStatutoryHoliday(tc.date); got != tc.statutory {
				t.Errorf("IsStatutoryHoliday = %v", got)
			}
		})
	}
	if c.Day("2025-10-01").Name == "" {
		t.Error("holiday name empty")
	}
}

func TestSpansComplete(t *testing.T) {
	c, _ := newCal(t)
	spans := [][2]string{{"2025-10-01", "2025-10-08"}, {"2025-01-28", "2025-02-04"}}
	for _, s := range spans {
		from, _ := time.Parse(dateLayout, s[0])
		to, _ := time.Parse(dateLayout, s[1])
		for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
			if !c.Day(d.Format(dateLayout)).IsOffDay {
				t.Errorf("%s should be off", d.Format(dateLayout))
			}
		}
	}
}

func TestUnpublishedYear(t *testing.T) {
	c, _ := newCal(t)
	if c.Published(2099) {
		t.Fatal("2099 should be unpublished")
	}
	// 2099-01-03 is a Saturday, 2099-01-05 a Monday.
	if d := c.Day("2099-01-03"); d.Kind != Normal || !d.IsOffDay {
		t.Errorf("weekend: %+v", d)
	}
	if !c.IsWorkday("2099-01-05") || c.IsWorkday("2099-01-03") {
		t.Error("plain weekday/weekend semantics broken")
	}
	yi := c.YearInfo(2099)
	if yi.Published || yi.Days == nil || len(yi.Days) != 0 {
		t.Errorf("YearInfo = %+v", yi)
	}
}

func TestUnsupportedRegion(t *testing.T) {
	_, p := newCal(t)
	fr := NewCalendar(p, "FR")
	if fr.Published(2025) || fr.IsStatutoryHoliday("2025-10-01") || !fr.IsWorkday("2025-10-01") {
		t.Error("FR must be plain weekend semantics")
	}
	if fr.IsWorkday("2025-10-11") {
		t.Error("plain Saturday is not a workday for FR")
	}
}

func TestYearInfo(t *testing.T) {
	c, _ := newCal(t)
	yi := c.YearInfo(2025)
	if !yi.Published || len(yi.Days) == 0 {
		t.Fatalf("YearInfo = %+v", yi)
	}
	for i := 1; i < len(yi.Days); i++ {
		if yi.Days[i-1].Date >= yi.Days[i].Date {
			t.Fatal("not sorted")
		}
	}
}

const validJSON = `{"year":2099,"days":[{"name":"X","date":"2099-01-01","isOffDay":true}]}`

func TestRefreshSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/2099.json" {
			_, _ = w.Write([]byte(validJSON))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c, p := newCal(t, WithURLTemplate(srv.URL+"/{year}.json"), WithAllowInsecure())
	p.Refresh(context.Background(), 2099, 2100)
	if !c.Published(2099) || !c.IsStatutoryHoliday("2099-01-01") {
		t.Error("refresh did not load 2099")
	}
	if c.Published(2100) {
		t.Error("404 year must stay unpublished")
	}
}

func TestRefreshFailureKeepsOld(t *testing.T) {
	big := `{"year":2025,"days":[],"pad":"` + strings.Repeat("a", maxBodyBytes) + `"}`
	cases := map[string]http.HandlerFunc{
		"500":       func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) },
		"malformed": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{nope")) },
		"oversized": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(big)) },
		"wrongyear": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(validJSON)) },
		"empty":     func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"year":2025,"days":[]}`)) },
		"baddate": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"year":2025,"days":[{"name":"x","date":"2025-13-40","isOffDay":true}]}`))
		},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			defer srv.Close()
			c, p := newCal(t, WithURLTemplate(srv.URL+"/{year}.json"), WithAllowInsecure())
			p.Refresh(context.Background(), 2025)
			if !c.IsStatutoryHoliday("2025-10-01") || c.Day("2025-09-28").Kind != Workday {
				t.Error("old data lost")
			}
		})
	}
}

func TestRefreshUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close() // connection refused
	c, p := newCal(t, WithURLTemplate(u+"/{year}.json"), WithAllowInsecure())
	p.Refresh(context.Background(), 2025, 2026)
	if !c.Published(2025) {
		t.Error("offline must keep snapshot")
	}
}

func TestRejectNonHTTPS(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(validJSON))
	}))
	defer srv.Close()
	c, p := newCal(t, WithURLTemplate(srv.URL+"/{year}.json"))
	p.Refresh(context.Background(), 2099)
	if hits.Load() != 0 || c.Published(2099) {
		t.Error("http url must be rejected without option")
	}
	if _, err := p.buildURL(2099); err == nil {
		t.Error("expected error")
	}
}

func TestRunDailyRefresh(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()
	ticks := make(chan time.Time)
	now := func() time.Time { return time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC) }
	after := func(time.Duration) <-chan time.Time { return ticks }
	_, p := newCal(t, WithURLTemplate(srv.URL+"/{year}.json"), WithNagerURLTemplate(srv.URL+"/{country}/{year}"), WithAllowInsecure(), WithClock(now, after))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.RunDailyRefresh(ctx); close(done) }()
	ticks <- time.Now() // first round done, second starts
	ticks <- time.Now() // second round done, third starts
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunDailyRefresh did not stop")
	}
	if got := hits.Load(); got < 6 {
		t.Errorf("hits = %d, want >= 6", got)
	}
}

func TestNagerRegions(t *testing.T) {
	_, p := newCal(t)
	tests := []struct {
		region, date string
		holiday      bool
	}{
		{"US", "2026-07-03", true}, // Independence Day, observed Friday
		{"US", "2026-07-06", false},
		{"CA", "2026-07-01", true},  // Canada Day (national)
		{"CA", "2026-02-16", false}, // provincial only
		{"CA-MB", "2026-02-16", true},
		{"CA-ON", "2026-02-16", true}, // Family Day in Ontario
		{"CA-QC", "2026-02-16", false},
		{"CN", "2026-07-01", false},
		{"FR", "2026-07-14", false},
	}
	for _, tc := range tests {
		t.Run(tc.region+"/"+tc.date, func(t *testing.T) {
			c := NewCalendar(p, tc.region)
			if got := c.IsStatutoryHoliday(tc.date); got != tc.holiday {
				t.Errorf("IsStatutoryHoliday = %v, want %v", got, tc.holiday)
			}
		})
	}
	if !NewCalendar(p, "CA-ON").Published(2026) || NewCalendar(p, "FR").Published(2026) {
		t.Error("Published mismatch")
	}
}

func TestValidRegion(t *testing.T) {
	for r, want := range map[string]bool{
		"CN": true, "US": true, "CA": true, "CA-ON": true, "US-NY": true,
		"": false, "FR": false, "CA-XX": false, "US-ON": false, "CN-11": false, "ca": false,
	} {
		if got := ValidRegion(r); got != want {
			t.Errorf("ValidRegion(%q) = %v, want %v", r, got, want)
		}
	}
}

func TestRefreshNager(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/CA/2099" {
			_, _ = w.Write([]byte(`[{"date":"2099-01-01","name":"X","global":true,"types":["Public"]},` +
				`{"date":"2099-02-02","name":"Y","global":false,"counties":["CA-BC"],"types":["Public"]},` +
				`{"date":"2099-03-03","name":"Z","global":true,"types":["Observance"]}]`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	_, p := newCal(t, WithNagerURLTemplate(srv.URL+"/{country}/{year}"), WithAllowInsecure())
	p.RefreshNager(context.Background(), 2099)
	bc, on := NewCalendar(p, "CA-BC"), NewCalendar(p, "CA-ON")
	if !bc.IsStatutoryHoliday("2099-01-01") || !bc.IsStatutoryHoliday("2099-02-02") {
		t.Error("BC data missing")
	}
	if !on.IsStatutoryHoliday("2099-01-01") || on.IsStatutoryHoliday("2099-02-02") || on.IsStatutoryHoliday("2099-03-03") {
		t.Error("ON filtering wrong")
	}
	if NewCalendar(p, "US").Published(2099) {
		t.Error("204 year must stay unpublished")
	}
}

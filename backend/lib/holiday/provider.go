package holiday

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

//go:embed data/*.json
var snapshotFS embed.FS

const (
	// DefaultURLTemplate is the default remote source; {year} is replaced.
	DefaultURLTemplate = "https://raw.githubusercontent.com/NateScarlet/holiday-cn/master/{year}.json"
	// DefaultRefreshInterval is the interval used by RunDailyRefresh.
	DefaultRefreshInterval = 24 * time.Hour

	fetchTimeout = 10 * time.Second
	maxBodyBytes = 5 << 20
	dateLayout   = "2006-01-02"
)

var errNotPublished = errors.New("holiday: year not published")

// rawDay mirrors one entry of the holiday-cn JSON file.
type rawDay struct {
	Name     string `json:"name"`
	Date     string `json:"date"`
	IsOffDay bool   `json:"isOffDay"`
}

type rawYear struct {
	Year int      `json:"year"`
	Days []rawDay `json:"days"`
}

// Provider holds holiday-cn data per year in memory and refreshes it remotely.
type Provider struct {
	mu            sync.RWMutex
	years         map[int]map[string]rawDay
	urlTemplate   string
	client        *http.Client
	allowInsecure bool
	now           func() time.Time
	after         func(time.Duration) <-chan time.Time
	interval      time.Duration
}

// Option configures a Provider.
type Option func(*Provider)

// WithURLTemplate sets the remote URL template ({year} is substituted).
func WithURLTemplate(t string) Option { return func(p *Provider) { p.urlTemplate = t } }

// WithHTTPClient replaces the HTTP client (the 10s per-request timeout still applies).
func WithHTTPClient(c *http.Client) Option { return func(p *Provider) { p.client = c } }

// WithAllowInsecure permits non-https URLs. Intended for tests only.
func WithAllowInsecure() Option { return func(p *Provider) { p.allowInsecure = true } }

// WithClock injects the time source used by RunDailyRefresh.
func WithClock(now func() time.Time, after func(time.Duration) <-chan time.Time) Option {
	return func(p *Provider) { p.now, p.after = now, after }
}

// WithRefreshInterval sets the interval between refresh rounds.
func WithRefreshInterval(d time.Duration) Option { return func(p *Provider) { p.interval = d } }

// NewProvider creates a Provider preloaded with the embedded snapshot.
// It never touches the network and fails only if the embedded data is corrupt.
func NewProvider(opts ...Option) (*Provider, error) {
	p := &Provider{
		years:       map[int]map[string]rawDay{},
		urlTemplate: DefaultURLTemplate,
		now:         time.Now,
		after:       time.After,
		interval:    DefaultRefreshInterval,
	}
	for _, o := range opts {
		o(p)
	}
	if p.client == nil {
		p.client = &http.Client{}
	}
	entries, err := snapshotFS.ReadDir("data")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		year, err := strconv.Atoi(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			return nil, fmt.Errorf("holiday: bad snapshot name %q", e.Name())
		}
		b, err := snapshotFS.ReadFile("data/" + e.Name())
		if err != nil {
			return nil, err
		}
		days, err := parseYear(b, year)
		if err != nil {
			return nil, fmt.Errorf("holiday: snapshot %s: %w", e.Name(), err)
		}
		p.years[year] = days
	}
	return p, nil
}

// parseYear validates and indexes a holiday-cn document for the given year.
func parseYear(b []byte, year int) (map[string]rawDay, error) {
	var ry rawYear
	if err := json.Unmarshal(b, &ry); err != nil {
		return nil, err
	}
	if ry.Year != year {
		return nil, fmt.Errorf("year mismatch: got %d want %d", ry.Year, year)
	}
	if len(ry.Days) == 0 {
		return nil, errors.New("no days")
	}
	out := make(map[string]rawDay, len(ry.Days))
	for _, d := range ry.Days {
		t, err := time.Parse(dateLayout, d.Date)
		if err != nil {
			return nil, fmt.Errorf("bad date %q", d.Date)
		}
		if t.Year() != year {
			return nil, fmt.Errorf("date %s outside year %d", d.Date, year)
		}
		out[d.Date] = d
	}
	return out, nil
}

func (p *Provider) lookup(year int) (map[string]rawDay, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	m, ok := p.years[year]
	return m, ok
}

func (p *Provider) buildURL(year int) (string, error) {
	raw := strings.ReplaceAll(p.urlTemplate, "{year}", strconv.Itoa(year))
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" && (!p.allowInsecure || u.Scheme != "http") {
		return "", fmt.Errorf("holiday: url scheme %q not allowed", u.Scheme)
	}
	return raw, nil
}

// fetch downloads and validates one year. The cache is not touched.
func (p *Provider) fetch(ctx context.Context, year int) (map[string]rawDay, error) {
	raw, err := p.buildURL(year)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	client := *p.client
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if r.URL.Scheme != "https" && !p.allowInsecure {
			return errors.New("redirect to non-https url")
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errNotPublished
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBodyBytes {
		return nil, errors.New("response body too large")
	}
	return parseYear(body, year)
}

// Refresh fetches the given years. A failing year keeps its old cache entry
// and logs a warning; it never returns an error. A 404 means "not yet
// published" and is logged at debug level only.
func (p *Provider) Refresh(ctx context.Context, years ...int) {
	for _, y := range years {
		days, err := p.fetch(ctx, y)
		switch {
		case err == nil:
			p.mu.Lock()
			p.years[y] = days
			p.mu.Unlock()
			log.Info().Int("year", y).Int("days", len(days)).Msg("holiday data refreshed")
		case errors.Is(err, errNotPublished):
			log.Debug().Int("year", y).Msg("holiday data not published yet")
		default:
			log.Warn().Err(err).Int("year", y).Msg("holiday data refresh failed; keeping cached data")
		}
	}
}

// RunDailyRefresh refreshes previous, current and next year immediately and
// then once per interval until ctx is cancelled.
func (p *Provider) RunDailyRefresh(ctx context.Context) {
	for {
		y := p.now().Year()
		p.Refresh(ctx, y-1, y, y+1)
		select {
		case <-ctx.Done():
			return
		case <-p.after(p.interval):
		}
	}
}

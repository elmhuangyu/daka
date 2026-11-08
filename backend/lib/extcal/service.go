package extcal

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/elmhuangyu/daka/backend/lib/secret"
)

// Cache defaults.
const (
	DefaultCacheTTL        = 15 * time.Minute
	DefaultCacheMaxEntries = 64
	DefaultCacheMaxBytes   = 32 << 20
)

// ErrUnknownSource is returned when a requested source id does not exist.
var ErrUnknownSource = errors.New("extcal: unknown source")

// ServiceOptions configures a Service. The zero value is usable: no
// encryption key (saving URLs fails with secret.ErrNoKey), production fetcher.
type ServiceOptions struct {
	Keys      *secret.Keyring
	AllowHTTP bool
	// Fetcher overrides the default one (tests).
	Fetcher *Fetcher
	Now     func() time.Time
	// CacheTTL, CacheMaxEntries and CacheMaxBytes bound the per-URL cache.
	CacheTTL        time.Duration
	CacheMaxEntries int
	CacheMaxBytes   int
}

// Service combines the encrypted store, the safe fetcher and a per-URL cache.
type Service struct {
	Store   *Store
	fetcher *Fetcher
	cache   *cache
	parsed  *parsedCache
}

// NewService builds a Service.
func NewService(o ServiceOptions) *Service {
	f := o.Fetcher
	if f == nil {
		f = NewFetcher(FetcherOptions{AllowHTTP: o.AllowHTTP})
	}
	now := o.Now
	if now == nil {
		now = time.Now
	}
	ttl, n, b := o.CacheTTL, o.CacheMaxEntries, o.CacheMaxBytes
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	if n <= 0 {
		n = DefaultCacheMaxEntries
	}
	if b <= 0 {
		b = DefaultCacheMaxBytes
	}
	return &Service{Store: NewStore(o.Keys, o.AllowHTTP), fetcher: f, cache: newCache(now, ttl, n, b), parsed: newParsedCache(now, ttl, n)}
}

// SourceResult is the outcome for one source; failures are isolated (D5).
type SourceResult struct {
	ID        string
	Name      string
	OK        bool
	Error     string // short code, never contains the URL
	Events    []ExtEvent
	Skipped   int
	Truncated bool
}

// Load fetches (or serves from cache) and parses every source of the stored
// column independently and concurrently. sourceID, when non-empty, restricts
// the call to that source.
func (s *Service) Load(ctx context.Context, userID, column, sourceID string, opt ParseOptions) ([]SourceResult, error) {
	es, err := Entries(column)
	if err != nil {
		return nil, err
	}
	if sourceID != "" {
		var picked []Entry
		for _, e := range es {
			if e.ID == sourceID {
				picked = append(picked, e)
			}
		}
		if len(picked) == 0 {
			return nil, ErrUnknownSource
		}
		es = picked
	}
	out := make([]SourceResult, len(es))
	var wg sync.WaitGroup
	for i, e := range es {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = s.loadOne(ctx, userID, e, opt)
		}()
	}
	wg.Wait()
	return out, nil
}

func (s *Service) loadOne(ctx context.Context, userID string, e Entry, opt ParseOptions) SourceResult {
	r := SourceResult{ID: e.ID, Name: e.Name}
	fail := func(code string) SourceResult {
		r.Error = code
		return r
	}
	rawURL, err := s.Store.URL(userID, e)
	if err != nil {
		if errors.Is(err, secret.ErrNoKey) {
			return fail("no_key")
		}
		return fail("decrypt_failed")
	}
	// Both caches are shared across users and keyed by the hash of the URL:
	// anyone who possesses the URL can fetch the feed anyway, so sharing leaks
	// nothing beyond what the URL already grants.
	sum := sha256.Sum256([]byte(rawURL))
	pk := parsedKey{sum: sum, from: opt.From.Unix(), to: opt.To.Unix(), loc: locName(opt.Loc)}
	if res, ok := s.parsed.get(pk); ok {
		r.OK, r.Events, r.Skipped, r.Truncated = true, res.Events, res.Skipped, res.Truncated
		return r
	}
	data, ok := s.cache.get(sum)
	if !ok {
		data, err = s.fetcher.Fetch(ctx, rawURL)
		if err != nil {
			// Log only the error class, never the URL.
			log.Warn().Str("source", e.ID).Str("code", Code(err)).Msg("external calendar fetch failed")
			return fail(Code(err))
		}
		s.cache.put(sum, data)
	}
	opt.Context = ctx
	res, err := Parse(data, opt)
	if err != nil {
		return fail("parse_failed")
	}
	if res.Events == nil {
		res.Events = []ExtEvent{}
	}
	s.parsed.put(pk, res)
	r.OK, r.Events, r.Skipped, r.Truncated = true, res.Events, res.Skipped, res.Truncated
	if r.Events == nil {
		r.Events = []ExtEvent{}
	}
	return r
}

type cacheEntry struct {
	data []byte
	exp  time.Time
}

// cache keeps raw feed bodies keyed by sha256(url), bounded by entry count and
// total bytes (oldest inserted evicted first).
type cache struct {
	now        func() time.Time
	ttl        time.Duration
	maxEntries int
	maxBytes   int

	mu    sync.Mutex
	m     map[[32]byte]cacheEntry
	order [][32]byte
	bytes int
}

func newCache(now func() time.Time, ttl time.Duration, maxEntries, maxBytes int) *cache {
	return &cache{now: now, ttl: ttl, maxEntries: maxEntries, maxBytes: maxBytes, m: map[[32]byte]cacheEntry{}}
}

func (c *cache) get(k [32]byte) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[k]
	if !ok {
		return nil, false
	}
	if !c.now().Before(e.exp) {
		c.removeLocked(k)
		return nil, false
	}
	return e.data, true
}

func (c *cache) put(k [32]byte, data []byte) {
	if len(data) > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.m[k]; ok {
		c.removeLocked(k)
	}
	for len(c.order) > 0 && (len(c.m) >= c.maxEntries || c.bytes+len(data) > c.maxBytes) {
		c.removeLocked(c.order[0])
	}
	c.m[k] = cacheEntry{data: data, exp: c.now().Add(c.ttl)}
	c.order = append(c.order, k)
	c.bytes += len(data)
}

func (c *cache) removeLocked(k [32]byte) {
	if e, ok := c.m[k]; ok {
		c.bytes -= len(e.data)
		delete(c.m, k)
	}
	for i, o := range c.order {
		if o == k {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

func (c *cache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.m)
}

func locName(l *time.Location) string {
	if l == nil {
		return "UTC"
	}
	return l.String()
}

type parsedKey struct {
	sum      [32]byte
	from, to int64
	loc      string
}

type parsedEntry struct {
	res ParseResult
	exp time.Time
}

// parsedCache memoizes parse results per (url hash, window, timezone) so a
// cached feed is not re-parsed on every request. It is bounded by entries
// (each holds at most MaxEvents events) and expires with the raw-body TTL.
type parsedCache struct {
	now        func() time.Time
	ttl        time.Duration
	maxEntries int

	mu    sync.Mutex
	m     map[parsedKey]parsedEntry
	order []parsedKey
}

func newParsedCache(now func() time.Time, ttl time.Duration, maxEntries int) *parsedCache {
	return &parsedCache{now: now, ttl: ttl, maxEntries: maxEntries, m: map[parsedKey]parsedEntry{}}
}

func (c *parsedCache) get(k parsedKey) (ParseResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[k]
	if !ok || !c.now().Before(e.exp) {
		return ParseResult{}, false
	}
	return e.res, true
}

func (c *parsedCache) put(k parsedKey, r ParseResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.m[k]; !ok {
		for len(c.order) > 0 && len(c.m) >= c.maxEntries {
			delete(c.m, c.order[0])
			c.order = c.order[1:]
		}
		c.order = append(c.order, k)
	}
	c.m[k] = parsedEntry{res: r, exp: c.now().Add(c.ttl)}
}

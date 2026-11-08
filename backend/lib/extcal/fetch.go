package extcal

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// Fetch errors. Their messages are static: they never include the URL, its
// query or any resolved address, because feed URLs embed secrets.
var (
	ErrBlockedAddress   = errors.New("extcal: address not allowed")
	ErrScheme           = errors.New("extcal: unsupported URL scheme")
	ErrInvalidURL       = errors.New("extcal: invalid URL")
	ErrTooLarge         = errors.New("extcal: response too large")
	ErrTimeout          = errors.New("extcal: timeout")
	ErrTooManyRedirects = errors.New("extcal: too many redirects")
	ErrBadStatus        = errors.New("extcal: unexpected HTTP status")
	ErrNotICalendar     = errors.New("extcal: response is not an iCalendar")
	ErrFetch            = errors.New("extcal: fetch failed")
)

// Fetcher limits.
const (
	DefaultTimeout      = 10 * time.Second
	DefaultMaxBody      = 5 << 20
	DefaultMaxRedirects = 3
	userAgent           = "daka-extcal"
)

// Resolver resolves host names; *net.Resolver satisfies it.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// FetcherOptions configures a Fetcher. The zero value is the production setup.
type FetcherOptions struct {
	// AllowHTTP permits http:// URLs (tests / development only).
	AllowHTTP bool
	// Resolver defaults to net.DefaultResolver.
	Resolver Resolver
	// DialContext dials an already validated "ip:port" address. Defaults to a
	// net.Dialer whose Control hook re-validates the final peer address.
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	// TLSConfig overrides the client TLS configuration (tests inject roots).
	TLSConfig *tls.Config
	// AnyPort lifts the port restriction (443 only; 80 and 443 with AllowHTTP is
	// NOT enough on its own). Tests only: AllowHTTP also allows any port.
	AnyPort bool
	// Timeout is the total request budget including redirects (default 10s).
	Timeout time.Duration
	// MaxBody is the response size cap in bytes (default 5 MiB).
	MaxBody int64
	// MaxRedirects is the number of redirect hops followed (default 3).
	MaxRedirects int
}

// Fetcher downloads iCalendar feeds safely. Safe for concurrent use.
type Fetcher struct {
	client       *http.Client
	allowHTTP    bool
	anyPort      bool
	timeout      time.Duration
	maxBody      int64
	maxRedirects int
}

// NewFetcher builds a Fetcher.
func NewFetcher(o FetcherOptions) *Fetcher {
	f := &Fetcher{allowHTTP: o.AllowHTTP, anyPort: o.AnyPort || o.AllowHTTP, timeout: o.Timeout, maxBody: o.MaxBody, maxRedirects: o.MaxRedirects}
	if f.timeout <= 0 {
		f.timeout = DefaultTimeout
	}
	if f.maxBody <= 0 {
		f.maxBody = DefaultMaxBody
	}
	if f.maxRedirects <= 0 {
		f.maxRedirects = DefaultMaxRedirects
	}
	res := o.Resolver
	if res == nil {
		res = net.DefaultResolver
	}
	dial := o.DialContext
	if dial == nil {
		d := &net.Dialer{Timeout: f.timeout, Control: controlCheck}
		dial = d.DialContext
	}
	tr := &http.Transport{
		Proxy:                  nil, // never honor proxy environment variables
		DialContext:            safeDialer(res, dial),
		TLSClientConfig:        o.TLSConfig,
		TLSHandshakeTimeout:    f.timeout,
		ResponseHeaderTimeout:  f.timeout,
		DisableKeepAlives:      true,
		MaxResponseHeaderBytes: 64 << 10,
	}
	f.client = &http.Client{
		Transport: tr,
		Jar:       nil, // no cookies
		Timeout:   f.timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > f.maxRedirects {
				return ErrTooManyRedirects
			}
			// Every hop is re-checked; the dialer re-validates the address.
			return f.checkURL(req.URL)
		},
	}
	return f
}

func (f *Fetcher) checkURL(u *url.URL) error {
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !f.allowHTTP {
			return ErrScheme
		}
	default:
		return ErrScheme
	}
	if u.Hostname() == "" {
		return ErrInvalidURL
	}
	// Only the standard HTTPS port, so the fetcher cannot be used to probe
	// arbitrary services. allow_http (dev / tests) lifts the restriction.
	if !f.anyPort {
		if port := u.Port(); port != "" && port != "443" {
			return ErrBlockedAddress
		}
	}
	return nil
}

// safeDialer resolves the host, validates EVERY resolved address and dials one
// of those validated IPs directly, so there is no second resolution a DNS
// rebinding attack could exploit.
func safeDialer(res Resolver, dial func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, ErrInvalidURL
		}
		var ips []netip.Addr
		if ip, perr := netip.ParseAddr(host); perr == nil {
			ips = []netip.Addr{ip}
		} else {
			ips, err = res.LookupNetIP(ctx, "ip", host)
			if err != nil || len(ips) == 0 {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, ErrFetch
			}
		}
		for _, ip := range ips {
			if !IPAllowed(ip) {
				return nil, ErrBlockedAddress
			}
		}
		for _, ip := range ips {
			c, derr := dial(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
			if derr == nil {
				return c, nil
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		return nil, ErrFetch
	}
}

// controlCheck is the last line of defense on the real dialer: it validates
// the address the socket is about to connect to.
func controlCheck(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return ErrBlockedAddress
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !IPAllowed(ip) {
		return ErrBlockedAddress
	}
	return nil
}

// Fetch downloads rawURL and returns the body, which must look like an
// iCalendar document. Errors never contain the URL.
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, ErrInvalidURL
	}
	if err := f.checkURL(u); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, ErrInvalidURL
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/calendar, text/plain;q=0.5, */*;q=0.1")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, classify(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, ErrBadStatus
	}
	if resp.ContentLength > f.maxBody {
		return nil, ErrTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBody+1))
	if err != nil {
		return nil, classify(ctx, err)
	}
	if int64(len(body)) > f.maxBody {
		return nil, ErrTooLarge
	}
	if !looksLikeICalendar(body) {
		return nil, ErrNotICalendar
	}
	return body, nil
}

func looksLikeICalendar(b []byte) bool {
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	b = bytes.TrimLeft(b, " \t\r\n")
	return len(b) >= 15 && strings.EqualFold(string(b[:15]), "BEGIN:VCALENDAR")
}

// classify maps any transport error to a sentinel without echoing its text
// (net/http errors embed the full URL).
func classify(ctx context.Context, err error) error {
	for _, s := range []error{ErrBlockedAddress, ErrScheme, ErrInvalidURL, ErrTooManyRedirects, ErrTooLarge} {
		if errors.Is(err, s) {
			return s
		}
	}
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return ErrTimeout
	}
	return ErrFetch
}

// Code returns a short machine-readable error code for API responses.
// Transport-level failures (connect problems, timeouts, non-200 statuses,
// redirect limits) are deliberately collapsed into one code so the endpoint
// cannot serve as a port-scan / reachability oracle.
func Code(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrBlockedAddress):
		return "blocked_address"
	case errors.Is(err, ErrScheme):
		return "unsupported_scheme"
	case errors.Is(err, ErrInvalidURL):
		return "invalid_url"
	case errors.Is(err, ErrTooLarge):
		return "too_large"
	case errors.Is(err, ErrNotICalendar):
		return "not_icalendar"
	default:
		return "fetch_failed"
	}
}

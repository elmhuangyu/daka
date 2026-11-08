package ratelimit

import (
	"math"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// ClientIPResolver determines the client IP of a request. The zero value uses
// only RemoteAddr. X-Forwarded-For is honored only when the immediate peer is
// inside one of Trusted; the header is then walked right to left and the first
// address that is not itself a trusted proxy is the client.
type ClientIPResolver struct {
	Trusted []netip.Prefix
}

func (r ClientIPResolver) trusted(a netip.Addr) bool {
	for _, p := range r.Trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// ClientIP returns the client IP as a string; if RemoteAddr cannot be parsed
// it is returned verbatim so such requests still share one bucket.
func (r ClientIPResolver) ClientIP(req *http.Request) string {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	peer, err := netip.ParseAddr(strings.SplitN(host, "%", 2)[0])
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	if len(r.Trusted) == 0 || !r.trusted(peer) {
		return peer.String()
	}
	hops := strings.Split(strings.Join(req.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		a, ok := parseHop(hops[i])
		if !ok {
			return peer.String() // malformed chain: do not trust it
		}
		if !r.trusted(a) {
			return a.String()
		}
	}
	return peer.String()
}

// parseHop parses an X-Forwarded-For entry: an IP with an optional :port
// ("1.2.3.4:80", "[::1]:80"), unmapped.
func parseHop(s string) (netip.Addr, bool) {
	s = strings.TrimSpace(s)
	if a, err := netip.ParseAddr(s); err == nil {
		return a.Unmap(), true
	}
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().Unmap(), true
	}
	return netip.Addr{}, false
}

// WriteRateLimited writes the 429 response with a Retry-After header
// (whole seconds, rounded up, at least 1).
func WriteRateLimited(w http.ResponseWriter, retryAfter time.Duration) {
	secs := max(1, int(math.Ceil(retryAfter.Seconds())))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte(`{"error":"rate limited"}`))
}

// Middleware applies l per client IP (as determined by ips) to every request.
// A nil limiter returns next unchanged.
func Middleware(l *Limiter, ips ClientIPResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if l == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ok, wait := l.Allow(ips.ClientIP(r)); !ok {
				WriteRateLimited(w, wait)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

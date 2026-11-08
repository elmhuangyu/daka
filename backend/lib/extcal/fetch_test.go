package extcal

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const goodICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nEND:VCALENDAR\r\n"

type fakeResolver map[string][]string

func (r fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	v, ok := r[host]
	if !ok {
		return nil, fmt.Errorf("no such host")
	}
	var out []netip.Addr
	for _, s := range v {
		out = append(out, netip.MustParseAddr(s))
	}
	return out, nil
}

type rig struct {
	srv    *httptest.Server
	port   string
	mu     sync.Mutex
	dialed []string
}

func (r *rig) dialedAddrs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.dialed...)
}

// newRig starts a TLS server and returns a fetcher whose resolver knows a few
// fake hosts and whose dialer redirects every validated address to the server.
func newRig(t *testing.T, h http.Handler, mut func(*FetcherOptions)) (*rig, *Fetcher) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	r := &rig{srv: srv, port: port}
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	opts := FetcherOptions{
		Resolver: fakeResolver{
			"example.com":    {"8.8.8.8"},
			"loop.test":      {"127.0.0.1"},
			"ten.test":       {"10.1.2.3"},
			"meta.test":      {"169.254.169.254"},
			"v6loop.test":    {"::1"},
			"mapped.test":    {"::ffff:10.0.0.1"},
			"mixed.test":     {"8.8.8.8", "192.168.1.1"},
			"cgnat.test":     {"100.64.0.1"},
			"ula.test":       {"fd00::1"},
			"zero.test":      {"0.0.0.0"},
			"internal.test":  {"10.0.0.5"},
			"rebinding.test": {"8.8.4.4"},
		},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			r.mu.Lock()
			r.dialed = append(r.dialed, addr)
			r.mu.Unlock()
			var d net.Dialer
			return d.DialContext(ctx, network, srv.Listener.Addr().String())
		},
		TLSConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		AnyPort:   true,
	}
	if mut != nil {
		mut(&opts)
	}
	return r, NewFetcher(opts)
}

func ics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/calendar")
	_, _ = w.Write([]byte(goodICS))
}

func TestFetchPublicHostSucceedsAndDialsValidatedIP(t *testing.T) {
	var gotUA, gotCookie string
	r, f := newRig(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotUA = req.Header.Get("User-Agent")
		gotCookie = req.Header.Get("Cookie")
		ics(w, req)
	}), nil)
	body, err := f.Fetch(context.Background(), "https://example.com:"+r.port+"/cal.ics?key=abc")
	require.NoError(t, err)
	require.Equal(t, goodICS, string(body))
	require.Equal(t, "daka-extcal", gotUA)
	require.Empty(t, gotCookie)
	require.Equal(t, []string{"8.8.8.8:" + r.port}, r.dialedAddrs()) // the validated IP, not the host name
}

func TestFetchRejectsInternalResolutions(t *testing.T) {
	r, f := newRig(t, http.HandlerFunc(ics), nil)
	for _, host := range []string{"loop.test", "ten.test", "meta.test", "v6loop.test", "mapped.test", "mixed.test", "cgnat.test", "ula.test", "zero.test"} {
		_, err := f.Fetch(context.Background(), "https://"+host+":"+r.port+"/x.ics?secret=KEY123")
		require.ErrorIs(t, err, ErrBlockedAddress, host)
	}
	require.Empty(t, r.dialedAddrs(), "nothing may be dialed")
}

func TestFetchRejectsIPLiterals(t *testing.T) {
	r, f := newRig(t, http.HandlerFunc(ics), nil)
	for _, h := range []string{"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.0.1", "169.254.169.254", "[::1]", "[fd00::1]", "[::ffff:10.0.0.1]", "[::ffff:127.0.0.1]", "0.0.0.0", "100.64.1.1", "224.0.0.1", "[fe80::1]", "198.18.0.1"} {
		_, err := f.Fetch(context.Background(), "https://"+h+":"+r.port+"/x")
		require.ErrorIs(t, err, ErrBlockedAddress, h)
	}
	require.Empty(t, r.dialedAddrs())
}

func TestFetchSchemes(t *testing.T) {
	r, f := newRig(t, http.HandlerFunc(ics), nil)
	for _, u := range []string{"http://example.com/x", "ftp://example.com/x", "file:///etc/passwd", "gopher://example.com", "https:///nohost"} {
		_, err := f.Fetch(context.Background(), u)
		require.Error(t, err, u)
	}
	_, err := f.Fetch(context.Background(), "http://example.com/x")
	require.ErrorIs(t, err, ErrScheme)
	require.Empty(t, r.dialedAddrs())
}

func TestFetchAllowHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(ics))
	defer srv.Close()
	f := NewFetcher(FetcherOptions{
		AllowHTTP: true,
		Resolver:  fakeResolver{"example.com": {"8.8.8.8"}},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, srv.Listener.Addr().String())
		},
	})
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	_, err := f.Fetch(context.Background(), "http://example.com:"+port+"/x")
	require.NoError(t, err)
	// allow_http does not weaken the address checks.
	_, err = f.Fetch(context.Background(), "http://127.0.0.1:"+port+"/x")
	require.ErrorIs(t, err, ErrBlockedAddress)
}

func TestFetchRedirectToInternalHostRejected(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/to-internal", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "https://internal.test/admin?token=1", http.StatusFound)
	})
	mux.HandleFunc("/to-literal", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "https://169.254.169.254/latest/meta-data", http.StatusFound)
	})
	mux.HandleFunc("/to-http", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "http://example.com/x", http.StatusFound)
	})
	mux.HandleFunc("/ok-redirect", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "https://example.com:"+req.URL.Query().Get("p")+"/final", http.StatusFound)
	})
	mux.HandleFunc("/final", ics)
	r, f := newRig(t, mux, nil)
	base := "https://example.com:" + r.port
	_, err := f.Fetch(context.Background(), base+"/to-internal")
	require.ErrorIs(t, err, ErrBlockedAddress)
	_, err = f.Fetch(context.Background(), base+"/to-literal")
	require.ErrorIs(t, err, ErrBlockedAddress)
	_, err = f.Fetch(context.Background(), base+"/to-http")
	require.ErrorIs(t, err, ErrScheme)
	// A redirect to another public host is followed (and re-resolved/validated).
	_, err = f.Fetch(context.Background(), base+"/ok-redirect?p="+r.port)
	require.NoError(t, err)
	// Only the redirect-free validated IP of the public host was ever dialed.
	for _, a := range r.dialedAddrs() {
		require.Equal(t, "8.8.8.8:"+r.port, a)
	}
}

func TestFetchRedirectLimit(t *testing.T) {
	var hops int
	var mu sync.Mutex
	r, f := newRig(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		hops++
		mu.Unlock()
		http.Redirect(w, req, req.URL.Path, http.StatusFound) // endless loop
	}), nil)
	_, err := f.Fetch(context.Background(), "https://example.com:"+r.port+"/loop")
	require.ErrorIs(t, err, ErrTooManyRedirects)
	require.Equal(t, 4, hops) // original request + 3 followed hops

	// Exactly three redirects are still followed.
	n := 0
	r2, f2 := newRig(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if n++; n <= 3 {
			http.Redirect(w, req, "/next", http.StatusFound)
			return
		}
		ics(w, req)
	}), nil)
	_, err = f2.Fetch(context.Background(), "https://example.com:"+r2.port+"/start")
	require.NoError(t, err)
}

func TestFetchOversizeBody(t *testing.T) {
	big := goodICS + strings.Repeat("X-FILLER:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\r\n", 100)
	r, f := newRig(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(big))
	}), func(o *FetcherOptions) { o.MaxBody = 1000 })
	_, err := f.Fetch(context.Background(), "https://example.com:"+r.port+"/x")
	require.ErrorIs(t, err, ErrTooLarge)

	// Chunked response without Content-Length is capped as well.
	r2, f2 := newRig(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(goodICS))
		w.(http.Flusher).Flush()
		for i := 0; i < 100; i++ {
			_, _ = w.Write([]byte(strings.Repeat("a", 100)))
		}
	}), func(o *FetcherOptions) { o.MaxBody = 1000 })
	_, err = f2.Fetch(context.Background(), "https://example.com:"+r2.port+"/x")
	require.ErrorIs(t, err, ErrTooLarge)
}

func TestFetchSlowServerTimesOut(t *testing.T) {
	r, f := newRig(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		select {
		case <-time.After(3 * time.Second):
		case <-req.Context().Done():
		}
		ics(w, req)
	}), func(o *FetcherOptions) { o.Timeout = 150 * time.Millisecond })
	start := time.Now()
	_, err := f.Fetch(context.Background(), "https://example.com:"+r.port+"/x")
	require.ErrorIs(t, err, ErrTimeout)
	require.Less(t, time.Since(start), 2*time.Second)
}

func TestFetchContentChecks(t *testing.T) {
	r, f := newRig(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/404":
			http.NotFound(w, req)
		case "/html":
			_, _ = w.Write([]byte("<html>login</html>"))
		default:
			ics(w, req)
		}
	}), nil)
	base := "https://example.com:" + r.port
	_, err := f.Fetch(context.Background(), base+"/404")
	require.ErrorIs(t, err, ErrBadStatus)
	_, err = f.Fetch(context.Background(), base+"/html")
	require.ErrorIs(t, err, ErrNotICalendar)
	_, err = f.Fetch(context.Background(), "://bad")
	require.ErrorIs(t, err, ErrInvalidURL)
}

func TestFetchCredentialsInURLAllowedButNeverInErrors(t *testing.T) {
	var gotAuth string
	r, f := newRig(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotAuth = req.Header.Get("Authorization")
		ics(w, req)
	}), nil)
	_, err := f.Fetch(context.Background(), "https://alice:pw@example.com:"+r.port+"/cal.ics")
	require.NoError(t, err)
	require.Contains(t, gotAuth, "Basic")

	secrets := []string{"SECRETKEY123", "alice", "hunter2", "private/path"}
	urls := []string{
		"https://alice:hunter2@loop.test:" + r.port + "/private/path/cal.ics?key=SECRETKEY123",
		"https://10.0.0.1/private/path?key=SECRETKEY123",
		"http://example.com/private/path?key=SECRETKEY123",
		"https://nonexistent.test/private/path?key=SECRETKEY123",
	}
	for _, u := range urls {
		_, err := f.Fetch(context.Background(), u)
		require.Error(t, err, u)
		for _, s := range secrets {
			require.NotContains(t, err.Error(), s, u)
		}
		parsed, _ := url.Parse(u)
		require.NotContains(t, err.Error(), parsed.Host)
	}
	// Timeouts and redirect failures as well.
	r2, f2 := newRig(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, req.URL.String(), http.StatusFound)
	}), nil)
	_, err = f2.Fetch(context.Background(), "https://example.com:"+r2.port+"/private/path?key=SECRETKEY123")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "SECRETKEY123")
	require.NotContains(t, err.Error(), "private/path")
}

func TestFetchIgnoresProxyEnvironment(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	r, f := newRig(t, http.HandlerFunc(ics), nil)
	_, err := f.Fetch(context.Background(), "https://example.com:"+r.port+"/x")
	require.NoError(t, err)
}

func TestControlCheckOnRealDialer(t *testing.T) {
	require.ErrorIs(t, controlCheck("tcp", "127.0.0.1:80", nil), ErrBlockedAddress)
	require.ErrorIs(t, controlCheck("tcp", "[::ffff:10.0.0.1]:80", nil), ErrBlockedAddress)
	require.NoError(t, controlCheck("tcp", "8.8.8.8:443", nil))
	// The default (real) dialer refuses loopback even with an IP literal.
	f := NewFetcher(FetcherOptions{AllowHTTP: true, Timeout: time.Second})
	_, err := f.Fetch(context.Background(), "http://127.0.0.1:1/x")
	require.ErrorIs(t, err, ErrBlockedAddress)
}

func TestIPAllowed(t *testing.T) {
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111", "::ffff:8.8.8.8", "172.32.0.1", "100.63.255.255"} {
		require.True(t, IPAllowed(netip.MustParseAddr(s)), s)
	}
	for _, s := range []string{"127.0.0.1", "127.9.9.9", "10.0.0.1", "172.16.0.1", "172.31.255.255", "192.168.1.1", "169.254.169.254",
		"::1", "fc00::1", "fd12::1", "fe80::1", "100.64.0.1", "100.127.255.255", "224.0.0.1", "ff02::1", "0.0.0.0", "0.1.2.3", "::",
		"::ffff:10.0.0.1", "::ffff:127.0.0.1", "198.18.0.5", "192.0.2.1", "203.0.113.9", "255.255.255.255", "2001:db8::1", "64:ff9b::7f00:1"} {
		require.False(t, IPAllowed(netip.MustParseAddr(s)), s)
	}
}

func TestFetchPortRestriction(t *testing.T) {
	f := NewFetcher(FetcherOptions{Resolver: fakeResolver{"example.com": {"8.8.8.8"}}})
	for _, u := range []string{"https://example.com:22/x", "https://example.com:8443/x", "https://example.com:6379/"} {
		_, err := f.Fetch(context.Background(), u)
		require.ErrorIs(t, err, ErrBlockedAddress, u)
	}
	require.Equal(t, "fetch_failed", Code(ErrTimeout))
	require.Equal(t, "fetch_failed", Code(ErrBadStatus))
	require.Equal(t, "fetch_failed", Code(ErrFetch))
	require.Equal(t, "fetch_failed", Code(ErrTooManyRedirects))
	require.Equal(t, "blocked_address", Code(ErrBlockedAddress))
	require.Equal(t, "too_large", Code(ErrTooLarge))
	require.Equal(t, "not_icalendar", Code(ErrNotICalendar))
}

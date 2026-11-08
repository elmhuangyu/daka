package auth

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// HostAllowlist is the set of Host header names the server answers to.
type HostAllowlist struct {
	names map[string]bool
}

// NewHostAllowlist allows localhost, every loopback IP literal, the host part
// of the listen address (unless it is a wildcard) and the extra names, which
// come from server.allowed_hosts for reverse-proxy / LAN setups.
func NewHostAllowlist(listen string, extra []string) HostAllowlist {
	a := HostAllowlist{names: map[string]bool{"localhost": true}}
	if host, _, err := net.SplitHostPort(listen); err == nil && host != "" {
		ip, perr := netip.ParseAddr(strings.SplitN(host, "%", 2)[0])
		if perr != nil || !ip.IsUnspecified() {
			a.names[normHost(host)] = true
		}
	}
	for _, h := range extra {
		if h = strings.TrimSpace(h); h != "" {
			a.names[normHost(hostOnly(h))] = true
		}
	}
	return a
}

// hostOnly strips an optional port and IPv6 brackets.
func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return strings.Trim(hostport, "[]")
}

func normHost(h string) string { return strings.TrimSuffix(strings.ToLower(h), ".") }

// Allowed reports whether the Host header value (with optional port) is allowed.
func (a HostAllowlist) Allowed(hostport string) bool {
	h := normHost(hostOnly(hostport))
	if a.names[h] {
		return true
	}
	ip, err := netip.ParseAddr(h)
	return err == nil && ip.IsLoopback() // an IP literal cannot be a rebound DNS name
}

// HostGuard rejects every request (all methods) whose Host is not allowed with
// 403 {"error":"forbidden host"}. This defeats DNS rebinding, where an attacker
// page resolves its own name to 127.0.0.1 and thereby becomes "same-origin"
// with the loopback server.
func HostGuard(a HostAllowlist) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !a.Allowed(r.Host) {
				writeError(w, http.StatusForbidden, "forbidden host")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

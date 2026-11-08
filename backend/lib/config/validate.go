package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"
)

// IsLoopbackListen reports whether the listen address (host:port) binds only to
// a loopback interface. An empty host means all interfaces and is not loopback.
// Hostnames other than "localhost" are never resolved and count as non-loopback.
func IsLoopbackListen(listen string) (bool, error) {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false, fmt.Errorf("invalid server.listen %q: %w", listen, err)
	}
	if strings.EqualFold(host, "localhost") {
		return true, nil
	}
	ip, perr := netip.ParseAddr(strings.SplitN(host, "%", 2)[0])
	if perr != nil {
		return false, nil // not an IP literal: treat as non-loopback
	}
	return ip.IsLoopback(), nil
}

// ParseTrustedProxies parses server.trusted_proxies entries (IP addresses or
// CIDR prefixes) into prefixes. A bare IP becomes a /32 or /128.
func ParseTrustedProxies(entries []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(entries))
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if p, err := netip.ParsePrefix(e); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(e)
		if err != nil {
			return nil, fmt.Errorf("invalid server.trusted_proxies entry %q (want an IP or CIDR)", e)
		}
		a = a.Unmap()
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}

// Validate enforces the cross-field rules. In single-user mode (no
// auth.issuer) a non-loopback listen address requires both
// auth.allow_insecure_public and auth.shared_secret (DESIGN 8).
func (c *Config) Validate() error {
	if _, err := time.LoadLocation(c.Timezone); err != nil || c.Timezone == "" {
		return fmt.Errorf("invalid timezone %q (want an IANA name such as \"UTC\" or \"Asia/Shanghai\")", c.Timezone)
	}
	if c.DB.Driver == DriverSQLite {
		dsn := c.DB.DSN.Reveal()
		switch {
		case dsn == "":
			return errors.New("db.dsn must not be empty for sqlite")
		case strings.HasPrefix(dsn, "file:") || strings.Contains(dsn, "?"):
			return errors.New(`db.dsn for sqlite must be a plain file path (no "file:" prefix or "?" query)`)
		}
	}
	if _, err := ParseTrustedProxies(c.Server.TrustedProxies); err != nil {
		return err
	}
	if rl := c.Server.RateLimit; !rl.Disabled && (!(rl.RPS > 0) || rl.Burst < 1) {
		return errors.New("server.rate_limit requires rps > 0 and burst >= 1 (or disabled: true)")
	}
	loopback, err := IsLoopbackListen(c.Server.Listen)
	if err != nil {
		return err
	}
	if c.Auth.Issuer != "" || loopback {
		return nil
	}
	if !c.Auth.AllowInsecurePublic {
		return fmt.Errorf("refusing to listen on non-loopback address %q in single-user mode: set auth.allow_insecure_public=true and auth.shared_secret to allow it", c.Server.Listen)
	}
	if c.Auth.SharedSecret == "" {
		return fmt.Errorf("listening on non-loopback address %q requires auth.shared_secret", c.Server.Listen)
	}
	return nil
}

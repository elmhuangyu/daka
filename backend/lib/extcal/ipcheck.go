package extcal

import (
	"net/netip"
)

var blockedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8",       // "this network"
		"100.64.0.0/10",   // CGNAT
		"192.0.0.0/24",    // IETF protocol assignments
		"192.0.2.0/24",    // TEST-NET-1
		"198.18.0.0/15",   // benchmarking
		"198.51.100.0/24", // TEST-NET-2
		"203.0.113.0/24",  // TEST-NET-3
		"240.0.0.0/4",     // reserved + broadcast
		"64:ff9b::/96",    // NAT64
		"64:ff9b:1::/48",  // local-use NAT64
		"100::/64",        // discard-only
		"2001::/32",       // Teredo
		"2001:db8::/32",   // documentation
		"2002::/16",       // 6to4
		"fec0::/10",       // deprecated site-local
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// IPAllowed reports whether ip is a globally routable unicast address that the
// fetcher may connect to. IPv4-mapped IPv6 addresses are judged as IPv4.
func IPAllowed(ip netip.Addr) bool {
	ip = ip.Unmap().WithZone("")
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

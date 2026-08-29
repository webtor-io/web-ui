// Package egress holds the one rule for outbound HTTP that carries a
// destination chosen by a user.
//
// It exists because the rule was written once, correctly, for Torznab
// indexers and then not applied to the two other places that fetch a
// user-supplied URL — the Stremio addon validator and the embed job's
// torrentUrl. Both reached cluster-internal services and the link-local
// metadata address. Keeping one implementation, rather than a second copy
// that drifts, is the point of this package.
//
// The check lives in the dialer rather than on the parsed URL because that is
// the only place that sees the address actually connected to: a hostname that
// resolves to 10.0.0.1, a DNS rebind between validation and fetch, and a
// redirect into the cluster all arrive here.
package egress

import (
	"net"
	"syscall"

	"github.com/pkg/errors"
)

// blockedRanges are the ranges Go's own predicates miss: carrier-grade NAT
// (which is also where one cloud's metadata service lives), benchmarking,
// IETF protocol assignments, reserved space, and the two IPv6 transition
// mechanisms that embed an arbitrary IPv4 address — 6to4 and NAT64 — through
// which 2002:7f00:1:: reaches 127.0.0.1.
var blockedRanges = []*net.IPNet{
	mustCIDR("100.64.0.0/10"),
	mustCIDR("192.0.0.0/24"),
	mustCIDR("198.18.0.0/15"),
	mustCIDR("240.0.0.0/4"),
	mustCIDR("2002::/16"),
	mustCIDR("64:ff9b::/96"),
}

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// IsPrivateIP reports whether an address must not be reachable from a shared
// deployment. The base rule is "global unicast only", which excludes
// loopback, link-local (and with it the 169.254.169.254 metadata endpoint),
// multicast and the unspecified address in one go; IsPrivate covers
// RFC1918/RFC4193, and blockedRanges the rest.
func IsPrivateIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return true
	}
	for _, n := range blockedRanges {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// DialControl returns a net.Dialer Control function that refuses private and
// otherwise non-routable destinations. A nil return means "no restriction",
// which is what a deployment opting in to private networks wants.
func DialControl(allowPrivate bool) func(network, address string, c syscall.RawConn) error {
	if allowPrivate {
		return nil
	}
	return func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return errors.Errorf("cannot resolve %s", address)
		}
		if IsPrivateIP(ip) {
			return errors.Errorf("refusing to connect to private address %s", ip)
		}
		return nil
	}
}

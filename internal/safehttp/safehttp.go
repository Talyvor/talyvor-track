// Package safehttp provides an HTTP client hardened against SSRF. Its dialer resolves the target host
// and refuses to connect to loopback, private (RFC1918 / ULA), link-local (169.254/16 incl. the cloud
// metadata endpoint 169.254.169.254, fe80::/10), unspecified, and multicast addresses — and it
// re-validates every redirect hop (each redirect re-dials through the same guard). Use it for every
// fetch of a user-supplied or fetched-then-followed URL (importers, webhooks).
package safehttp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// ErrBlockedAddress is returned when a fetch targets a non-public (SSRF-sensitive) address.
var ErrBlockedAddress = errors.New("safehttp: refusing to connect to a non-public address")

// reserved are ranges the net.IP predicates miss: CGNAT (Alibaba Cloud's metadata endpoint 100.100.100.200
// and tailnets live there), "this network", IETF assignments, benchmarking, class E and broadcast, and the
// NAT64 prefixes, which reach any IPv4 address — internal ones included — through a translator.
var reserved = func() []*net.IPNet {
	var out []*net.IPNet
	for _, c := range []string{"100.64.0.0/10", "0.0.0.0/8", "192.0.0.0/24", "198.18.0.0/15", "240.0.0.0/4",
		"64:ff9b::/96", "64:ff9b:1::/48"} {
		_, n, _ := net.ParseCIDR(c)
		out = append(out, n)
	}
	return out
}()

// blocked reports whether ip is in an SSRF-sensitive range that must never be dialed.
func blocked(ip net.IP) bool {
	for _, n := range reserved {
		if n.Contains(ip) {
			return true
		}
	}
	return ip.IsLoopback() || // 127.0.0.0/8, ::1
		ip.IsPrivate() || // 10/8, 172.16/12, 192.168/16, fc00::/7 (ULA)
		ip.IsLinkLocalUnicast() || // 169.254.0.0/16 (incl. metadata 169.254.169.254), fe80::/10
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsUnspecified() || // 0.0.0.0, ::
		ip.IsMulticast()
}

// resolve looks host up and refuses it when any address it resolves to is in a blocked range.
func resolve(ctx context.Context, host string) ([]net.IP, error) {
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("safehttp: no addresses for %q", host)
	}
	for _, ip := range ips {
		if blocked(ip) {
			return nil, fmt.Errorf("%w: %s resolves to %s", ErrBlockedAddress, host, ip)
		}
	}
	return ips, nil
}

// Check refuses a host the client would refuse to dial, so a URL can be rejected when it is
// registered. The dial still re-checks: a name can resolve differently later.
func Check(ctx context.Context, host string) error {
	_, err := resolve(ctx, host)
	return err
}

// safeDialContext resolves the host, rejects any resolved address in a blocked range, and dials the
// resolved IP directly (so a DNS name can't rebind to an internal IP between the check and the connect).
func safeDialContext(base *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := resolve(ctx, host)
		if err != nil {
			return nil, err
		}
		// Dial the already-validated address, not the hostname (defeats DNS rebinding).
		return base.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
}

// Client returns an *http.Client with the SSRF dial guard installed on both the initial request and
// every redirect hop.
func Client(timeout time.Duration) *http.Client {
	base := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	tr := &http.Transport{
		// No proxy: through one, the dial would check the proxy's address and never the target's.
		Proxy:                 nil,
		DialContext:           safeDialContext(base),
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: tr,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("safehttp: stopped after 10 redirects")
			}
			return nil // each redirect re-dials through safeDialContext, which re-validates the target
		},
	}
}

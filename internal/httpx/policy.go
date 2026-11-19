package httpx

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// maxRedirects is the most redirects one request follows.
const maxRedirects = 5

// refusedPrefixes are the special-purpose ranges the netip predicates do not
// cover. Read-only after initialisation.
var refusedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),   // shared address space, where tailnet addresses live
	netip.MustParsePrefix("0.0.0.0/8"),       // "this network"
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, including 255.255.255.255
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation
	netip.MustParsePrefix("198.51.100.0/24"), // documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
	netip.MustParsePrefix("64:ff9b:1::/48"),  // local-use NAT64
	netip.MustParsePrefix("2001::/32"),       // Teredo
}

// IPv6 ranges that embed an IPv4 address, which the policy judges instead.
var (
	nat64     = netip.MustParsePrefix("64:ff9b::/96") // IPv4 in the last 4 bytes
	sixToFour = netip.MustParsePrefix("2002::/16")    // IPv4 in bytes 2 to 5
)

// blockedAddr reports whether the address policy refuses a connection to a.
func blockedAddr(a netip.Addr) bool {
	a = a.Unmap().WithZone("")
	if !a.IsValid() ||
		a.IsLoopback() ||
		a.IsPrivate() ||
		a.IsLinkLocalUnicast() ||
		a.IsLinkLocalMulticast() ||
		a.IsInterfaceLocalMulticast() ||
		a.IsMulticast() ||
		a.IsUnspecified() {
		return true
	}
	for _, p := range refusedPrefixes {
		if p.Contains(a) {
			return true
		}
	}
	b := a.As16()
	switch {
	case nat64.Contains(a):
		return blockedAddr(netip.AddrFrom4([4]byte(b[12:16])))
	case sixToFour.Contains(a):
		return blockedAddr(netip.AddrFrom4([4]byte(b[2:6])))
	}
	return false
}

// refuseBlocked is the dialer's Control function. It sees the resolved
// address of every connection attempt, so a hostname that resolves to a
// refused address is caught whatever it resolved to at config load.
func refuseBlocked(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		// Fail closed: an address this check cannot read is not dialled.
		return fmt.Errorf("%w: unreadable address %q", ErrBlockedAddress, address)
	}
	if blockedAddr(ap.Addr()) {
		return fmt.Errorf("%w: %s", ErrBlockedAddress, ap.Addr())
	}
	return nil
}

// allowedHost reports whether host is one the operator named in
// Options.AllowPrivateHosts. The match is exact and ignores case.
func (c *Client) allowedHost(host string) bool {
	_, ok := c.allowed[strings.ToLower(host)]
	return ok
}

// checkURL applies the scheme and URL shape rules to one hop.
func (c *Client) checkURL(u *url.URL) error {
	host := u.Hostname()
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && host != "" && c.allowedHost(host):
	default:
		return fmt.Errorf("%w: %q", ErrSchemeNotAllowed, u.Scheme)
	}
	if u.User != nil {
		return errUserinfo
	}
	if host == "" {
		return errNoHost
	}
	return nil
}

// newTransport returns the one transport every request uses: pooled and
// HTTP/2-capable like the default, with no proxy (a proxy would dial on our
// behalf and hide the real address from the dialer) and the policy dialer.
func (c *Client) newTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	open := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	guarded := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second, Control: refuseBlocked}
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("dial %s: %w", addr, err)
		}
		// The exemption follows the hostname being dialled, so a redirect
		// from an allowed host to another host gets the full policy.
		if c.allowedHost(host) {
			return open.DialContext(ctx, network, addr)
		}
		return guarded.DialContext(ctx, network, addr)
	}
	return t
}

// policyTransport checks the scheme and URL shape of every request it sends,
// which covers the first request and each redirect hop.
type policyTransport struct {
	c    *Client
	base http.RoundTripper
}

// RoundTrip refuses a request the URL policy rejects and sends the rest.
func (p policyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := p.c.checkURL(r.URL); err != nil {
		if r.Body != nil {
			r.Body.Close() // a RoundTripper closes the body even on error
		}
		return nil, fmt.Errorf("request to %s: %w", r.URL.Host, err)
	}
	return p.base.RoundTrip(r)
}

// checkRedirect caps the number of hops. It adds no headers, so those
// net/http drops on a cross-domain redirect stay dropped.
func checkRedirect(_ *http.Request, via []*http.Request) error {
	if len(via) > maxRedirects {
		return errRedirectLimit
	}
	return nil
}

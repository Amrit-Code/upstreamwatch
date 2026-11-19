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

// RoundTrip refuses a request the URL policy rejects and sends the rest. A
// redirect hop whose chain has left the first request's host is sent with
// only the headers keptOnForeignRedirect keeps. This repeats checkRedirect's
// stripping because net/http adds cookie-jar cookies after CheckRedirect
// returns, just before calling the transport.
func (p policyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := p.c.checkURL(r.URL); err != nil {
		if r.Body != nil {
			r.Body.Close() // a RoundTripper closes the body even on error
		}
		return nil, fmt.Errorf("request to %s: %w", r.URL.Host, err)
	}
	if leftFirstHost(r) {
		// A RoundTripper must not modify the request it was given.
		r = r.Clone(r.Context())
		stripForeign(r.Header)
	}
	return p.base.RoundTrip(r)
}

// leftFirstHost reports whether r is a redirect hop and r or an earlier hop of
// its chain went to a host (host[:port], ignoring case) other than the
// chain's first request. net/http links each hop to the one before through
// Request.Response.Request, so the chain itself is the mark: once a hop has
// left the first host, every later hop, including one back to it, is marked.
func leftFirstHost(r *http.Request) bool {
	hosts := []string{r.URL.Host}
	for p := r; p.Response != nil && p.Response.Request != nil; {
		p = p.Response.Request
		hosts = append(hosts, p.URL.Host)
	}
	first := hosts[len(hosts)-1]
	for _, h := range hosts[:len(hosts)-1] {
		if !strings.EqualFold(h, first) {
			return true
		}
	}
	return false
}

// keptOnForeignRedirect reports whether a request header survives a redirect
// to another host. Everything else, credentials and validators included, is
// removed.
func keptOnForeignRedirect(key string) bool {
	switch http.CanonicalHeaderKey(key) {
	case "User-Agent", "Accept", "Accept-Encoding", "Accept-Language":
		return true
	}
	return false
}

// stripForeign removes from h every header keptOnForeignRedirect does not
// keep.
func stripForeign(h http.Header) {
	for k := range h {
		if !keptOnForeignRedirect(k) {
			delete(h, k)
		}
	}
}

// checkRedirect caps the number of hops and, once any hop of the chain has
// left the first request's host, removes every header keptOnForeignRedirect
// does not keep. It adds no headers, so those net/http drops on a
// cross-domain redirect stay dropped. policyTransport strips again after
// net/http adds cookie-jar cookies.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > maxRedirects {
		return errRedirectLimit
	}
	// net/http copies the first request's headers onto every hop, so the
	// whole chain is checked: a hop back to the first host after a foreign
	// one must stay stripped.
	first := via[0].URL.Host
	left := !strings.EqualFold(req.URL.Host, first)
	for _, hop := range via[1:] {
		if !strings.EqualFold(hop.URL.Host, first) {
			left = true
		}
	}
	if left {
		stripForeign(req.Header)
	}
	return nil
}

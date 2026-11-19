package web

import (
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

// allowedHosts returns the Host header values, lowercased, that the server
// answers when it listens on listen (host:port) and is reached through
// baseURL (may be empty). A listen value that is not host:port contributes
// nothing, so requests are refused rather than let through.
func allowedHosts(listen, baseURL string) map[string]struct{} {
	allowed := make(map[string]struct{})
	add := func(h string) { allowed[strings.ToLower(h)] = struct{}{} }

	if host, port, err := net.SplitHostPort(listen); err == nil {
		addr, addrErr := netip.ParseAddr(host)
		switch {
		case host == "" || (addrErr == nil && addr.IsUnspecified()):
			// Listening on every interface: answer only the loopback names,
			// so a LAN or rebound name cannot reach the UI.
			add(net.JoinHostPort("127.0.0.1", port))
			add(net.JoinHostPort("localhost", port))
			add(net.JoinHostPort("::1", port))
		case addrErr == nil && addr.IsLoopback():
			add(listen)
			add(net.JoinHostPort("localhost", port))
		default:
			add(listen)
		}
	}
	if baseURL != "" {
		// NewServer has already checked that baseURL parses with a host.
		if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
			add(u.Host)
		}
	}
	return allowed
}

// hostCheck answers 421 Misdirected Request to every request whose Host
// header is not in allowed. It is what stops a DNS-rebinding page from talking
// to the UI under the attacker's hostname.
func hostCheck(allowed map[string]struct{}, logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allowed[strings.ToLower(r.Host)]; !ok {
			logger.Warn("request rejected, host not allowed", "host", r.Host)
			http.Error(w, "misdirected request: host not allowed", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

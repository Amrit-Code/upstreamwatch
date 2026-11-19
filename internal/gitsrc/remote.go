package gitsrc

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strings"

	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// remote is a target URL that passed the URL and address checks, with the
// config and environment one network operation against it needs.
type remote struct {
	url  string
	host string
	// config pins the host to the checked address.
	config []string
	// env carries the credential header, when the target has a token.
	env []string
	// secrets are the credential strings env carries, for scrubbing from
	// git's error output.
	secrets []string
}

// checkRemote applies the URL policy and the address check to repo's URL and
// returns what a network operation needs. It runs before every ls-remote and
// fetch, and starts no git process.
func (g *CLI) checkRemote(ctx context.Context, repo model.TargetRepo) (remote, error) {
	u, err := checkURL(repo.URL, g.opts.AllowFileURLs)
	if err != nil {
		return remote{}, err
	}
	r := remote{url: repo.URL, host: u.Host}
	if u.Scheme == "https" && !repo.AllowPrivateHost {
		pin, err := g.checkAddress(ctx, u)
		if err != nil {
			return remote{}, err
		}
		if pin != "" {
			r.config = []string{"http.curloptResolve=" + pin}
		}
	}
	if repo.Token != "" {
		r.env, r.secrets = credentialEnv(u.Host, repo.Token)
	}
	return r, nil
}

// checkURL parses raw and refuses anything but an https URL with a host and
// no user information, or a file URL when allowFile is set. The errors name
// the reason and never the URL, which could carry a credential.
func checkURL(raw string, allowFile bool) (*url.URL, error) {
	if raw == "" {
		return nil, errors.New("target URL is empty")
	}
	if strings.HasPrefix(raw, "-") {
		return nil, errors.New(`target URL starts with "-"`)
	}
	sep := strings.Index(raw, "://")
	if helper := strings.Index(raw, "::"); helper >= 0 && (sep < 0 || helper < sep) {
		return nil, errors.New(`remote helper syntax "<transport>::" not allowed, only https`)
	}
	if sep < 0 {
		if colon := strings.IndexByte(raw, ':'); colon >= 0 && !strings.Contains(raw[:colon], "/") {
			return nil, errors.New("scp-style address not allowed, only https")
		}
		return nil, errors.New("local path not allowed, only https")
	}
	u, err := url.Parse(raw)
	if err != nil {
		// url.Parse errors quote parts of the input, so none of it is kept.
		return nil, errors.New("target URL does not parse")
	}
	switch scheme := strings.ToLower(u.Scheme); {
	case scheme == "https":
	case scheme == "file" && allowFile:
	default:
		return nil, fmt.Errorf("scheme %q not allowed, only https", u.Scheme)
	}
	if u.User != nil {
		return nil, errors.New("target URL with user information not allowed")
	}
	if strings.ToLower(u.Scheme) == "https" && u.Hostname() == "" {
		return nil, errors.New("target URL has no host")
	}
	return u, nil
}

// checkAddress resolves u's host and refuses it when any address is in a
// range httpx.BlockedAddr refuses. It returns the curl resolve entry that pins
// git to the first checked address, or "" for an IP literal, which git does
// not resolve.
func (g *CLI) checkAddress(ctx context.Context, u *url.URL) (string, error) {
	host := u.Hostname()
	if literal, err := netip.ParseAddr(host); err == nil {
		if httpx.BlockedAddr(literal) {
			return "", fmt.Errorf("host %q resolves to a private or reserved address", host)
		}
		return "", nil
	}
	addrs, err := g.opts.LookupHost(ctx, host)
	if err != nil {
		return "", fmt.Errorf("resolve host %q: %w", host, err)
	}
	if len(addrs) == 0 {
		return "", fmt.Errorf("host %q resolves to no address", host)
	}
	for _, a := range addrs {
		if httpx.BlockedAddr(a) {
			return "", fmt.Errorf("host %q resolves to a private or reserved address", host)
		}
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	return resolveEntry(host, port, addrs[0]), nil
}

// resolveEntry formats one http.curloptResolve value, host:port:address, with
// an IPv6 address in brackets.
func resolveEntry(host, port string, a netip.Addr) string {
	a = a.Unmap().WithZone("")
	addr := a.String()
	if a.Is6() {
		addr = "[" + addr + "]"
	}
	return net.JoinHostPort(host, port) + ":" + addr
}

// credentialEnv passes token to git as an Authorization header scoped to
// host, through environment config only: never argv, the URL or a file. It
// also returns the strings that reveal the token, longest first, so error
// text can be scrubbed of them.
func credentialEnv(host string, token model.Secret) (env, secrets []string) {
	userPass := "x-access-token:" + token.Reveal()
	basic := base64.StdEncoding.EncodeToString([]byte(userPass))
	env = []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.https://" + host + "/.extraHeader",
		"GIT_CONFIG_VALUE_0=Authorization: Basic " + basic,
	}
	secrets = []string{userPass, basic, strings.TrimPrefix(userPass, "x-access-token:")}
	slices.SortStableFunc(secrets, func(a, b string) int { return len(b) - len(a) })
	return env, secrets
}

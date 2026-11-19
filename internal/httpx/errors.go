package httpx

import (
	"errors"
	"fmt"
	"net/url"
	"time"
)

var (
	// ErrBlockedAddress reports that the address policy refused a connection
	// to a loopback, private, link-local, multicast, unspecified or
	// 100.64.0.0/10 address.
	ErrBlockedAddress = errors.New("address not allowed")
	// ErrSchemeNotAllowed reports a URL whose scheme is not https (or not
	// http for a host named in Options.AllowPrivateHosts).
	ErrSchemeNotAllowed = errors.New("scheme not allowed")
	// ErrBodyTooLarge reports a response body longer than the size cap.
	ErrBodyTooLarge = errors.New("response body too large")
)

// The URL shape refusals other than the scheme. They are plain errors for
// callers; httpx tells them apart only to avoid retrying a policy refusal.
var (
	errUserinfo      = errors.New("URL has userinfo")
	errNoHost        = errors.New("URL has no host")
	errRedirectLimit = fmt.Errorf("stopped after %d redirects", maxRedirects)
)

// StatusError is returned for a final response that is neither 2xx nor a 304
// to a conditional request.
type StatusError struct {
	Method string
	Host   string
	Path   string // empty when Request.SecretPath
	Status int
}

// Error names the method, host, path (unless secret) and status.
func (e *StatusError) Error() string {
	return fmt.Sprintf("%s %s%s: status %d", e.Method, e.Host, e.Path, e.Status)
}

// RateLimitError is returned when the server asks for a wait longer than
// Options.MaxWait, or attempts run out on a rate-limit response.
type RateLimitError struct {
	Host    string
	ResetAt time.Time // UTC; when to try again
}

// Error names the host and the time to try again.
func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limited by %s until %s", e.Host, e.ResetAt.UTC().Format(time.RFC3339))
}

// isPolicyRefusal reports whether err is a refusal by the URL or address
// policy, which no retry can change.
func isPolicyRefusal(err error) bool {
	return errors.Is(err, ErrBlockedAddress) ||
		errors.Is(err, ErrSchemeNotAllowed) ||
		errors.Is(err, errUserinfo) ||
		errors.Is(err, errNoHost) ||
		errors.Is(err, errRedirectLimit)
}

// stripURLError removes the *url.Error layers net/http adds, because they
// carry the full URL, query string and path included.
func stripURLError(err error) error {
	for {
		ue, ok := err.(*url.Error)
		if !ok {
			return err
		}
		err = ue.Err
	}
}

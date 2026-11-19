package httpx

import (
	"context"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// baseBackoff is the first wait of the exponential backoff.
const baseBackoff = 500 * time.Millisecond

// retryableMethod reports whether a request may be sent more than once.
func retryableMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

// rateLimited reports whether a response is a rate-limit answer: a 429, or
// GitHub's 403 carrying retry-after or x-ratelimit-remaining: 0.
func rateLimited(status int, h http.Header) bool {
	switch status {
	case http.StatusTooManyRequests:
		return true
	case http.StatusForbidden:
		return h.Get("Retry-After") != "" || strings.TrimSpace(h.Get("X-Ratelimit-Remaining")) == "0"
	}
	return false
}

// retryableStatus reports whether a response to a GET or HEAD is retried.
func retryableStatus(status int, h http.Header) bool {
	switch status {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return rateLimited(status, h)
}

// requestedWait returns the wait the server asked for: Retry-After on any
// response, or x-ratelimit-reset on a rate-limit response without a usable
// Retry-After. ok is false when the server asked for nothing.
func requestedWait(status int, h http.Header, now time.Time) (wait time.Duration, ok bool) {
	if d, ok := parseRetryAfter(h.Get("Retry-After"), now); ok {
		return d, true
	}
	if rateLimited(status, h) {
		if reset, ok := parseUnix(h.Get("X-Ratelimit-Reset")); ok {
			return max(reset.Sub(now), 0), true
		}
	}
	return 0, false
}

// parseRetryAfter reads a Retry-After value in delta seconds or as an HTTP
// date. A date in the past is a wait of zero.
func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		if n < 0 {
			return 0, false
		}
		return time.Duration(min(n, math.MaxInt64/int64(time.Second))) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(t.Sub(now), 0), true
	}
	return 0, false
}

// parseUnix reads a Unix time in whole seconds, as x-ratelimit-reset gives it.
func parseUnix(v string) (time.Time, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || n < 0 {
		return time.Time{}, false
	}
	return time.Unix(n, 0).UTC(), true
}

// parseRateLimit reads the rate-limit headers of a response.
func parseRateLimit(h http.Header) RateLimit {
	var rl RateLimit
	if n, err := strconv.Atoi(strings.TrimSpace(h.Get("X-Ratelimit-Remaining"))); err == nil {
		rl.Known = true
		rl.Remaining = n
	}
	if t, ok := parseUnix(h.Get("X-Ratelimit-Reset")); ok {
		rl.ResetAt = t
	}
	return rl
}

// backoffCeiling is the upper bound of the wait after the given failed
// attempt (1-based): 500 ms doubled per attempt, capped at maxWait.
func backoffCeiling(attempt int, maxWait time.Duration) time.Duration {
	d := baseBackoff
	for i := 1; i < attempt && d < maxWait; i++ {
		d *= 2
	}
	return min(d, maxWait)
}

// fullJitter returns a uniformly random wait in [0, ceiling).
func fullJitter(ceiling time.Duration) time.Duration {
	if ceiling <= 0 {
		return 0
	}
	return rand.N(ceiling)
}

// sleepCtx waits for d or until ctx is done, whichever comes first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

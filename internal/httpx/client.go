package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// Defaults for the zero values of Options.
const (
	defaultTimeout      = 2 * time.Minute
	defaultMaxBodyBytes = 64 << 20
	defaultMaxAttempts  = 3
	defaultMaxWait      = 30 * time.Second
	defaultUserAgent    = "upstreamwatch"
	githubAPIHost       = "api.github.com"
	githubMinGap        = 100 * time.Millisecond
	// discardLimit is how much of a failed response's body is read, only so
	// the connection can be reused; the body itself is not returned.
	discardLimit = 64 << 10
)

// Options configures a Client. Zero values take the defaults given per field.
type Options struct {
	Timeout           time.Duration            // whole request including the body; default 2 minutes
	MaxBodyBytes      int64                    // response body cap; default 64 MiB
	MaxAttempts       int                      // attempts per retryable request; default 3
	MaxWait           time.Duration            // longest single wait between attempts; default 30 seconds
	UserAgent         string                   // default "upstreamwatch"
	AllowPrivateHosts []string                 // hostnames (no port) the operator named; exempt from the address and scheme policy
	MinGap            map[string]time.Duration // minimum gap between requests per host; default {"api.github.com": 100ms}
}

// ValidatorMode selects how a request handles ETag and Last-Modified.
type ValidatorMode int

const (
	// ValidatorsNone sends a plain request.
	ValidatorsNone ValidatorMode = iota
	// ValidatorsCached keeps ETag and Last-Modified in the CacheStore, keyed
	// by the exact URL. GET only.
	ValidatorsCached
	// ValidatorsCaller sends Request.ETag and returns Response.ETag; the
	// caller stores it.
	ValidatorsCaller
)

// Request is one outbound HTTP request.
type Request struct {
	Method       string        // default GET
	URL          string        // sent byte for byte; also the CacheStore key
	Header       http.Header   // the adapter sets Authorization here; httpx holds no token
	Body         []byte        // request body, if any
	Validators   ValidatorMode // how ETag and Last-Modified are handled
	ETag         string        // ValidatorsCaller only
	MaxBodyBytes int64         // overrides Options.MaxBodyBytes when positive
	SecretPath   bool          // the URL path is a credential (webhooks): log and report the host only
}

// Response is the result of a request that ended in 2xx, or in 304 to a
// conditional request.
type Response struct {
	Status      int
	Header      http.Header
	Body        []byte
	NotModified bool   // 304 to a conditional request; Body is empty
	ETag        string // the response's ETag, if any
	URL         string // the final URL after redirects
	RateLimit   RateLimit
}

// RateLimit is what a response said about the caller's rate-limit budget.
type RateLimit struct {
	Known     bool      // the response carried x-ratelimit-remaining
	Remaining int       // requests left in the window, when Known
	ResetAt   time.Time // UTC; zero if the response gave none
}

// Client is the shared outbound HTTP client. It applies the URL and address
// policy, timeouts, the body cap, one retry layer, per-host pacing,
// conditional requests and rate-limit accounting. It is safe for concurrent
// use.
type Client struct {
	cache   CacheStore
	opts    Options
	logger  *slog.Logger
	allowed map[string]struct{}
	gates   map[string]*gate
	client  *http.Client

	now    func() time.Time
	sleep  func(ctx context.Context, d time.Duration) error
	jitter func(ceiling time.Duration) time.Duration
}

// New returns a Client. cache holds the validators of ValidatorsCached
// requests. It does no I/O.
func New(cache CacheStore, opts Options, logger *slog.Logger) *Client {
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = defaultMaxBodyBytes
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = defaultMaxAttempts
	}
	if opts.MaxWait <= 0 {
		opts.MaxWait = defaultMaxWait
	}
	if opts.UserAgent == "" {
		opts.UserAgent = defaultUserAgent
	}
	if len(opts.MinGap) == 0 {
		opts.MinGap = map[string]time.Duration{githubAPIHost: githubMinGap}
	}

	c := &Client{
		cache:   cache,
		opts:    opts,
		logger:  logger,
		allowed: make(map[string]struct{}, len(opts.AllowPrivateHosts)),
		gates:   make(map[string]*gate, len(opts.MinGap)),
		now:     time.Now,
		sleep:   sleepCtx,
		jitter:  fullJitter,
	}
	for _, h := range opts.AllowPrivateHosts {
		c.allowed[strings.ToLower(h)] = struct{}{}
	}
	for h, gap := range opts.MinGap {
		if gap > 0 {
			c.gates[strings.ToLower(h)] = newGate(gap)
		}
	}
	c.client = &http.Client{
		Transport:     policyTransport{c: c, base: c.newTransport()},
		Timeout:       opts.Timeout,
		CheckRedirect: checkRedirect,
	}
	return c
}

// HTTPClient returns an *http.Client that applies the URL and address policy,
// the timeout and the redirect rules, but no retries, pacing or body cap. It
// exists for libraries that insist on their own client (the Anthropic SDK).
func (c *Client) HTTPClient() *http.Client {
	hc := *c.client
	return &hc
}

// target describes a request in logs and errors: method and host, plus the
// path unless it is secret. It never includes the query string or userinfo.
type target struct {
	method string
	host   string
	path   string
}

func newTarget(method string, u *url.URL, secret bool) target {
	t := target{method: method, host: u.Host}
	if !secret {
		t.path = u.Path
	}
	return t
}

func (t target) String() string {
	if t.host == "" {
		return t.method + " (no host)" + t.path
	}
	return t.method + " " + t.host + t.path
}

// hostOnly drops the path; a policy refusal names the host only.
func (t target) hostOnly() target {
	t.path = ""
	return t
}

// Do sends req, retrying a GET or HEAD where the rules allow, and returns the
// response of a 2xx, or of a 304 to a conditional request. Any other final
// status is a *StatusError, or a *RateLimitError when the server's rate limit
// is the reason. Errors name the method, host and path (the host only with
// SecretPath) and never the query string.
func (c *Client) Do(ctx context.Context, req Request) (Response, error) {
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	u, err := url.Parse(req.URL)
	if err != nil {
		// The parse error quotes the URL, which may hold a query or a secret.
		return Response{}, fmt.Errorf("%s request: invalid URL", method)
	}
	tgt := newTarget(method, u, req.SecretPath)
	if err := c.checkURL(u); err != nil {
		return Response{}, fmt.Errorf("%s: %w", tgt.hostOnly(), err)
	}

	cond, err := c.validators(ctx, method, req)
	if err != nil {
		return Response{}, fmt.Errorf("%s: %w", tgt, err)
	}
	maxBody := c.opts.MaxBodyBytes
	if req.MaxBodyBytes > 0 {
		maxBody = req.MaxBodyBytes
	}
	attempts := 1
	if retryableMethod(method) {
		attempts = c.opts.MaxAttempts
	}

	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return Response{}, fmt.Errorf("%s: %w", tgt, err)
		}
		out, err := c.attempt(ctx, tgt, u, req, cond, maxBody, attempt)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return Response{}, fmt.Errorf("%s: %w", tgt, ctxErr)
			}
			if attempt >= attempts || isPolicyRefusal(err) || errors.Is(err, ErrBodyTooLarge) {
				return Response{}, err
			}
			if err := c.wait(ctx, tgt, 0, c.jitter(backoffCeiling(attempt, c.opts.MaxWait)), attempt); err != nil {
				return Response{}, err
			}
			continue
		}

		switch {
		case out.status >= 200 && out.status <= 299:
			return c.success(ctx, tgt, req, out)
		case out.status == http.StatusNotModified && out.conditional:
			return Response{
				Status:      out.status,
				Header:      out.header,
				NotModified: true,
				ETag:        out.header.Get("ETag"),
				URL:         out.finalURL,
				RateLimit:   parseRateLimit(out.header),
			}, nil
		}
		if attempts == 1 || !retryableStatus(out.status, out.header) {
			return Response{}, c.statusError(tgt, out.status)
		}

		limited := rateLimited(out.status, out.header)
		now := c.now()
		wait, requested := requestedWait(out.status, out.header, now)
		if requested && wait > c.opts.MaxWait {
			return Response{}, &RateLimitError{Host: tgt.host, ResetAt: now.Add(wait).UTC()}
		}
		if attempt >= attempts {
			if !limited {
				return Response{}, c.statusError(tgt, out.status)
			}
			if !requested {
				wait = backoffCeiling(attempt, c.opts.MaxWait)
			}
			return Response{}, &RateLimitError{Host: tgt.host, ResetAt: now.Add(wait).UTC()}
		}
		if !requested {
			wait = c.jitter(backoffCeiling(attempt, c.opts.MaxWait))
		}
		if err := c.wait(ctx, tgt, out.status, wait, attempt); err != nil {
			return Response{}, err
		}
	}
}

// validators returns the conditional headers the request carries on every
// attempt, loading them from the CacheStore in ValidatorsCached mode.
func (c *Client) validators(ctx context.Context, method string, req Request) (http.Header, error) {
	cond := http.Header{}
	switch req.Validators {
	case ValidatorsNone:
	case ValidatorsCaller:
		if req.ETag != "" {
			cond.Set("If-None-Match", req.ETag)
		}
	case ValidatorsCached:
		if method != http.MethodGet {
			return nil, errors.New("cached validators need a GET request")
		}
		if c.cache == nil {
			return nil, errors.New("cached validators need a cache store")
		}
		v, err := c.cache.GetHTTPValidator(ctx, req.URL)
		switch {
		case errors.Is(err, model.ErrNotFound):
		case err != nil:
			return nil, fmt.Errorf("load cached validators: %w", err)
		default:
			if v.ETag != "" {
				cond.Set("If-None-Match", v.ETag)
			}
			if v.LastModified != "" {
				cond.Set("If-Modified-Since", v.LastModified)
			}
		}
	default:
		return nil, fmt.Errorf("unknown validator mode %d", req.Validators)
	}
	return cond, nil
}

// outcome is what one attempt received.
type outcome struct {
	status      int
	header      http.Header
	body        []byte
	finalURL    string
	conditional bool // the request carried If-None-Match or If-Modified-Since
}

// attempt sends the request once, through the host's pacing gate, and reads
// the body of a 2xx through the cap.
func (c *Client) attempt(ctx context.Context, tgt target, u *url.URL, req Request, cond http.Header, maxBody int64, attempt int) (outcome, error) {
	if g := c.gates[strings.ToLower(u.Hostname())]; g != nil {
		if err := g.acquire(ctx, c.now, c.sleep); err != nil {
			return outcome{}, fmt.Errorf("%s: wait for pacing gate: %w", tgt, err)
		}
		defer g.release(c.now)
	}

	var body io.Reader
	if len(req.Body) > 0 {
		body = bytes.NewReader(req.Body)
	}
	hreq, err := http.NewRequestWithContext(ctx, tgt.method, req.URL, body)
	if err != nil {
		return outcome{}, fmt.Errorf("%s: build request: %w", tgt, stripURLError(err))
	}
	if req.Header != nil {
		hreq.Header = req.Header.Clone()
	}
	if hreq.Header.Get("User-Agent") == "" {
		hreq.Header.Set("User-Agent", c.opts.UserAgent)
	}
	for k, vs := range cond {
		hreq.Header[k] = vs
	}
	conditional := hreq.Header.Get("If-None-Match") != "" || hreq.Header.Get("If-Modified-Since") != ""

	start := c.now()
	resp, err := c.client.Do(hreq)
	if err != nil {
		c.logAttempt(tgt, 0, start, attempt)
		err = stripURLError(err)
		if isPolicyRefusal(err) {
			return outcome{}, fmt.Errorf("%s: %w", tgt.hostOnly(), err)
		}
		return outcome{}, fmt.Errorf("%s: %w", tgt, err)
	}
	defer resp.Body.Close()

	out := outcome{
		status:      resp.StatusCode,
		header:      resp.Header,
		finalURL:    resp.Request.URL.String(),
		conditional: conditional,
	}
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		out.body, err = readCapped(resp.Body, maxBody)
	} else {
		// Best effort: only drains the connection for reuse.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, discardLimit))
	}
	c.logAttempt(tgt, resp.StatusCode, start, attempt)
	if err != nil {
		return outcome{}, fmt.Errorf("%s: read body: %w", tgt, stripURLError(err))
	}
	return out, nil
}

// readCapped reads all of r, or fails with ErrBodyTooLarge and no partial
// body when r holds more than limit bytes.
func readCapped(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%w (limit %d bytes)", ErrBodyTooLarge, limit)
	}
	return b, nil
}

// success builds the Response of a 2xx and, in ValidatorsCached mode, stores
// the validators it carries.
func (c *Client) success(ctx context.Context, tgt target, req Request, out outcome) (Response, error) {
	etag := out.header.Get("ETag")
	if req.Validators == ValidatorsCached {
		lastMod := out.header.Get("Last-Modified")
		if etag != "" || lastMod != "" {
			v := model.HTTPValidator{
				URL:          req.URL,
				ETag:         etag,
				LastModified: lastMod,
				FetchedAt:    c.now().UTC(),
				Status:       out.status,
			}
			if err := c.cache.SaveHTTPValidator(ctx, v); err != nil {
				return Response{}, fmt.Errorf("%s: save validators: %w", tgt, err)
			}
		}
	}
	return Response{
		Status:    out.status,
		Header:    out.header,
		Body:      out.body,
		ETag:      etag,
		URL:       out.finalURL,
		RateLimit: parseRateLimit(out.header),
	}, nil
}

func (c *Client) statusError(tgt target, status int) *StatusError {
	return &StatusError{Method: tgt.method, Host: tgt.host, Path: tgt.path, Status: status}
}

// wait logs a retry and sleeps before the next attempt. status is 0 after a
// transport error.
func (c *Client) wait(ctx context.Context, tgt target, status int, d time.Duration, attempt int) error {
	c.logger.Warn("http retry",
		"method", tgt.method, "host", tgt.host, "status", status,
		"wait_ms", d.Milliseconds(), "attempt", attempt)
	if err := c.sleep(ctx, d); err != nil {
		return fmt.Errorf("%s: wait to retry: %w", tgt, err)
	}
	return nil
}

// logAttempt writes the per-attempt debug line. status is 0 after a
// transport error.
func (c *Client) logAttempt(tgt target, status int, start time.Time, attempt int) {
	attrs := []any{"method", tgt.method, "host", tgt.host}
	if tgt.path != "" {
		attrs = append(attrs, "path", tgt.path)
	}
	attrs = append(attrs, "status", status, "duration_ms", c.now().Sub(start).Milliseconds(), "attempt", attempt)
	c.logger.Debug("http request", attrs...)
}

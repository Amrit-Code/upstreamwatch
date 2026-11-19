package depsdev

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
)

const (
	defaultBaseURL = "https://api.deps.dev"
	// checkPath fetches one small, published package version; the package
	// name is percent-encoded as the API requires.
	checkPath = "/v3/systems/go/packages/github.com%2Fgoogle%2Fuuid/versions/v1.6.0"
	// checkTimeout bounds the reachability probe.
	checkTimeout = 10 * time.Second
)

// Options configures a Resolver.
type Options struct {
	BaseURL string // default https://api.deps.dev
}

// Resolver talks to the deps.dev API through the shared HTTP client.
type Resolver struct {
	hc     *httpx.Client
	base   string
	logger *slog.Logger
}

// New returns a Resolver. It does no I/O.
func New(hc *httpx.Client, opts Options, logger *slog.Logger) *Resolver {
	base := opts.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	return &Resolver{hc: hc, base: strings.TrimSuffix(base, "/"), logger: logger}
}

// Check fetches one known package version to prove the deps.dev API answers.
// It is unauthenticated and read-only.
func (r *Resolver) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	host := hostOf(r.base)
	if _, err := r.hc.Do(ctx, httpx.Request{URL: r.base + checkPath, Validators: httpx.ValidatorsNone}); err != nil {
		return fmt.Errorf("%s: %w", host, err)
	}
	r.logger.Debug("deps.dev check passed", "host", host)
	return nil
}

// hostOf returns the host of base, or base itself if it does not parse.
func hostOf(base string) string {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return base
	}
	return u.Host
}

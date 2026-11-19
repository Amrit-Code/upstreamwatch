package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const (
	defaultBaseURL = "https://api.github.com"
	apiVersion     = "2022-11-28"
	// checkTimeout bounds the reachability probe.
	checkTimeout = 10 * time.Second
)

// Options configures a Source.
type Options struct {
	BaseURL string // default https://api.github.com
}

// Source talks to the GitHub REST API through the shared HTTP client, with
// the one token it was built with.
type Source struct {
	hc     *httpx.Client
	token  model.Secret
	base   string
	logger *slog.Logger
}

// New returns a Source that sends token, when it is not empty, on every
// request. It does no I/O.
func New(hc *httpx.Client, token model.Secret, opts Options, logger *slog.Logger) *Source {
	base := opts.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	return &Source{hc: hc, token: token, base: strings.TrimSuffix(base, "/"), logger: logger}
}

// Check calls GET /rate_limit, which does not count against the rate limit,
// to prove the API answers and, with a token, that GitHub accepts it.
func (s *Source) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	host := hostOf(s.base)
	resp, err := s.hc.Do(ctx, httpx.Request{
		URL:        s.base + "/rate_limit",
		Header:     s.header(),
		Validators: httpx.ValidatorsNone,
	})
	if err != nil {
		if se, ok := errors.AsType[*httpx.StatusError](err); ok && se.Status == http.StatusUnauthorized && s.token != "" {
			return fmt.Errorf("%s: token %s rejected (401)", host, s.token)
		}
		if s.token != "" {
			return fmt.Errorf("%s (token %s): %w", host, s.token, err)
		}
		return fmt.Errorf("%s: %w", host, err)
	}
	s.logger.Debug("github check passed", "host", host, "authenticated", s.token != "",
		"rate_limit_remaining", resp.RateLimit.Remaining)
	return nil
}

// header returns the headers every API request carries.
func (s *Source) header() http.Header {
	h := http.Header{}
	h.Set("Accept", "application/vnd.github+json")
	h.Set("X-GitHub-Api-Version", apiVersion)
	if s.token != "" {
		h.Set("Authorization", "Bearer "+s.token.Reveal())
	}
	return h
}

// hostOf returns the host of base, or base itself if it does not parse.
func hostOf(base string) string {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return base
	}
	return u.Host
}

package osv

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
	defaultBaseURL   = "https://api.osv.dev"
	defaultBucketURL = "https://storage.googleapis.com/osv-vulnerabilities"
	defaultBatchSize = 500
	// checkPath fetches one small, long-published advisory.
	checkPath = "/v1/vulns/GO-2020-0001"
	// checkTimeout bounds the reachability probe.
	checkTimeout = 10 * time.Second
)

// Options configures a Client. Zero values take the defaults given per field.
type Options struct {
	BaseURL   string // default https://api.osv.dev
	BucketURL string // default https://storage.googleapis.com/osv-vulnerabilities
	BatchSize int    // default 500
}

// Client talks to the OSV API and the OSV bucket through the shared HTTP
// client. It sends no token.
type Client struct {
	hc        *httpx.Client
	base      string
	bucket    string
	batchSize int
	logger    *slog.Logger
}

// New returns a Client. It does no I/O.
func New(hc *httpx.Client, opts Options, logger *slog.Logger) *Client {
	base := opts.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	bucket := opts.BucketURL
	if bucket == "" {
		bucket = defaultBucketURL
	}
	batch := opts.BatchSize
	if batch <= 0 {
		batch = defaultBatchSize
	}
	return &Client{
		hc:        hc,
		base:      strings.TrimSuffix(base, "/"),
		bucket:    strings.TrimSuffix(bucket, "/"),
		batchSize: batch,
		logger:    logger,
	}
}

// Check fetches one known advisory to prove the OSV API answers. It is
// unauthenticated and read-only.
func (c *Client) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	host := hostOf(c.base)
	if _, err := c.hc.Do(ctx, httpx.Request{URL: c.base + checkPath, Validators: httpx.ValidatorsNone}); err != nil {
		return fmt.Errorf("%s: %w", host, err)
	}
	c.logger.Debug("osv check passed", "host", host)
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

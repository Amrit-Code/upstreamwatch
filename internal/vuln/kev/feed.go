package kev

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const (
	defaultURL = "https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json"
	// checkTimeout bounds the reachability probe.
	checkTimeout = 10 * time.Second
	// dateLayout is the format of the catalog's dateAdded field.
	dateLayout = "2006-01-02"
)

// cvePattern is the shape of a CVE id; catalog entries without one are
// skipped.
var cvePattern = regexp.MustCompile(`^CVE-\d{4}-\d{4,}$`)

// Options configures a Feed.
type Options struct {
	URL string // default https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json
}

// Feed downloads the KEV catalog through the shared HTTP client. The catalog
// is public, so no token is sent.
type Feed struct {
	hc     *httpx.Client
	url    string
	logger *slog.Logger
}

// New returns a Feed. It does no I/O.
func New(hc *httpx.Client, opts Options, logger *slog.Logger) *Feed {
	u := opts.URL
	if u == "" {
		u = defaultURL
	}
	return &Feed{hc: hc, url: u, logger: logger}
}

// catalog is the part of the KEV JSON document the feed reads.
type catalog struct {
	Vulnerabilities []struct {
		CVEID     string `json:"cveID"`
		DateAdded string `json:"dateAdded"`
	} `json:"vulnerabilities"`
}

// Refresh downloads the catalog and returns one KEV signal per entry whose
// cveID is a CVE id. KEVAddedAt is midnight UTC of the entry's dateAdded, or
// zero when the date is missing or malformed.
func (f *Feed) Refresh(ctx context.Context) ([]model.Signal, error) {
	resp, err := f.hc.Do(ctx, httpx.Request{URL: f.url, Validators: httpx.ValidatorsNone})
	if err != nil {
		return nil, fmt.Errorf("kev: fetch %s: %w", diagURL(f.url), err)
	}
	signals, err := parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("kev: decode %s: %w", diagURL(f.url), err)
	}
	f.logger.Info("kev refresh finished", "signals", len(signals))
	return signals, nil
}

// parse turns a KEV JSON document into signals.
func parse(body []byte) ([]model.Signal, error) {
	var c catalog
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, err
	}
	signals := make([]model.Signal, 0, len(c.Vulnerabilities))
	for _, v := range c.Vulnerabilities {
		if !cvePattern.MatchString(v.CVEID) {
			continue
		}
		s := model.Signal{Kind: model.SignalKEV, CVE: v.CVEID}
		// A missing or malformed date leaves KEVAddedAt zero; the entry still counts.
		if t, err := time.Parse(dateLayout, v.DateAdded); err == nil {
			s.KEVAddedAt = t.UTC()
		}
		signals = append(signals, s)
	}
	return signals, nil
}

// Check sends a HEAD request for the catalog to prove its host answers,
// without downloading it.
func (f *Feed) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	host := hostOf(f.url)
	if _, err := f.hc.Do(ctx, httpx.Request{Method: http.MethodHead, URL: f.url, Validators: httpx.ValidatorsNone}); err != nil {
		return fmt.Errorf("kev: check %s: %w", host, err)
	}
	f.logger.Debug("kev check passed", "host", host)
	return nil
}

// invalidURL stands in for a feed URL that does not parse or has no host, so
// diagnostics never echo raw input.
const invalidURL = "(invalid URL)"

// diagURL returns raw as scheme://host/path for errors and logs, without
// userinfo, query or fragment, which may carry credentials.
func diagURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return invalidURL
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}).String()
}

// hostOf returns the host of raw, without userinfo.
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return invalidURL
	}
	return u.Host
}

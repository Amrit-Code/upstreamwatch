package osv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const (
	// toolchainPURL is upstreamwatch's pseudo-package for the Go toolchain,
	// which OSV calls stdlib.
	toolchainPURL = "pkg:golang/toolchain"
	// ecosystemGo is OSV's name for the Go ecosystem.
	ecosystemGo = "Go"
	// maxPageRounds caps the follow-up requests for paged results.
	maxPageRounds = 50
)

// osvPackage names a package the way OSV does.
type osvPackage struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

// osvQuery is one entry of a querybatch request.
type osvQuery struct {
	Package   osvPackage `json:"package"`
	Version   string     `json:"version"`
	PageToken string     `json:"page_token,omitempty"`
}

type batchRequest struct {
	Queries []osvQuery `json:"queries"`
}

type batchResponse struct {
	Results []batchResult `json:"results"`
}

type batchResult struct {
	Vulns []struct {
		ID       string `json:"id"`
		Modified string `json:"modified"`
	} `json:"vulns"`
	NextPageToken string `json:"next_page_token"`
}

// toQuery maps a package version to an OSV query. It reports false for a
// pair OSV cannot be asked about: a package type with no OSV ecosystem, an
// empty version, or a package URL whose name does not decode.
func toQuery(pv model.PackageVersion) (osvQuery, bool) {
	eco, ok := model.OSVEcosystem(pv.PURL)
	if !ok || pv.Version == "" {
		return osvQuery{}, false
	}
	_, rest, _ := strings.Cut(strings.TrimPrefix(pv.PURL, "pkg:"), "/")
	name, err := url.PathUnescape(rest)
	if err != nil || name == "" {
		return osvQuery{}, false
	}
	version := pv.Version
	toolchain := pv.PURL == toolchainPURL
	if toolchain {
		name = "stdlib"
		version = strings.TrimPrefix(version, "go")
	}
	if eco == ecosystemGo {
		version = strings.TrimPrefix(version, "v")
	}
	if toolchain {
		version = completeMinor(version)
	}
	if version == "" {
		return osvQuery{}, false
	}
	return osvQuery{Package: osvPackage{Name: name, Ecosystem: eco}, Version: version}, true
}

// completeMinor turns a two-component numeric version ("1.26") into a
// three-component one ("1.26.0"); any other version is returned unchanged.
func completeMinor(v string) string {
	major, minor, ok := strings.Cut(v, ".")
	if !ok || !allDigits(major) || !allDigits(minor) {
		return v
	}
	return v + ".0"
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// refSet is the union of advisory refs, one per id with the greatest
// modified time, in first-seen order.
type refSet struct {
	index map[string]int
	refs  []model.AdvisoryRef
}

func (s *refSet) add(ref model.AdvisoryRef) {
	if i, ok := s.index[ref.ID]; ok {
		if ref.Modified.After(s.refs[i].Modified) {
			s.refs[i].Modified = ref.Modified
		}
		return
	}
	s.index[ref.ID] = len(s.refs)
	s.refs = append(s.refs, ref)
}

// QueryBatch returns the advisories affecting each pair, one ref per id with
// the greatest modified time seen. Pairs OSV has no ecosystem for, and pairs
// with no version, are skipped. Inputs are sent in batches of
// Options.BatchSize, and paged results are followed for up to 50 rounds.
func (c *Client) QueryBatch(ctx context.Context, pvs []model.PackageVersion) ([]model.AdvisoryRef, error) {
	queries := make([]osvQuery, 0, len(pvs))
	for _, pv := range pvs {
		if q, ok := toQuery(pv); ok {
			queries = append(queries, q)
		}
	}

	set := &refSet{index: map[string]int{}}
	requests := 0
	pending := queries
	for round := 0; len(pending) > 0; round++ {
		// Round 0 is the initial queries; every later round is a follow-up.
		if round > maxPageRounds {
			return nil, fmt.Errorf("osv: querybatch: %s: results still paged after %d follow-up rounds", hostOf(c.base), maxPageRounds)
		}
		var next []osvQuery
		for start := 0; start < len(pending); start += c.batchSize {
			chunk := pending[start:min(start+c.batchSize, len(pending))]
			results, err := c.queryOnce(ctx, chunk)
			if err != nil {
				return nil, err
			}
			requests++
			for i, r := range results {
				for _, v := range r.Vulns {
					set.add(v)
				}
				if r.NextPageToken != "" {
					q := chunk[i]
					q.PageToken = r.NextPageToken
					next = append(next, q)
				}
			}
		}
		pending = next
	}
	c.logger.Debug("osv querybatch finished",
		"pairs", len(pvs), "queries", len(queries), "requests", requests, "advisories", len(set.refs))
	return set.refs, nil
}

// pageResult is one query's refs and the token for its next page, if any.
type pageResult struct {
	Vulns         []model.AdvisoryRef
	NextPageToken string
}

// queryOnce sends one querybatch request and returns one result per query,
// in order.
func (c *Client) queryOnce(ctx context.Context, queries []osvQuery) ([]pageResult, error) {
	host := hostOf(c.base)
	body, err := json.Marshal(batchRequest{Queries: queries})
	if err != nil {
		return nil, fmt.Errorf("osv: querybatch: %s: encode request: %w", host, err)
	}
	resp, err := c.hc.Do(ctx, httpx.Request{
		Method:     http.MethodPost,
		URL:        c.base + "/v1/querybatch",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       body,
		Validators: httpx.ValidatorsNone,
	})
	if err != nil {
		return nil, fmt.Errorf("osv: querybatch: %s: %w", host, err)
	}
	var decoded batchResponse
	if err := json.Unmarshal(resp.Body, &decoded); err != nil {
		return nil, fmt.Errorf("osv: querybatch: %s: decode response: %w", host, err)
	}
	if len(decoded.Results) != len(queries) {
		return nil, fmt.Errorf("osv: querybatch: %s: got %d results for %d queries", host, len(decoded.Results), len(queries))
	}
	out := make([]pageResult, len(decoded.Results))
	for i, r := range decoded.Results {
		out[i].NextPageToken = r.NextPageToken
		for _, v := range r.Vulns {
			if v.ID == "" {
				return nil, fmt.Errorf("osv: querybatch: %s: result %d: advisory with no id", host, i)
			}
			mod, err := parseTime(v.Modified)
			if err != nil {
				return nil, fmt.Errorf("osv: querybatch: %s: result %d: modified: %w", host, i, err)
			}
			out[i].Vulns = append(out[i].Vulns, model.AdvisoryRef{ID: v.ID, Modified: mod})
		}
	}
	return out, nil
}

// errBadTime reports a timestamp that is not RFC 3339. The value itself is
// left out, because it comes from a response body of any length.
var errBadTime = errors.New("time is not RFC 3339")

// parseTime parses an RFC 3339 time, with or without fractional seconds,
// into UTC.
func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, errBadTime
	}
	return t.UTC(), nil
}

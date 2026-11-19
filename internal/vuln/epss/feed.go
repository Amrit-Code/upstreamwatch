package epss

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const (
	defaultURL = "https://epss.empiricalsecurity.com/epss_scores-current.csv.gz"
	// maxBodyBytes caps the downloaded (possibly compressed) file.
	maxBodyBytes = 64 << 20
	// maxDecompressedBytes caps the CSV after gunzip.
	maxDecompressedBytes = 256 << 20
	// checkTimeout bounds the reachability probe.
	checkTimeout = 10 * time.Second
)

// errDecompressedTooLarge reports a gzipped feed whose CSV exceeds the cap.
var errDecompressedTooLarge = errors.New("decompressed feed too large")

// Options configures a Feed.
type Options struct {
	URL string // default https://epss.empiricalsecurity.com/epss_scores-current.csv.gz
}

// Feed downloads the EPSS scores through the shared HTTP client. The feed is
// public, so no token is sent.
type Feed struct {
	hc              *httpx.Client
	url             string
	logger          *slog.Logger
	maxDecompressed int64
}

// New returns a Feed. It does no I/O.
func New(hc *httpx.Client, opts Options, logger *slog.Logger) *Feed {
	u := opts.URL
	if u == "" {
		u = defaultURL
	}
	return &Feed{hc: hc, url: u, logger: logger, maxDecompressed: maxDecompressedBytes}
}

// Refresh downloads the scores, gunzipping them when the body is gzip, and
// returns one EPSS signal per row. Rows with a malformed score or percentile,
// or one outside 0..1, are skipped; more than 1% skipped rows, or a missing
// cve,epss,percentile header, is an error.
func (f *Feed) Refresh(ctx context.Context) ([]model.Signal, error) {
	resp, err := f.hc.Do(ctx, httpx.Request{URL: f.url, Validators: httpx.ValidatorsNone, MaxBodyBytes: maxBodyBytes})
	if err != nil {
		return nil, fmt.Errorf("epss: fetch %s: %w", f.url, err)
	}
	r, err := f.csvReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("epss: decode %s: %w", f.url, err)
	}
	signals, skipped, err := parse(r)
	if err != nil {
		return nil, fmt.Errorf("epss: decode %s: %w", f.url, err)
	}
	f.logger.Info("epss refresh finished", "signals", len(signals), "skipped", skipped)
	return signals, nil
}

// csvReader returns a reader of the CSV in body: body itself, or its
// decompressed content, capped, when body starts with the gzip magic bytes.
func (f *Feed) csvReader(body []byte) (io.Reader, error) {
	if len(body) < 2 || body[0] != 0x1f || body[1] != 0x8b {
		return bytes.NewReader(body), nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gunzip: %w", err)
	}
	return &cappedReader{r: zr, left: f.maxDecompressed}, nil
}

// cappedReader fails with errDecompressedTooLarge once more than left bytes
// have been read from r.
type cappedReader struct {
	r    io.Reader
	left int64
}

func (c *cappedReader) Read(p []byte) (int, error) {
	if c.left < 0 {
		return 0, errDecompressedTooLarge
	}
	// Read one byte past the cap so a feed of exactly the cap still ends cleanly.
	if int64(len(p)) > c.left+1 {
		p = p[:c.left+1]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	if c.left < 0 {
		return 0, errDecompressedTooLarge
	}
	return n, err
}

// parse reads an EPSS CSV: leading '#' lines, the header cve,epss,percentile,
// then rows. It returns the signals and the number of skipped rows.
func parse(r io.Reader) ([]model.Signal, int, error) {
	br := bufio.NewReader(r)
	if err := skipComments(br); err != nil {
		return nil, 0, err
	}

	cr := csv.NewReader(br)
	// Rows with the wrong number of fields are counted as skipped, not fatal.
	cr.FieldsPerRecord = -1
	cr.ReuseRecord = true

	header, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return nil, 0, errors.New("missing header cve,epss,percentile")
	}
	if err != nil {
		return nil, 0, fmt.Errorf("read header: %w", err)
	}
	if len(header) != 3 || header[0] != "cve" || header[1] != "epss" || header[2] != "percentile" {
		return nil, 0, errors.New("missing header cve,epss,percentile")
	}

	var signals []model.Signal
	skipped := 0
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("read row: %w", err)
		}
		s, ok := parseRow(rec)
		if !ok {
			skipped++
			continue
		}
		signals = append(signals, s)
	}

	if total := len(signals) + skipped; skipped*100 > total {
		return nil, 0, fmt.Errorf("%d of %d rows malformed (more than 1%%)", skipped, total)
	}
	return signals, skipped, nil
}

// skipComments consumes the lines at the start of br that begin with '#'.
func skipComments(br *bufio.Reader) error {
	for {
		b, err := br.Peek(1)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read comment lines: %w", err)
		}
		if b[0] != '#' {
			return nil
		}
		if _, err := br.ReadString('\n'); err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("read comment lines: %w", err)
		}
	}
}

// parseRow turns one cve,epss,percentile record into a signal, reporting
// false when it has the wrong number of fields or a value that is not a
// number in 0..1.
func parseRow(rec []string) (model.Signal, bool) {
	if len(rec) != 3 {
		return model.Signal{}, false
	}
	score, ok := unitFloat(rec[1])
	if !ok {
		return model.Signal{}, false
	}
	pct, ok := unitFloat(rec[2])
	if !ok {
		return model.Signal{}, false
	}
	return model.Signal{Kind: model.SignalEPSS, CVE: rec[0], EPSS: score, EPSSPercentile: pct}, true
}

// unitFloat parses s as a number in 0..1. NaN fails the range check.
func unitFloat(s string) (float64, bool) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || !(v >= 0 && v <= 1) {
		return 0, false
	}
	return v, true
}

// Check sends a HEAD request for the feed to prove its host answers, without
// downloading it. Redirects are followed.
func (f *Feed) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	host := hostOf(f.url)
	if _, err := f.hc.Do(ctx, httpx.Request{Method: http.MethodHead, URL: f.url, Validators: httpx.ValidatorsNone}); err != nil {
		return fmt.Errorf("epss: check %s: %w", host, err)
	}
	f.logger.Debug("epss check passed", "host", host)
	return nil
}

// hostOf returns the host of raw, or raw itself if it does not parse.
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return u.Host
}

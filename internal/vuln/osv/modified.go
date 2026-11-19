package osv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// modifiedCSVMaxBytes caps the download of one ecosystem's modified_id.csv,
// which lists every record in the ecosystem.
const modifiedCSVMaxBytes = 256 << 20

// ModifiedSince reads ecosystem's modified_id.csv from the OSV bucket and
// returns the refs of records modified strictly after since, newest first. A
// zero since returns every record.
func (c *Client) ModifiedSince(ctx context.Context, ecosystem string, since time.Time) ([]model.AdvisoryRef, error) {
	host := hostOf(c.bucket)
	resp, err := c.hc.Do(ctx, httpx.Request{
		Method:       http.MethodGet,
		URL:          c.bucket + "/" + url.PathEscape(ecosystem) + "/modified_id.csv",
		Validators:   httpx.ValidatorsNone,
		MaxBodyBytes: modifiedCSVMaxBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("osv: modified ids %s: %s: %w", ecosystem, host, err)
	}
	refs, err := parseModified(resp.Body, since)
	if err != nil {
		return nil, fmt.Errorf("osv: modified ids %s: %s: %w", ecosystem, host, err)
	}
	return refs, nil
}

// parseModified reads "<RFC 3339 time>,<id>" lines, newest first, up to the
// first line whose time is at or before since, and returns the refs before
// it in file order. A zero since reads every line. A trailing newline and
// CRLF line ends are accepted.
func parseModified(body []byte, since time.Time) ([]model.AdvisoryRef, error) {
	var refs []model.AdvisoryRef
	rest := body
	for n := 1; len(rest) > 0; n++ {
		line, tail, _ := bytes.Cut(rest, []byte("\n"))
		rest = tail
		line = bytes.TrimSuffix(line, []byte("\r"))
		ts, id, ok := bytes.Cut(line, []byte(","))
		if !ok || len(id) == 0 {
			return nil, fmt.Errorf("line %d: %w", n, errBadLine)
		}
		t, err := parseTime(string(ts))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if !since.IsZero() && !t.After(since) {
			break
		}
		refs = append(refs, model.AdvisoryRef{ID: string(id), Modified: t})
	}
	return refs, nil
}

// errBadLine reports a modified_id.csv line that is not "<time>,<id>".
var errBadLine = errors.New(`not "<time>,<id>"`)

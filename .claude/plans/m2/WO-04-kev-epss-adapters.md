# WO-04: KEV and EPSS feed adapters

Milestone: M2
Agent: go-mech   Model: opus
Depends on: none (interface files committed)
Report file: `.claude/plans/m2/WO-04-kev-epss-adapters.report.md`
Ticket: UPW-48

## Objective

Two adapters implement `vuln.ExploitSignals`: `kev.Feed` reads CISA's Known Exploited Vulnerabilities JSON and `epss.Feed` reads the daily EPSS CSV. Each also has `Check` for `upw doctor`. Serves M2 checklist item 7.

## Owned files

- `internal/vuln/kev/` (new package, all files, `testdata/`)
- `internal/vuln/epss/` (new package, all files, `testdata/`)

Read-only: `internal/model/*.go`, `internal/vuln/interfaces.go`, `internal/httpx/`, `internal/vuln/osv/client.go` (copy its constructor and `Check` style), `go.mod`, `go.sum`.

## Interfaces

### Consumed

```go
// internal/vuln/interfaces.go
type ExploitSignals interface {
	Refresh(ctx context.Context) ([]model.Signal, error)
}
// internal/model/advisory.go
type SignalKind string // SignalKEV, SignalEPSS
type Signal struct {
	Kind           SignalKind
	CVE            string
	KEVAddedAt     time.Time
	EPSS           float64
	EPSSPercentile float64
}
```

### Produced

```go
// internal/vuln/kev
type Options struct{ URL string } // default https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json
type Feed struct{ /* unexported */ }
func New(hc *httpx.Client, opts Options, logger *slog.Logger) *Feed
func (f *Feed) Refresh(ctx context.Context) ([]model.Signal, error)
func (f *Feed) Check(ctx context.Context) error

// internal/vuln/epss
type Options struct{ URL string } // default https://epss.empiricalsecurity.com/epss_scores-current.csv.gz
type Feed struct{ /* unexported */ }
func New(hc *httpx.Client, opts Options, logger *slog.Logger) *Feed
func (f *Feed) Refresh(ctx context.Context) ([]model.Signal, error)
func (f *Feed) Check(ctx context.Context) error
```

## Requirements

1. KEV `Refresh`: `GET` the URL with `httpx.ValidatorsNone`. The body is JSON with a `vulnerabilities` array whose objects have `cveID` and `dateAdded` (`YYYY-MM-DD`). Return one `Signal{Kind: SignalKEV, CVE, KEVAddedAt}` per entry, `KEVAddedAt` at midnight UTC of that date (zero when the date is missing or malformed; the entry is still returned). Entries whose `cveID` does not match `^CVE-\d{4}-\d{4,}$` are skipped.
2. EPSS `Refresh`: `GET` the URL with `httpx.ValidatorsNone` and `MaxBodyBytes` 64 MiB, following redirects (httpx does; the old host `epss.cyentia.com` redirects). If the response body starts with the gzip magic bytes `1f 8b`, gunzip it (cap the decompressed size at 256 MiB, error beyond); otherwise read it as plain CSV. Skip leading lines starting with `#` (the first line is `#model_version:...,score_date:...`), then expect the header `cve,epss,percentile`, then rows. Return one `Signal{Kind: SignalEPSS, CVE, EPSS, EPSSPercentile}` per row, both floats in 0..1. A row with a malformed float or a value outside 0..1 is skipped and counted; more than 1% skipped rows is an error. A missing header is an error.
3. `Check` for each: one cheap request to the feed's host that proves reachability without downloading the whole feed: `HEAD` the feed URL; any 2xx (after redirects) passes. Errors name the host.
4. Errors name the feed and host (`kev: fetch https://www.cisa.gov/...: ...`). No token is sent; both feeds are public.
5. Constructors do no I/O. Use the injected `*slog.Logger` for one info line per refresh with the count of signals (and skipped rows for EPSS).

## Tests to include

- `TestKEVRefresh`: a small JSON fixture with three entries, one with a bad CVE id and one with no date → two signals, dates as specified.
- `TestEPSSRefresh_Gzip`: a gzipped fixture with the comment line, header and five rows → five signals with exact floats.
- `TestEPSSRefresh_Plain`: same content uncompressed.
- `TestEPSSRefresh_Redirect`: server A answers 301 to server B, which serves the feed → signals returned.
- `TestEPSSRefresh_BadRows`: 2 bad rows of 10 → error (over 1%); `TestEPSSRefresh_NoHeader` → error.
- `TestCheck` for each: 200 → nil; 500 → error naming the host.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race ./internal/vuln/kev/ ./internal/vuln/epss/` | `ok` |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Applying signals to advisories, deciding when to refresh (the vulns job).
- Wiring into `upw doctor` (the wiring work order).

## Skills and docs

Load with the Skill tool: `outbound-client`.
Read: PLAN.md section 5.4 (Enrichment) and 12 (Facts not verified: the EPSS host move); `docs/external-sources.md`.

## Report

Write the full report to the report file named at the top, in the form given in your agent definition, then reply with only the short form. If a question's answer would change behaviour and nothing here or in the docs answers it, stop and report NEEDS_CONTEXT rather than guessing.

## Rulings


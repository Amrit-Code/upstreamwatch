# WO-03: OSV adapter: querybatch, record fetch and the modified_id.csv delta

Milestone: M2
Agent: go-builder   Model: opus
Depends on: none (interface files committed)
Report file: `.claude/plans/m2/WO-03-osv-adapter.report.md`
Ticket: UPW-45

## Objective

`osv.Client` (`internal/vuln/osv`) implements `vuln.AdvisorySource`: it queries OSV for package versions, fetches full records and converts them into `model.Advisory` with upstreamwatch's own package URLs and a derived severity, and reads the per-ecosystem `modified_id.csv` delta. Serves M2 checklist items 2, 3 and 6.

## Owned files

- `internal/vuln/osv/` (all files; `client.go` exists with `New`, `Options` and `Check` — keep them working)
- `internal/vuln/osv/testdata/` (new)

Read-only: `internal/model/*.go`, `internal/vuln/interfaces.go`, `internal/httpx/`, `go.mod`, `go.sum`. Do not add a dependency (the `git-pkgs/vulns` library is not in `go.mod`; write the client on `httpx` and `encoding/json`).

## Interfaces

### Consumed

```go
// internal/vuln/interfaces.go
type AdvisorySource interface {
	QueryBatch(ctx context.Context, pvs []model.PackageVersion) ([]model.AdvisoryRef, error)
	Fetch(ctx context.Context, id string) (model.Advisory, error)
	ModifiedSince(ctx context.Context, ecosystem string, since time.Time) ([]model.AdvisoryRef, error)
}
```

`internal/model/advisory.go`: `PackageVersion`, `AdvisoryRef`, `Advisory`, `AffectedPackage`, `AffectedRange`, `RangeEvent`, `SeverityLabel`, `SourceDB`, `OSVEcosystem`, `PURLType`. `internal/httpx`: `Client.Do`, `Request` (`Method`, `URL`, `Header`, `Body`, `Validators`, `MaxBodyBytes`), `Response`, `*StatusError`.

### Produced

```go
// internal/vuln/osv
type Options struct {
	BaseURL   string // default https://api.osv.dev
	BucketURL string // default https://storage.googleapis.com/osv-vulnerabilities
	BatchSize int    // default 500
}
func (c *Client) QueryBatch(ctx context.Context, pvs []model.PackageVersion) ([]model.AdvisoryRef, error)
func (c *Client) Fetch(ctx context.Context, id string) (model.Advisory, error)
func (c *Client) ModifiedSince(ctx context.Context, ecosystem string, since time.Time) ([]model.AdvisoryRef, error)
```

## Requirements

1. Package URL to OSV query (unexported function, table-tested): the ecosystem is `model.OSVEcosystem(purl)`; pairs with no ecosystem, or an empty version, are skipped. The name is the purl's namespace and name, percent-decoded: `pkg:golang/github.com/gin-gonic/gin` → `github.com/gin-gonic/gin`; `pkg:pypi/pyyaml` → `pyyaml`; `pkg:githubactions/actions/checkout` → `actions/checkout`; `pkg:npm/%40scope/name` → `@scope/name`. Special case: `pkg:golang/toolchain` → name `stdlib`.
2. Version to OSV: for ecosystem Go strip one leading `v` (`v1.9.1` → `1.9.1`); for `pkg:golang/toolchain` additionally complete a two-component version with `.0` (`1.26` → `1.26.0`) and strip a leading `go` (`go1.26.3` → `1.26.3`). Other ecosystems unchanged.
3. `QueryBatch`: `POST {BaseURL}/v1/querybatch` with JSON `{"queries":[{"package":{"name":N,"ecosystem":E},"version":V}, ...]}`, at most `BatchSize` queries per request. Response `{"results":[{"vulns":[{"id":..,"modified":..}],"next_page_token":".."}]}`, one result per query in order. For each result with a `next_page_token`, send that query again with `"page_token":<token>` (several such queries may share one follow-up request), until no tokens remain, capped at 50 follow-up rounds (error beyond). Return the union of refs, one per id, keeping the greatest `modified`. Requests use `httpx.ValidatorsNone` and the header `Content-Type: application/json`.
4. `Fetch`: `GET {BaseURL}/v1/vulns/{id}` with the id path-escaped; a 404 (`*httpx.StatusError` with status 404) returns an error wrapping `model.ErrNotFound`. Parse the OSV schema fields `id`, `aliases`, `summary`, `details`, `severity[]` (`type`, `score`), `published`, `modified`, `withdrawn`, `database_specific`, `affected[]` (`package` {`ecosystem`, `name`, `purl`}, `ranges[]` {`type`, `repo`, `events[]`}, `versions`, `ecosystem_specific`, `database_specific`). Times parse RFC 3339 (with or without fractional seconds) into UTC. `Raw` is the exact response body.
5. OSV package to upstreamwatch package for each `affected[]` entry: Go `stdlib` and `toolchain` → `pkg:golang/toolchain` (Ecosystem `golang`, Name `toolchain`); other Go → `pkg:golang/<name>` (Ecosystem `golang`); PyPI → `pkg:pypi/<name lower-cased with runs of -, _ and . replaced by ->` (Ecosystem `pypi`); GitHub Actions → `pkg:githubactions/<name>` (Ecosystem `github-actions`); npm → `pkg:npm/<name with a leading @ encoded as %40>` (Ecosystem `npm`); other ecosystems in the `model.OSVEcosystem` table → `pkg:<type>/<name>` with Ecosystem the purl type. An entry in an ecosystem not in that table (Debian, Alpine and so on) is dropped. Ecosystems with a suffix such as `Debian:12` are matched on the part before the colon. `SourceDB` is `model.SourceDB(id)`.
6. Severity: if the record's top-level `database_specific.severity` is `LOW`, `MODERATE`/`MEDIUM`, `HIGH` or `CRITICAL` (any case), use it (`MODERATE` → medium). Otherwise take the first `severity[]` entry of type `CVSS_V3`, compute its CVSS v3.1 base score from the vector with the specification's formula (unexported, table-tested against at least five published vector/score pairs, including `CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H` → 9.8 and a scope-changed vector), store `CVSSScore` and `CVSSVector`, and label 9.0 to 10 critical, 7.0 to 8.9 high, 4.0 to 6.9 medium, 0.1 to 3.9 low. A malformed vector, a CVSS_V4-only record or no severity at all gives `SeverityUnknown` and no score.
7. `ModifiedSince(ecosystem, since)`: `GET {BucketURL}/{ecosystem path-escaped}/modified_id.csv` (`GitHub Actions` → `GitHub%20Actions`) with `httpx.ValidatorsNone` and `MaxBodyBytes` 256 MiB. Each line is `<RFC 3339 time>,<id>`, newest first. Read lines until the first whose time is at or before `since`, and return the refs before it in file order. A malformed line is an error naming the line number. A zero `since` returns every line.
8. Every error names the host and what was being done (`osv: querybatch: ...`), never a body larger than 200 bytes. No token is sent.

## Tests to include

All against `httptest` servers through an `httpx` client that allows loopback (see the existing `client_test.go`):

- `TestQueryBatch_Batches`: 1,001 pairs with `BatchSize` 500 → three requests, union of ids; a skipped docker purl and an empty version are not sent.
- `TestQueryBatch_PageToken`: one result carries `next_page_token` → a follow-up request carries `page_token` for that query only; refs from both pages returned.
- `TestQueryBatch_Mapping`: the request body for toolchain `1.26`, Go `v1.9.1`, PyPI and Actions pairs matches a golden JSON.
- `TestFetch_GoRecord`: a stored real OSV record for a Go stdlib advisory in `testdata/` → affected package `pkg:golang/toolchain`, SEMVER ranges and events in order, Raw equals the file.
- `TestFetch_GHSARecord`: a GHSA record with `database_specific.severity: "HIGH"` and a CVSS v3 vector → Severity high, CVSS stored.
- `TestFetch_NotFound` → `errors.Is(err, model.ErrNotFound)`.
- `TestCVSSv3BaseScore` (table).
- `TestModifiedSince`: a CSV of five lines, since equal to the third line's time → the first two refs.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race ./internal/vuln/osv/` | `ok` |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Deciding what to query or fetch, cursors in `kv`, matching (the vulns job and WO-06).
- KEV and EPSS (WO-04).

## Skills and docs

Load with the Skill tool: `outbound-client`.
Read: PLAN.md section 5.4; `docs/external-sources.md` (OSV); the OSV schema at https://ossf.github.io/osv-schema/ if you need a field's exact meaning (the API is public; you may fetch real records from `https://api.osv.dev/v1/vulns/<id>` once each to build `testdata/`).

## Report

Write the full report to the report file named at the top, in the form given in your agent definition, then reply with only the short form. If a question's answer would change behaviour and nothing here or in the docs answers it, stop and report NEEDS_CONTEXT rather than guessing.

## Rulings


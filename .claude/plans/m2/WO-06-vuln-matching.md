# WO-06: Advisory matching rules: range evaluation, fixed version, confidence, exposure window

Milestone: M2
Agent: go-builder   Model: opus
Depends on: none (interface files committed)
Report file: `.claude/plans/m2/WO-06-vuln-matching.report.md`
Ticket: UPW-46

## Objective

Pure, unexported functions in `internal/vuln` decide whether an advisory's affected entry covers the version a dependency interval pins, which fixed version applies, how confident the match is, and the exposure window. The vulns job (a later work order in the same package) calls them. Serves M2 checklist items 2, 4, 5 and 6.

## Owned files

- `internal/vuln/match.go`, `internal/vuln/match_test.go` (new)
- `internal/vuln/version.go`, `internal/vuln/version_test.go` (new, if you want version handling in its own file)

Read-only: `internal/vuln/interfaces.go`, `internal/vuln/doc.go`, `internal/model/*.go`, `.go-arch-lint.yml` (it already lets `internal/vuln` import `github.com/git-pkgs/vers`), `go.mod`, `go.sum` (`vers` v0.7.2 is in `go.mod` as indirect; importing it directly is fine and the lead tidies `go.mod` at merge).

## Interfaces

### Consumed

`internal/model/advisory.go` (`Advisory`, `AffectedPackage`, `AffectedRange`, `RangeEvent`, `RangeType` constants, `Confidence` constants, `SourceDB`, `PURLType`), `internal/model/timeline.go` (`DepInterval`). `github.com/git-pkgs/vers` v0.7.2: `CompareWithScheme(a, b, scheme string) int`, `ValidWithScheme`, and the other exported helpers.

### Produced (unexported, in package `vuln`)

```go
// exposureBasis is vulns.exposure_basis: "published" (default) or "pin".
type exposureBasis string

const (
	basisPublished exposureBasis = "published"
	basisPin       exposureBasis = "pin"
)

// matchResult is the outcome of evaluating one affected entry against one
// pinned version.
type matchResult struct {
	affected     bool
	fixedVersion string           // first fixed version above the pin, "" if none known
	confidence   model.Confidence // set when affected
	reason       string           // why, one line; set whether or not affected
}

// evaluate decides whether aff (an entry of adv whose package is the
// interval's package) covers the version iv pins.
func evaluate(adv model.Advisory, aff model.AffectedPackage, iv model.DepInterval) matchResult

// exposureWindow returns the exposure window of a match on iv.
func exposureWindow(iv model.DepInterval, published time.Time, basis exposureBasis) (from, to time.Time)

// normalizeVersion returns the version in the form the advisory database
// uses for purl, and whether it is an exact pin.
func normalizeVersion(purl, version string) (v string, exact bool)
```

## Requirements

1. `normalizeVersion`: for `pkg:golang/...` strip one leading `v`; for `pkg:golang/toolchain` also strip a leading `go`, and complete a two-component version with `.0` (`1.26` → `1.26.0`) returning `exact = false` (a floating image tag or go directive); every other non-empty version is `exact = true`. An empty version returns `("", false)`.
2. Version comparison uses `vers.CompareWithScheme` with the scheme for the package's ecosystem (Go → the Go/semver scheme, PyPI → PEP 440, npm, cargo, gem, nuget, maven, composer, pub, hex by name; GitHub Actions → semver after stripping a leading `v`; find the scheme names the library accepts and record them in the report). The OSV value `introduced: "0"` is lower than every version.
3. `evaluate` returns not affected, with a reason, when: `adv.IsWithdrawn()`; `aff.Package.PURL != iv.Package.PURL`; the interval's version is empty (`reason` "no resolved version").
4. Range evaluation follows the OSV schema's algorithm for `SEMVER` and `ECOSYSTEM` ranges: the version is affected by a range when some `introduced` event is at or below it and no `fixed` or `limit` event between that introduction and the version is at or below it, and, for `last_affected`, the version is at or below the `last_affected` value. Evaluate events sorted by version, not in record order. A range whose events cannot be compared (a value the scheme rejects) is skipped and noted in the reason.
5. The explicit `versions` list matches when it contains the normalised version exactly (string equality after normalising both sides the same way).
6. `GIT` ranges are never evaluated. An entry whose only ranges are `GIT` is affected only through its `versions` list.
7. `fixedVersion`: the smallest `fixed` event value, across the entry's SEMVER and ECOSYSTEM ranges that matched, that is greater than the pinned version; empty when the matched ranges have only `last_affected` or no upper bound, or when the match came from the `versions` list alone.
8. Confidence of an affected result (first rule that applies):
   - **low**: matched only through the `versions` list (including GIT-only entries, reason "GIT ranges not evaluated; matched on the versions list").
   - **medium**: the advisory's `SourceDB` is `CVE` or anything other than `GO`, `PYSEC`, `RUSTSEC`, `GHSA`; or the matching range has no `fixed` event (only `last_affected`, or open-ended); or the pin is not exact (`normalizeVersion` returned false, reason names the floating version, for example "toolchain 1.26 treated as 1.26.0").
   - **high**: everything else (native database, a matching range with a `fixed` event, exact pin).
   The reason is one short line naming the rule, for example `native GO record, fixed in 1.26.4, exact pin`.
9. `exposureWindow`: `to = iv.To` (zero while open). With `basisPin`, `from = iv.From`. With `basisPublished`, `from = max(iv.From, published)`; when `published` is zero, `from = iv.From`; when the interval is closed and `published` is at or after `iv.To` (the pin left before the advisory was public), `from = iv.From`. All times UTC.
10. No I/O, no logging, no global state; safe for concurrent use.

## Tests to include

Table tests in `match_test.go`, one row per case, each naming the case:

- SEMVER `introduced 0, fixed 1.2.3`: pins 1.2.2 (affected, fixed 1.2.3, high for a GO id), 1.2.3 (not), v1.2.2 Go form (affected).
- Two ranges in one entry, pin in the second; events listed out of order in the record.
- `introduced 1.0.0, last_affected 1.4.0`: pins 1.4.0 (affected, medium, fixed empty) and 1.4.1 (not).
- Open-ended `introduced 2.0.0` only: pin 3.0 affected, medium.
- `limit` event.
- ECOSYSTEM PyPI with PEP 440 versions (`1.0rc1` < `1.0`, `2.0.post1` > `2.0`).
- `versions` list only: pin listed → affected, low; not listed → not.
- GIT-only entry with and without the pin in `versions`.
- Toolchain `1.26` (floating) against `introduced 1.26.0-0, fixed 1.26.4` → affected, medium, fixed 1.26.4; toolchain `go1.26.3` → exact.
- `CVE-` id with a fixed event → medium.
- Withdrawn advisory, other package, empty version → not affected with the stated reasons.
- `exposureWindow` cases: pin basis; published before the pin; published inside the interval; published after a closed interval; zero published; open interval.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -run 'TestEvaluate\|TestExposureWindow\|TestNormalizeVersion' ./internal/vuln/` | `ok` |
| `git status --porcelain` | only owned files and the report file (and, if `go build` rewrites it, the `// indirect` comment on `vers` in `go.mod`: report that, do not keep other `go.mod` edits) |

## Out of scope

- Querying, storing, findings, KEV and EPSS (the vulns job, WO-01, WO-03, WO-04).
- Ranking (WO-05).

## Skills and docs

No domain skill needed.
Read: PLAN.md section 5.4 (Matching, Exposure, Confidence); the OSV schema's "Evaluation" section at https://ossf.github.io/osv-schema/#evaluation.

## Report

Write the full report to the report file named at the top, in the form given in your agent definition, then reply with only the short form. If a question's answer would change behaviour and nothing here or in the docs answers it, stop and report NEEDS_CONTEXT rather than guessing.

## Rulings


# WO-15: Referee test: advisory parity with osv-scanner

Milestone: M2
Agent: go-mech   Model: opus
Depends on: WO-01 to WO-13 (merged)
Report file: `.claude/plans/m2/WO-15-osv-scanner-parity.report.md`
Ticket: UPW-58

## Objective

`make e2e` also runs a tagged test that indexes the two public fixture repositories already used by the timeline referee, runs the real vulns job against api.osv.dev, and compares the advisory ids of the open matches for each HEAD lockfile with what `osv-scanner` v2.7.1 reports for the same file. Any difference fails the test unless a listed exception, backed by a recorded decision, explains it. Serves M2 checklist item 2.

## Owned files

- `cmd/upw/advisory_parity_e2e_test.go` (new; first line `//go:build e2e`)
- `cmd/upw/testdata/advisory_parity/` (new, if needed for an exceptions file)
- `Makefile`: widen the `e2e` target's `-run` to `'TestParity|TestAdvisoryParity'` only
- `.github/workflows/ci.yml`: in the existing `e2e` job, install osv-scanner v2.7.1 (release binary, SHA-256 verified like git-pkgs) only

Read-only: every production file, `cmd/upw/parity_e2e_test.go` (you may call its helpers; it is in the same package and build tag), `go.mod`, `go.sum`. A difference you believe is an upstreamwatch bug is reported, not patched.

## Interfaces

### Consumed

Read the merged code: the fixture list and indexing helpers in `cmd/upw/parity_e2e_test.go` (glow at a pinned commit, cleo at a pinned commit); `vuln.NewService`, `osv.New`, `httpx.New`; `store.Open`, `(*store.DB).Migrate`, `ListAllIntervals`, and a way to read the matches (use `GetFindingDetail` over `ListUnresolvedFindings(model.KindAdvisory)`, or add nothing and read `advisory_matches` through the store methods that exist; do not add SQL to the test).

`osv-scanner scan source --format json -L <lockfile>` (v2.7.1) prints JSON with `results[].packages[].vulnerabilities[].id` and `groups[].ids` (aliases grouped). Install locally with `go install github.com/google/osv-scanner/v2/cmd/osv-scanner@v2.7.1` (the lead has approved this tool for the Mac; it does not enter `go.mod`).

## Requirements

1. For each fixture, index it at its pinned commit exactly as the timeline referee does, then run `vuln.Service` (real OSV client, no KEV or EPSS feeds) over a fresh store.
2. Our side, per HEAD manifest path: the set of advisory ids of matches that are open (`ExposedTo` zero) on intervals of that manifest.
3. osv-scanner side: run it on the same file in the non-bare clone at the pinned commit (`go.mod` for glow, `poetry.lock` for cleo); collect every vulnerability id. Note that osv-scanner reports Go standard library advisories from a `go.mod`'s `go`/`toolchain` line as package `stdlib`; ours appear under `pkg:golang/toolchain` and must be included on our side.
4. Compare as sets per file. Treat two ids as equal when osv-scanner groups them as aliases of each other and our side has one of them.
5. Exceptions: a small table, each entry naming fixture, file, advisory id, side and a one-line reason pointing at a recorded decision in `docs/decisions.md` ("Lead decisions for M2", for example the two-component toolchain version being evaluated as `.0`). An exception that matches nothing fails the test.
6. On failure print, per file, both sets and the differences, sorted.
7. If unexplained differences remain, do not add exceptions for them and do not change production code: report DONE_WITH_CONCERNS with the differing ids, your analysis of which side is wrong (fetch the advisory from `https://api.osv.dev/v1/vulns/<id>` and reason from its ranges and the pinned version) and the smallest reproduction.
8. The test skips with a clear message when `osv-scanner` or `git-pkgs` is not on PATH.

## Tests to include

- `TestAdvisoryParity/glow`, `TestAdvisoryParity/cleo`.
- A small unit test under the tag for the alias-aware set comparison and a stale exception.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go vet -tags e2e ./cmd/upw/` | exit 0 |
| `go test -tags e2e -race -count=1 -run TestAdvisoryParity -v -timeout 20m ./cmd/upw/` | `ok`, or DONE_WITH_CONCERNS per requirement 7 |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- The private-target parity run on the VM (owner item 20). Fixing production code.

## Skills and docs

Load with the Skill tool: `go-test` is preloaded for test-writers only; read `.claude/skills/go-test/SKILL.md` section on referee tests.
Read: PLAN.md 5.4 (Differential test); `.claude/plans/m1/WO-10-gitpkgs-parity.md` for how the timeline referee was specified.

## Report

Write the full report to the report file named at the top, in the form given in your agent definition, then reply with only the short form. If a question's answer would change behaviour and nothing here or in the docs answers it, stop and report NEEDS_CONTEXT rather than guessing.

## Rulings

- 2026-11-19 (lead): on the Mac osv-scanner 2.7.1 is installed at `$HOME/go/bin/osv-scanner` (built with `CGO_ENABLED=0`; the cgo build fails to link). Look it up with `exec.LookPath("osv-scanner")` first, then `$(go env GOPATH)/bin/osv-scanner`; skip only when neither exists.

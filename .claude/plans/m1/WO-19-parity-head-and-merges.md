# WO-19: Make the parity test see HEAD state and merge-dated events

Milestone: M1
Agent: test-writer   Model: opus
Depends on: everything through WO-16 (merged)
Report file: `.claude/plans/m1/WO-19-parity-head-and-merges.report.md`
Ticket: UPW-38 (follow-up from the Fable review)

## Objective

The Fable review found that `TestParity` drops git-pkgs events off the first-parent chain and at merges, and upw's events at merges, so the one behaviour that distinguishes upstreamwatch's walker (dating a merged change at the merge) is invisible to the referee, and a stale HEAD state would pass. After this work order the referee also checks the state at HEAD and the merge-dated events.

## Owned files

- `cmd/upw/parity_e2e_test.go`

Read-only: every other file. A difference you believe is an upstreamwatch bug is reported (DONE_WITH_CONCERNS with the smallest reproduction), not patched, and not hidden with an exception unless a recorded decision in `docs/decisions.md` explains it.

## Requirements

1. **HEAD state.** For each fixture, compare upw's open intervals at HEAD (`ListIntervals`, `IsOpen`) with the dependencies at HEAD computed independently: parse each handled manifest blob at the pinned HEAD commit with `extract.New(...).Parse` read straight from the clone (`git show <sha>:<path>` in the test's non-bare clone, applying the timeline's exclusion rules and first-occurrence dedup), keyed by (manifest path, package URL) with version and requirement. They must be equal. This is independent of the walk, so a stale incremental state fails it.
2. **Merge-dated events.** For each merge commit M on the first-parent chain, collapse the git-pkgs events of the commits M brought in (commits reachable from M's second parent and not from its first parent) into a net per-(manifest, package) change (first `added`/`changed` to last state; added-then-removed cancels; PyPI names normalised as today), and compare it with upw's events at M (same normalisation and exceptions as the rest of the test). Add a separate tally in the log line.
3. Existing behaviour stays: pins, floors, exceptions (each single-event, cited), skip without git-pkgs.

## Tests to include

- `TestParity/glow`, `TestParity/cleo` with the two new checks.
- Unit tests (under the tag) for the merge collapse: added then changed → one changed to the last version; added then removed → nothing; two changes → one changed with the first previous and last new version.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go vet -tags e2e ./cmd/upw/` | exit 0 |
| `make e2e` | `ok`, both subtests PASS; or DONE_WITH_CONCERNS with the differences reported |
| `git status --porcelain` | only the owned file and the report file |

## Skills and docs

Preloaded by your agent definition; do not reload. Read: `cmd/upw/parity_e2e_test.go`, `docs/decisions.md` ("Lead decisions for M1"), `.claude/plans/m1/WO-10-gitpkgs-parity.md` and its report.

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

None.

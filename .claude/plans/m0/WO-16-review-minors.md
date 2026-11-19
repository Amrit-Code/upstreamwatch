# WO-16: Minor findings from the M0 reviews

Milestone: M0
Agent: go-mech   Model: opus
Depends on: WO-10, WO-13, WO-14 (merged)
Report file: `.claude/plans/m0/WO-16-review-minors.report.md`
Ticket: UPW-26

## Objective

Close four minor findings the reviewers raised on merged M0 work, so they do not surface later as bugs.

## Owned files

- `internal/httpx/policy.go`, `internal/httpx/policy_test.go`, `internal/httpx/client_test.go`
- `internal/config/load.go`, and one new test file `internal/config/load_multidoc_test.go`
- `internal/sched/integration_test.go` (comment only)
- `internal/query/interfaces.go` (doc comment only; the lead allows this one edit)

Everything else is read-only.

## Requirements

1. `internal/httpx`: on a cross-host redirect, `Content-Type` is kept along with `User-Agent`, `Accept`, `Accept-Encoding` and `Accept-Language`, so a POST re-sent on a 307 or 308 still describes its body. Every other header is still removed. Update the doc comment and extend the cross-host redirect tests: a POST with a JSON body redirected with 307 to another host arrives with its `Content-Type` and body and without `Authorization` or `X-Api-Key`.
2. `internal/httpx`: the address policy also refuses `fec0::/10` (deprecated site-local) and IPv4-compatible IPv6 addresses (`::a.b.c.d`, the `::/96` prefix other than `::` and `::1`, which are already refused), judging the latter by the embedded IPv4 address in the same way as NAT64. Add table cases: `fec0::1` refused, `::10.0.0.1` refused, `::8.8.8.8` allowed.
3. `internal/config`: `Load` rejects a file holding more than one YAML document. After decoding the first document, a second decode must return `io.EOF`; anything else is an error `parse <path>: more than one YAML document`. Tests: a two-document file is rejected; a file with a trailing `---` and nothing after it, and a file ending in `...`, still load.
4. `internal/sched/integration_test.go`: replace the stale `BUG:` comment above the cancelled-context `Stop` case with a plain statement of the requirement (Stop returns nil when no chain is running, whatever the context). No code change.
5. `internal/query/interfaces.go`: the doc comment on `ListOpenFindings` says "highest score first, then lowest ID". No signature change.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -count=3 ./internal/httpx/ ./internal/config/` | `ok` for both |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Anything else in the reviewers' notes (the email channel requiring a username, the data-directory mode for a bare filename, the git environment's `HOME`); they are recorded for later milestones.

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `outbound-client`.

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git.

## Rulings

- 2026-11-19: Accepted as built and not changed here: `-once` with a single job and `-repo` with `run all` are usage errors; doctor numbers notify channels by their position in the config; the pipeline integration tests advance a `synctest` fake clock with `time.Sleep`.

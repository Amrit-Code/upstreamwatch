# WO-16: Fixes from the M2 security review

Milestone: M2
Agent: go-mech   Model: opus
Depends on: WO-01 to WO-14 (merged)
Report file: `.claude/plans/m2/WO-16-security-fixes.report.md`
Ticket: UPW-60

## Objective

Fix the three findings of the M2 security review (one medium, two low) so the milestone's security review is clean.

## Owned files

- `internal/report/render.go`, `internal/report/render_test.go`, `internal/report/testdata/` (golden updates)
- `internal/notify/discord/notifier.go`, its tests and `testdata/`
- `internal/notify/slack/notifier.go`, its tests and `testdata/`
- `internal/vuln/osv/record.go`, its tests
- `cmd/upw/joblock.go`, `cmd/upw/joblock_test.go`

Read-only: everything else, `go.mod`, `go.sum`.

## Requirements

1. (medium) Bare URLs in upstream advisory text must not reach any channel as clickable links. In `internal/report/render.go`, for upstream-derived text (the summary or details-derived line, and any other text that comes from the advisory record), defang URLs once for every channel: replace each case-insensitive `https://` with `hxxps[://]`, `http://` with `hxxp[://]`, and any other `<scheme>://` with `<scheme>[://]`. Internal text (package URLs such as `pkg:golang/...`, repo slugs, advisory ids, the finding URL built from `http.base_url`) is not changed. Golden test with a summary containing `https://phish.example/login` and `HTTP://x.example`.
2. (low) Discord: also escape `<` (backslash) in `escapeRune`, and send `allowed_mentions: {"parse": []}` on the heartbeat message too. Slack: escape `*`, `_`, `~` and backtick in upstream text so it cannot break the bold wrapper (prefix each with a zero-width space, or another approach that Slack renders literally; state which in the report and show it in the golden).
3. (low) OSV: `Fetch` passes `MaxBodyBytes: 2 << 20` (2 MiB) per request; a record whose `id` differs from the id requested is an error (no record saved); `Details` longer than 64 KiB is truncated on a rune boundary with `…` appended (Raw keeps the exact body).
4. (follow-up) `cmd/upw/joblock.go`: when acquiring the shared pipeline lock has to wait, log one Info line naming the job and the lock path before waiting, so a manual `upw run` behind a long scheduled job does not look hung.

## Tests to include

- Golden and unit tests for each requirement; each must fail with the fix reverted (state in the report that you checked).

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race ./internal/report/ ./internal/notify/... ./internal/vuln/osv/ ./cmd/upw/` | `ok` |
| `git status --porcelain` | only owned files and the report file |

## Skills and docs

Load with the Skill tool: `outbound-client`, `security-check` (read only the threat list).

## Report

Write the full report to the report file named at the top, in the form given in your agent definition, then reply with only the short form.

## Rulings

- 2026-11-20 (lead), on the BLOCKED report: `cmd/upw/jobs.go` is added to your owned files for the one-argument edit (pass `logger` to `withPipelineLock`), as you proposed.
- 2026-11-20 (lead), on the Astra review: WO16-R1 is fixed in a round (defang the "Now: <rank reason>" line of a changed advisory item; the rank reason can carry an upstream fixed version). WO16-R2 (failed digests stored before this change are resent un-defanged) is dismissed: M2 has never been deployed, so no stored digest payload predates WO-16.

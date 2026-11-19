# WO-18: Rebuild flag, blob cap key, per-job locks, terminal-safe errors, package name

Milestone: M1
Agent: go-builder   Model: opus
Depends on: everything through WO-16 (merged); lead commit adding `model.Scope.Rebuild`
Report file: `.claude/plans/m1/WO-18-cli-config-store-followups.report.md`
Ticket: UPW-41 (M1 acceptance; findings of the Fable review)

## Objective

The `cmd/upw`, `internal/config` and `internal/store` side of the Fable review's findings: a way to force a timeline rebuild, the blob cap as a config key, protection against two processes running the same job on one clone, escaped job errors on the terminal, and the package name keeping its first spelling as the decision says.

## Owned files

- `cmd/upw/` (all files except `parity_e2e_test.go`, which WO-19 owns)
- `internal/config/` (all files)
- `internal/store/timeline.go` and `internal/store/timeline_test.go` (the package upsert only)
- `deploy/upw.example.yaml`

Read-only: `internal/timeline/` and `internal/gitsrc/` (WO-17 runs in parallel), `internal/model/`.

## Findings to fix (Fable review, in substance)

- **[medium, part]** No way to force a rebuild of a repo's timeline.
- **[medium, part]** The blob cap is not configurable.
- **[low]** Two processes can run the same job on one clone (`docker compose exec upw upw run ingest` beside `upw serve`): the loser can delete the winner's staging ref, two timeline runs collide on `UNIQUE(repo_id, sha)`, and `prune` can remove another fetch's in-flight pack.
- **[low]** `upw run` prints job errors (which carry scrubbed git stderr a remote controls) unescaped.
- **[low]** `internal/store/timeline.go` package upsert does `DO UPDATE SET name = excluded.name ...`, contradicting the decision that `packages.name` keeps the first spelling, and flipping the row on every run when two repos spell a name differently.

## Requirements

1. **`-rebuild` flag** on `upw run`: valid only with job `timeline` (and with `-repo` or alone); sets `model.Scope{RepoSlug, Rebuild: true}`. Any other job with `-rebuild`, or `run all -once -rebuild`, is a usage error (exit 2). `upw help` and the run synopsis mention it. (The timeline honours `Scope.Rebuild` after WO-17; until then the flag is accepted and passed through.)
2. **`git.max_blob_mb`** (int, default 8, at least 1) → `gitsrc.Options.MaxBlobBytes` in `gitOptions`; documented in `deploy/upw.example.yaml`; `TestGitOptions_Strict` updated.
3. **Per-job cross-process lock.** In `cmd/upw`, wrap each registered job so that `Run` first takes an exclusive `flock` on `<data_dir>/locks/<job>.lock` (directory mode 0700, file 0600), waiting while the context allows; release on return. Use `syscall.Flock` with `LOCK_EX` in a goroutine-friendly way (for example `LOCK_EX|LOCK_NB` retried every 200 ms until the context ends), so a blocked wait honours cancellation. The `Name` of the wrapped job is unchanged. A test proves two runners on the same data directory never run the same job at once (use a job that records overlap) and that a cancelled waiter returns the context error.
4. **Terminal-safe job errors**: every place `cmd/upw` prints an error or a stats line to stdout or stderr (main's `upw: <err>`, `runAll`, `cmdRun`) passes the text through the same quoting rule as `upw timeline` text output (fields containing a `unicode.IsControl` rune other than `\n` and `\t`, or invalid UTF-8, printed with `strconv.Quote`); reuse the helper, do not duplicate the rule.
5. **Package name**: the package upsert no longer changes `name` on conflict (insert sets it; conflicts update only `ecosystem` if it was empty, and `updated_ts` only when something changed). Test: two upserts with `Jinja2` then `jinja2` for the same PURL leave `Jinja2` and the second does not move `updated_ts`.

## Tests to include

- `TestRun_RebuildFlag`: `upw run timeline -rebuild -repo x` builds `Scope{RepoSlug: "x", Rebuild: true}` (assert through a fake job or the runner); `upw run ingest -rebuild` → exit 2; `upw run all -once -rebuild` → exit 2.
- Config: `git.max_blob_mb` default and validation.
- `TestJobLock_NoOverlap` and `TestJobLock_CancelledWait` as in requirement 3.
- `TestRun_ErrorIsTerminalSafe`: a job failing with an error containing `\x1b[31m` → the printed line contains the quoted form, not the raw escape.
- Store: the package-name test in requirement 5.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -count=1 ./cmd/upw/ ./internal/config/ ./internal/store/` | `ok` |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Timeline and gitsrc behaviour (WO-17), the parity test (WO-19).

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `go-test`, `sqlite-store`.
Read: `docs/decisions.md` ("Lead decisions for M1", the entry from the Fable review), `cmd/upw/runjob.go`, `cmd/upw/jobs.go`, `cmd/upw/timeline.go` (the quoting helper), `internal/store/timeline.go`.

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

None.

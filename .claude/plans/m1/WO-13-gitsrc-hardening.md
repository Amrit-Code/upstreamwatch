# WO-13: gitsrc hardening from the M1 security review

Milestone: M1
Agent: go-builder   Model: opus
Depends on: everything through WO-12 (merged)
Report file: `.claude/plans/m1/WO-13-gitsrc-hardening.report.md`
Ticket: UPW-40 (M1 security review)

## Objective

Close the two `gitsrc` findings of the M1 security review: a hostile target must not be able to exhaust memory through `git log` output or large blobs, and a fetch rejected for exceeding the clone cap must not leave its objects on disk.

## Owned files

- `internal/gitsrc/` (all files)

Read-only for you: everything else, including `internal/timeline/` (WO-14 runs in parallel) and `cmd/upw/`.

## Findings to fix (from the security review, verbatim in substance)

- **[medium] No memory bounds** (part owned here): `internal/gitsrc/history.go` buffers `git log` output in an unbounded `bytes.Buffer`; `internal/gitsrc/blobs.go` allocates up to `MaxBlobBytes` (default 5 MiB) per blob.
- **[medium] Soft clone cap; rejected fetch leaves objects**: `internal/gitsrc/sync.go` polls the clone size once a second, and after an over-cap later fetch it deletes the staging ref but the pack or loose objects stay, so the clone stays over the cap and each scheduled run downloads again; with git's default `fetch.unpackLimit` a small, highly deltified pack is exploded into loose objects.

## Requirements

1. `Options.MaxBlobBytes` zero now means 1 MiB (was 5 MiB). Update the doc comment.
2. New `Options.MaxLogBytes` (zero means 64 MiB): `ManifestCommits` stops reading and returns an error naming the limit when `git log` output exceeds it, killing the git process. No unbounded buffer remains in `history.go`.
3. Every fetch passes `-c fetch.unpackLimit=1 -c transfer.unpackLimit=1`, so received packs are kept as single pack files.
4. The size watch polls every 250 ms (was 1 s).
5. When a later fetch into an existing clone is rejected for exceeding `MaxCloneBytes`, after deleting the staging ref run `git prune --expire=now` and `git repack -a -d` is not required; then measure again: if the clone is still above the cap, remove the clone directory entirely (the next sync re-creates it from scratch) and say so in the returned error. The branch ref is never moved by a rejected fetch (unchanged behaviour).
6. Other errors from a later fetch (network, auth) leave the clone as it was (unchanged).

## Tests to include

- `TestManifestCommits_LogCap`: a fixture with enough commits that the output exceeds a small `MaxLogBytes` → error mentioning the cap; no goroutine or process left behind.
- `TestReadBlobs_DefaultCap`: with `MaxBlobBytes` zero, a 2 MiB blob → `TooLarge`; a 512 KiB blob is read.
- Extend `TestSync_IncrementalCapKeepsHead` (or add `TestSync_IncrementalCapRemovesObjects`): after a rejected over-cap later fetch, the clone directory's size is back at or below the pre-fetch size plus a small slack, or the clone is gone and the error says it was removed; the next sync with a large enough cap succeeds.
- A test (or an assertion in an existing one) that fetch invocations carry both `unpackLimit=1` settings.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -count=1 -v ./internal/gitsrc/` | `ok`, the tests above PASS |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Blob chunking, dependency caps and events caps in the timeline (WO-14).
- Token host restriction and CLI output escaping (WO-14).

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `outbound-client`, `go-test`.
Read: `.claude/skills/outbound-client/references/GIT-INVOCATION.md`, `internal/gitsrc/sync.go`, `history.go`, `blobs.go`.

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

None.

# WO-15: Redact skipped dependency strings in extractor debug logs

Milestone: M1
Agent: go-mech   Model: opus
Depends on: WO-14 (merged)
Report file: `.claude/plans/m1/WO-15-extract-log-redaction.report.md`
Ticket: UPW-40 (M1 security review, follow-up from the WO-14 review)

## Objective

`internal/extract/library.go` logs the raw string of a skipped dependency at debug level (`"docker action skipped", "uses", dep.Name` and the `local package skipped` line). A workflow step such as `uses: docker://user:pw@host/img`, or a lockfile path entry with credentials, would put the credential in a log line. After this work order every string an extractor logs passes through `model.Redact`.

## Owned files

- `internal/extract/` (all files directly in it; not `internal/extract/unity/`)

Read-only: everything else.

## Requirements

1. Every log attribute in `internal/extract` whose value comes from file content (dependency names, `uses` values, URLs, paths from inside a lockfile) is passed through `model.Redact` before logging. Attributes that are fixed strings or the manifest path given to `Parse` stay as they are.
2. No behaviour change otherwise: the same dependencies are kept and skipped.

## Tests to include

- `TestParse_SkippedDockerActionLogRedacted`: a workflow with `uses: docker://user:SECRET@registry.example/img:1` parsed with a debug-level logger writing to a buffer → the skipped step is still skipped, and the buffer does not contain `SECRET`.
- The same for a poetry.lock local package entry whose source URL contains `user:SECRET@`.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -count=1 -run 'LogRedacted' -v ./internal/extract/` | both tests PASS |
| `git status --porcelain` | only owned files and the report file |

## Skills and docs

Preloaded by your agent definition; do not reload. Read: `internal/extract/library.go`, `internal/model/redact.go`.

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

None.

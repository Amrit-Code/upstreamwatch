# WO-07: Redact secrets from job error text

Milestone: M1
Agent: go-mech   Model: opus
Depends on: none
Report file: `.claude/plans/m1/WO-07-redact-job-errors.report.md`
Ticket: new, from the M0 security review (finding waived for M0 by the owner on 2026-11-19, fixed in M1)

## Objective

A failed job's error text can contain a URL with credentials or a token echoed by an outside tool. The runner stores that text in `runs.error`, in the `job_failure` finding's rank reason, and in its log line, and both `/health` and the findings page show it. After this work order every one of those places shows the text with secrets redacted. Serves M1 checklist item 16.

## Owned files

- `internal/pipeline/redact.go`, `internal/pipeline/redact_test.go` (new)
- `internal/pipeline/runner.go` (only the lines that turn the job error into text, and the returned error)
- `internal/pipeline/runner_test.go` (add tests only)

Lead-owned, read-only for you: `internal/pipeline/interfaces.go`, `internal/model/`.

## Interfaces

### Produced

```go
// internal/pipeline/redact.go
// redact returns s with credentials replaced by "[REDACTED]".
func redact(s string) string
```

## Requirements

1. `redact` replaces, wherever they appear in `s`:
   - URL userinfo: in `scheme://user:password@host` and `scheme://token@host`, the userinfo becomes `[REDACTED]` (`https://[REDACTED]@host/x`). Scheme-less `user:pass@host` is left alone (too many false positives).
   - GitHub tokens: `github_pat_` followed by `[A-Za-z0-9_]{20,}`, and `gh[pousr]_` followed by `[A-Za-z0-9]{20,}`.
   - Anthropic and OpenAI style keys: `sk-ant-` followed by `[A-Za-z0-9_-]{10,}`, and `sk-` followed by `[A-Za-z0-9_-]{20,}`.
   - HTTP authorization values: `Authorization: <scheme> <value>` (header name matched case-insensitively), and `Bearer <value>` and `Basic <value>` (matched case-sensitively, capitalised as shown), where `<value>` is the following run of non-space characters; the scheme word stays and the value becomes `[REDACTED]`.
   - `x-access-token:<value>` up to the next `@`, space or end.
   Everything else is unchanged. The function is pure and safe for concurrent use (regular expressions compiled once at package level).
2. In `Runner.Run`, the job error text is computed once as `redact(jobErr.Error())` and that text is used for `run.Error`, for the rank reason (still truncated with `truncateUTF8` to `maxReasonErrorBytes`, after redaction), and for the `error` attribute of the "job failed" log line.
3. The error `Run` returns for a failed job has the redacted text as its message and still unwraps to the job's error, so `errors.Is(err, context.Canceled)` and similar checks keep working. Wrap it with the existing `run job <name>: ` prefix as today.
4. Nothing else in the runner changes.

## Tests to include

- `TestRedact`: a table covering each form in requirement 1, one case with several secrets in one string, and negative cases that must not change (`https://github.com/org/repo`, `user@example.com`, `sk-1` (too short), `basic idea`).
- `TestRunner_RedactsJobError`: a job failing with `fetch https://u:hunter2@git.example/x: github_pat_` plus 30 characters → the `runs` row's error (through the fake `RunStore`), the finding's rank reason, the captured log output and the returned error's text contain neither `hunter2` nor the token, and do contain `[REDACTED]`.
- `TestRunner_RedactedErrorUnwraps`: a job returning an error wrapping `context.Canceled` → `errors.Is(returned, context.Canceled)` is true.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -run 'Redact' -v ./internal/pipeline/` | `ok`, the three tests PASS |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Redacting text anywhere other than the runner. `/health` and the findings page read what the runner stored.
- Changing what jobs put in their errors.

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `go-test`.
Read: `internal/pipeline/runner.go`, `docs/architecture.md` (the paragraph after the second sequence diagram).

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

None.

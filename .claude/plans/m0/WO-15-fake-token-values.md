# WO-15: Rename fake token values that trip the repository hygiene grep

Milestone: M0
Agent: go-mech   Model: opus
Depends on: none
Report file: `.claude/plans/m0/WO-15-fake-token-values.report.md`
Ticket: UPW-26 (checklist item 18)

## Objective

M0 checklist item 18 greps the tree for real-looking secrets and must print nothing. It currently matches made-up values in tests. Rename them so the grep is clean without weakening any test.

## Owned files

- `internal/config/load_test.go`
- `internal/model/secret_test.go`
- any other tracked `*_test.go` or `testdata/` file the acceptance grep matches

Everything else is read-only, including all non-test code and everything under `.claude/` and `docs/`.

## Requirements

1. Replace every test value matching `github_pat_[A-Za-z0-9]`, `sk-ant-[A-Za-z0-9]` or `discord(app)?\.com/api/webhooks/[0-9]` with a value that does not match those patterns and has the same length class for redaction (longer than 8 characters, same last four characters where a test asserts them), for example `fake-token-abcdefgh1234` and `fake-token-literal`.
2. No assertion is weakened or removed; every test still proves what it proved.
3. No non-test file changes.

## Acceptance

| Command | Expected |
|---|---|
| `git grep -nE -e 'github_pat_[A-Za-z0-9]' -e 'sk-ant-[A-Za-z0-9]' -e 'discord(app)?\.com/api/webhooks/[0-9]' -- ':!.claude/plans/m0/WO-15-fake-token-values.md' ':!.claude/skills' ':!mvp-plan' ':!docs'` | no output for any file you own; list any remaining match outside your files in the report |
| `make check` | exit 0 |
| `git status --porcelain` | only owned files and the report file |

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git.

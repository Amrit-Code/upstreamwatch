# WO-<nn>: <title>

Milestone: M<n>
Agent: <go-builder | go-mech | test-writer>   Model: <opus | sonnet>
Depends on: <WO ids already merged to main, or "none">
Report file: `.claude/plans/m<n>/WO-<nn>-<slug>.report.md`

## Objective

<One to three sentences: what exists when this is done, and which milestone item it serves.>

## Owned files

You may create and edit only these paths. Everything else is read-only.

- `<internal/pkg/>` (all files in the package)
- `<internal/pkg/testdata/>`

Lead-owned, read-only for you (stop and report if one is wrong):

- `<internal/model/file.go>`
- `<path of the file declaring a consumed interface>`
- `go.mod`, `go.sum`

## Interfaces

### Consumed

```go
// <path>
<exact declarations copied from the committed file>
```

### Produced

```go
// <path this work order creates>
<exact exported declarations other work orders will call: names, parameters, results>
```

## Requirements

1. <One checkable behaviour, with the exact value from PLAN.md or docs/.>
2. <...>

## Tests to include

- `<TestName>`: <input> -> <expected result>
- <...>

Add a few sanity cases of your own for each new exported function. A full suite is a later work order.

## Acceptance

Run from the repository root. Each command must give the stated result.

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -run '<TestName>' ./<internal/pkg>/` | `ok` |
| <CLI invocation or other observation> | <exact output or effect> |

## Out of scope

- <Adjacent work an agent might build by accident> (owned by <WO id or milestone>)

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: <domain skills, or "none">.
Read: <docs/file.md sections that hold the facts this work order relies on>.

## Report

Write the full report to the report file named at the top, in this form:

```
Status: DONE | DONE_WITH_CONCERNS | BLOCKED | NEEDS_CONTEXT
Work order: <path>
Files changed:
- <path> (created | modified | deleted)
Commands run:
- `<command>` -> exit <n>
  <verbatim output: the last 30 lines, and the full output of anything that failed>
Decisions: <choices the work order left open and what was chosen, one line each, or "none">
Outside scope: <bugs, contradictions, missing interface methods or doc errors noticed outside the owned files and not acted on, or "none">
Concerns: <DONE_WITH_CONCERNS only: what is doubted and why>
Question or blocker: <NEEDS_CONTEXT or BLOCKED only: the exact question or blocker, what was tried, what would unblock it>
```

Then reply with only:

```
Status: <status>
Report: <report file path>
make check: exit <n> <and the failing package or test, if any>
<BLOCKED or NEEDS_CONTEXT: the question or blocker in full>
```

If a question's answer would change behaviour and nothing here or in the docs answers it, stop and report NEEDS_CONTEXT rather than guessing.

## Rulings

<The lead appends dated answers to questions here. They are part of the requirements.>

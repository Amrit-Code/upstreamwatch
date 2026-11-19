---
name: work-order
description: Turns a slice of an upstreamwatch milestone into a work order file that a subagent can execute cold, then dispatches it to the right agent and model. For the lead session only. Covers sizing a task, owned files, lead-owned interface files, exact signatures consumed and produced, acceptance commands, out of scope, skills to load, parallel waves, agent and model routing, the short dispatch message, and the report contract every executing agent follows. Triggers on planning a milestone, breaking work into tasks, writing a work order or brief, dispatching go-builder, go-mech or test-writer, deciding what can run in parallel, choosing a model for a subagent, or answering a NEEDS_CONTEXT question. Not for accepting a returned result (use review-gate), closing a milestone (use milestone-accept), or how the Go code itself is written (go-code, go-test and the domain skills).
---

# Work orders

A work order is the whole specification a subagent gets. The agent sees no conversation, no plan discussion and no earlier task; it sees the work-order file, the files it points at, and the skills its definition preloads. Everything the agent cannot decide alone goes in the file. Everything a capable Go engineer would decide the same way stays out.

Write work orders as `.claude/plans/m<n>/WO-<nn>-<slug>.md`, `nn` two digits in dispatch order within the milestone, `slug` a few lowercase words (`.claude/plans/m1/WO-03-timeline-diff.md`). Start from [templates/work-order.md](templates/work-order.md). Work orders and their reports are committed with the change they describe, and the repository is public, so neither may contain a token, a private repository name or a hostname.

## Who does what

| Role | May | May not |
|---|---|---|
| Lead session | plan; write interface files and work orders; assign migration filenames; edit `go.mod`/`go.sum`; dispatch agents; rerun checks; commit and merge to `main`; change milestone checklist status | hand its own judgement calls to an agent as if they were settled facts |
| go-builder, go-mech | edit and create files inside the work order's owned files; run any read-only command and `make check` | commit, push, merge, stash or otherwise write to git; spawn agents or forking skills; edit interface files, `go.mod`, `go.sum`, `docs/` or anything outside owned files |
| test-writer | the same, limited to `*_test.go`, `testdata/` and `internal/testutil/` when the work order lists it | edit production code, even to fix a bug it found |
| reviewer, security-reviewer | read, run read-only commands and focused tests | write anything |
| docs-keeper | edit under `docs/` only | edit code, plans or skills |

## Writing one

1. **Pick the slice.** Read the milestone in `mvp-plan/PLAN.md` section 10, its checklist in the milestone-accept skill, and the docs the slice touches (`docs/architecture.md` for which package owns what, `docs/interfaces.md` and `docs/data-access.md` for contracts, `docs/database.md` for tables). Size it by the rules below.
2. **Write the interface files first.** Any Go declaration that another package or a parallel task depends on (an `interfaces.go` holding outbound or store interfaces, an `internal/model` type, a migration file) is written by the lead, passes `make check`, and is committed before dispatch. These files, and `.go-arch-lint.yml`, are lead-owned for the life of the work order. An agent that finds one wrong stops and reports it; it does not edit it.
3. **Copy signatures exactly.** Under Interfaces, paste the Go declarations from the committed file with its path. Paraphrase ("a method that lists issues") forces the agent to guess names and types.
4. **State requirements as checkable behaviour,** one per line, with the exact values from PLAN.md or the docs (thresholds, header names, cron strings, column names). A requirement that cannot be checked by a test or a command ("handle errors properly") decides nothing; replace it or cut it.
5. **Name the tests the lead needs to see**: the cases that prove the requirements, with inputs and expected outputs where they are not obvious. Builders add a few sanity tests beyond these; full suites are a later test-writer work order.
6. **Write acceptance commands** run from the repository root, each with its expected result: always `make check` → exit 0, plus focused `go test -race -run` commands or CLI invocations with the output that means it worked. Before the M0 work order that creates the Makefile lands, list the equivalent commands (`gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test -race ./...`).
7. **Fence the scope.** Out of scope lists the adjacent things an agent might build by accident and which work order or milestone owns them.
8. **List skills and docs.** The agent's definition preloads its standing skills; name only the domain skills to load with the Skill tool (`sqlite-store`, `outbound-client`, `llm-triage`, `web-ui`, `upw-deploy`) and the specific docs sections to read.
9. **Self-check before dispatch.** Every name and type used in Requirements exists in Interfaces or in committed code. Every requirement has a test or an acceptance command. Owned files do not overlap any other open work order. Nothing in the file is a placeholder.

## Sizing

A work order is the smallest unit that carries its own tests and is worth reviewing on its own. Fold scaffolding, config keys and wiring into the work order whose deliverable needs them; split only where the lead could reject one part while accepting the other. Typical units: one domain package with its tests; one adapter with its fake-server tests; one store interface with its migration and methods; one page with its handler and golden test; one CLI command with its wiring. A work order that needs more than one design decision the lead has not made is too big or not ready.

## Parallel waves

Dispatch several work orders at once only when their owned files are disjoint, none consumes something another in the same wave produces, and every interface file they share is already committed. Shared chokepoints (`cmd/upw/main.go`, the router in `internal/web`, migration numbers, `go.mod`) belong to one work order per wave or to the lead; the lead assigns each migration its exact filename in the work order so two tasks never pick the same number.

Run each parallel agent with `isolation: "worktree"` so one agent's half-written package cannot break another's `make check`. Commit the work-order files to `main` and push them first: a worktree only sees committed files, and agent worktrees are created from `origin/main`, not the local branch (M1 wave 1 started one commit behind because the groundwork was committed but not pushed). The agent's result names its worktree; review-gate runs there, and the lead merges accepted worktrees into `main` one at a time, rerunning `make check` on `main` after each merge. A single sequential work order may run in the main working tree.

## Routing

| Work | Agent | Model |
|---|---|---|
| Design-heavy packages: store and migrations framework, timeline diffing and intervals, vulnerability matching and confidence, prefilter scoring, triage (cache key, budget, evidence check), findings state machine, `pipeline.Runner`, `httpx` | `go-builder` | `opus` |
| Fully specified work: adapters to a documented API, notifiers, CLI wiring, templates and handlers whose behaviour the work order spells out, config keys, migrations written out in the work order | `go-mech` | `sonnet` |
| Full test suites, fixture git repos, golden files, fake servers for a merged package | `test-writer` | `sonnet` |
| Correctness review of a design-heavy result | `reviewer` | `sonnet` |
| Milestone security review | `security-reviewer` | `sonnet` |
| Docs upkeep | `docs-keeper`, through the docs-sync skill | `sonnet` |

Pass `model` explicitly on every Agent call, matching the table, because an omitted model can fall back to the lead's own model and silently multiply the cost. When a `go-mech` work order comes back BLOCKED because it needs judgement rather than information, re-dispatch the same file to `go-builder` with `model: "opus"` instead of retrying the same agent unchanged.

## Dispatch

The dispatch message stays short and points at the file, so the work order remains the single source of requirements:

```
Execute the work order at .claude/plans/m1/WO-03-timeline-diff.md. Read it first; it is your full specification. Write your report to the report file it names and reply in the short form.
```

Answers to questions go into the work-order file under Rulings, dated, and the agent is resumed with SendMessage pointing at them. Do not answer only in the message: the file is what the reviewer and any later fix round read.

## Report contract

Every executing agent (`go-builder`, `go-mech`, `test-writer`) writes its full report to `.claude/plans/m<n>/WO-<nn>-<slug>.report.md` and replies with the short form. The same contract is in each agent definition and in the template; change all of them together.

Report file:

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

Short reply:

```
Status: <status>
Report: <report file path>
make check: exit <n> <and the failing package or test, if any>
<BLOCKED or NEEDS_CONTEXT: the question or blocker in full>
```

Status meanings:

- **DONE**: every requirement met, `make check` exit 0, every acceptance command gave its expected result.
- **DONE_WITH_CONCERNS**: the same, and something still looks wrong: a correctness doubt, a requirement that seems mistaken, a file growing past its purpose. For `test-writer`, also the status when a kept test fails because it exposed a production bug.
- **BLOCKED**: the work cannot be finished inside the owned files: a check fails for a reason outside them, an interface file is wrong or missing a method, a module is not in `go.mod`, or the task is larger than the work order says.
- **NEEDS_CONTEXT**: the work order, docs and skills do not answer a question whose answer changes behaviour.

Stopping to ask is an acceptable outcome; guessing is not, because a guess that compiles looks exactly like a decision and reaches `main` unnoticed. Fix rounds append a `## Fix round <r>` section to the same report file with the same fields.

## Not covered here

Accepting or rejecting a returned result and the fix loop: review-gate. Closing a milestone: milestone-accept. Keeping `docs/` current: docs-sync. Layering and package ownership: upw-architecture. How code and tests are written: go-code, go-test.

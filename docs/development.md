# Development

This document describes how upstreamwatch is built: where builds run, which agents do which work, which skills they load, the check command, and the git and review rules. Facts about the system itself live in the other docs; the skills point at them instead of repeating them. See the [index](README.md).

Milestones M0 to M5 and their acceptance criteria live in `mvp-plan/PLAN.md`, section 10. Read that section for what to build next and when a milestone is done: [mvp-plan/PLAN.md](../mvp-plan/PLAN.md).

## Where builds run

Builds and tests run on the Mac with Go 1.27.x installed through Homebrew. The homelab VM is for deployment only. This replaces the dev loop in plan section 8, which put the Go toolchain on the VM. The `upw-deploy` skill covers deployment, and the owner's `homelab` skill stays the authority for the VM itself.

## Model routing

The lead session orchestrates. It plans, writes the interface files and work orders, reruns checks, merges and reports to the owner. Other work is routed by how much design it needs:

- Opus builds design-heavy packages: timeline, vuln matching, prefilter, triage, store.
- Sonnet does fully specified work (adapters, notifiers, CLI wiring, templates), writes tests, reviews diffs, runs the security review, and keeps the docs current.

Reviewers and checkers use Sonnet rather than Opus. A reviewer reads a diff against a written brief, which is cheaper work than design.

## Agents

Agent definitions are in `.claude/agents/`.

| Agent | Model | Does | Cannot |
|---|---|---|---|
| `go-builder` | Opus | Builds design-heavy packages | Change interface files or another agent's package |
| `go-mech` | Sonnet | Builds fully specified work: adapters, notifiers, CLI wiring, templates | Change interface files or another agent's package |
| `test-writer` | Sonnet | Writes full test suites, fixtures and golden files | Edit production code; it reports bugs instead |
| `reviewer` | Sonnet | Reads a diff against its work order and reports only what affects correctness | Write anything (read-only) |
| `security-reviewer` | Sonnet | Runs the `security-check` skill at the end of each milestone | Write anything (read-only) |
| `docs-keeper` | Sonnet | Updates `docs/` to match the code, using the `docs-sync` skill | Edit anything outside `docs/` |

Interface files are written first and owned by the lead session. Builders work in parallel only on packages that share no files.

## Skills

Skills are in `.claude/skills/<name>/SKILL.md`. Each tells an agent how to do a kind of work and points at `docs/` for facts.

| Skill | What it covers |
|---|---|
| `upw-architecture` | The layers, which package owns what, and the import rules; points at [architecture.md](architecture.md), [interfaces.md](interfaces.md) and [data-access.md](data-access.md). Every builder loads it. |
| `go-code` | Go style for this repo: errors, context, slog, constructors, naming, no placeholders. |
| `go-test` | Table tests, hand-written fakes, golden files, fake HTTP servers for each outside API, fixture git repos, the two referee tests. |
| `sqlite-store` | How to add a table, a migration and a store method; transactions; single-writer rules; pragmas. |
| `outbound-client` | How to write an adapter on `httpx`: timeouts, one retry layer, ETags, GitHub rate-limit headers, the private-address check, safe `git` invocation. |
| `llm-triage` | Provider contract, prompt versioning, cache key, cost cap, treating issue text as data, testing with a fake provider. |
| `web-ui` | `html/template` layout, forms, escaping rules, same-origin check, golden tests of rendered pages. |
| `upw-deploy` | Image build, compose, `tailscale serve` and the promotion checklist, on top of the `homelab` skill. |
| `security-check` | Read-only review with file and line, the path from input to sink, and a fix. |
| `work-order` | How the lead briefs a builder: objective, owned files, interfaces consumed and produced, acceptance commands, out of scope, report format. |
| `review-gate` | How a result is accepted: the lead reruns the checks, a fresh reviewer reads the diff against the brief, and fixes are capped at three rounds. |
| `milestone-accept` | One checklist per milestone with the exact commands and expected results from the plan. Only the lead marks an item passed. |
| `docs-sync` | How to bring `docs/` back in line with the code and update the "Last synced" line in [README.md](README.md). |

## make check

One command, `make check`, runs gofmt, `go vet`, build, `go test -race`, and the layering lint. It is the same command for agents, for the lead session and for CI. A task is not done until it passes.

## Git policy

The project has two parties: the owner and the lead session. The lead session commits and merges straight to `main` for now. Subagents never commit, push or merge; they leave their changes in the working tree and report. The repository is public from the first commit, so nothing private goes into a commit: no private repository names, hostnames, tailnet names or tokens. The module path is `github.com/Amrit-Code/upstreamwatch`.

## Review weight

- Every task: `make check`.
- Design-heavy tasks: also a fresh `reviewer` agent that reads the diff against the work order.
- Once per milestone: a security review by `security-reviewer`.

## Dependencies

The libraries named in the plan are approved. The CLI uses the standard library `flag`, with no Cobra or Viper. Anything else needs the owner's yes. Tests use no frameworks such as testify; fakes are written by hand. See [external-sources.md](external-sources.md) for the library list.

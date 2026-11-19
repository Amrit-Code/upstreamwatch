# upstreamwatch

A single Go binary (`upw`) that mines a git repo's history into a dependency timeline, matches advisories, watches upstream issue trackers, and sends a ranked daily digest. Public repository: nothing private goes in a committed file.

This file holds only the working model and the rules that are easy to break. The detail lives elsewhere.

## Where the detail lives

| Question | Where |
|---|---|
| Scope, pipeline, milestones, acceptance | `mvp-plan/PLAN.md` |
| Layers, packages, entry points, request traces | `docs/architecture.md` |
| Interfaces and their adapters | `docs/interfaces.md` |
| Database access and tables | `docs/data-access.md`, `docs/database.md` |
| Decisions and corrections to the plan | `docs/decisions.md` |
| How the build is run | `docs/development.md` |
| How to do a kind of work | the skill for it under `.claude/skills/` |

## Working model

- The lead session plans, writes interface files and work orders (`work-order`), accepts results (`review-gate`), closes milestones (`milestone-accept`), and commits straight to `main`.
- Implementation runs in subagents: `go-builder` (Opus) for design-heavy packages, `go-mech` (Sonnet) for fully specified work, `test-writer` (Sonnet) for test suites. `reviewer` and `security-reviewer` (Sonnet) are read-only. Always pass the model explicitly.
- Subagents never commit, push, merge, spawn agents, or edit files outside their work order.
- Briefs are self-contained; a subagent sees only its work order and the skills it loads.

## Rules that are easy to break

- `make check` is the only evidence that something works. Rerun it; do not accept a report that it passed.
- Only `internal/store` runs SQL. Adapters never import each other or the store. Interfaces are declared in the package that uses them.
- After a change to packages, interfaces, schema, routes, CLI commands, schedules, config or external sources, run `docs-sync` so `docs/` stays true.
- A new dependency needs the owner's approval. The approved list is in `go-code`.
- `mvp-plan/TARGETS.local.md` is private and gitignored. Never copy from it into a committed file.
- Builds and tests run on the Mac. The homelab VM is for the deployed instance only.

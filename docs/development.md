# Development

This document describes how upstreamwatch is built: where builds run, which agents do which work, which skills they load, the check command, and the git and review rules. Facts about the system itself live in the other docs; the skills point at them instead of repeating them. See the [index](README.md).

Milestones M0 to M5 and their acceptance criteria live in `mvp-plan/PLAN.md`, section 10. Read that section for what to build next and when a milestone is done: [mvp-plan/PLAN.md](../mvp-plan/PLAN.md).

## Where builds run

Builds and tests run on the Mac with Go 1.27.x installed through Homebrew. The homelab VM is for deployment only. This replaces the dev loop in plan section 8, which put the Go toolchain on the VM. The `upw-deploy` skill covers deployment, and the owner's `homelab` skill stays the authority for the VM itself. By the owner's ruling of 2026-11-19, the Mac runs only `make check` (unit tests and lint); running the server, health checks and container checks happen on the homelab VM.

## Model routing

The lead session orchestrates. It plans, writes the interface files and work orders, reruns checks, merges and reports to the owner. Other work is routed by how much design it needs:

- Opus builds design-heavy packages: timeline, vuln matching, prefilter, triage, store.
- Sonnet does fully specified work (adapters, notifiers, CLI wiring, templates), writes tests, reviews diffs, runs the security review, and keeps the docs current.

Since the build start (see [decisions.md](decisions.md)), all code and tests are written by Opus agents: `go-builder`, `go-mech` and `test-writer` are each dispatched with `model: "opus"`, which overrides the default in their definitions. Sonnet remains for docs upkeep and the milestone security review.

Reviewers and checkers use a lower tier than the builders. A reviewer reads a diff against a written brief, which is cheaper work than design.

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

The Model column is the default in each agent's definition. The lead passes the model explicitly at dispatch, and the three builders run on Opus (see Model routing).

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

One command, `make check`, runs six steps in order: gofmt, `go vet`, build, `go test -race`, the layering lint and the standard-library import check. It is the same command for agents, for the lead session and for CI. A task is not done until it passes.

- The layering lint is `go-arch-lint`, run with `go run <module>@<version>` (v1.19.2) so it never enters `go.mod`; its rules are in `.go-arch-lint.yml`. Its deep scan is off (`deepScan: false`), because the scan treats `cmd/upw` passing `*store.DB` into a constructor that takes a consumer-declared interface as the store depending on that consumer, which is how the services are wired. The import rules are still checked in full.
- The import check is `scripts/check-imports.sh`. It uses `go list` to fail when `database/sql` is imported outside `internal/store`, or `os/exec` outside `internal/gitsrc` and `internal/testutil`. It looks at non-test imports only.

## make e2e

`make e2e` is separate from `make check`: it needs the network and `git-pkgs` on `PATH`, so it is not part of the acceptance command. It runs `go test -tags e2e -race -count=1 -run TestParity -v -timeout 20m ./cmd/upw/`, the referee test of `cmd/upw/parity_e2e_test.go`, which compares the timelines of two pinned public repositories with `git pkgs history` (see [external-sources.md](external-sources.md)). `upw.dev.yaml` lists four public repositories as fixtures. It is a test run with no server or container, so it runs on the Mac (with `git-pkgs` from Homebrew) as well as in CI; the owner's ruling keeps servers and containers on the VM.

## CI and the image

`.github/workflows/ci.yml` runs on pushes to `main`, on `v*` tags and on pull requests. The `check` job runs `make check` with the Go version from `go.mod`, then `govulncheck` v1.9.0 over `./...` (`go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...`). The `e2e` job runs only on pushes to `main`: it installs `git-pkgs` v0.22.0 from the project's release archive, verifies its SHA-256 checksum, and runs `make e2e` (30-minute job limit). The `image` job builds `deploy/Dockerfile` for `linux/amd64` after `check` passes and, only on a `v*` tag, pushes it to the GitHub container registry as the tag and `latest`. The image is Alpine with `git`, `ca-certificates` and `tzdata`, runs as a non-root user, and starts `upw serve -config /etc/upstreamwatch/upw.yaml`. `deploy/compose.yml` publishes port 8080 on the host's loopback only. See the `upw-deploy` skill for deployment.

## Git policy

The project has two parties: the owner and the lead session. The lead session commits and merges straight to `main` for now. Subagents never commit, push or merge; they leave their changes in the working tree and report. The repository is public from the first commit, so nothing private goes into a commit: no private repository names, hostnames, tailnet names or tokens. The module path is `github.com/Amrit-Code/upstreamwatch`.

## Review weight

- Every task: `make check`.
- Every work order's diff is inspected by GPT-6 Astra (Codex CLI, read-only, through the claudex-loop runner's `inspect` mode) before commit. Astra never edits; findings go back to the building agent. This holds while Codex weekly usage is below 80%; at or above 80% the Sonnet `reviewer` agent takes over, as described in the `review-gate` skill.
- Design-heavy tasks: also a fresh `reviewer` agent that reads the diff against the work order.
- Once per milestone: a security review by `security-reviewer`.

## Dependencies

The libraries named in the plan are approved, and so is `go.yaml.in/yaml/v3` (pinned at v3.0.6) for the config loader. The CLI uses the standard library `flag`, with no Cobra or Viper. Anything else needs the owner's yes. Tests use no frameworks such as testify; fakes are written by hand. See [external-sources.md](external-sources.md) for the library list.

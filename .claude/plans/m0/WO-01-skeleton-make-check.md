# WO-01: Repository skeleton and `make check` with the layering lint

Milestone: M0
Agent: go-builder   Model: opus
Depends on: none (baseline commit holds docs, skills, `go.mod`, `.gitignore`)
Report file: `.claude/plans/m0/WO-01-skeleton-make-check.report.md`
Tickets: UPW-10, UPW-11

## Objective

After this work order the repository builds, has the M0 package directories, an Apache-2.0 license, and one command, `make check`, that runs gofmt, vet, build, race tests, the go-arch-lint layering check and the standard-library import check. Every later work order is accepted by that command, so it has to fail when a layering rule is broken.

## Owned files

You may create and edit only these paths. Everything else is read-only.

- `LICENSE`
- `Makefile`
- `.go-arch-lint.yml` (the lead normally owns this file; for this work order only, you write it)
- `scripts/check-imports.sh`
- `cmd/upw/main.go`, `cmd/upw/main_test.go`
- `internal/config/doc.go`
- `internal/model/doc.go`
- `internal/store/doc.go`
- `internal/pipeline/doc.go`
- `internal/sched/doc.go`
- `internal/web/doc.go`

Lead-owned, read-only for you (stop and report if one is wrong):

- `go.mod` (module `github.com/Amrit-Code/upstreamwatch`, `go 1.27`; no dependencies are needed for this work order)
- `.gitignore`
- everything under `docs/`, `.claude/`, `mvp-plan/`

## Interfaces

### Consumed

None.

### Produced

```go
// cmd/upw/main.go
func main()
// run is the testable entry point. It returns the process exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int
```

`Makefile` targets other work orders and CI will call: `check`, `build` (writes `bin/upw`), `fmt`.

## Requirements

1. `LICENSE` is the unmodified Apache License 2.0 text.
2. Each of `internal/config`, `internal/model`, `internal/store`, `internal/pipeline`, `internal/sched`, `internal/web` contains exactly one file, `doc.go`, holding a package doc comment (one or two sentences taken from that package's row in the Packages table of `docs/architecture.md`) and the `package` clause. No other declarations.
3. `cmd/upw/main.go` follows the scaffold in the go-code skill's `templates/cmd-upw.md`: `main` builds the signal context with `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`, calls `run`, and calls `os.Exit` once. `run` dispatches on `args[0]`. No subcommand exists yet, so the behaviour is:
   - no arguments: usage on stderr, return 2;
   - `-h`, `--help` or `help`: usage on stdout, return 0;
   - anything else: `upw: unknown command "<name>"` and the usage on stderr, return 2.
   The usage text names the binary and says that commands are added per milestone; it lists no command that does not exist.
4. `make check` runs these steps in this order and stops at the first failure, printing a line naming each step before it runs (`==> gofmt`, `==> vet`, `==> build`, `==> test`, `==> arch-lint`, `==> import-check`):
   1. gofmt: fails if `gofmt -l .` prints anything, and prints the offending files;
   2. `go vet ./...`;
   3. `go build ./...`;
   4. `go test -race ./...`;
   5. `go run github.com/fe3dback/go-arch-lint@v1.19.2 check` (run with `go run <module>@<version>` so the tool never enters `go.mod`; this is recorded in `docs/decisions.md`);
   6. `scripts/check-imports.sh`.
5. `make build` writes `bin/upw` from `./cmd/upw`. `make fmt` runs `gofmt -w .`.
6. `scripts/check-imports.sh` is a POSIX `sh` script that exits non-zero, naming the package and the import, when any non-test package other than `internal/store` imports `database/sql` (or a subpackage of it), or any non-test package other than `internal/gitsrc` or one under `internal/testutil` imports `os/exec`. The sketch is in `.claude/skills/upw-architecture/references/ARCH-LINT.md`. It also exits non-zero if `go list` itself fails.
7. `.go-arch-lint.yml` implements the import rules in the upw-architecture skill ("Import rules" table) and the component table in `references/ARCH-LINT.md`, verified against go-arch-lint v1.19.2's real behaviour, not only the sketch:
   - every service and every adapter is its own component, so two services can never import each other;
   - `_test.go` files are excluded;
   - vendors are restricted (`depOnAnyVendor: false`); `internal/config` may use the vendor `go.yaml.in/yaml/v3`; `internal/store` may use `modernc.org/sqlite`; `internal/sched` may use `github.com/robfig/cron/v3`; the rest as in the reference;
   - components for packages that do not exist yet (all the M1 to M5 packages in the reference) are included if the tool accepts component paths with no matching directory; if it rejects them, include only the six `internal/` packages above plus `cmd/upw`, and say so under Decisions with the tool's error text.
8. Confirm the four "Behaviours to confirm at build time" in `references/ARCH-LINT.md` (same-component imports, glob semantics, `excludeFiles` for tests, how the tool is run) by experiment and record each result in one line under Decisions.
9. The lint bites. Each of these throwaway experiments makes `make check` fail at the step named, and `make check` passes again once the file is removed. Run them, record the failing output in the report, and leave no throwaway file behind:
   - `internal/model/tmp.go` importing `database/sql` → fails at `import-check`, naming `internal/model` and `database/sql`;
   - `internal/model/tmp.go` importing `github.com/Amrit-Code/upstreamwatch/internal/store` → fails at `arch-lint`;
   - `internal/pipeline/tmp.go` importing `github.com/Amrit-Code/upstreamwatch/internal/sched` → fails at `arch-lint`;
   - `internal/web/tmp.go` importing `os/exec` → fails at `import-check`.
10. `make check` works from a clean clone with only Go 1.27 and network access to the Go module proxy: no globally installed tool is assumed.

## Tests to include

In `cmd/upw/main_test.go`, table-driven over `run`:

- `TestRunNoArgs`: `run(ctx, nil, ...)` → 2, usage on stderr, stdout empty
- `TestRunHelp`: `-h`, `--help`, `help` → 0, usage on stdout, stderr empty
- `TestRunUnknownCommand`: `bogus` → 2, stderr contains `unknown command "bogus"`

## Acceptance

Run from the repository root. Each command must give the stated result.

| Command | Expected |
|---|---|
| `make check` | exit 0; the six `==>` lines appear in order |
| `go list ./...` | lists `cmd/upw` and the six `internal/` packages |
| `make build && ./bin/upw -h; echo $?` | usage text, then `0` |
| `./bin/upw; echo $?` | usage on stderr, then `2` |
| `head -3 go.mod` | `module github.com/Amrit-Code/upstreamwatch` and `go 1.27`, unchanged |
| the four experiments in requirement 9 | each fails at the named step; clean tree passes |
| `git status --porcelain` | only the owned files and the report file |

## Out of scope

- Config loading, the store, the Runner, the scheduler, the web server, `upw serve`, `upw doctor` (WO-03 onward).
- `internal/model` types and any `interfaces.go` (WO-02).
- `README.md`, `upw.example.yaml`, Dockerfile, compose, CI workflow (later M0 work orders).
- Adding any module to `go.mod`.
- `staticcheck` in `make check` (not part of the agreed check command).

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: none.
Read: `.claude/skills/upw-architecture/references/ARCH-LINT.md` (whole file); `.claude/skills/go-code/templates/cmd-upw.md`; `docs/architecture.md` section "Packages"; `docs/decisions.md` section "Corrections to PLAN.md" (last bullet).

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

If a question's answer would change behaviour and nothing here or in the docs answers it, stop and report NEEDS_CONTEXT rather than guessing. Do not commit, stash or otherwise write to git.

## Rulings

- 2026-11-19: The owner approved `go.yaml.in/yaml/v3` as the YAML library; use that path for the `config` vendor entry.

# WO-14: Timeline, CLI and config hardening from the M1 security review

Milestone: M1
Agent: go-builder   Model: opus
Depends on: everything through WO-12 (merged)
Report file: `.claude/plans/m1/WO-14-timeline-cli-config-hardening.report.md`
Ticket: UPW-40 (M1 security review)

## Objective

Close the remaining findings of the M1 security review: credentials embedded in manifest dependency strings must never be stored or shown; one target's history must have bounded memory use; `upw timeline` must not write terminal control sequences from repository data; a GitHub token must only ever be sent to GitHub. Also add `govulncheck` to CI (PLAN.md section 11).

## Owned files

- `internal/timeline/` (all files except `interfaces.go`)
- `internal/config/` (validation of target auth)
- `cmd/upw/timeline.go`, `cmd/upw/timeline_test.go`
- `.github/workflows/ci.yml` (one govulncheck step)

Read-only for you: `internal/gitsrc/` (WO-13 runs in parallel), `internal/extract/`, `internal/model/`, every other file.

## Findings to fix

- **[medium] Credential in a PEP 508 direct reference stored as the package name**: `pyproject.toml` `dependencies = ["pkg @ git+https://x-access-token:<token>@github.com/org/repo.git"]` reaches the library's `parsePEP508`, which returns the whole string as the name; it lands in `packages`, the UI, the CLI and later outbound requests. `model.Redact` is never applied to extracted names, versions or requirements.
- **[medium] No memory bounds** (part owned here): `internal/timeline/service.go` reads blobs in chunks of 500 refs (500 × 5 MiB), keeps unbounded dependencies per file and events per run in memory.
- **[low] Terminal escape injection** in `cmd/upw/timeline.go` text output: manifest paths and package URLs are printed raw.
- **[low] Token sent to any host**: `auth: targets` or `auth: upstream` sends the token to whatever host the target URL names.

## Requirements

1. **Dependency sanitising, in the timeline service, applied to every extractor's output** (so both extractors are covered in one place): drop a dependency, log it at debug with the manifest path only (never the dropped string), and count it in a new stats key `deps_dropped`, when its `Package.Name` or `Package.PURL` contains any control character (below 0x20 or 0x7f), any whitespace, the substring `://`, or `@`. Then pass `Version` and `Requirement` of every kept dependency through `model.Redact`.
2. **Dependencies per file**: a parsed file with more than 20,000 dependencies is treated as a parse error (warn, `parse_errors`, previous state kept).
3. **Blob chunking**: read blobs in chunks of at most 32 refs (with gitsrc's new 1 MiB default cap from WO-13, at most 32 MiB of blob data at once).
4. **Events per target run**: if a target's run would produce more than 500,000 new events, fail that target with an error naming the limit before calling `ApplyTimeline` (the other targets continue, as for any target failure).
5. **CLI text output**: in `upw timeline` text mode, any field containing a control character (below 0x20 or 0x7f) is printed as its `strconv.Quote` form. JSON output is unchanged.
6. **Token host restriction (config validation)**: a target with `auth: targets` or `auth: upstream` whose URL host is not `github.com` is rejected by config validation with an error naming `targets[i].auth` and the host. `auth: none` is allowed for any host.
7. **CI**: in the `check` job of `.github/workflows/ci.yml`, add a step after `make check` that runs `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...`; a reported vulnerability fails the job.

## Tests to include

- `TestRun_DropsUnsafeDependencies`: a fake extractor returning names `pkg @ git+https://x-access-token:SECRET@github.com/o/r.git`, `a\x1b[2Jb`, `ok-name` and a requirement `https://user:SECRET@host/x` → only `ok-name` is kept, its requirement is redacted, `deps_dropped` is 2, and `SECRET` appears nowhere in the update passed to `ApplyTimeline` or in the captured logs.
- `TestRun_TooManyDependencies` (20,001 → parse error, state kept).
- `TestRun_ReadsBlobsInChunks` updated for 32.
- `TestRun_EventCap` (use a small injectable limit via an unexported test hook or option, documented, defaulting to 500,000).
- `TestTimeline_TextEscapesControlChars` in `cmd/upw`.
- Config: `auth: targets` with `https://gitlab.com/x/y` → error; with `https://github.com/x/y` → ok; `auth: none` with any https host → ok.
- An integration case in `internal/timeline/integration_test.go`: a fixture `pyproject.toml` with a direct-reference dependency carrying `user:SECRET@` → after ingest and timeline, `SECRET` is in no stored row (query packages, dep_events and dep_versions).

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -count=1 ./internal/timeline/ ./internal/config/ ./cmd/upw/` | `ok` |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- gitsrc memory and clone-cap changes (WO-13).
- Changing the extractors themselves.

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `go-test`.
Read: `internal/timeline/service.go`, `cmd/upw/timeline.go`, `internal/config/validate.go`, `internal/model/redact.go`, `.github/workflows/ci.yml`, `docs/decisions.md` ("Lead decisions for M1").

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

2026-11-19: `cmd/upw/runjob_test.go` is added to the owned files for the timeline stats expected-output line only (insert `deps_dropped=0` in sorted position). Keep `deps_dropped` emitted when zero, like every other key.

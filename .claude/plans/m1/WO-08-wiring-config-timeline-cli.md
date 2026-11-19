# WO-08: Config keys, job wiring, the timeline query and `upw timeline`

Milestone: M1
Agent: go-builder   Model: opus
Depends on: WO-01 to WO-07 (merged)
Report file: `.claude/plans/m1/WO-08-wiring-config-timeline-cli.report.md`
Ticket: UPW-35 (and the wiring parts of UPW-29 and UPW-34)

## Objective

`upw run ingest` and `upw run timeline` work against real targets, the ingest schedule runs both, the new config keys exist, and `upw timeline <slug> [package]` prints a repo's intervals through a new read method on `query.Service`. Serves M1 checklist items 2 to 10 and 13.

## Owned files

- `internal/config/` (all files)
- `internal/query/service.go`, `internal/query/timeline.go` (new), `internal/query/*_test.go`
- `cmd/upw/` (all files except `doctor.go`, which needs no change, and `parity_e2e_test.go` and `testdata/parity/`, which WO-10 creates in parallel)
- `deploy/upw.example.yaml`, `upw.dev.yaml`

Not owned: `Makefile` and `.github/workflows/ci.yml` (WO-10).

Lead-owned, read-only for you: `internal/query/interfaces.go`, `internal/model/`, the interfaces of `internal/ingest` and `internal/timeline`, every other package.

## Interfaces

### Consumed

```go
// internal/query/interfaces.go (new, lead-owned)
type TimelineStore interface {
	GetRepo(ctx context.Context, slug string) (model.TargetRepo, error)
	ListRepos(ctx context.Context) ([]model.TargetRepo, error)
	ListIntervals(ctx context.Context, repoID int64) ([]model.DepInterval, error)
}

// Merged in wave 1; read their files for the exact Options types:
func gitsrc.New(opts gitsrc.Options, logger *slog.Logger) *gitsrc.CLI
func extract.New(opts extract.Options, logger *slog.Logger) *extract.Extractor
func unity.New() *unity.Extractor
func ingest.NewService(git ingest.GitSource, repos ingest.RepoStore, opts ingest.Options, logger *slog.Logger) *ingest.Service
func timeline.NewService(git timeline.GitSource, repos timeline.RepoStore, store timeline.TimelineStore, extractors []timeline.Extractor, unity timeline.Extractor, opts timeline.Options, logger *slog.Logger) *timeline.Service
```

### Produced

```go
// internal/query
// TimelineFilter narrows a repo's intervals. Zero fields do not filter.
type TimelineFilter struct {
	Package  string    // exact version-less package URL
	Contains string    // case-insensitive substring of the package URL
	On       time.Time // only intervals in force at this instant (DepInterval.Contains)
}

func NewService(runs RunStore, findings FindingStore, timeline TimelineStore, logger *slog.Logger) *Service
// Timeline returns the repo and its intervals that pass f, in store order.
// An unknown slug is an error wrapping model.ErrNotFound.
func (s *Service) Timeline(ctx context.Context, slug string, f TimelineFilter) (model.TargetRepo, []model.DepInterval, error)
// Repos returns every stored repo ordered by slug.
func (s *Service) Repos(ctx context.Context) ([]model.TargetRepo, error)

// internal/config (added)
type Target struct {
	// ... existing fields, plus:
	AllowPrivateHosts bool // exempts this target's host from the private-address check
}
type Git struct {
	MaxCloneMB   int
	CloneTimeout time.Duration
	FetchTimeout time.Duration
}
// Config gains a Git field.
```

## Requirements

Config

1. New per-target key `targets[].allow_private_hosts` (bool, default false) → `Target.AllowPrivateHosts`.
2. New section `git:` with `max_clone_mb` (int, default 2048, must be at least 1), `clone_timeout` and `fetch_timeout` (Go duration strings, defaults `30m` and `10m`, must parse and be positive). Validation errors name the field, in the existing style.
3. `deploy/upw.example.yaml` documents both, in the style of its other keys. `upw.dev.yaml` gets four public targets, all `auth: none`: `glow` (`https://github.com/charmbracelet/glow`), `cleo` (`https://github.com/python-poetry/cleo`), `naughtyattributes` (`https://github.com/dbrizov/NaughtyAttributes`, `unity: true`) and `upstreamwatch` (`https://github.com/Amrit-Code/upstreamwatch`).

Wiring (`cmd/upw`)

4. `registeredJobs` (or what replaces it) builds, in this order, the ingest job and the timeline job from the config and the store. The schedule chain for `ingest` then runs ingest, timeline (`scheduleEntries` already filters by registration).
5. One `gitsrc.CLI` shared by both jobs, built by a function `gitOptions(cfg config.Config) gitsrc.Options` that returns `ReposDir` `<data_dir>/repos`, `HomeDir` `<data_dir>/git-home`, `MaxCloneBytes` `max_clone_mb` MiB, the two timeouts, `AllowFileURLs` false, `LookupHost` nil. A test asserts these exact values, in particular `AllowFileURLs == false`.
6. Ingest targets: one `model.TargetRepo` per config target with `Slug`, `URL`, `Branch`, `AllowPrivateHost` from `AllowPrivateHosts`, and `Token` = `auth.github_targets_token` for `auth: targets`, `auth.github_upstream_token` for `auth: upstream`, zero for `auth: none`. A test covers the three mappings.
7. Timeline: `TargetSettings` from each target's slug, `unity` and `ecosystems`; extractors `[]timeline.Extractor{extract.New(extract.Options{DeriveToolchainFromImages: cfg.Vulns.DeriveToolchainFromImages}, logger)}` and `unity.New()`.
8. `serve.go` passes the store as the query service's `TimelineStore`.

Query

9. `Timeline`: `GetRepo(slug)` (not found → wrapped `model.ErrNotFound`), `ListIntervals(repo.ID)`, then keep intervals passing every non-zero filter field. `Repos`: `ListRepos`.

CLI `upw timeline`

10. `upw timeline <slug> [package-url] [-on YYYY-MM-DD] [-json] [-config path] [-log-level level]`. The positional package URL sets `TimelineFilter.Package`. `-on` sets `On` to the last instant of that UTC day (23:59:59.999999999); an unparseable date is a usage error (exit 2). Listed in `upw help`.
11. It loads the config and opens and migrates the store like `upw run`, but builds no runner and starts nothing.
12. Text output: a header line, then one line per interval with columns `PACKAGE VERSION FROM TO DIRECT MANIFEST`, aligned with `text/tabwriter`. `PACKAGE` is the package URL; `VERSION` the version, or `(<requirement>)` when the version is empty; `FROM` and `TO` are UTC dates `2006-01-02`, `TO` is `now` for an open interval; `DIRECT` is `yes`, `no` or `?`. No intervals → print `no intervals` to stdout and exit 0.
13. `-json`: a JSON array (`[]` when empty) of objects with keys `package`, `ecosystem`, `name`, `version`, `requirement`, `direct` (`true`, `false` or `null`), `manifest`, `from` and `to` (RFC 3339 UTC, `to` `null` when open), `from_sha`, `to_sha` (`null` when open). Indented with two spaces.
14. Unknown slug → `upw: unknown repo "<slug>"` on stderr, exit 1.

## Tests to include

- Config: defaults for the `git` section; each validation error; `allow_private_hosts` parsed.
- `TestGitOptions_Strict` (requirement 5), `TestIngestTargets_Tokens` (requirement 6), a test that the registered job names are `ingest`, `timeline` in that order.
- Query: `TestService_Timeline` with a fake store: each filter field alone and combined; unknown slug.
- CLI: `TestTimeline_Text` and `TestTimeline_JSON` against a temp data directory whose store you seed with `ApplyTimeline` (an open and a closed interval, a requirement-only interval, all three directness values) → exact expected output; `TestTimeline_UnknownRepo`; `TestTimeline_BadDate`.
- Existing `cmd/upw` tests still pass, updated only where the registered jobs legitimately change their expectations.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go build -o bin/upw ./cmd/upw && bin/upw run ingest -config upw.dev.yaml -repo upstreamwatch && bin/upw run timeline -config upw.dev.yaml -repo upstreamwatch && bin/upw timeline upstreamwatch -config upw.dev.yaml` | both runs succeed; the table lists the Go modules of `go.mod`, `pkg:golang/toolchain`, `pkg:docker/golang` and `pkg:docker/alpine` from `deploy/Dockerfile`, and the Actions used in `.github/workflows/` |
| `bin/upw run timeline -config upw.dev.yaml -repo upstreamwatch` (second time) | stats show `commits_walked=0` and `events_written=0` |
| `git status --porcelain` | only owned files and the report file (`data-dev/` is gitignored) |

## Out of scope

- The `/repos/{slug}` web page (WO-09).
- The git-pkgs parity test (WO-10).
- Docs under `docs/` (the lead runs docs-sync).

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `go-test`.
Read: `docs/architecture.md` ("CLI" and "Schedules"), `docs/decisions.md` ("Lead decisions for M1"), PLAN.md section 7 (CLI surface), `cmd/upw/runjob.go` and `cmd/upw/app.go`.

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

None.

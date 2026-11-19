# WO-09: `/repos` and `/repos/{slug}` timeline pages

Milestone: M1
Agent: go-mech   Model: opus
Depends on: WO-08 (merged)
Report file: `.claude/plans/m1/WO-09-repos-timeline-page.report.md`
Ticket: UPW-36

## Objective

The web UI shows each target's dependency timeline: a list of repos at `GET /repos`, and at `GET /repos/{slug}` an interval table with a filter box, a per-package history view and a "pinned on date" selector, all server-rendered with no JavaScript. Serves M1 checklist item 12.

## Owned files

- `internal/web/` (all files except `interfaces.go`), including `templates/repos.html`, `templates/timeline.html` (new), `testdata/*.golden`
- `cmd/upw/serve.go`: only the `web.NewServer` call, to pass the query service for the new interface

Lead-owned, read-only for you: `internal/web/interfaces.go`, `internal/query/`, `internal/model/`.

## Interfaces

### Consumed

```go
// internal/web/interfaces.go (lead adds before dispatch)
// TimelineReader supplies the repo list and timeline pages.
type TimelineReader interface {
	// Repos returns every stored repo ordered by slug.
	Repos(ctx context.Context) ([]model.TargetRepo, error)
	// Timeline returns the repo and its intervals that pass f. An unknown
	// slug is an error wrapping model.ErrNotFound.
	Timeline(ctx context.Context, slug string, f query.TimelineFilter) (model.TargetRepo, []model.DepInterval, error)
}

// internal/query (merged in WO-08)
type TimelineFilter struct {
	Package  string    // exact version-less package URL
	Contains string    // case-insensitive substring of the package URL
	On       time.Time // only intervals in force at this instant
}
```

`*query.Service` satisfies `TimelineReader`.

### Produced

```go
func NewServer(health HealthReader, findings FindingsReader, timeline TimelineReader, opts Options, logger *slog.Logger) (*Server, error)
```

## Requirements

1. Routes `GET /repos` and `GET /repos/{slug}`, registered like the existing ones; both carry the existing security headers and pass the host check. The layout gets a navigation link to `/repos` next to the existing findings link.
2. `GET /repos`: a table of repos with slug (linking to `/repos/{slug}`), URL, indexed branch, last indexed commit (first 12 characters), last fetch time (UTC, `2006-01-02 15:04`) and the last fetch error when there is one. No repos → an empty state line `No repositories indexed yet.`
3. `GET /repos/{slug}` query parameters: `q` → `TimelineFilter.Contains`; `pkg` → `TimelineFilter.Package`; `on` (`YYYY-MM-DD`) → `TimelineFilter.On` at the last instant of that UTC day. An `on` value that does not parse → 400 with a short message page. An unknown slug → 404 page. Any other reader error → 500 page with a generic message; the error is logged, not shown.
4. The page shows: a header with the repo slug, URL, branch and last indexed commit; a GET form with a text input `q`, a date input `on` and a submit button, prefilled with the current values; a link to clear the filters; then the interval table with columns Package, Version, From, To, Direct, Manifest. Package is a link to `?pkg=<url-escaped purl>` (keeping `on` if set). Version shows the requirement in parentheses when the version is empty. From and To are UTC dates; To shows `now` for an open interval. Direct shows `yes`, `no` or `?`.
5. When `pkg` is set, the page is the per-package history: a heading naming the package and a "back to all packages" link, the same table restricted to that package (every interval, open and closed, in store order).
6. Open intervals are visually distinguished from closed ones with a CSS class (`open`), styled in `static/style.css` in keeping with the existing stylesheet.
7. A note under the table, also the `title` attribute of the From and To headers: `Dates are the committer date of the first-parent commit, so a merged change is dated when it was merged.`
8. No intervals after filtering → `No intervals match.`; no intervals at all → `Not indexed yet.`
9. Everything from the repo (package names, versions, requirements, manifest paths, the fetch error) is rendered through `html/template` escaping only; no `template.HTML`, no `safe` helpers.
10. Handlers stay thin: parse input, call the reader, render. Filtering is the query service's job.

## Tests to include

Golden-file tests in the existing style (`-update` flag, `testdata/*.golden`), with a fake `TimelineReader`:

- `TestRepos_List` and `TestRepos_Empty`.
- `TestTimeline_All`: open, closed and requirement-only intervals, all three directness values.
- `TestTimeline_PackageHistory`: `?pkg=` view.
- `TestTimeline_FilterPassedThrough`: `?q=gin&on=2026-03-01` → the fake receives `Contains: "gin"` and `On` = 2026-03-01T23:59:59.999999999Z, and the form is prefilled.
- `TestTimeline_BadDate` → 400; `TestTimeline_UnknownRepo` → 404; `TestTimeline_ReaderError` → 500 and the error text is not in the body.
- `TestTimeline_Escapes`: a version `<script>alert(1)</script>` and a manifest path with `"` and `&` → escaped in the body, no raw `<script>`.
- Existing web and `cmd/upw` tests still pass.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -run 'TestRepos\|TestTimeline' -v ./internal/web/` | `ok`, the tests above PASS |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Any change to the query service or the store.
- JavaScript, client-side filtering, charts.
- Visual redesign of the existing pages.

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `web-ui`, `go-test`.
Read: `internal/web/server.go`, `internal/web/handlers.go`, `internal/web/templates/`, `internal/web/server_test.go`, `docs/architecture.md` ("HTTP routes").

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

None.

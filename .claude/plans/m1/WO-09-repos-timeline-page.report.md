Status: DONE
Work order: .claude/plans/m1/WO-09-repos-timeline-page.md
Files changed:
- cmd/upw/serve.go (modified: `web.NewServer(q, q, q, ...)` only)
- internal/web/server.go (modified: `timeline TimelineReader` field and parameter, routes `GET /repos` and `GET /repos/{slug}`, pages repos, timeline, error parsed at startup)
- internal/web/repos.go (created: handlers, view structs, formatting)
- internal/web/templates/layout.html (modified: nav link to /repos)
- internal/web/templates/repos.html (created)
- internal/web/templates/timeline.html (created)
- internal/web/templates/error.html (created: short message page for 400/404/500)
- internal/web/static/style.css (modified: `--open-bg`, `tr.open`, `.meta`, `.filter`, `.note`, `h2`, `code`)
- internal/web/fakes_test.go (modified: fakeTimeline, newTimelineServer, newLoggingTimelineServer)
- internal/web/server_test.go (modified: new NewServer argument; /repos paths added to TestSecurityHeaders)
- internal/web/repos_test.go (created)
- internal/web/testdata/findings_empty.golden, findings_list.golden (modified: the nav link line only)
- internal/web/testdata/repos_list.golden, repos_empty.golden, timeline_all.golden, timeline_package.golden (created; read and checked by hand)

Commands run:
- `make check` -> exit 0
  ```
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.950s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.256s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.335s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.202s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	2.986s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.578s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.899s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.413s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.172s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.532s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.696s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.652s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	4.639s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	2.705s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.232s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.224s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.406s
  ==> arch-lint
  go run github.com/fe3dback/go-arch-lint@v1.19.2 check --output-color=false
  module: github.com/Amrit-Code/upstreamwatch
  linters:
     On | Base: component imports # always on
     On | Advanced: vendor imports # switch 'allow.depOnAnyVendor = false' (or delete) to on
    Off | Advanced: method calls and dependency injections # switch 'allow.deepScan = true' (or delete) to on

  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
  ```
- `go test -race -count=1 -run 'TestRepos|TestTimeline' -v ./internal/web/` -> exit 0 (filtered to result lines)
  ```
  --- PASS: TestTimeline_UnknownRepo (0.00s)
  --- PASS: TestTimeline_ReaderError (0.00s)
  --- PASS: TestRepos_Empty (0.00s)
  --- PASS: TestRepos_ReaderError (0.00s)
  --- PASS: TestTimeline_NotIndexed (0.00s)
  --- PASS: TestRepos_List (0.00s)
  --- PASS: TestTimeline_PackageHistory (0.00s)
  --- PASS: TestTimeline_Escapes (0.00s)
  --- PASS: TestTimeline_All (0.00s)
  --- PASS: TestRepos_HostCheck (0.00s)
  --- PASS: TestTimeline_FilterPassedThrough (0.00s)
  --- PASS: TestTimeline_NoMatch (0.00s)
  --- PASS: TestTimeline_BadDate (0.00s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.191s
  ```
- `git status --porcelain` -> exit 0: only the files listed above (plus this report).

Decisions:
- "Not indexed yet." vs "No intervals match.": with an empty result, the handler shows "Not indexed yet." when the filter is zero (no q, pkg or on) and "No intervals match." otherwise; the reader returns only filtered intervals, so this is the only distinction available without a second call.
- 400/404/500 "pages" are a new `error.html` page in the layout (title plus one line). Messages: 400 "The on date must be a date in the form YYYY-MM-DD.", 404 "No repository with that name." (the slug is not echoed), 500 "Internal server error." `GET /repos` reader errors also render the 500 page. The existing findings 500 (plain `http.Error`) is unchanged.
- The page links keep `on` as the work order says. The "back to all packages" link also keeps `on` (`/repos/{slug}?on=...`). "Clear filters" goes to `/repos/{slug}`.
- The filter form also appears in the package-history view, with a hidden `pkg` input so that submitting it stays on that package.
- `pkg` and `q` are passed to the reader as given, so `?pkg=&on=` gives the package intervals in force on that day. Without `on`, it gives every interval in store order.
- "Indexed branch" and the header branch are `TargetRepo.DefaultBranch`. A zero `LastFetchAt` and an empty `LastIndexedSHA` render as empty cells.
- Version is `(requirement)` only when the version is empty and the requirement is not; if both are empty the cell is empty.
- The repo and package links are built in Go (`url.PathEscape` for the slug, `url.QueryEscape` for the purl and `on`) and rendered as plain strings through html/template.
- The repo URL is shown as text, not a link (no link was requested).
- The date note is a small `datenote` template inside timeline.html, used for both `title` attributes and the note paragraph.

Outside scope:
- docs/architecture.md "HTTP routes": `GET /repos` is missing from the table, and the paragraph after it still says that only `GET /`, `/health` and `/static/style.css` are served. Both are for docs-sync.

Concerns: none
Question or blocker: none

## Gate (lead, 2026-11-19)

make check rerun: exit 0; the 13 page tests pass. Diff inside owned files; serve.go changes only the NewServer call; findings goldens changed only for the new nav link. No template.HTML; package and filter links built with url.QueryEscape and url.PathEscape; the escaping test covers a script tag in a version and quotes in a path. Fully specified go-mech work order rendering through html/template only, so no separate reviewer; the milestone security review covers the page. Accepted.

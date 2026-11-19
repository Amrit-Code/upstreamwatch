Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m2/WO-12-web-findings-pages.md
Files changed:
- cmd/upw/serve.go (modified: only the web.NewServer call and the findings import)
- internal/query/interfaces.go (modified: FindingStore is now ListFindings + GetFindingDetail; ListOpenFindings removed)
- internal/query/service.go (modified: now func() time.Time field, OpenFindings removed)
- internal/query/findings.go (created: Service.Findings, Service.FindingDetail)
- internal/query/findings_test.go (created)
- internal/query/service_test.go (modified: OpenFindings test and old fake removed)
- internal/query/integration_test.go (modified: reads through Findings/FindingDetail)
- internal/web/interfaces.go (modified: FindingsReader replaced, FindingActions added)
- internal/web/server.go (modified: NewServer signature, actions and now fields, five new routes, "finding" page parsed)
- internal/web/handlers.go (modified: old findings handler and view structs moved out)
- internal/web/findings.go (created: GET /, GET /f/{id}, POST /f/{id}/ack|snooze|dismiss|reopen)
- internal/web/repos.go (modified: newIntervalRow uses the shared intervalVersion helper; output unchanged)
- internal/web/templates/findings.html (modified)
- internal/web/templates/finding.html (created)
- internal/web/static/style.css (modified: styles for details pre, action forms, h3, muted version and pin date)
- internal/web/export_test.go (modified: SetNow)
- internal/web/fakes_test.go (modified: fakeFindings for the new interface, fakeActions, server helpers)
- internal/web/server_test.go (modified: new NewServer arity, findings tests moved to findings_test.go, /f paths in the security-header test)
- internal/web/findings_test.go (created)
- internal/web/testdata/findings_empty.golden (modified: filter form added)
- internal/web/testdata/findings_list.golden (modified)
- internal/web/testdata/findings_list_suppressed.golden (created)
- internal/web/testdata/finding_advisory.golden (created)
- internal/web/testdata/finding_job_failure.golden (created)

Commands run:
- `go test -race -count=1 ./internal/web/ -update` -> exit 0 (goldens regenerated, then read line by line: escaping, row order, dismissed row present only in the suppressed page, pin date shown only on the closed exposure)
- `make check` -> exit 0
  ```
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	2.030s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.274s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.394s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.189s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/findings	1.187s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	9.343s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.576s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.638s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.211s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.193s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ops	1.534s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.639s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.509s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/report	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.445s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	7.380s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	3.675s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.218s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	1.179s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	1.260s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	1.236s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.305s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.669s
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
- `go test -race ./internal/web/ ./internal/query/ ./cmd/upw/` -> exit 0
  ```
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	(cached)
  ```
- `git status --porcelain` -> only the owned files listed above (plus this report file once written)
- `go test -race -count=1 -cover ./internal/web/ ./internal/query/` -> exit 0
  ```
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.418s	coverage: 93.5% of statements
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.453s	coverage: 100.0% of statements
  ```

Tests added: query TestFindings_PassesFilterAndNow (zero Now set from the clock in UTC, given Now kept), TestFindings_Empty, TestFindings_StoreErrors (list and detail), TestFindingDetail_Found, TestFindingDetail_NotFound (unknown id, store error). web: TestFindingsList_Golden (empty; advisory + job_failure + expired snooze with dismissed hidden; the same with suppressed=1), TestFindingsList_EmptyMessages, TestFindings_Filter, TestFindings_BadFilter, TestFindings_ReadErrors, TestFindingPage_Golden (advisory with two exposures, hostile summary/details/reason, KEV and EPSS; job_failure), TestFindingPage_ReopenOnlyWhenNotOpen, TestFindingPage_NotFound, TestFindingPage_ReadError, TestAck_PostRedirect, TestAck_PostWithoutNote, TestSnooze_Post (7d gives 168h, plus 1d and 30d), TestSnooze_BadFor, TestDismiss_Post (CRLF and lone CR become LF), TestReopen_Post, TestPost_InvalidTransition, TestPost_NotFound, TestPost_ServiceError, TestPost_BodyTooLarge, TestPost_CrossOrigin (Origin https://evil.example + Sec-Fetch-Site cross-site gives 403 for all four actions, fake not called), TestGet_NeverChangesState. A compile-time check `var _ web.FindingActions = (*findings.Service)(nil)` sits in findings_test.go.

Decisions:
- Repo filter: GET / reads the repo list through the existing TimelineReader.Repos, both to fill the select and to turn the slug into FindingFilter.RepoID; an unknown slug is 400 "No repository with that name."
- The web server takes a `now func() time.Time` (time.Now, fixed in tests through export_test.go SetNow) to compute FindingState.Effective; the filter it passes to Findings leaves Now zero so query sets it.
- "Both set f.Now" in requirement 1: FindingDetail(id) has no filter, so only Findings sets Now; FindingDetail passes straight through.
- Job name for a job_failure row is the fingerprint with its "job_failure|" prefix cut (pipeline.JobFailureFingerprint's format); issue rows have an empty advisory/job cell.
- "Confidence of the linked match" is the exposure whose Match.DepVersionID equals Finding.DepVersionID; blank when there is none.
- The link to /f/{id} is on the kind cell, which every row has.
- Severity shows "unknown" for an advisory with no severity; CVSS shows "none" without a vector; published/modified/KEV dates show "unknown" when zero; EPSS shows "score 0.9741, percentile 0.9987" or "not known".
- A body over 16 KiB answers 413 Request Entity Too Large with the error page; another form-parse failure answers 400.
- The invalid-transition 400 page includes the error text ("The finding was not changed: ack finding 12: finding 12 is resolved: invalid finding state change"); none of the model's messages carry the note itself.
- The pin start is shown beside the exposure start when the two differ by UTC date (both dates are shown as dates), with a note naming vulns.exposure_basis below the table.
- The state line shows "snoozed until <time> (UTC)" while a snooze runs; Reopen is shown when the effective state is not open (an ended snooze counts as open, so no Reopen).
- Ack, Snooze and Dismiss forms are always shown (re-acking replaces the note, per Finding.Transition).
- The exposures table shows "No exposures recorded." when an advisory finding has none.
- newIntervalRow in repos.go now shares the version-or-(requirement) helper with the findings pages; timeline output is unchanged (its goldens pass untouched).

Outside scope:
- docs/architecture.md routes table lists `POST /f/{id}/ack|snooze|dismiss` and not `reopen`; the paragraph after it still describes GET / as "unresolved findings in state open or acked" and the web interfaces as TimelineReader only. docs-sync should add the reopen route, the repo/kind/suppressed parameters, the 404/400/413 behaviour and the FindingsReader/FindingActions interfaces.
- docs/data-access.md: query's FindingStore now uses ListFindings and GetFindingDetail instead of ListOpenFindings. store.DB.ListOpenFindings is kept as instructed; it is now used only by store tests and other packages' integration tests (pipeline, ingest, sched, timeline).
- docs/architecture.md acknowledge sequence shows `Ack(findingID, note)`; the real call is `Ack(ctx, id, note, "web")`.

Concerns:
- The 413 status for an oversized body and the 400 page echoing the model's error text are my choices (the work order says "refused" and "400 with the error page"); the lead may prefer a fixed message or 400 for the oversized case.
- The job name comes from parsing the fingerprint format in internal/web. If the fingerprint format ever changes, the list shows the full fingerprint in that column instead of failing. A model method or a stored job name would be sturdier, but that is outside the owned files.

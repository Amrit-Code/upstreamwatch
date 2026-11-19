Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m1/WO-14-timeline-cli-config-hardening.md
Files changed:
- internal/timeline/sanitize.go (created)
- internal/timeline/sanitize_test.go (created)
- internal/timeline/service.go (modified)
- internal/timeline/service_test.go (modified)
- internal/timeline/integration_test.go (modified)
- internal/config/validate.go (modified)
- internal/config/load_git_test.go (modified)
- cmd/upw/timeline.go (modified)
- cmd/upw/timeline_test.go (modified)
- cmd/upw/runjob_test.go (modified; expected stats line only, per the 2026-11-19 ruling)
- .github/workflows/ci.yml (modified)

Commands run:
- `git log --oneline -1` -> exit 0
  a0df331 plans: WO-13 and WO-14 from the M1 security review; decision on token hosts and unsafe dependency names
- `make check` -> exit 0
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.892s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.168s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.312s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.180s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	2.755s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.567s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.729s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.224s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.180s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.476s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.655s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.638s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	4.441s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	3.095s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.235s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.231s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.568s
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
- `go test -race -count=1 ./internal/timeline/ ./internal/config/ ./cmd/upw/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	3.053s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.128s
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	2.384s
- `go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...` (the new CI step, run locally) -> exit 0
  No vulnerabilities found.
- `go test -race -cover -count=1 ./internal/timeline/ ./internal/config/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	3.062s	coverage: 98.4% of statements
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.235s	coverage: 98.5% of statements
- `git status --porcelain` -> exit 0
   M .claude/plans/m1/WO-14-timeline-cli-config-hardening.md   (the lead's Rulings edit, not mine)
   M .github/workflows/ci.yml
   M cmd/upw/runjob_test.go
   M cmd/upw/timeline.go
   M cmd/upw/timeline_test.go
   M internal/config/load_git_test.go
   M internal/config/validate.go
   M internal/timeline/integration_test.go
   M internal/timeline/service.go
   M internal/timeline/service_test.go
  ?? internal/timeline/sanitize.go
  ?? internal/timeline/sanitize_test.go
  (plus this report file once written)
- Mutation check: with the drop in sanitizeDeps disabled, TestTimeline_DirectReferenceCredentialNotStored failed (packages.name held "evil @ git+https://user:SECRET@github.com/...", 2 rows in packages, dep_events and dep_versions); restored, it passes.

Tests added or changed:
- internal/timeline: TestRun_DropsUnsafeDependencies, TestRun_TooManyDependencies (20,001 -> parse error, state kept), TestRun_AtDependencyLimitIsRead (exactly 20,000 is read), TestRun_EventCap (4 cases: over the limit fails before apply, at the limit applies, other targets continue, default 500,000), TestRun_ReadsBlobsInChunks (65 refs -> [32 32 1]), TestRun_FailureInLaterBlobChunkKeepsCounters (33 refs, second chunk fails, files_read 32), TestUnsafeIdentifier (16 cases), TestSanitizeDeps (5 cases), integration TestTimeline_DirectReferenceCredentialNotStored (reads every column of packages, dep_events, dep_versions, manifests and commits with database/sql in the test file).
- internal/config: TestLoad_TokenHost (8 cases: gitlab, other host, api.github.com, look-alike github.com.example.com rejected; github.com, github.com:443, auth none on gitlab and on any host accepted), TestLoad_TokenHostSkipsUnparseableURL.
- cmd/upw: TestTimeline_TextEscapesControlChars (text quoting plus JSON unchanged); TestRun_AllOnceNoTargets expected line gains deps_dropped=0.

Decisions:
- Sanitising lives in a pure helper `sanitizeDeps` (new file internal/timeline/sanitize.go), applied after a successful parse and the 20,000 check, before the ecosystem filter, so dropped counts include dependencies a target's ecosystem allowlist would later remove.
- "Whitespace" is unicode.IsSpace (so U+00A0 and U+2028 also drop a dependency), in addition to the ASCII control range the work order names.
- One debug line per manifest file with dropped dependencies, "timeline unsafe dependencies dropped", attributes repo, commit, path and dropped (a count); no dependency-derived value is logged.
- The 20,000 check counts the extractor's raw output; the warn line reuses "timeline manifest parse failed" with error "<n> dependencies, more than the limit of 20000".
- The event cap is checked after each commit's diff, so at most one commit's events beyond the limit are held; error text "more than 500000 new events in one run, the per-target limit". Injectable through the unexported `Service.maxEvents` field (doc-commented), set to `defaultMaxEvents` by NewService.
- CLI: terminalSafe is applied to the three repository-data columns (package URL, version or "(requirement)", manifest path); the date and DIRECT columns are generated and never hold control characters. For a requirement-only row the whole displayed "(req)" value is quoted.
- Token host: compared with strings.EqualFold against "github.com" on url.Hostname(), so a port is ignored and case does not matter; subdomains (api.github.com) and look-alikes are rejected. A URL that does not parse or has no host gets no auth error, since checkTargetURL already reports it. Error: `targets[i].auth: <auth> sends a GitHub token, so the host must be github.com, got "<host>"; use auth none for other hosts`.

Outside scope:
- Doc rows for docs-sync: new timeline stats key `deps_dropped` (docs/architecture.md job/stats description if it lists the timeline counters); config validation rule for `targets[].auth` host (docs that describe target auth, deploy/upw.example.yaml comment could mention github.com only); the govulncheck CI step (docs/development.md).
- internal/timeline/service.go logs extractor parse errors verbatim ("timeline manifest parse failed", "error", err). If an extractor ever echoes file content in its error, a credential in a manifest could reach the log; not changed here.

Concerns:
- The rule that drops any name containing "@" would drop every scoped npm package ("@types/node") if an npm extractor is added later. No current extractor (go.mod, Python, Docker, compose, Actions, Unity) produces such a name, so nothing is lost today, but the rule in docs/decisions.md will need revisiting before npm support.
- Requirement and version values are now redacted before the diff. A repo indexed before this change that already stored an unredacted requirement will get one "changed" event for that dependency on its next run, because the stored and new requirement differ.
- The CLI quoting covers C0 controls and 0x7f only, as the work order says. C1 controls (U+0080 to U+009F, for example U+009B CSI) pass through unquoted; most UTF-8 terminals ignore them, some do not.

## Fix round 1

Status: DONE
Work order: .claude/plans/m1/WO-14-timeline-cli-config-hardening.md
Files changed:
- cmd/upw/timeline.go (modified: terminalSafe quotes a field when it is not valid UTF-8 or holds any rune for which unicode.IsControl is true, which covers C0, DEL and C1)
- cmd/upw/timeline_test.go (modified: TestTimeline_TextEscapesControlChars gains a row with U+009B in the package URL and an invalid byte 0xff in the version; it also asserts the output is valid UTF-8 and holds no unicode.IsControl rune apart from newlines)
- internal/timeline/service.go (modified: the "timeline manifest parse failed" warn line logs model.Redact(err.Error()) instead of err)
- internal/timeline/service_test.go (modified: cannedExtractor can return an error; new TestRun_ParseErrorLogRedacted)
Commands run:
- `go test -race -count=1 ./internal/timeline/ ./internal/config/ ./cmd/upw/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	3.035s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.182s
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	2.421s
- `make check` -> exit 0
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	2.442s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.879s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	2.996s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	(cached)
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
Decisions:
- Extractor parse-error text never enters a target error in internal/timeline: a parse failure is a warning plus `parse_errors` and the target goes on. So the redaction applies to the one place the text appears, the warn log line.
- The redaction test checks the log for "https://[REDACTED]@h/x" and also checks that SECRET is in neither the log nor the ApplyTimeline update.
Outside scope: none new.
Concerns: none. Concerns 1 and 2 from the first round were accepted by the lead; concern 3 is fixed here.

## Gate (lead, 2026-11-19)

make check rerun: exit 0; timeline, config and cmd/upw tests ok; CI diff is the one govulncheck step. Round 0 concerns: npm scope rule noted in decisions for later, one-off changed events accepted; fix round 1 (C1 and invalid UTF-8 quoting, parse-error log redaction) rechecked. Sonnet reviewer: ACCEPT, three minors accepted as is (stored events from earlier builds are not re-sanitised: the VM database is re-indexed on redeploy; per-commit and per-file caps checked after parsing, bounded by the gitsrc blob cap). Outside scope: two extractor debug lines log raw skipped names (WO-15). Accepted.

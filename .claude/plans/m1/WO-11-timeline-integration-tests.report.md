Status: DONE
Work order: .claude/plans/m1/WO-11-timeline-integration-tests.md
Files changed:
- internal/timeline/integration_test.go (created)
- internal/ingest/integration_test.go (created)
- .claude/plans/m1/WO-11-timeline-integration-tests.report.md (created)
Commands run:
- `git log --oneline -1` -> exit 0
  c5adadb plans: WO-11 ingest and timeline integration tests
- `go test -race -count=1 -run 'TestTimeline_|TestIngest_' -v ./internal/timeline/ ./internal/ingest/` -> exit 0
  (the --- lines and package results from the output)
  --- PASS: TestTimeline_ReadsFromClone (0.66s)
  --- PASS: TestTimeline_Ecosystems (0.10s)
      --- PASS: TestTimeline_Ecosystems/derivation_off (0.60s)
      --- PASS: TestTimeline_Ecosystems/derivation_on (0.60s)
  --- PASS: TestTimeline_FirstParentSemantics (0.81s)
  --- PASS: TestTimeline_SecondRunDoesZeroWork (0.87s)
  --- PASS: TestTimeline_IncrementalKeepsIntervalIDs (0.98s)
  --- PASS: TestTimeline_ManifestDeletedAndReadded (0.99s)
  --- PASS: TestTimeline_FetchFailureKeepsLastGoodCommit (1.02s)
  --- PASS: TestTimeline_Unity (0.20s)
      --- PASS: TestTimeline_Unity/unity_off (0.57s)
      --- PASS: TestTimeline_Unity/unity_on (0.37s)
  --- PASS: TestTimeline_RewrittenHistory (1.10s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	2.326s
  --- PASS: TestIngest_FailedFirstSyncLeavesNoClone (0.20s)
  --- PASS: TestIngest_DefaultBranchAndBareClone (0.44s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.661s
- `go test -race -count=3 -run 'TestTimeline_|TestIngest_' ./internal/timeline/ ./internal/ingest/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	4.508s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	2.607s
- `go test -race -count=1 -cover ./internal/timeline/ ./internal/ingest/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	2.480s	coverage: 98.1% of statements
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.862s	coverage: 100.0% of statements
- `go test -race -count=1 -run 'TestTimeline_|TestIngest_' -coverpkg=./internal/timeline,./internal/ingest,./internal/gitsrc ./internal/timeline/ ./internal/ingest/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	2.452s	coverage: 66.6% of statements in ./internal/timeline, ./internal/ingest, ./internal/gitsrc
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.791s	coverage: 63.2% of statements in ./internal/timeline, ./internal/ingest, ./internal/gitsrc
- `make check` -> exit 0
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.693s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.240s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.368s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.184s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	2.763s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.559s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.659s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.227s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.372s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.489s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.511s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.438s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	3.810s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	2.507s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.221s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.210s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.446s
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
- `git status --porcelain` -> exit 0 (before this report was written)
  ?? internal/ingest/integration_test.go
  ?? internal/timeline/integration_test.go
Decisions:
- The stack helper (`newStack`) is written once in each package's integration_test.go and not in internal/testutil, because the layering lint lets testutil import only model, so a helper there could not import store, gitsrc, the services or pipeline. It builds a temp data directory, store.Open plus Migrate, one gitsrc.CLI (AllowFileURLs, temp ReposDir and HomeDir), ingest and timeline services (the real extract and unity extractors) and a pipeline.Runner with both jobs. Every run goes through Runner.Run.
- The timeline stack has `retarget`, which rebuilds both services and the Runner around the same store and gitsrc.CLI (so the same clones), the way a restart with an edited config would. TestTimeline_FetchFailureKeepsLastGoodCommit uses it to point the target at a `file://` path that does not exist. The test then points the target back at the fixture and checks that the job_failure finding is resolved and the unseen commit gets indexed.
- ingest.Options.Now is a fixed clock, so LastFetchAt is asserted exactly. The Runner and store clocks stay real; run times are not asserted. synctest is not used because these tests wait on git subprocesses.
- Ecosystems: the Dockerfile has a third stage, `FROM build AS check`, as well as the two FROM lines the work order names. Without a stage referenced by name, the "no pkg:docker/build" assertion could not fail.
- Ecosystems: go.mod uses `go 1.26.1` and `toolchain go1.26.1`, so the asserted version (1.26.1) does not depend on which directive wins when the two differ. The docs do not say which one wins.
- Ecosystems: packages are identified by Ecosystem plus Name rather than PURL for docker and GitHub Actions, because those PURL spellings come from the manifests library. pkg:golang/toolchain is identified by PURL, as the work order says. There is also a derivation-off subtest, where the toolchain has an interval only in go.mod.
- Unity: I read "ProjectVersion.txt changed twice" together with the required two intervals as follows. The file is created at D1, edited at D2 without changing the version, and changed to a new version at D3. That gives intervals [D1, D3) at 2022.3.10f1 and [D3, open) at 6000.0.23f1. The Unity-off fixture also has a workflow, which proves the target was indexed.
- RewrittenHistory: main is force-moved with `git switch --orphan` plus `git branch --force` through `Repo.Git`. internal/testutil/gitrepo needed no new helper and is unchanged.
- DefaultBranchAndBareClone: the fixture also has a `main` branch with a newer commit, so indexing the wrong branch would show. The target carries a placeholder token, and every file under the repos directory is checked for the token, `x-access-token` and `Authorization` (M1 item 3 in-process).
- Two tests beyond the list: TestTimeline_ReadsFromClone (the timeline builds after the fixture is deleted, so it reads only the bare clone under the data directory) and TestIngest_FailedFirstSyncLeavesNoClone (a first sync that fails leaves no clone directory, no stored branch or clone path, a job_failure finding, and the timeline skips the target).
- Case counts:
  - TestTimeline_FirstParentSemantics: 1 case (exact events, no events at branch commits, exact intervals, LastIndexedSHA)
  - TestTimeline_SecondRunDoesZeroWork: 1 case (second ingest and timeline stats, events and intervals DeepEqual, IDs included)
  - TestTimeline_IncrementalKeepsIntervalIDs: 1 case (one new changed event, unchanged rows identical, old interval keeps its ID and gets ToSHA, new open interval)
  - TestTimeline_FetchFailureKeepsLastGoodCommit: 1 case plus a recovery phase
  - TestTimeline_ManifestDeletedAndReadded: 1 case over two incremental runs
  - TestTimeline_Ecosystems: 2 subtests (derivation on and off), 11 package checks each
  - TestTimeline_Unity: 2 subtests (Unity on and off)
  - TestTimeline_RewrittenHistory: 1 case plus a follow-up incremental run
  - TestTimeline_ReadsFromClone: 1 case
  - TestIngest_DefaultBranchAndBareClone: 1 case
  - TestIngest_FailedFirstSyncLeavesNoClone: 1 case
Outside scope: A note, not a finding: my first draft's go.mod required `example.com/b v2.0.0` without a `/v2` suffix. modfile rejects that, and the timeline run still succeeded, counting the failure only in the `parse_errors` stat with zero events. This matches the M1 decision that a manifest that fails to parse produces no events. It does mean a parse failure on a real target is visible only in run stats and logs, not as a finding. The fixtures now use valid versions.
Concerns: none
Question or blocker: none

## Gate (lead, 2026-11-19)

make check rerun: exit 0; the 11 integration tests pass. Lead read the first-parent test: it asserts the exact event list, the exact intervals and that no event lands on a branch-side commit. Test-only work order, no production change, so no separate reviewer. Accepted.

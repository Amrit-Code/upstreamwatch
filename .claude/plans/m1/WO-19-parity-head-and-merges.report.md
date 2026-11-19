Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m1/WO-19-parity-head-and-merges.md
Files changed:
- cmd/upw/parity_e2e_test.go (modified)
- .claude/plans/m1/WO-19-parity-head-and-merges.report.md (created, this report)

Commands run:
- `git log --oneline -1` -> exit 0
  eb623f2 model: Scope.Rebuild; plans: WO-17 to WO-19 from the Fable review of M1
- `make check` -> exit 0 (last lines)
  ```
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.998s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.287s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.440s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.205s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	9.278s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.555s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.772s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.404s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.190s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.528s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.437s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.389s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	4.477s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	3.251s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.229s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.221s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.530s
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
  A second run with output discarded printed `make check exit=0`.
- `go vet -tags e2e ./cmd/upw/` -> exit 0 (no output)
- `make e2e` -> exit 0. Output below with the `=== RUN`, `=== PAUSE` and `=== CONT` lines and the `--- PASS` lines of unit-test subtests removed; nothing else changed:
  ```
  go test -tags e2e -race -count=1 -run TestParity -v -timeout 20m ./cmd/upw/
      parity_e2e_test.go:642: glow: timeline stats map[commits_walked:259 deps_dropped:0 events_written:662 failed:0 files_excluded:0 files_read:274 files_skipped:0 intervals:596 parse_errors:0 resets:0 skipped:0 targets:1]
      parity_e2e_test.go:652: glow: 873 first-parent commits, 2 merges; git-pkgs 633 raw events; upw 649, git-pkgs 633 normalised; 20 excused; at merges: upw 1, git-pkgs 1 net, 0 differing
      parity_e2e_test.go:675: glow: state at HEAD: upw 73 open intervals, tree 73 dependencies
      parity_e2e_test.go:642: cleo: timeline stats map[commits_walked:213 deps_dropped:0 events_written:705 failed:0 files_excluded:0 files_read:296 files_skipped:0 intervals:597 parse_errors:0 resets:0 skipped:0 targets:1]
      parity_e2e_test.go:652: cleo: 506 first-parent commits, 37 merges; git-pkgs 914 raw events; upw 705, git-pkgs 772 normalised; 73 excused; at merges: upw 84, git-pkgs 86 net, 0 differing
      parity_e2e_test.go:675: cleo: state at HEAD: upw 58 open intervals, tree 58 dependencies
  --- PASS: TestParity (7.73s)
      --- PASS: TestParity/glow (3.74s)
      --- PASS: TestParity/cleo (3.99s)
  --- PASS: TestParityNormaliseMerge (0.00s)
  --- PASS: TestParityNormalisePyPIRespellings (0.00s)
  --- PASS: TestParityHeadStateFromIntervals (0.00s)
  --- PASS: TestParityPackageName (0.01s)
  --- PASS: TestParityCompare (0.01s)
  --- PASS: TestParityCompareHeadState (0.01s)
  --- PASS: TestParityExcluded (0.01s)
  --- PASS: TestParityCollapseMerge (0.01s)
  === NAME  TestParityHeadStateFromTree
      parity_e2e_test.go:1507: broken/go.mod at 01cc71328d57c15b5aebd955340c3da07343729d does not parse, contributes nothing: parse broken/go.mod: broken/go.mod:4: syntax error (unterminated block started at broken/go.mod:3:1)
  --- PASS: TestParityHeadStateFromTree (0.11s)
  --- PASS: TestParityMergeBroughtIn (0.19s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	9.164s
  ```
  (The `broken/go.mod` line is the fixture's intended parse failure; it is logged, not a failure.)
- First run of the new checks, before adding exceptions (`go test -tags e2e -race -count=1 -run TestParity -v -timeout 20m ./cmd/upw/`) -> exit 1. glow passed. cleo failed on 2 merge-dated differences and nothing else; the HEAD state matched on both fixtures:
  ```
  cleo: 506 first-parent commits, 37 merges; git-pkgs 914 raw events; upw 705, git-pkgs 772 normalised; 71 excused; at merges: upw 84, git-pkgs 86 net, 2 differing
  only in upw (0):
  only in git-pkgs (2):
    765562a8421e5a9016ddff7e75fb88f5ec7fee28 poetry.lock pypi coverage changed 5.1
    765562a8421e5a9016ddff7e75fb88f5ec7fee28 poetry.lock pypi importlib-metadata changed 1.6.0
  cleo: state at HEAD: upw 58 open intervals, tree 58 dependencies
  ```
  Analysis: poetry.lock at the merge's first parent (9ed38e40) and at the merge lists coverage twice (4.5.4, then 5.0.4 / 5.1) and importlib-metadata twice (0.23, then 1.5.2 / 1.6.0). The brought-in commit 36993d5f moved only the second entry. upstreamwatch keeps the first entry, which is unchanged, so it emits nothing. This is the recorded decision "the first occurrence in the file wins", so I added two single-event git-pkgs-side exceptions that cite `reasonFirstWins`. Then the run passed (above).
- Mutation check (temporary edit, file restored from a byte copy and verified with `cmp`): (a) parse the tree at `fx.commit~30` instead of the pin, to stand in for a stale HEAD state; (b) drop upw's events at the merges 765562a8 and 380f7108, which is the old behaviour. `go test -tags e2e -race -count=1 -run 'TestParity$' -v ./cmd/upw/` -> both fixtures FAIL. glow: `open intervals differ from the manifests at HEAD ... (50 differences)` and `at merges: upw 0, git-pkgs 1 net, 1 differing`. cleo: `(65 differences)` and `at merges: upw 74, git-pkgs 86 net, 10 differing`. So both new checks catch what they are meant to catch.
- `env PATH=/opt/homebrew/Cellar/go/1.27.4/libexec/bin:/usr/bin:/bin go test -tags e2e -count=1 -run 'TestParity$' -v ./cmd/upw/` (git-pkgs not on PATH) -> exit 0
  ```
  === RUN   TestParity
      parity_e2e_test.go:602: git-pkgs not on PATH, timeline parity not checked: exec: "git-pkgs": executable file not found in $PATH
  --- SKIP: TestParity (0.00s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	0.271s
  ```
- `git status --porcelain` -> exit 0
  ```
   M cmd/upw/parity_e2e_test.go
  ```
  (this report file is added after that listing)

Decisions:
- HEAD state (requirement 1). `headStateFromTree` lists the pinned commit with `git ls-tree -r -z --full-tree` in the test's non-bare clone and keeps blob entries only, so gitlinks are skipped. It drops paths with a `testdata`, `vendor` or `node_modules` directory segment (`parityExcluded`, written in the test from the decision text; the fixtures set no `exclude_paths`) and paths `Parse(path, nil)` reports as unsupported. It reads each remaining file with `git show --no-textconv <sha>:<path>`, parses it with the same `extract.New(...)`, and keys the result by (manifest path, package URL) with version and requirement, keeping the first occurrence of a package URL. A file that fails to parse contributes nothing and is logged, so a difference shows up in the comparison. `headStateFromIntervals` takes the open intervals from `ListIntervals` (`IsOpen`) and returns an error when one key has two open intervals. `compareHeadState` returns sorted difference lines, capped at 50 in the failure message. A floor of 10 dependencies per side (`minHeadDeps`) stops an empty-against-empty pass. Direct flag and scope are not compared, as the work order specifies.
- HEAD state does not apply `sanitizeDeps` (unsafe-identifier drop, `model.Redact`) or the 20,000-dependency cap, because the work order names only the exclusion rules and the dedup. Neither produced a difference on the fixtures.
- Merge-dated events (requirement 2). For each first-parent merge M, the brought-in commits are `git rev-list --topo-order --reverse M^2 ^M^1`. Every first-parent merge in both fixtures has exactly two parents (checked: 2 in glow, 37 in cleo). `normaliseMerge` keeps the git-pkgs events of those commits with the same ecosystem, handled-path and PEP 503 name rules as `normaliseGitPkgs` (shared through `keepIndexed`), collapses respellings per commit (existing `collapseRespellings`), and stable-sorts the events into commit order, because git-pkgs prints commits in its own order. It then nets them with `collapseMerge`. The result goes through `toParityEvents`, which normaliseGitPkgs now also uses.
- `collapseMerge` netting rule. The state before is inferred from a package's first event: absent before `added`, present before `modified` (at `previous_requirement`) or `removed` (at `requirement`, where git-pkgs puts the old value of a removal; observed in the cleo output). The state after comes from the last event. absent→present = added at the last requirement; present→absent = removed; present→present = modified from the first previous to the last requirement, or nothing when the two are equal; absent→absent = nothing. Unknown change types are an error.
- One comparison, not two. The merge-dated git-pkgs events are appended to the normal git-pkgs list, and `normaliseUpw` no longer drops upw's events at merges. One `compareParity` runs over the combined lists. Every exception therefore still has to match exactly one difference across both checks, and the "unused exception fails" rule keeps working. The log line has a separate tally: `at merges: upw N, git-pkgs M net, K differing`. A floor of 1 merge-dated event per side (`minMergeEvents`) means a broken brought-in computation cannot pass by comparing nothing. Each fixture has at least one: glow 1/1, cleo 84/86.
- `normaliseUpw(events)` lost its `merges` parameter. `TestParityCompare` now expects upw's merge event to be kept.
- Exceptions added (2, both cleo, git-pkgs side, single events at merge 765562a8, `reasonFirstWins`): coverage changed 5.1, importlib-metadata changed 1.6.0. No exception was removed. All 91 earlier entries still match: glow 20 excused, cleo 73 = 71 + 2.
- Observed in the data: git-pkgs v0.22.0 reports no events at any merge commit in either fixture. Every git-pkgs event off the first-parent chain (glow 1, cleo 94) falls in the second-parent set of a first-parent merge, so the merge check now covers all of them.
- Unit tests and case counts:
  - `collapseMerge` (`TestParityCollapseMerge`): 12 cases (11 table rows plus unknown change type). They include the three the work order lists: added then changed (see Concerns), added then removed gives nothing, and two changes give one modified from the first previous to the last version.
  - `normaliseMerge` (`TestParityNormaliseMerge`): 3 cases: the main fixture (commit-order sort, respelling, name normalisation, drops by commit, ecosystem and path, removal requirement cleared), no brought-in commits, and unknown change type.
  - `headStateFromIntervals` (`TestParityHeadStateFromIntervals`): 3 cases: closed intervals ignored, two open intervals for one key is an error, and nil input.
  - `compareHeadState` (`TestParityCompareHeadState`): 7 rows.
  - `parityExcluded` (`TestParityExcluded`): 9 rows.
  - `headStateFromTree` (`TestParityHeadStateFromTree`): 1 fixture repo built with `gitrepo`. It checks the root and nested manifests, three excluded directories, an unparsable go.mod, a non-manifest file, and that the given commit is read rather than a later commit or the work tree.
  - `revList`/`revSet` brought-in arguments (`TestParityMergeBroughtIn`): 2 assertions on a `gitrepo` merge fixture: the brought-in commits come oldest first, and only the merge is listed as a first-parent merge.
  - `normaliseUpw` / `normaliseGitPkgs` (`TestParityCompare`, `TestParityNormalisePyPIRespellings`): existing cases, updated for the new signature.

Outside scope:
- WO-17 runs in parallel and changes the chain check, the blob cap and the fold. This worktree is at eb623f2, which predates it. `make e2e` should be rerun once WO-17 is on main. The HEAD and merge checks are the ones that would catch a regression there.
- docs: `docs/development.md` (or wherever `make e2e` is described, if anywhere) could say that the referee now also checks the HEAD state and merge-dated events. Nothing I found in `docs/` describes the parity test's coverage today.

Concerns:
- AMBIGUOUS wording, resolved by the requirement text. Please confirm. The "Tests to include" bullet says "added then changed → one changed to the last version". Requirement 2 asks for a *net* change "first added/changed to last state", compared with upw's events at M, which diff M against its first parent. A package the branch added and then bumped is absent at M^1, so upstreamwatch emits `added` at the last version. Encoding `changed` would make the referee report a false difference on every such package. I read the bullet as "one event, carrying the last version", and `TestParityCollapseMerge` expects `added` at the last version. No such sequence occurs in either fixture's merges, so the e2e result does not depend on this reading. If the lead meant `changed`, only that one table row changes.
- No BUG, FLAKY or skipped tests. `TestParity` skips only when git-pkgs is absent, as before.

## Gate (lead, 2026-11-19)

make check: exit 0; go vet -tags e2e: ok; make e2e (lead): glow HEAD state and 1/1 merge events match, cleo HEAD state and merge events match with 2 single-event first-occurrence exceptions; PASS. Concern accepted: the net change against the merge's first parent is `added` when the branch introduced the package (the work order's "changed" wording was loose). Mutation check by the agent: dropping upw's merge events fails both fixtures. Test-only work order. Accepted.

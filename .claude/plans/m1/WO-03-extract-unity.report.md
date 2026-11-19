Status: NEEDS_CONTEXT
Work order: .claude/plans/m1/WO-03-extract-unity.md
Files changed:
- internal/extract/unity/unity.go (created)
- internal/extract/unity/unity_test.go (created)
- internal/extract/unity/testdata/naughtyattributes/ProjectSettings/ProjectVersion.txt (created)
- internal/extract/unity/testdata/naughtyattributes/Packages/manifest.json (created)
- internal/extract/unity/testdata/naughtyattributes/Packages/packages-lock.json (created)
- .claude/plans/m1/WO-03-extract-unity.report.md (created)

Commands run:
- `make check` -> exit 0
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.667s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.230s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.091s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	1.391s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.552s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/ingest	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.413s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.375s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.660s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.416s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.404s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	2.841s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/timeline	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.206s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.206s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.317s
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
- `go test -race -count=1 -v ./internal/extract/unity/` -> exit 0 (filtered to result lines)
  --- PASS: TestPatterns (0.00s)
  --- PASS: TestParse_Manifest (0.00s)
  --- PASS: TestParse_PackagesLock (0.00s)
  --- PASS: TestParse_ProjectVersion (0.00s)
  --- PASS: TestParse_PackagesLock_Depth (0.00s)
  --- PASS: TestParse_Unsupported (0.00s)
  --- PASS: TestParse_UserinfoNeverReturned (0.00s)
  --- PASS: TestParse_Malformed (0.00s)
  --- PASS: TestParse_RealProject (0.00s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.113s
- `go test -race -coverprofile=... ./internal/extract/unity/` + `go tool cover -func` -> total 98.9%
- `git status --porcelain` -> exit 0
  ?? .claude/plans/m1/WO-03-extract-unity.report.md
  ?? internal/extract/

Decisions:
- Testdata from github.com/dbrizov/NaughtyAttributes (MIT, default branch `master`), fetched with `gh api .../contents/<path> --jq .content | base64 -d` on 2026-11-19; editor 2022.3.62f3.
- An `m_EditorVersion:` line with an empty value counts as no version line (no dependencies, no error).
- A negative `depth` in packages-lock.json maps to DirectUnknown, as if absent.
- `ssh://git@host/...` loses its `git@` user: requirement 5 says a URL loses "any userinfo"; only the scp-like `git@host:org/x.git` form keeps its user, as the work order states.
- Requirement 7: an scp-like value with a `user:password@` prefix (no scheme) loses that prefix; a bare user (`git@`) is kept.
- Userinfo removal is done with `strings`, not `net/url`, so the rest of the value is returned byte-for-byte. It is applied to every Requirement, and to Version for plain/registry/builtin values; for real version strings this changes nothing.
- A git URL is checked before `file:`, so `file:///x/repo.git` (a local git dependency in Unity) is treated as git.
- Parse returns `Dependencies: nil` when there are none.
- Errors are `parse <path>: <cause>`; the unsupported case wraps `model.ErrUnsupportedFile`.
- Two tests beyond the list: `TestParse_UserinfoNeverReturned` and `TestParse_PackagesLock_Depth` (sanity cases for requirement 7 and the depth mapping). The external test file also asserts `var _ timeline.Extractor = (*unity.Extractor)(nil)`.

Outside scope:
- The worktree started at 4dff154, one commit before the M1 groundwork; the lead fast-forwarded it to 0b4f89a while I worked. All checks above ran after that fast-forward.

Question or blocker:
The work order and `docs/decisions.md` disagree on `Direct` for `Packages/packages-lock.json`.
- WO-03 requirement 6: `DirectYes` when `depth` is 0, `DirectNo` when above 0, `DirectUnknown` when absent.
- `docs/decisions.md`, "Lead decisions for M1": "A lockfile cannot tell direct from transitive dependencies, so its entries are stored with `direct` NULL. The matching manifest (pyproject.toml, `Packages/manifest.json`) records the same packages as direct with their requirement." Naming `Packages/manifest.json` as the matching manifest puts packages-lock.json under this rule, so every entry would be DirectUnknown.
This changes the stored `direct` column. Everything else is finished and passes. The code currently follows the work order (`lockDirectness` in `internal/extract/unity/unity.go`, tested by `TestParse_PackagesLock`, `TestParse_PackagesLock_Depth` and `TestParse_RealProject`). Which rule applies to packages-lock.json? If it is the decision, the change is to return `model.DirectUnknown` for every entry, drop `Depth` from `lockEntry` and update the three tests. If it is the work order, the decision text needs an exception for packages-lock.json, which records `depth`.

## Fix round 1

Status: DONE
Work order: .claude/plans/m1/WO-03-extract-unity.md
Files changed:
- internal/extract/unity/unity.go (modified)
- internal/extract/unity/unity_test.go (modified)
- .claude/plans/m1/WO-03-extract-unity.report.md (modified)
Commands run:
- `go test -race -count=1 -v ./internal/extract/unity/` -> exit 0 (filtered to result lines)
  --- PASS: TestPatterns (0.00s)
  --- PASS: TestParse_PackagesLock (0.00s)
  --- PASS: TestParse_Malformed (0.00s)
  --- PASS: TestParse_ProjectVersion (0.00s)
  --- PASS: TestParse_Unsupported (0.00s)
  --- PASS: TestParse_UserinfoNeverReturned (0.00s)
  --- PASS: TestParse_Manifest (0.00s)
  --- PASS: TestParse_PackagesLock_Depth (0.00s)
  --- PASS: TestParse_RealProject (0.00s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.229s
- `make check` -> exit 0 (last 30 lines)
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.127s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/ingest	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/timeline	[no test files]
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
- `git status --porcelain` -> exit 0
   M .claude/plans/m1/WO-03-extract-unity.md
   A internal/extract/unity/testdata/naughtyattributes/Packages/manifest.json
   A internal/extract/unity/testdata/naughtyattributes/Packages/packages-lock.json
   A internal/extract/unity/testdata/naughtyattributes/ProjectSettings/ProjectVersion.txt
   A internal/extract/unity/unity.go
   A internal/extract/unity/unity_test.go
  ?? .claude/plans/m1/WO-03-extract-unity.report.md
Decisions: In `parseManifest`, the `file:` test now runs before `isGitURL`, so every `file:` value gets an empty Version (finding [minor] unity.go:114-116). This replaces the round 0 decision that `file:///x/repo.git` counts as git. Added `"com.example.localgit": "file:../x.git#abc"` to `TestParse_Manifest`, expecting Version "" and Requirement `file:../x.git#abc`. The depth mapping stays as it was, per the Rulings.
Outside scope: The work-order file shows as modified and the package files as staged ("A"). I did not make either change; both match the lead's ruling and staging.

## Gate (lead, 2026-11-19)

NEEDS_CONTEXT answered in Rulings (requirement 6 stands). make check rerun: exit 0. Sonnet reviewer: ACCEPT with one minor finding (`file:` values ending in `.git#ref` treated as git), fixed in fix round 1 and rechecked (make check exit 0, unity tests ok). Second minor (weak `assertNoSecret` helper) dismissed: exact-output assertions carry the check. Accepted.

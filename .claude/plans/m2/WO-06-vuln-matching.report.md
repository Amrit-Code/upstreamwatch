Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m2/WO-06-vuln-matching.md
Files changed:
- internal/vuln/match.go (created)
- internal/vuln/match_test.go (created)
- internal/vuln/version.go (created)
- internal/vuln/version_test.go (created)
- .claude/plans/m2/WO-06-vuln-matching.report.md (created)

Commands run:
- `make check` -> exit 0
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	2.169s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.261s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.552s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.392s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/findings	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	9.371s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.583s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.738s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.218s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.173s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/ops	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.566s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.477s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/report	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.457s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	5.450s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	3.747s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.228s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	1.078s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.223s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.402s
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
  (rerun with output to a log file: `make check > check.log 2>&1; echo $?` printed exit=0)
- `go test -race -count=1 -run 'TestEvaluate|TestExposureWindow|TestNormalizeVersion' ./internal/vuln/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	1.093s
- `git status --porcelain` -> exit 0
  ?? internal/vuln/match.go
  ?? internal/vuln/match_test.go
  ?? internal/vuln/version.go
  ?? internal/vuln/version_test.go
  (before this report was written; go.mod was not rewritten: `git diff go.mod` is empty, so `vers` is still marked `// indirect` and the lead's `go mod tidy` will move it)
- `go test -race -count=1 -coverprofile=cover.out ./internal/vuln/` -> exit 0
  coverage: 92.1% of statements (evaluate 95.7%, evalRange 94.3%, exposureWindow 100%, normalizeVersion 100%, schemeFor 46.2%: only the golang, pypi, npm, githubactions and unknown branches are exercised)

Decisions:
- vers scheme names (all accepted by vers v0.7.2 `CompareWithScheme` and `ValidWithScheme`): golang -> "go", pypi -> "pypi", npm -> "npm", cargo -> "cargo", gem -> "gem", nuget -> "nuget", maven -> "maven", composer -> "composer", pub -> "pub", hex -> "hex", githubactions -> "semver" with a leading "v" stripped from both sides. Other package types (docker, upm, generic) have no scheme.
- The ecosystem scheme is used for SEMVER ranges too (requirement 2), not a generic semver scheme. For Go the "go" scheme falls back to semver ordering when neither side has a "v", which is the case after normalizeVersion.
- A package type with no scheme, or a pin the scheme rejects, skips every non-GIT range with a note in the reason; the versions list is still checked. A range type other than SEMVER, ECOSYSTEM or GIT is skipped with a note.
- A range event with no field set makes its range uncomparable (skipped and noted, like a rejected value).
- Events at the same version are sorted introduced first, then fixed, last_affected, limit, so `introduced X, fixed X` is an empty range and `introduced X, last_affected X` covers X.
- `limit` is treated like `fixed` for whether the version is affected, but is never a fixed version.
- "Matching range has a fixed event" (confidence rule) is read as "a fixed version above the pin was found" (fixedVersion non-empty). A range like `introduced 0, fixed 1.0, introduced 2.0` with pin 3.0 has a fixed event but is open-ended above the pin, so it is medium with no fixed version. When several ranges match, one with a fix above the pin is enough for high.
- fixedVersion follows requirement 7 literally: the lowest fixed event above the pin across matched ranges, whatever event closes the pin's own segment.
- Versions-list matching normalises both sides with normalizeVersion and then the scheme's canonical form (so a GitHub Actions `v4.1.0` pin matches a listed `4.1.0`).
- Toolchain normalisation strips one "v", then "go", and completes only a version of exactly two all-digit components (`1.26`, `go1.26`); `1.26rc1` is returned unchanged and exact.
- The source database is `model.SourceDB(adv.ID)` (not `aff.SourceDB`), so it holds even if an entry's SourceDB is unset.
- Reason wording: range matches read `<native|non-native> <DB> record, <fixed in X|no fixed version>, <exact pin|<name> <orig> treated as <normalised>>`, with `<name>` the last path segment of the package URL (`toolchain`); misses read `<v> outside every evaluated range and not on the versions list` or `GIT ranges not evaluated; <v> not on the versions list`; other package reads `entry is for <purl>, not <purl>`; withdrawn reads `advisory withdrawn`. Skip notes are appended after `; `.
- exposureWindow treats any basis other than "pin" as "published" (the documented default); "closed" means `!iv.IsOpen()` (ToSHA set), as the model defines it.

Outside scope:
- PLAN.md section 5.4 lists more medium rules than the work order (a GHSA counts as native only "with a fixed event for this ecosystem"; a CVE id is medium only "with no ecosystem alias"; "multiple conflicting ranges across aliases") and low for "only a requirement string". The work order's narrower rules were implemented as written; docs/decisions.md does not record the narrowing.
- The OSV schema says SEMVER ranges use SemVer 2.0 ordering whatever the ecosystem; requirement 2 says to use the ecosystem's scheme. Identical for Go, npm, cargo and hex; it differs only for a SEMVER range on a PyPI, Maven, NuGet, RubyGems or Composer package, which OSV databases rarely publish.

Concerns:
- GitHub Actions pins such as `v4` are floating major tags but normalizeVersion returns them as exact (requirement 1 says every non-Go version is exact), so a match on one can be high, and how the semver scheme orders a one-component `4` against `4.1.0` was not tested here. Worth a ruling before the vulns job relies on it.
- The schemeFor branches for cargo, gem, nuget, maven, composer, pub and hex are not exercised by tests here (the full suite in WO-14 can cover them).

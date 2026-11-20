Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m2/WO-15-osv-scanner-parity.md
Files changed:
- cmd/upw/advisory_parity_e2e_test.go (created)
- Makefile (modified: e2e `-run` widened to `'TestParity|TestAdvisoryParity'`)
- .github/workflows/ci.yml (modified: e2e job installs osv-scanner v2.7.1 linux_amd64 release binary, SHA-256 verified)

Commands run:
- `make check` -> exit 0
  (last lines)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	(cached)
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
  (An earlier uncached run after the main edits also exited 0.)
- `go vet -tags e2e ./cmd/upw/` -> exit 0 (no output)
- `go test -tags e2e -race -count=1 -run TestAdvisoryParity -v -timeout 20m ./cmd/upw/` -> exit 1 (expected under requirement 7; see Concerns)
  Output, with the === RUN/PAUSE/CONT lines of the offline unit tests left out:
  === RUN   TestAdvisoryParity
  === RUN   TestAdvisoryParity/glow
      advisory_parity_e2e_test.go:280: glow: timeline stats map[commits_walked:259 deps_dropped:0 events_written:662 failed:0 files_excluded:0 files_read:274 files_skipped:0 intervals:596 parse_errors:0 resets:0 skipped:0 targets:1]
      advisory_parity_e2e_test.go:288: glow: vulns stats map[advisories_saved:258 delta_irrelevant:0 delta_refs:0 epss_errors:0 epss_updated:0 fetch_not_found:0 fetched:258 findings_resolved:0 findings_upserted:276 intervals:596 kev_errors:0 kev_updated:0 matches:2097 matches_open:23 pairs_new:557 query_refs:258 unversioned:5]
      advisory_parity_e2e_test.go:307: open advisory matches differ from osv-scanner
          glow go.mod: upw 18 ids, osv-scanner 8 ids; 0 differences excused
          upw (18): GO-2026-6599 GO-2026-6600 GO-2026-6603 GO-2026-6604 GO-2026-6605 GO-2026-6607 GO-2026-6608 GO-2026-6609 GO-2026-6610 GO-2026-6611 GO-2026-6612 GO-2026-6613 GO-2026-6617 GO-2026-6629 GO-2026-6809 GO-2026-6859 GO-2026-6860 GO-2026-6866
          osv-scanner (8): GO-2026-6603 GO-2026-6610 GO-2026-6611 GO-2026-6612 GO-2026-6617 GO-2026-6629 GO-2026-6860 GO-2026-6866
          only in upw (10): GO-2026-6599 GO-2026-6600 GO-2026-6604 GO-2026-6605 GO-2026-6607 GO-2026-6608 GO-2026-6609 GO-2026-6613 GO-2026-6809 GO-2026-6859
          only in osv-scanner (0):
  === RUN   TestAdvisoryParity/cleo
      advisory_parity_e2e_test.go:280: cleo: timeline stats map[commits_walked:213 deps_dropped:0 events_written:705 failed:0 files_excluded:0 files_read:296 files_skipped:0 intervals:597 parse_errors:0 resets:0 skipped:0 targets:1]
      advisory_parity_e2e_test.go:288: cleo: vulns stats map[advisories_saved:137 delta_irrelevant:0 delta_refs:0 epss_errors:0 epss_updated:0 fetch_not_found:0 fetched:137 findings_resolved:0 findings_upserted:137 intervals:597 kev_errors:0 kev_updated:0 matches:962 matches_open:34 pairs_new:486 query_refs:137 unversioned:77]
      advisory_parity_e2e_test.go:309: cleo poetry.lock: upw 34 ids, osv-scanner 34 ids; 0 differences excused
          upw (34): GHSA-2wc2-fm75-p42x GHSA-2xpw-w6gg-jr37 GHSA-38jv-5279-wg99 GHSA-4j63-6c4x-wfg4 GHSA-5239-wwwm-4pmq GHSA-65pc-fj4g-8rjx GHSA-6655-6qjp-jx7x GHSA-6q48-6c7w-9gw2 GHSA-6w46-j5rx-g56g GHSA-7g44-g9rc-x53w GHSA-836r-79rf-4m37 GHSA-8988-9cw3-xx77 GHSA-gc5v-m9x4-r6x2 GHSA-gjv8-xp57-g29c GHSA-gm62-xv2j-4w53 GHSA-j934-xhv5-fg8f GHSA-mmhr-3c5m-f64f GHSA-qccp-gfcp-xxvc GHSA-vxq7-64xx-v4gw PYSEC-2026-141 PYSEC-2026-1845 PYSEC-2026-1994 PYSEC-2026-1996 PYSEC-2026-1998 PYSEC-2026-215 PYSEC-2026-2275 PYSEC-2026-2987 PYSEC-2026-3071 PYSEC-2026-3072 PYSEC-2026-4170 PYSEC-2026-4171 PYSEC-2026-4175 PYSEC-2026-4177 PYSEC-2026-4320
          osv-scanner (34): (the same 34 ids)
          only in upw (0):
          only in osv-scanner (0):
  --- FAIL: TestAdvisoryParity (98.32s)
      --- FAIL: TestAdvisoryParity/glow (65.83s)
      --- PASS: TestAdvisoryParity/cleo (32.48s)
  --- PASS: TestAdvisoryParityOpenIDs (0.00s)
  --- PASS: TestAdvisoryParityDecodeOSVScanner (0.00s)
  --- PASS: TestAdvisoryParityCompare (0.00s)
      (all 7 subtests PASS)
  FAIL
  FAIL	github.com/Amrit-Code/upstreamwatch/cmd/upw	98.685s
- `git status --porcelain` -> only owned files (plus this report once written):
   M .github/workflows/ci.yml
   M Makefile
  ?? cmd/upw/advisory_parity_e2e_test.go
- CI checksum: `osv-scanner_SHA256SUMS` from the v2.7.1 release, the release API's asset digest and a local `shasum -a 256` of the downloaded `osv-scanner_linux_amd64` all give fb7dab19a8bd9bbe5c3189f09d4b7ee1820c9553a23bac202648a56efd2f6ee8.

Decisions:
- Only the file the WO names per fixture is compared (`advisoryParityFiles`: glow go.mod, cleo poetry.lock). Open advisory ids on any other manifest are logged as "not compared". Both fixtures currently have none.
- Sanity guard, modelled on the timeline referee: the compared file must have at least `minHeadDeps` (10) open intervals with a resolved version, so a run where both sides are empty cannot pass.
- Indexing is a copy of `indexWithUpw` (`indexForAdvisories`): same store open and migrate, same gitsrc options, ingest, extractor (`DeriveToolchainFromImages: false`) and timeline. It is a copy because `indexWithUpw` does not return the store and is read-only to me.
- Our side is read with `ListUnresolvedFindings(model.KindAdvisory)` followed by `GetFindingDetail`, taking the exposures with `Match.IsOpen()` and grouping them by `Interval.ManifestPath`. The test adds no SQL.
- vulns job: `osv.New(httpx.New(db, httpx.Options{}, logger), osv.Options{}, logger)`, KEV and EPSS nil, `vuln.Options{}`. Any error from Run fails the test.
- Alias handling: an osv-scanner id outside every group counts as a group of its own. When our side has no id from a group, every id in that group is listed under "only in osv-scanner". Exceptions match one id on one side for one fixture and file.
- The `paritySide` type is reused, with a new `sideOSVScanner = "osv-scanner"`. The exceptions table lives in code and is empty; no testdata directory was needed.
- osv-scanner exit code 1 (vulnerabilities found) counts as success. Any other non-zero exit fails the test.
- osv-scanner runs with `parityEnv` plus `GOFLAGS=-modcacherw`. Its default Go call analysis downloads modules into the empty HOME's module cache. Those files are read-only by default, so `t.TempDir` cleanup failed: "TempDir RemoveAll cleanup: ... permission denied" in the first run. The command itself is exactly the WO's: `scan source --format json -L <abs lockfile>`.
- The skip on missing git-pkgs follows requirement 8, even though this test never calls git-pkgs.
- In CI the binary is installed with `install -m 0755` into `$RUNNER_TEMP/osv-scanner`, which is added to `$GITHUB_PATH`. This matches how the git-pkgs step is laid out.

Outside scope:
- The WO, requirement 3, is wrong for osv-scanner v2.7.1. With default config, osv-scanner does not report Go standard library advisories from a go.mod `go`/`toolchain` line. `pkg/osvscanner/osvscanner.go` `filterAndOverrideGoVersion` drops the go.mod `stdlib` package unless the config key `ScanGoModVersion = true` is set (osv-scanner docs/configuration.md, "Scan Go Mod Version"). PLAN.md 5.4's differential-test line inherits the same assumption.
- The Makefile comment above `e2e` still says the target only compares timelines with git pkgs history. I left it alone because the WO limited the Makefile change to `-run`.

Concerns:
- glow go.mod shows 10 unexplained differences, all only in upw: GO-2026-6599 GO-2026-6600 GO-2026-6604 GO-2026-6605 GO-2026-6607 GO-2026-6608 GO-2026-6609 GO-2026-6613 GO-2026-6809 GO-2026-6859.
- Analysis: upstreamwatch is right. Every one of these is a Go `stdlib` advisory (fetched from https://api.osv.dev/v1/vulns/<id>), matched on `pkg:golang/toolchain` at 1.26.7 from glow's `go 1.26.7` directive. Their SEMVER ranges are introduced 0, fixed 1.26.9 for GO-2026-6599/6600/6604/6605/6607/6608/6609/6613; fixed 1.26.10 for GO-2026-6809; fixed 1.26.11 for GO-2026-6859 (each also has a 1.27.x branch). All of them contain 1.26.7. osv-scanner leaves them out only because, by default, it does not scan the go.mod Go version.
- Smallest reproduction: take glow go.mod at 8b0600fa445535b9c53194c0d4f7fe3cf700ef3e.
  - `osv-scanner scan source --format json -L go.mod` gives 8 ids, from golang.org/x/net 0.56.0 and golang.org/x/text 0.39.0 only.
  - Add `--config=<file containing "ScanGoModVersion = true">` and it reports `stdlib 1.26.7` with 15 ids. The union is then exactly upw's 18 ids.
- cleo poetry.lock matches exactly: 34 ids on each side, with nothing only on one side.
- Following requirement 7, I added no exception and changed no production code. The lead needs to choose one of these:
  (a) Pass osv-scanner a `--config` file with `ScanGoModVersion = true`, for example in `cmd/upw/testdata/advisory_parity/`. That matches the WO's evident intent of comparing toolchain advisories on both sides, and both fixtures would then pass.
  (b) Record a decision and add 10 upw-side exceptions.
  (c) Drop `pkg:golang/toolchain` from our side, which contradicts requirement 3.

## Fix round 1

Status: DONE
Work order: .claude/plans/m2/WO-15-osv-scanner-parity.md (lead ruling: option (a))
Files changed:
- cmd/upw/testdata/advisory_parity/osv-scanner.toml (created: `ScanGoModVersion = true`, with a comment explaining why)
- cmd/upw/advisory_parity_e2e_test.go (modified)
  - New const `osvScannerConfig` points at the config file.
  - `runOSVScanner` resolves that path to an absolute one and runs `osv-scanner scan source --format json --config <abs config> -L <abs lockfile>`.
  - `GOFLAGS=-modcacherw` stays in the environment.
  - No exceptions were added; `advisoryParityExceptions` is still empty.
Commands run:
- `go vet -tags e2e ./cmd/upw/` -> exit 0 (no output)
- `go test -tags e2e -race -count=1 -run TestAdvisoryParity -v -timeout 20m ./cmd/upw/` -> exit 0
  (=== RUN/PAUSE/CONT lines omitted)
      advisory_parity_e2e_test.go:309: glow go.mod: upw 18 ids, osv-scanner 18 ids; 0 differences excused
          upw (18): GO-2026-6599 GO-2026-6600 GO-2026-6603 GO-2026-6604 GO-2026-6605 GO-2026-6607 GO-2026-6608 GO-2026-6609 GO-2026-6610 GO-2026-6611 GO-2026-6612 GO-2026-6613 GO-2026-6617 GO-2026-6629 GO-2026-6809 GO-2026-6859 GO-2026-6860 GO-2026-6866
          osv-scanner (18): (the same 18 ids)
          only in upw (0):
          only in osv-scanner (0):
      advisory_parity_e2e_test.go:309: cleo poetry.lock: upw 34 ids, osv-scanner 34 ids; 0 differences excused
          only in upw (0):
          only in osv-scanner (0):
  --- PASS: TestAdvisoryParity (103.92s)
      --- PASS: TestAdvisoryParity/glow (66.84s)
      --- PASS: TestAdvisoryParity/cleo (37.07s)
  --- PASS: TestAdvisoryParityOpenIDs (0.00s)
  --- PASS: TestAdvisoryParityDecodeOSVScanner (0.00s)
  --- PASS: TestAdvisoryParityCompare (0.00s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	105.288s
- `make check` -> exit 0 (arch-lint: "OK - No warnings found"; import-check passed)
- `git status --porcelain` -> only owned files and this report:
   M .github/workflows/ci.yml
   M Makefile
  ?? .claude/plans/m2/WO-15-osv-scanner-parity.report.md
  ?? cmd/upw/testdata/
  ?? cmd/upw/advisory_parity_e2e_test.go
Decisions:
- The config is passed with `--config`, not left as an `osv-scanner.toml` beside the lockfile. `--config` also overrides any `osv-scanner.toml` a fixture repository might ship, so the fixture cannot change the referee's settings.
- The path is resolved with `filepath.Abs` against the package directory, which `go test` uses as the working directory.
Outside scope:
- The Makefile comment above `e2e` is unchanged (see the original report).
- docs-sync may want to record the `ScanGoModVersion` ruling under "Lead decisions for M2" and in PLAN.md 5.4's differential-test line.
Concerns: none

## Fix round 2

Status: DONE
Work order: .claude/plans/m2/WO-15-osv-scanner-parity.md (review findings 1 and 2, applied on top of d523935)
Files changed:
- cmd/upw/advisory_parity_e2e_test.go (modified)
  - After the comparison, the subtest now fails when either side has no advisory ids for the compared file.
  - The failure message gives both counts, names the likely causes (OSV unreachable, or the fixture pin no longer has open advisories) and prints the full report.
- Makefile (modified)
  - The comment above `e2e` now says the target runs both referees: timelines against git pkgs history, and open advisory matches at HEAD against osv-scanner.
  - It also says the target needs the network, git-pkgs on PATH and osv-scanner on PATH or in GOPATH/bin.
Commands run:
- `go vet -tags e2e ./cmd/upw/` -> exit 0 (no output)
- `go test -tags e2e -race -count=1 -run 'TestAdvisoryParityCompare|TestAdvisoryParityDecode|TestAdvisoryParityOpenIDs' ./cmd/upw/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.378s
- `make check` -> exit 0
  ==> arch-lint
  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
- I did not rerun the networked TestAdvisoryParity, as the lead allowed. In fix round 1 it found 18 ids per side for glow and 34 for cleo, so the new guard would not have fired on that run.
- `git status --porcelain`:
   M Makefile
   M cmd/upw/advisory_parity_e2e_test.go
  (the report file is also modified once this section is appended)
Decisions: the guard reports with `t.Errorf` rather than `t.Fatalf`, so the comparison report with both sets is still printed on the same run.
Outside scope: none
Concerns: none

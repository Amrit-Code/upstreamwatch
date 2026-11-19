Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m1/WO-10-gitpkgs-parity.md
Files changed:
- cmd/upw/parity_e2e_test.go (created)
- Makefile (modified: `e2e` target and `.PHONY` entry only)
- .github/workflows/ci.yml (modified: one `e2e` job only)
- .claude/plans/m1/WO-10-gitpkgs-parity.report.md (created, this report)

Commands run:
- `git log --oneline -1` -> exit 0
  e62c5fe plans: WO-08 and WO-10 owned files kept apart
- `make check` -> exit 0
  ```
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.659s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.263s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.359s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.191s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	2.484s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.549s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.193s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.410s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.180s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.471s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.411s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.386s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	3.095s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	1.212s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.236s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.233s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.350s
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
- `go vet -tags e2e ./cmd/upw/` -> exit 0 (no output)
- `make e2e` -> exit 2 (requirement 9: glow PASS, cleo FAIL on unexplained differences; TestParityCompare PASS)
  Full output, with only the `=== PAUSE` / `=== CONT` / `=== RUN TestParityCompare/...` lines of the passing unit test removed:
  ```
  go test -tags e2e -race -count=1 -run TestParity -v -timeout 20m ./cmd/upw/
  === RUN   TestParity
  === RUN   TestParity/glow
      parity_e2e_test.go:377: glow: timeline stats map[commits_walked:259 events_written:655 failed:0 files_read:274 files_skipped:0 intervals:589 parse_errors:0 resets:0 skipped:0 targets:1]
      parity_e2e_test.go:380: glow: 873 first-parent commits, 2 merges; git-pkgs 633 raw events; upw 641, git-pkgs 632 normalised; 13 excused
  === RUN   TestParity/cleo
      parity_e2e_test.go:377: cleo: timeline stats map[commits_walked:213 events_written:705 failed:0 files_read:296 files_skipped:0 intervals:597 parse_errors:0 resets:0 skipped:0 targets:1]
      parity_e2e_test.go:380: cleo: 506 first-parent commits, 37 merges; git-pkgs 914 raw events; upw 621, git-pkgs 695 normalised; 71 excused
      parity_e2e_test.go:383: timeline differs from git pkgs history
          cleo: upw 621 events, git-pkgs 695 events (normalised); 71 differences excused
          only in upw (24):
            08bcb3c078c13fd1a2ff91a52b0b29e55024942c poetry.lock pypi Babel changed 2.14.0
            1e8c6398540769b0ca0f2604313c57319ddfd765 poetry.lock pypi Babel changed 2.13.1
            4c0b80cb3aa2bdb9f552a56ac069eaac0c43daeb poetry.lock pypi Babel changed 2.17.0
            57c6b9ee79164c099aed385413566ac3f3d2af4e poetry.lock pypi Babel changed 2.12.1
            ac0c22ebb8ce14290b7b754de58248156daadc09 poetry.lock pypi Babel changed 2.16.0
            bfcdd7213230d347ee4dcffadd255b7785adeb8a poetry.lock pypi Babel changed 2.11.0
            08bcb3c078c13fd1a2ff91a52b0b29e55024942c poetry.lock pypi Jinja2 changed 3.1.3
            4c0b80cb3aa2bdb9f552a56ac069eaac0c43daeb poetry.lock pypi Jinja2 changed 3.1.6
            ac0c22ebb8ce14290b7b754de58248156daadc09 poetry.lock pypi Jinja2 changed 3.1.5
            016e0d2568bb9f16a7d22505428982e80aa3214b poetry.lock pypi MarkupSafe changed 2.1.3
            08bcb3c078c13fd1a2ff91a52b0b29e55024942c poetry.lock pypi MarkupSafe changed 2.1.5
            4c0b80cb3aa2bdb9f552a56ac069eaac0c43daeb poetry.lock pypi MarkupSafe changed 3.0.3
            57c6b9ee79164c099aed385413566ac3f3d2af4e poetry.lock pypi MarkupSafe changed 2.1.2
            ac0c22ebb8ce14290b7b754de58248156daadc09 poetry.lock pypi MarkupSafe changed 3.0.2
            016e0d2568bb9f16a7d22505428982e80aa3214b poetry.lock pypi Pygments changed 2.16.1
            08bcb3c078c13fd1a2ff91a52b0b29e55024942c poetry.lock pypi Pygments changed 2.17.2
            4c0b80cb3aa2bdb9f552a56ac069eaac0c43daeb poetry.lock pypi Pygments changed 2.19.2
            57c6b9ee79164c099aed385413566ac3f3d2af4e poetry.lock pypi Pygments changed 2.15.1
            ac0c22ebb8ce14290b7b754de58248156daadc09 poetry.lock pypi Pygments changed 2.18.0
            34aa73b0a1aa5fc1c1f8ca347118a9ea4a33c96c poetry.lock pypi Sphinx changed 7.1.2
            ac0c22ebb8ce14290b7b754de58248156daadc09 poetry.lock pypi Sphinx changed 7.4.7
            bfcdd7213230d347ee4dcffadd255b7785adeb8a poetry.lock pypi Sphinx changed 5.3.0
            34aa73b0a1aa5fc1c1f8ca347118a9ea4a33c96c pyproject.toml pypi Sphinx changed ^7.1
            fd264efa3702c6de0231b8235741881d70bb4e3e pyproject.toml pypi Sphinx removed
          only in git-pkgs (33):
            bfcdd7213230d347ee4dcffadd255b7785adeb8a poetry.lock pypi Babel removed
            bfcdd7213230d347ee4dcffadd255b7785adeb8a poetry.lock pypi Jinja2 removed
            bfcdd7213230d347ee4dcffadd255b7785adeb8a poetry.lock pypi MarkupSafe removed
            bfcdd7213230d347ee4dcffadd255b7785adeb8a poetry.lock pypi Pygments removed
            bfcdd7213230d347ee4dcffadd255b7785adeb8a poetry.lock pypi Sphinx removed
            08bcb3c078c13fd1a2ff91a52b0b29e55024942c poetry.lock pypi babel changed 2.14.0
            1e8c6398540769b0ca0f2604313c57319ddfd765 poetry.lock pypi babel changed 2.13.1
            4c0b80cb3aa2bdb9f552a56ac069eaac0c43daeb poetry.lock pypi babel changed 2.17.0
            57c6b9ee79164c099aed385413566ac3f3d2af4e poetry.lock pypi babel changed 2.12.1
            ac0c22ebb8ce14290b7b754de58248156daadc09 poetry.lock pypi babel changed 2.16.0
            bfcdd7213230d347ee4dcffadd255b7785adeb8a poetry.lock pypi babel added 2.11.0
            08bcb3c078c13fd1a2ff91a52b0b29e55024942c poetry.lock pypi jinja2 changed 3.1.3
            4c0b80cb3aa2bdb9f552a56ac069eaac0c43daeb poetry.lock pypi jinja2 changed 3.1.6
            ac0c22ebb8ce14290b7b754de58248156daadc09 poetry.lock pypi jinja2 changed 3.1.5
            bfcdd7213230d347ee4dcffadd255b7785adeb8a poetry.lock pypi jinja2 added 3.1.2
            016e0d2568bb9f16a7d22505428982e80aa3214b poetry.lock pypi markupsafe changed 2.1.3
            08bcb3c078c13fd1a2ff91a52b0b29e55024942c poetry.lock pypi markupsafe changed 2.1.5
            4c0b80cb3aa2bdb9f552a56ac069eaac0c43daeb poetry.lock pypi markupsafe changed 3.0.3
            57c6b9ee79164c099aed385413566ac3f3d2af4e poetry.lock pypi markupsafe changed 2.1.2
            ac0c22ebb8ce14290b7b754de58248156daadc09 poetry.lock pypi markupsafe changed 3.0.2
            bfcdd7213230d347ee4dcffadd255b7785adeb8a poetry.lock pypi markupsafe added 2.1.1
            016e0d2568bb9f16a7d22505428982e80aa3214b poetry.lock pypi pygments changed 2.16.1
            08bcb3c078c13fd1a2ff91a52b0b29e55024942c poetry.lock pypi pygments changed 2.17.2
            4c0b80cb3aa2bdb9f552a56ac069eaac0c43daeb poetry.lock pypi pygments changed 2.19.2
            57c6b9ee79164c099aed385413566ac3f3d2af4e poetry.lock pypi pygments changed 2.15.1
            ac0c22ebb8ce14290b7b754de58248156daadc09 poetry.lock pypi pygments changed 2.18.0
            bfcdd7213230d347ee4dcffadd255b7785adeb8a poetry.lock pypi pygments added 2.13.0
            34aa73b0a1aa5fc1c1f8ca347118a9ea4a33c96c poetry.lock pypi sphinx changed 7.1.2
            ac0c22ebb8ce14290b7b754de58248156daadc09 poetry.lock pypi sphinx changed 7.4.7
            bfcdd7213230d347ee4dcffadd255b7785adeb8a poetry.lock pypi sphinx added 5.3.0
            34aa73b0a1aa5fc1c1f8ca347118a9ea4a33c96c pyproject.toml pypi Sphinx removed
            34aa73b0a1aa5fc1c1f8ca347118a9ea4a33c96c pyproject.toml pypi sphinx added ^7.1
            fd264efa3702c6de0231b8235741881d70bb4e3e pyproject.toml pypi sphinx removed
  --- FAIL: TestParity (6.00s)
      --- PASS: TestParity/glow (2.64s)
      --- FAIL: TestParity/cleo (3.36s)
  === RUN   TestParityCompare
  --- PASS: TestParityCompare (0.00s)
      --- PASS: TestParityCompare/equal (0.00s)
      --- PASS: TestParityCompare/exceptions_excuse_both_sides (0.00s)
      --- PASS: TestParityCompare/requirement_differs (0.00s)
      --- PASS: TestParityCompare/exception_excuses_its_side_only (0.00s)
      --- PASS: TestParityCompare/exception_that_matches_nothing_fails (0.00s)
      --- PASS: TestParityCompare/multiset_counts_duplicates (0.00s)
      --- PASS: TestParityCompare/other_fixture's_exception_is_ignored (0.00s)
  FAIL
  FAIL	github.com/Amrit-Code/upstreamwatch/cmd/upw	6.347s
  FAIL
  make: *** [e2e] Error 1
  ```
- `env PATH=/opt/homebrew/Cellar/go/1.27.4/libexec/bin:/usr/bin:/bin go test -tags e2e -count=1 -run TestParity -v ./cmd/upw/` (requirement 10: git-pkgs absent from PATH) -> exit 0
  ```
  === RUN   TestParity
      parity_e2e_test.go:350: git-pkgs not on PATH, timeline parity not checked: exec: "git-pkgs": executable file not found in $PATH
  --- SKIP: TestParity (0.00s)
  === RUN   TestParityCompare
  --- PASS: TestParityCompare (0.00s)
  ...(7 subtests PASS)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	0.289s
  ```
- `curl -sSL https://github.com/git-pkgs/git-pkgs/releases/download/v0.22.0/checksums.txt` -> exit 0; line used:
  `7f65d7e9b3ca1f07c249e79c4e1283fdea9c16b6b10022b4b546292e96dcb08e  git-pkgs_0.22.0_linux_amd64.tar.gz`
  The downloaded linux amd64 archive hashed to the same value with `shasum -a 256`, and its listing contains the `git-pkgs` binary at the archive root (the workflow extracts that member).
- `git status --porcelain` -> exit 0
  ```
   M .github/workflows/ci.yml
   M Makefile
  ?? cmd/upw/parity_e2e_test.go
  ```
  (this report file is added after that listing)

Decisions:
- Test package: `package main`, like the other tests in cmd/upw, under `//go:build e2e`.
- Exceptions live in a Go table in the test, not in testdata (no `cmd/upw/testdata/parity/` created). Each entry is per fixture and per side. Non-empty fields must equal the event's and empty fields match any value. An exception applies only to differences, so an entry whose difference goes away matches nothing and fails the test.
- Exception granularity: glow and the duplicate-entry cases are per (commit, manifest, package). The two scope-only commits are one entry each (commit, poetry.lock, side git-pkgs, change changed). I checked that the poetry.lock diff at those two commits changes only `groups`, `markers` and `content-hash` lines, so no version change can hide under them.
- Differing events are sorted by manifest path, ecosystem, name, SHA, change and requirement. The report prints the normalised counts per side and up to 50 events per side.
- git and git-pkgs run with the caller's environment plus a temp HOME and XDG dirs, `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null` and `GIT_TERMINAL_PROMPT=0`, so local git config cannot change either side.
- upstreamwatch's logger writes Warn and above to `t.Output()`, so parse warnings show up in `-v` output.
- CI job: `if: github.event_name == 'push' && github.ref == 'refs/heads/main'`, no `needs:`. Version and SHA-256 are literals in job `env`, checked with `sha256sum -c`. The binary goes to `$RUNNER_TEMP/git-pkgs` and is added to `$GITHUB_PATH`. The checkout and setup-go actions are pinned to the same commit SHAs as the `check` job.
- Excepted differences and the decision each rests on ("Lead decisions for M1"):
  - glow, 13 events: go.mod `replace` directives (local `=> ../charm`, `../boba`, `../bubbles`, and the fork `github.com/muesli/gitcha => github.com/meowgorithm/gitcha`). upstreamwatch applies them and git-pkgs ignores them. Decision: go.mod is parsed with modfile because the manifests library ignores `replace`.
  - cleo, 21 events at 0919e782, 016e0d25, 1e8c6398 and 10363c14: poetry.lock lists one package two or three times with different versions for different Python markers. Examples at 0919e782: colorama 0.4.1/0.4.3, more-itertools 5.0.0/7.2.0/8.2.0. git-pkgs reports every entry. upstreamwatch keeps the first and reports a change when the first is dropped (10363c14). Decision: the first occurrence in the file wins.
  - cleo, 50 events at acb16a77 (PEP 621 move) and fd264efa (dependency-groups migration): only `groups` and `markers` changed in poetry.lock, and git-pkgs reports `modified` with an empty previous_requirement. Decision: a change of only the direct flag or scope emits no event.

Outside scope:
- The go-test template `templates/referee-tests.md` disagrees with this work order in four places. It uses build tag `referee` (the WO says `e2e`), test names `TestReferee*` (WO: `TestParity`), fails on a missing binary (WO: skip), and pins fixtures to a commit listed in `eval/fixtures/` (WO: clone HEAD). I followed the work order. The skill template needs updating, or the WO choice needs recording as a decision.
- docs rows to sync: `docs/development.md` could list `make e2e`, and the CI job is new. Nothing in `docs/` names the e2e target today.
- Not pinning the fixtures means a new upstream commit to glow or cleo can add differences or make an exception stale, so `make e2e` and the CI job can start failing without any change here. Pinning would need an exact commit per fixture, which the WO does not give.

Concerns:
The cleo subtest fails on 24 upstreamwatch-only and 33 git-pkgs-only events. No recorded decision explains them, so I added no exceptions for them (requirement 9). All 57 come from PyPI package-name capitalisation, in two forms:

1. Name spelling on events (most of the 57). Example: at 08bcb3c078c13fd1a2ff91a52b0b29e55024942c poetry.lock, upw reports `Babel changed 2.14.0` and git-pkgs reports `babel changed 2.14.0`. Commit, manifest, change and requirement are identical; only the case of the name differs. Cause: `internal/store/timeline.go` `upsertPackages` keeps one `packages.name` per PURL, written from the first event of the run. `ListEvents` reads the name from that row, so every event of `pkg:pypi/babel` reads back as "Babel", the spelling of the oldest lock, even after the lock switched to lowercase at bfcdd721. git-pkgs reports the name as written at each commit.
2. Case-only renames. At bfcdd7213230d347ee4dcffadd255b7785adeb8a poetry.lock, Poetry rewrote `Babel`/`Jinja2`/`MarkupSafe`/`Pygments`/`Sphinx` in lowercase. git-pkgs keys on the name string and reports `<Old> removed` plus `<new> added`. upstreamwatch keys on the PURL, which purl normalises to lowercase. It reports `changed` where the version also moved (Babel 2.10.3 -> 2.11.0, Sphinx 5.2.3 -> 5.3.0) and nothing where it did not (Jinja2 3.1.2, MarkupSafe 2.1.1, Pygments 2.13.0). The same happens in pyproject.toml at 34aa73b0a1aa5fc1c1f8ca347118a9ea4a33c96c (`Sphinx ^5.2.3` -> `sphinx ^7.1`).

Which side is wrong: on identity, upstreamwatch is right. PyPI names are case-insensitive (PEP 503), so a case-only rename is not a removal plus an addition. git-pkgs' remove+add is an artefact of keying on the raw string. On the name field, upstreamwatch's event output does not show the name as written at that commit. That is a property of the store design (one name per package), not a parser bug. Two ways out, both for the lead or owner:
- (a) Record a decision that packages are identified by PURL, the stored name is a display name, and PyPI names compare case-insensitively. That would justify exceptions or a PEP 503 name normalisation on both sides of the test.
- (b) Change upstreamwatch to keep the name per event. Option (a) needs only a test change after the decision.

Smallest reproductions:
- Name field: cleo 08bcb3c078c13fd1a2ff91a52b0b29e55024942c `poetry.lock`, package babel: upw `Babel`, git-pkgs `babel`.
- Case-only rename: cleo bfcdd7213230d347ee4dcffadd255b7785adeb8a `poetry.lock`, package Jinja2/jinja2 3.1.2: git-pkgs `Jinja2 removed` and `jinja2 added 3.1.2`, upw no event.

Until one of these lands, `make e2e` exits 2 on cleo, and the new CI `e2e` job will fail on every push to main.

## Fix round 1

Status: DONE
Work order: .claude/plans/m1/WO-10-gitpkgs-parity.md (Rulings of 2026-11-19)
Files changed:
- cmd/upw/parity_e2e_test.go (modified)
- .claude/plans/m1/WO-10-gitpkgs-parity.report.md (modified: this section)

Commands run:
- `git ls-remote https://github.com/charmbracelet/glow HEAD` -> exit 0
  `8b0600fa445535b9c53194c0d4f7fe3cf700ef3e	HEAD`
- `git ls-remote https://github.com/python-poetry/cleo HEAD` -> exit 0
  `30df50748e7cf52195cf2f22262b432e6320fe21	HEAD`
- `go vet -tags e2e ./cmd/upw/` -> exit 0 (no output)
- `make e2e` -> exit 0
  The `=== RUN`, `=== PAUSE` and `=== CONT` lines of subtests are removed; nothing else is changed:
  ```
  go test -tags e2e -race -count=1 -run TestParity -v -timeout 20m ./cmd/upw/
  === RUN   TestParity
      parity_e2e_test.go:455: glow: timeline stats map[commits_walked:259 events_written:655 failed:0 files_read:274 files_skipped:0 intervals:589 parse_errors:0 resets:0 skipped:0 targets:1]
      parity_e2e_test.go:458: glow: 873 first-parent commits, 2 merges; git-pkgs 633 raw events; upw 641, git-pkgs 632 normalised; 13 excused
      parity_e2e_test.go:455: cleo: timeline stats map[commits_walked:213 events_written:705 failed:0 files_read:296 files_skipped:0 intervals:597 parse_errors:0 resets:0 skipped:0 targets:1]
      parity_e2e_test.go:458: cleo: 506 first-parent commits, 37 merges; git-pkgs 914 raw events; upw 621, git-pkgs 686 normalised; 71 excused
  --- PASS: TestParity (6.80s)
      --- PASS: TestParity/glow (3.20s)
      --- PASS: TestParity/cleo (3.60s)
  === RUN   TestParityCompare
  === RUN   TestParityPackageName
  === RUN   TestParityNormalisePyPIRespellings
  --- PASS: TestParityNormalisePyPIRespellings (0.00s)
  --- PASS: TestParityCompare (0.00s)
      --- PASS: TestParityCompare/exceptions_excuse_both_sides (0.00s)
      --- PASS: TestParityCompare/requirement_differs (0.00s)
      --- PASS: TestParityCompare/exception_that_matches_nothing_fails (0.00s)
      --- PASS: TestParityCompare/other_fixture's_exception_is_ignored (0.00s)
      --- PASS: TestParityCompare/multiset_counts_duplicates (0.00s)
      --- PASS: TestParityCompare/equal (0.00s)
      --- PASS: TestParityCompare/exception_excuses_its_side_only (0.00s)
  --- PASS: TestParityPackageName (0.00s)
      --- PASS: TestParityPackageName/pypi/Jinja2 (0.00s)
      --- PASS: TestParityPackageName/docker/Library/Some_Image (0.00s)
      --- PASS: TestParityPackageName/pypi/a__b-.c (0.00s)
      --- PASS: TestParityPackageName/golang/github.com/BurntSushi/toml (0.00s)
      --- PASS: TestParityPackageName/pypi/Typing_Extensions (0.00s)
      --- PASS: TestParityPackageName/pypi/zope.interface (0.00s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	8.252s
  ```
- `make check` -> exit 0
  ```
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	(cached)
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
  ```
- `git status --porcelain` -> exit 0
  ```
   M .claude/plans/m1/WO-10-gitpkgs-parity.md
   M .github/workflows/ci.yml
   M Makefile
  ?? .claude/plans/m1/WO-10-gitpkgs-parity.report.md
  ?? cmd/upw/parity_e2e_test.go
  ```
  The change to the work-order file is the lead's Rulings edit. I did not touch it.

Decisions:
- PEP 503 normalisation, in `packageName(ecosystem, name)`: lower case, then each run of `[-_.]` becomes `-`. It applies only when the ecosystem is `pypi`, on both sides, before comparing. Other ecosystems keep their names unchanged.
- Collapsing respellings, in `collapseRespellings`: this runs on git-pkgs PyPI events after their names are normalised. A `removed` and an `added` at the same commit and manifest with the same normalised name form a pair. If the two requirements are equal, both events are dropped. If they differ, the pair becomes one `modified` event with the added event's requirement, which normalisation then maps to `changed`. When one key has several pairs, they are matched in file order. Following the ruling, the collapse also applies when the raw names were already identical.
- Fixture pins: after `git clone`, the test runs `git reset --hard --quiet <commit>` in the clone, so git-pkgs, `rev-list` and the file:// sync all see the pinned branch. Each pin is a `commit` field in `parityFixtures` with a "Default-branch HEAD on 2026-11-19" comment. glow is pinned to 8b0600fa445535b9c53194c0d4f7fe3cf700ef3e and cleo to 30df50748e7cf52195cf2f22262b432e6320fe21. Both are the HEADs that round 0 compared, and the event counts are unchanged.
- No exception was added or removed. The 36 entries from round 0 still each match a difference: glow 13 excused, cleo 71 excused.
- New unit tests: `TestParityPackageName` (6 rows) and `TestParityNormalisePyPIRespellings`. The second covers four cases: same version gives no event, a new version gives `changed`, a lone event is only renamed, and events in different manifests or in golang are not collapsed. It also checks upw-side name normalisation.

Outside scope: none new. The round-0 notes on the go-test referee template were ruled on (keep `e2e` and the skip); the template itself still says `referee`.

## Fix round 2

Status: DONE
Work order: .claude/plans/m1/WO-10-gitpkgs-parity.md (worktree fast-forwarded to main at ce1c74b, which includes WO-12)
Files changed:
- cmd/upw/parity_e2e_test.go (modified)
- .claude/plans/m1/WO-10-gitpkgs-parity.report.md (modified: this section)

Commands run:
- `git log --oneline -3` -> exit 0
  ```
  ce1c74b Merge branch 'worktree-agent-ac108369710dee19b'
  e263429 timeline, config: direct-flag changes emit events; excluded paths (WO-12)
  2feea45 web: /repos and /repos/{slug} timeline pages (WO-09)
  ```
- `make e2e` before any edit -> exit 2. glow failed; cleo passed. Relevant lines:
  ```
  glow: upw 648 events, git-pkgs 632 events (normalised); 13 differences excused
  only in upw (7):
    1a13810c340b8833627eecf08c100c3b39c71f31 go.mod golang github.com/charmbracelet/lipgloss changed v1.1.1-0.20250404203927-76690c660834
    19c6af747f4edd867cb70e0794d53255bfeb2745 go.mod golang github.com/fsnotify/fsnotify changed v1.6.0
    6b365eea95f7541d4af441d971010b09f6082a0e go.mod golang github.com/muesli/cancelreader changed v0.2.2
    e0bd2dd466dfaef5230000a46de0c9c877f76f7c go.mod golang github.com/muesli/gitcha changed v0.0.0-20200714001838-c071c3c6c6e1
    6b365eea95f7541d4af441d971010b09f6082a0e go.mod golang github.com/rivo/uniseg changed v0.4.7
    1f2b3671c715f4a84d8103f617ee35d821fb347b go.mod golang golang.org/x/crypto changed v0.0.0-20201221181555-eec23a3978ad
    1f2b3671c715f4a84d8103f617ee35d821fb347b go.mod golang golang.org/x/term changed v0.0.0-20201210144234-2321bbc49cbf
  only in git-pkgs (0):
  --- FAIL: TestParity/glow (2.64s)
  ```
  Timeline stats reported `files_excluded:0` for both fixtures, so the excluded-paths decision changed nothing here.
- I compared the require line in go.mod at each of the 7 commits against its first parent, fetching the files from raw.githubusercontent.com. Each line has the same version on both sides; only a trailing `// indirect` is added or removed:
  ```
  1a13810c parent ['github.com/charmbracelet/lipgloss v1.1.1-0.20250404203927-76690c660834']
  1a13810c commit ['github.com/charmbracelet/lipgloss v1.1.1-0.20250404203927-76690c660834 // indirect']
  19c6af74 parent ['github.com/fsnotify/fsnotify v1.6.0 // indirect']
  19c6af74 commit ['github.com/fsnotify/fsnotify v1.6.0']
  6b365eea parent ['github.com/muesli/cancelreader v0.2.2 // indirect']
  6b365eea commit ['github.com/muesli/cancelreader v0.2.2']
  e0bd2dd4 parent ['github.com/muesli/gitcha v0.0.0-20200714001838-c071c3c6c6e1 // indirect']
  e0bd2dd4 commit ['github.com/muesli/gitcha v0.0.0-20200714001838-c071c3c6c6e1']
  6b365eea parent ['github.com/rivo/uniseg v0.4.7 // indirect']
  6b365eea commit ['github.com/rivo/uniseg v0.4.7']
  1f2b3671 parent ['golang.org/x/crypto v0.0.0-20201221181555-eec23a3978ad']
  1f2b3671 commit ['golang.org/x/crypto v0.0.0-20201221181555-eec23a3978ad // indirect']
  1f2b3671 parent ['golang.org/x/term v0.0.0-20201210144234-2321bbc49cbf // indirect']
  1f2b3671 commit ['golang.org/x/term v0.0.0-20201210144234-2321bbc49cbf']
  ```
- `go vet -tags e2e ./cmd/upw/` -> exit 0 (no output)
- `make e2e` after the edits -> exit 0. Output below, with the `=== RUN`, `=== PAUSE` and `=== CONT` lines of subtests removed:
  ```
  go test -tags e2e -race -count=1 -run TestParity -v -timeout 20m ./cmd/upw/
  === RUN   TestParity
      parity_e2e_test.go:466: glow: timeline stats map[commits_walked:259 events_written:662 failed:0 files_excluded:0 files_read:274 files_skipped:0 intervals:596 parse_errors:0 resets:0 skipped:0 targets:1]
      parity_e2e_test.go:469: glow: 873 first-parent commits, 2 merges; git-pkgs 633 raw events; upw 648, git-pkgs 632 normalised; 20 excused
      parity_e2e_test.go:466: cleo: timeline stats map[commits_walked:213 events_written:705 failed:0 files_excluded:0 files_read:296 files_skipped:0 intervals:597 parse_errors:0 resets:0 skipped:0 targets:1]
      parity_e2e_test.go:469: cleo: 506 first-parent commits, 37 merges; git-pkgs 914 raw events; upw 621, git-pkgs 686 normalised; 71 excused
  --- PASS: TestParity (6.07s)
      --- PASS: TestParity/glow (2.73s)
      --- PASS: TestParity/cleo (3.34s)
  === RUN   TestParityCompare
  === RUN   TestParityPackageName
  === RUN   TestParityNormalisePyPIRespellings
  --- PASS: TestParityNormalisePyPIRespellings (0.00s)
  --- PASS: TestParityPackageName (0.00s)
      (6 subtests PASS)
  --- PASS: TestParityCompare (0.00s)
      (7 subtests PASS)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	7.441s
  ```
- `make check` -> exit 0
  ```
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	2.570s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.297s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.972s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.707s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.721s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	2.715s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.678s
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
- `git status --porcelain` -> exit 0
  ```
   M .claude/plans/m1/WO-10-gitpkgs-parity.md
   M .github/workflows/ci.yml
   M Makefile
  ?? .claude/plans/m1/WO-10-gitpkgs-parity.report.md
  ?? cmd/upw/parity_e2e_test.go
  ```
  The work-order file change is the lead's Rulings edit; I did not touch it.

Decisions:
- I added 7 exceptions, glow only and upw side only. Each one names a commit, `go.mod`, a package and the change `changed`. Each cites the new direct-flag decision through `reasonDirect`: "only the go.mod // indirect comment changed: a change of the direct flag emits changed".
- I removed no exceptions. All 36 from earlier rounds still match a difference (glow 13 of 20 excused, cleo 71), and the test would fail on any exception that matched nothing.
- I reworded `reasonScopeOnly` to follow the revised decision text: "a change of scope alone emits no event". The old wording still mentioned the direct flag. The two cleo scope-only exceptions stand, because those commits change only poetry.lock `groups` and `markers` lines.
- The excluded-paths decision explains no difference in either fixture, since `files_excluded` is 0 in both. No exception cites it.

Outside scope: none new.

## Fix round 3

Status: DONE
Work order: .claude/plans/m1/WO-10-gitpkgs-parity.md (review minors 1-4)
Files changed:
- cmd/upw/parity_e2e_test.go (modified)
- .github/workflows/ci.yml (modified: `timeout-minutes: 30` on the `e2e` job)
- .claude/plans/m1/WO-10-gitpkgs-parity.report.md (modified: this section)

Commands run:
- I made a temporary listing of every difference. The test was edited locally so that `compareParity` got nil exceptions and `maxListedDiffs` was 5000. I ran `go test -tags e2e -count=1 -run 'TestParity$' -v ./cmd/upw/` once and saved its output to my scratchpad. The file was then restored from a byte copy taken just before; the later `grep` shows `maxListedDiffs = 50` and `parityExceptions` passed again. Result: glow 18 upw-only and 2 git-pkgs-only differences, cleo 3 upw-only and 68 git-pkgs-only, 91 in total. These are the same 91 events that the old 36 exceptions excused (20 + 71).
- `go vet -tags e2e ./cmd/upw/` -> exit 0 (no output)
- `make e2e` -> exit 0. Output below, with the `=== RUN`, `=== PAUSE`, `=== CONT` and `--- PASS` lines of unit-test subtests removed:
  ```
  go test -tags e2e -race -count=1 -run TestParity -v -timeout 20m ./cmd/upw/
  === RUN   TestParity
      parity_e2e_test.go:508: glow: timeline stats map[commits_walked:259 events_written:662 failed:0 files_excluded:0 files_read:274 files_skipped:0 intervals:596 parse_errors:0 resets:0 skipped:0 targets:1]
      parity_e2e_test.go:511: glow: 873 first-parent commits, 2 merges; git-pkgs 633 raw events; upw 648, git-pkgs 632 normalised; 20 excused
      parity_e2e_test.go:508: cleo: timeline stats map[commits_walked:213 events_written:705 failed:0 files_excluded:0 files_read:296 files_skipped:0 intervals:597 parse_errors:0 resets:0 skipped:0 targets:1]
      parity_e2e_test.go:511: cleo: 506 first-parent commits, 37 merges; git-pkgs 914 raw events; upw 621, git-pkgs 686 normalised; 71 excused
  --- PASS: TestParity (5.96s)
      --- PASS: TestParity/glow (2.65s)
      --- PASS: TestParity/cleo (3.30s)
  === RUN   TestParityCompare
  === RUN   TestParityPackageName
  === RUN   TestParityNormalisePyPIRespellings
  --- PASS: TestParityNormalisePyPIRespellings (0.00s)
  --- PASS: TestParityPackageName (0.00s)
  --- PASS: TestParityCompare (0.00s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	7.368s
  ```
- `make check` -> exit 0
  ```
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	(cached)
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
  ```
- `git status --porcelain` -> exit 0
  ```
   M .claude/plans/m1/WO-10-gitpkgs-parity.md
   M .github/workflows/ci.yml
   M Makefile
  A  cmd/upw/parity_e2e_test.go
  ?? .claude/plans/m1/WO-10-gitpkgs-parity.report.md
  ```
  Someone other than me staged the test file (`A`) between rounds. I ran no git write. The staged copy is the round-2 version, and my round-3 edits are in the working tree.

Decisions:
- Minors 1 and 2: the exception table now has 91 entries, one per differing event, generated from the full listing above. Every entry sets Fixture, Side, SHA, ManifestPath, Ecosystem, Name and Change. Every entry except a `removed` sets Requirement too; normalisation stores a `removed` event's requirement as empty, so it is matched exactly anyway.
  - The entries break down as replace 13, direct flag 7, first occurrence wins 21 and scope only 50.
  - The 50 scope-only entries list each package and version at the two commits, so a real version bump there would no longer be excused.
  - The run excuses exactly 91 differences, and an entry that matched nothing would fail the test. So each entry matches exactly one event.
  - The constants that named packages and commits more than once are gone, because every entry is now written out in full.
- Minor 3: `minParityEvents = 100`. TestParity fails a fixture when either side has fewer than 100 normalised events. The check runs before the comparison and is reported separately from it. The current counts are glow 648 upw / 632 git-pkgs and cleo 621 / 686.
- Minor 4: `timeout-minutes: 30` is on the CI `e2e` job.
- The respelling collapse is unchanged, as ruled.

Outside scope: none new.

## Gate (lead, 2026-11-19)

Round 0 DONE_WITH_CONCERNS (57 unexplained cleo differences, all PyPI name respellings) resolved by the PyPI identity decision and the normalisation ruling; fixtures pinned. Round 2 reran after WO-12 (7 direct-flag exceptions). Sonnet reviewer: ACCEPT with minors (broad scope-only and replace/first-wins entries, no event floor, no CI timeout), all fixed in round 3: 91 single-event exceptions, a 100-event floor, timeout-minutes 30. Lead reran make check (exit 0) and make e2e (glow 20 excused, cleo 71 excused, both PASS) after round 3. Accepted.

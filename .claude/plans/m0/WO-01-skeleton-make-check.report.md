Status: DONE
Work order: .claude/plans/m0/WO-01-skeleton-make-check.md
Files changed:
- LICENSE (created)
- Makefile (created)
- .go-arch-lint.yml (created)
- scripts/check-imports.sh (created)
- cmd/upw/main.go (created)
- cmd/upw/main_test.go (created)
- internal/config/doc.go (created)
- internal/model/doc.go (created)
- internal/store/doc.go (created)
- internal/pipeline/doc.go (created)
- internal/sched/doc.go (created)
- internal/web/doc.go (created)

Commands run:
- `curl -sSL https://www.apache.org/licenses/LICENSE-2.0.txt -o LICENSE && shasum -a 256 LICENSE` -> exit 0
  cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30  LICENSE
  (same hash as the copies in the local Go module cache, e.g. cloud.google.com/go@v0.120.0/LICENSE)

- `make check` (clean tree, final run) -> exit 0
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/config	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/model	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/pipeline	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/sched	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/store	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/web	[no test files]
  ==> arch-lint
  go run github.com/fe3dback/go-arch-lint@v1.19.2 check --output-color=false
  module: github.com/Amrit-Code/upstreamwatch
  linters:
     On | Base: component imports # always on
     On | Advanced: vendor imports # switch 'allow.depOnAnyVendor = false' (or delete) to on
     On | Advanced: method calls and dependency injections # switch 'allow.deepScan = true' (or delete) to on

  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh

- `go test -race -count=1 -v ./cmd/upw` -> exit 0
  --- PASS: TestRunNoArgs (0.00s)
      --- PASS: TestRunNoArgs/empty (0.00s)
      --- PASS: TestRunNoArgs/nil (0.00s)
  --- PASS: TestRunUnknownCommand (0.00s)
      --- PASS: TestRunUnknownCommand/unknown_word (0.00s)
      --- PASS: TestRunUnknownCommand/extra_arguments (0.00s)
      --- PASS: TestRunUnknownCommand/unknown_flag (0.00s)
  --- PASS: TestRunHelp (0.00s)
      --- PASS: TestRunHelp/help_command (0.00s)
      --- PASS: TestRunHelp/short_flag (0.00s)
      --- PASS: TestRunHelp/long_flag (0.00s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.107s

- Clean-clone check: copied the owned files plus go.mod and .gitignore into a scratch directory, then
  `GOMODCACHE=<empty> GOCACHE=<empty> GOFLAGS=-modcacherw make check` -> exit 0
  40 "go: downloading" lines (go-arch-lint and its dependencies fetched from the proxy), then the same
  six `==>` steps as above ending in "OK - No warnings found" and import-check passing.

- `go list ./...` -> exit 0
  github.com/Amrit-Code/upstreamwatch/cmd/upw
  github.com/Amrit-Code/upstreamwatch/internal/config
  github.com/Amrit-Code/upstreamwatch/internal/model
  github.com/Amrit-Code/upstreamwatch/internal/pipeline
  github.com/Amrit-Code/upstreamwatch/internal/sched
  github.com/Amrit-Code/upstreamwatch/internal/store
  github.com/Amrit-Code/upstreamwatch/internal/web

- `make build && ./bin/upw -h; echo $?` -> 0
  go build -o bin/upw ./cmd/upw
  usage: upw <command> [flags]

  upw has no commands yet; they are added milestone by milestone.

  Run "upw -h" to print this help.
  0

- `./bin/upw; echo $?` -> 2 (stdout empty when stderr is discarded; usage on stderr)
  usage: upw <command> [flags]

  upw has no commands yet; they are added milestone by milestone.

  Run "upw -h" to print this help.
  2

- `./bin/upw bogus 1>/dev/null; echo $?` -> 2
  upw: unknown command "bogus"

  usage: upw <command> [flags]
  ...
  2

- `head -3 go.mod` -> exit 0
  module github.com/Amrit-Code/upstreamwatch

  go 1.27

- `make fmt` -> exit 0 (gofmt -w .; no file changed)
- `gofmt -l .` -> exit 0, no output

- Experiment 1: `internal/model/tmp.go` importing `database/sql`, `make check` -> exit 2 at import-check
  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
  check-imports: forbidden standard-library imports:
  github.com/Amrit-Code/upstreamwatch/internal/model imports database/sql (only internal/store may)
  make: *** [check] Error 1

- Experiment 2: `internal/model/tmp.go` importing `.../internal/store`, `make check` -> exit 2 at arch-lint
  Component model shouldn't depend on github.com/Amrit-Code/upstreamwatch/internal/store in /Users/amrit/Documents/upstreamwatch/internal/model/tmp.go:3
  --
  total notices: 1
  exit status 1
  make: *** [check] Error 1

- Experiment 3: `internal/pipeline/tmp.go` importing `.../internal/sched`, `make check` -> exit 2 at arch-lint
  Component pipeline shouldn't depend on github.com/Amrit-Code/upstreamwatch/internal/sched in /Users/amrit/Documents/upstreamwatch/internal/pipeline/tmp.go:3
  --
  total notices: 1
  exit status 1
  make: *** [check] Error 1

- Experiment 4: `internal/web/tmp.go` importing `os/exec`, `make check` -> exit 2 at import-check
  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
  check-imports: forbidden standard-library imports:
  github.com/Amrit-Code/upstreamwatch/internal/web imports os/exec (only internal/gitsrc and internal/testutil may)
  make: *** [check] Error 1

  Each tmp.go was removed right after its run; `make check` on the clean tree then exited 0 and
  `ls internal/*/` shows only doc.go in each package.

- check-imports.sh sanity runs in a scratch module (copy of the script):
  allowed cases (internal/store importing database/sql/driver, internal/gitsrc, internal/testutil and
  internal/testutil/gitfixture importing os/exec, a _test.go in internal/web importing os/exec) -> exit 0;
  look-alike prefixes internal/storex (database/sql/driver) and internal/testutilx (os/exec) -> exit 1, both named;
  a package directory with two package clauses -> "check-imports: go list ./... failed", exit 1.

- `go run github.com/fe3dback/go-arch-lint@v1.19.2 check` with the 44-component config and no
  ignoreNotFoundComponents -> exit 1, one error per missing package, first one:
  not found directories for 'internal/httpx' in '/Users/amrit/Documents/upstreamwatch/internal/httpx'

- `git status --porcelain` -> exit 0
   M .gitignore
  ?? .claude/plans/
  ?? .go-arch-lint.yml
  ?? LICENSE
  ?? Makefile
  ?? cmd/
  ?? internal/
  ?? scripts/
  (the .gitignore change is not mine; see Outside scope)

Decisions:
- Components for M1 to M5 packages: kept in .go-arch-lint.yml. v1.19.2 rejects them by default ("not found directories for 'internal/httpx' in '.../internal/httpx'", one per missing package) but accepts them with `allow.ignoreNotFoundComponents: true`, which I set. This loses no safety, because any project file that matches no component fails the check ("File /internal/notify/x/deep/d.go not attached to any component in archfile"), so a mistyped component path cannot hide a real package.
- Behaviour 1 (same-component imports): REJECTED by v1.19.2 ("Component grp shouldn't depend on example.com/exp/internal/grp/b"), the opposite of the sketch's assumption; they are allowed only if the component lists itself in mayDependOn. One component per service and adapter kept anyway; subpackages of `extract/**` or `testutil/**` cannot import each other.
- Behaviour 2 (globs): `dir/*` matches direct children only, not `dir` itself and not grandchildren; `dir/**` matches `dir` itself and every depth (confirmed with the `mapping` command).
- Behaviour 3 (excludeFiles): `^.*_test\.go$` excludes only the test files; the non-test files of the same package are still checked (a violating svc.go was reported while the violating svc_test.go was not; without the rule both were reported).
- Behaviour 4 (running the tool): `go run github.com/fe3dback/go-arch-lint@v1.19.2 check` works with Go 1.27.4, needs no install and leaves go.mod untouched; it fetched cleanly into an empty module cache.
- Empty deps entries such as `grp: {}` are rejected ("should have ref in 'mayDependOn'/'canUse' or at least one flag of ['anyProjectDeps', 'anyVendorDeps']"), so components needing only `model` are omitted from `deps`, which the tool accepts.
- Vendors not yet in go.mod (yaml, sqlite, cron and the rest) are accepted in `vendors`; a disallowed vendor import is reported ("Component other shouldn't depend on go.yaml.in/yaml/v3") and an allowed one passes, checked in a scratch module.
- `--output-color=false` is passed to go-arch-lint: it prints ANSI colour codes even when not on a terminal, which made CI logs and reports unreadable. It changes display only.
- `run` keeps the parameter names `ctx` and `getenv` from the Produced signature although no subcommand uses them yet; the doc comment says so.
- Usage text: "usage: upw <command> [flags]", a line saying there are no commands yet and that they are added milestone by milestone, and a pointer to `upw -h`.
- The gofmt step fails if gofmt itself errors, as well as when it lists files.
- check-imports.sh derives the module path with `go list -m` instead of hard-coding it, cds to the repository root from its own location, and prints violations to stderr.
- LICENSE is the apache.org LICENSE-2.0.txt byte for byte (sha256 above).

Outside scope:
- `.gitignore` shows as modified (the `upw.dev.yaml` line removed; file mtime 12:00, after my first read of it at the start of the run showed the line present). I never wrote it. Someone else changed it during this run; please confirm the change is intended before committing.
- `.claude/skills/upw-architecture/references/ARCH-LINT.md`, "Behaviours to confirm at build time" item 1, assumes same-component imports are allowed; v1.19.2 rejects them. Item 4 still lists `go install` or a `tool` directive; the decision is `go run <module>@<version>`. The sketch's empty `mayDependOn: [ ]` entries and its comment about empty deps entries should also note that `{}` is rejected.
- `docs/development.md` ("make check") lists gofmt, vet, build, race tests and the layering lint but not the standard-library import check that is now the sixth step. For docs-sync.

Concerns: none
Question or blocker: none

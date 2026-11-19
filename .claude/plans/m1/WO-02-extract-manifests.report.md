Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m1/WO-02-extract-manifests.md
Files changed:
- internal/extract/extract.go (created)
- internal/extract/gomod.go (created)
- internal/extract/dockerfile.go (created)
- internal/extract/compose.go (created)
- internal/extract/library.go (created)
- internal/extract/extract_test.go (created)
- internal/extract/gomod_test.go (created)
- internal/extract/dockerfile_test.go (created)
- internal/extract/compose_test.go (created)
- internal/extract/library_test.go (created)
- internal/extract/testdata/uv.lock (created)
- internal/extract/testdata/poetry.lock (created)

Commands run:
- `make check` -> exit 0
  ```
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.603s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.260s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.169s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	1.210s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.603s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/ingest	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.254s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.197s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.554s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.513s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.480s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	2.937s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/timeline	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.248s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.241s
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
  ```
- `go test -race -count=1 -run 'TestPatterns|TestParse|TestDockerPURL' -v ./internal/extract/` -> exit 0
  (top-level results; subtests all PASS)
  ```
  --- PASS: TestPatterns (0.00s)
  --- PASS: TestParse_Actions (0.00s)
  --- PASS: TestDockerPURLMatchesLibrary (0.00s)
  --- PASS: TestParse_Unsupported (0.00s)
  --- PASS: TestParse_Compose (0.00s)
  --- PASS: TestParse_Dockerfile (0.00s)
  --- PASS: TestParse_GoMod (0.00s)
  --- PASS: TestParseImageRef (0.01s)
  --- PASS: TestParse_InvalidContent (0.01s)
  --- PASS: TestParse_Python (0.00s)
  --- PASS: TestParse_PURLVersionless (0.00s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.173s
  ```
- `go list -f '{{join .Imports " "}}' ./internal/extract/` -> exit 0
  ```
  errors fmt github.com/Amrit-Code/upstreamwatch/internal/model github.com/git-pkgs/manifests go.yaml.in/yaml/v3 golang.org/x/mod/modfile log/slog path regexp strings
  ```
  No `os`, `net`, `net/http` or `os/exec`.
- `git status --porcelain` -> exit 0 (run before this report was written; the report file is the only addition since)
  ```
  ?? internal/extract/
  ```
- `go test -race -count=1 -coverprofile=... ./internal/extract/` -> exit 0, coverage 95.8% of statements.

Decisions:
- Docker PURL: obtained by calling `manifests.Parse("Dockerfile", "FROM <name>\n")` and taking its PURL (version stripped), so our PURL is by construction what the library gives an untagged reference, including the library's percent-encoding of characters such as `+`, `%`, `#`. The `git-pkgs/purl` package cannot be imported (not in the extract component's `canUse`). A name the library reads differently (for example containing whitespace) is a parse error.
- Actions `docker://` steps: the library does return a PURL for them (`pkg:githubactions/docker:/`, malformed), contrary to requirement 15's premise. They are skipped by name prefix `docker://`, in addition to skipping any library entry with an empty PURL; requirement 15 names `docker://` as skipped.
- Library entries without a PURL are skipped for every library-parsed format, not only Actions (an entry without a package identity cannot be stored).
- Scope for uv.lock, poetry.lock and Actions entries is taken from the library (it reports `runtime`), as requirement 13 does for requirements and pyproject; Direct for Actions comes from the library (`DirectYes`).
- Exact pin: surrounding whitespace is ignored, so `== 3.0` gives Version `3.0`; Requirement stays as the library returned it (`== 3.0`).
- go.mod replace lookup: a replace naming the exact old version is preferred over a path-wide replace for the same module, as in the go command.
- go.mod "toolchain only" Requirement uses the value as written: `toolchain go1.25.1`.
- The pkg:golang/toolchain dependency is appended after the require lines.
- Derived toolchain: the regexp is anchored at the tag start with no boundary check, read literally ("starts with"), so a tag such as `1.26rc1` would derive `1.26`.
- Dockerfile: a FROM with no image, or with extra words other than `AS <name>`, is a parse error ("malformed FROM instruction"); comment and blank lines inside a continuation are skipped as the Docker builder does; `scratch` and stage names compare case-insensitively; a stage name is registered after its own line is checked, so `FROM x AS x` still names the image `x`.
- Compose: services are read from a `yaml.Node` to keep file order, each service decoded into a small struct so anchors, aliases and `<<` merge keys resolve; a non-mapping `services`, a non-string image, or an empty tag/digest/name in a reference is a parse error; a null or absent `services` yields nothing.
- Logger use: Debug lines `image with a variable skipped` (attrs `path`, `image`) and `docker action skipped` (attrs `path`, `uses`). Nothing logged at Info or above.
- Patterns() returns a fresh copy on every call.

Outside scope:
- The manifests library lowercases golang PURLs (`github.com/BurntSushi/toml` -> `pkg:golang/github.com/burntsushi/toml`). This package follows requirement 6/3 literally and keeps the module path's case (`pkg:golang/github.com/BurntSushi/toml`); Go module paths are case-sensitive and OSV Go entries use the real case. Any later code that builds golang PURLs through the library (for example the Unity extractor or M2 matching) would produce a different identity for the same module.
- `go.mod` still marks `github.com/git-pkgs/manifests` and `golang.org/x/mod` `// indirect`; the package now imports both directly, so the lead's planned `go mod tidy` will flip them.
- docs/decisions.md says the library "reports build-stage names (`FROM builder`) as images and drops the tag of an image pinned by digest"; both confirmed in v0.14.0 (it returns Version `sha256:...` for `alpine@sha256:...` and no tag for `node:22@sha256:...`). No doc change needed.

Concerns:
- uv.lock: the library reports the project itself (the `source = { editable = "." }` package, here `example-app`) as a dependency. It is kept, since the work order asks for no filtering, and `TestParse_Python` pins that behaviour. Upstream mapping or advisory matching could then attach a public PyPI package of the same name to the project. The lead may want a rule that drops editable or virtual root packages.
- Requirement 15's assumption about the library (no PURL for `docker://`) is wrong for v0.14.0; handled as described under Decisions, but the work order text should be corrected if it is reused.

## Fix round 1

Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m1/WO-02-extract-manifests.md (Rulings of 2026-11-19; worktree at 7e053d2)
Files changed:
- internal/extract/library.go (modified: poetry.lock local-path entries dropped; `isLocalSource` added)
- internal/extract/library_test.go (modified: poetry.lock expectations, uv.lock comment, `TestIsLocalSource`)
- internal/extract/gomod_test.go (modified: `TestGoPURLMatchesLibrary`)
- internal/extract/testdata/poetry.lock (modified: directory, file and git packages added)

Commands run:
- `make check` -> exit 0
  ```
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.710s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.547s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.139s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.401s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.825s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/ingest	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.221s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.404s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.588s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.531s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.469s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	2.706s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/timeline	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.217s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.200s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.313s
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
- `go test -race -count=1 -run 'TestPatterns|TestParse|TestDockerPURL|TestGoPURL' -v ./internal/extract/` -> exit 0
  ```
  --- PASS: TestPatterns (0.00s)
  --- PASS: TestParse_Actions (0.00s)
  --- PASS: TestDockerPURLMatchesLibrary (0.00s)
  --- PASS: TestParse_Compose (0.00s)
  --- PASS: TestGoPURLMatchesLibrary (0.00s)
  --- PASS: TestParse_Unsupported (0.00s)
  --- PASS: TestParse_Dockerfile (0.00s)
  --- PASS: TestParse_GoMod (0.01s)
  --- PASS: TestParseImageRef (0.01s)
  --- PASS: TestParse_InvalidContent (0.01s)
  --- PASS: TestParse_Python (0.00s)
  --- PASS: TestParse_PURLVersionless (0.01s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.173s
  ```
- `go list -f '{{join .Imports " "}}' ./internal/extract/` -> exit 0
  ```
  errors fmt github.com/Amrit-Code/upstreamwatch/internal/model github.com/git-pkgs/manifests go.yaml.in/yaml/v3 golang.org/x/mod/modfile log/slog path regexp strings
  ```
- `git status --porcelain` -> exit 0
  ```
   M .claude/plans/m1/WO-02-extract-manifests.md
  ?? .claude/plans/m1/WO-02-extract-manifests.report.md
  ?? internal/extract/compose.go
  ?? internal/extract/compose_test.go
  ?? internal/extract/dockerfile.go
  ?? internal/extract/dockerfile_test.go
  ?? internal/extract/extract.go
  ?? internal/extract/extract_test.go
  ?? internal/extract/gomod.go
  ?? internal/extract/gomod_test.go
  ?? internal/extract/library.go
  ?? internal/extract/library_test.go
  ?? internal/extract/testdata/
  ```
  The modified work order file is the lead's Rulings edit, present uncommitted in this worktree; I did not edit it.

What the library returns (probed with manifests v0.14.0 and purl v0.2.1 in a temporary test, since removed):
- poetry.lock: `Dependency.RegistryURL` is `[package.source].url`. Directory and file packages carry a local path (`../example-lib`, `dist/...tar.gz`). Git and legacy-registry packages carry an https URL. PyPI packages carry nothing.
- uv.lock: `Dependency.RegistryURL` is `source.registry` only. Editable, virtual, path, directory, git and URL sources all come back with it empty, and every other field (Integrity, Source, Declarations, Sources) is empty as well. The library gives no way to tell a local package from a git or URL one.
- go.mod with purl v0.2.1: `manifests.Parse` now gives `pkg:golang/github.com/BurntSushi/toml`, case kept.

Decisions:
- poetry.lock entries are dropped when the library's source location is a filesystem path: non-empty and either a `file://` URL or a string with no `://`. Git, URL and alternative-registry packages are kept, with the `repository_url` qualifier stripped as before. A Debug line `local package skipped` (attrs `path`, `package`) is logged.
- The check applies to poetry.lock only. In uv.lock the same field holds the package index (`source.registry`), not the package's own source, so a local flat index there does not make its packages local.
- uv.lock: nothing is dropped, following the ruling ("if the library gives no way to tell, report that instead of guessing by name"). `TestParse_Python` keeps the editable root `example-app`, with a comment giving the reason.
- `TestGoPURLMatchesLibrary` covers `github.com/BurntSushi/toml`, a `/v2` path and `gopkg.in/yaml.v3`. Each must equal `pkg:golang/<module path>` and match what `manifests.Parse` returns.

Outside scope: none new.

Concerns:
- uv.lock still reports the project's own editable or virtual root, and any path dependencies, as PyPI packages. Fixing that needs one of the following, and each is a lead or owner call: (a) drop every uv.lock entry with an empty registry, which also loses git and URL dependencies; (b) parse uv.lock's `source` tables in this package, which needs a TOML library (`github.com/BurntSushi/toml` is in go.sum as an indirect dependency but is not approved and not in the extract component's `canUse`); (c) get the library to expose the uv source kind upstream.

## Fix round 2

Status: DONE
Work order: .claude/plans/m1/WO-02-extract-manifests.md (Rulings "2026-11-19, on fix round 1")
Files changed:
- internal/extract/uvlock.go (created: `uvLocalPackages`, `inlineTableFirstKey`)
- internal/extract/uvlock_test.go (created: `TestParse_UVLockLocalSources`, `TestParse_UVLockTyper`, `TestUVLocalPackages`)
- internal/extract/library.go (modified: uv.lock entries named by `uvLocalPackages` are dropped)
- internal/extract/library_test.go (modified: the editable `example-app` in `testdata/uv.lock` is now expected to be dropped)
- internal/extract/testdata/typer-uv.lock (created: `uv.lock` of github.com/fastapi/typer, MIT, fetched with `gh api repos/fastapi/typer/contents/uv.lock -q .content | base64 -d`; the latest commit touching that file is 9d9e0b33726d45fdaa914aa596595aa077ac468b; 2012 lines, 85 `[[package]]` tables, only `typer` has a non-registry source, `{ editable = "." }`)

Commands run:
- `gh api repos/fastapi/typer/contents/uv.lock -q .content | base64 -d > internal/extract/testdata/typer-uv.lock` -> exit 0
- `make check` -> exit 0
  ```
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.223s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	(cached)
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
  ```
- `go test -race -count=1 -run 'TestPatterns|TestParse|TestDockerPURL|TestGoPURL' -v ./internal/extract/` -> exit 0
  ```
  --- PASS: TestPatterns (0.00s)
  --- PASS: TestParse_Actions (0.00s)
  --- PASS: TestDockerPURLMatchesLibrary (0.00s)
  --- PASS: TestParse_Compose (0.00s)
  --- PASS: TestGoPURLMatchesLibrary (0.00s)
  --- PASS: TestParse_Unsupported (0.00s)
  --- PASS: TestParse_Dockerfile (0.00s)
  --- PASS: TestParse_UVLockLocalSources (0.01s)
  --- PASS: TestParse_Python (0.00s)
  --- PASS: TestParseImageRef (0.01s)
  --- PASS: TestParse_GoMod (0.00s)
  --- PASS: TestParse_PURLVersionless (0.01s)
  --- PASS: TestParse_InvalidContent (0.01s)
  --- PASS: TestParse_UVLockTyper (0.07s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.202s
  ```
- `go list -f '{{join .Imports " "}}' ./internal/extract/` -> exit 0
  ```
  errors fmt github.com/Amrit-Code/upstreamwatch/internal/model github.com/git-pkgs/manifests go.yaml.in/yaml/v3 golang.org/x/mod/modfile log/slog path regexp strconv strings
  ```
- `git status --porcelain` -> exit 0
  ```
   M .claude/plans/m1/WO-02-extract-manifests.md
  ?? .claude/plans/m1/WO-02-extract-manifests.report.md
  ?? internal/extract/compose.go
  ?? internal/extract/compose_test.go
  ?? internal/extract/dockerfile.go
  ?? internal/extract/dockerfile_test.go
  ?? internal/extract/extract.go
  ?? internal/extract/extract_test.go
  ?? internal/extract/gomod.go
  ?? internal/extract/gomod_test.go
  ?? internal/extract/library.go
  ?? internal/extract/library_test.go
  ?? internal/extract/testdata/
  ?? internal/extract/uvlock.go
  ?? internal/extract/uvlock_test.go
  ```
  The modified work order file is still the lead's Rulings edit; I did not edit it.

Decisions:
- How the scan works: it reads the content line by line, with trailing whitespace and `\r` trimmed. `[[package]]` starts a package. Any other line starting with `[` (such as `[package.metadata]`) ends that package's own keys, so keys under sub-tables are never read. Only unindented `name = ` and `source = ` lines count, so `{ name = ... }` items inside indented arrays are ignored. The name goes through `strconv.Unquote`. If that fails (for example a single-quoted literal), the name stays unknown and the package is kept. The source kind is the text between `{` and the first `=`. A source line with no `{` or no `=` on the same line is unreadable, and the package is kept. A package is marked local once both its name and a local kind (`editable`, `virtual`, `directory`, `path`) are known, in either order.
- Matching is by name, as the ruling says. Entries from the library whose `Name` is in the local set are dropped, with the same Debug line as poetry.lock (`local package skipped`, attrs `path` and `package`).
- `TestParse_UVLockTyper` asserts four things: `typer` is absent; `click` 8.3.1, `rich` 15.0.0, `shellingham` 1.5.4 and `annotated-types` 0.7.0 remain, with version-less PURLs and `DirectUnknown`; and the total is 84 (85 tables with distinct names, minus `typer`).
- The hand-written case also puts a `[package.metadata]` sub-table and an indented array that mention `editable` under the root package, to show that they do not mark anything.

Outside scope: none new.

Concerns: none. The uv.lock concern from fix round 1 is settled by the ruling and implemented.

## Fix round 3

Status: DONE
Work order: .claude/plans/m1/WO-02-extract-manifests.md (review finding: heredoc bodies parsed as instructions)
Files changed:
- internal/extract/dockerfile.go (modified: `dockerfileLines` skips heredoc bodies; `heredoc`, `heredocMarker` and `heredocs` added)
- internal/extract/dockerfile_test.go (modified: three heredoc cases in `TestParse_Dockerfile`; `TestHeredocs` added)
- internal/extract/extract_test.go (modified: `TestParse_InvalidContent` gains a malformed FROM after a closed heredoc)

Commands run:
- `make check` -> exit 0
  ```
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.227s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	(cached)
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
  ```
- `go test -race -count=1 -run 'TestPatterns|TestParse|TestDockerPURL|TestGoPURL|TestHeredocs' -v ./internal/extract/` -> exit 0
  ```
  --- PASS: TestParse_Actions (0.00s)
  --- PASS: TestPatterns (0.00s)
  --- PASS: TestParse_UVLockLocalSources (0.01s)
  --- PASS: TestDockerPURLMatchesLibrary (0.00s)
  --- PASS: TestParse_Unsupported (0.00s)
  --- PASS: TestParse_Compose (0.00s)
  --- PASS: TestParse_GoMod (0.01s)
  --- PASS: TestGoPURLMatchesLibrary (0.00s)
  --- PASS: TestParse_InvalidContent (0.01s)
  --- PASS: TestHeredocs (0.01s)
  --- PASS: TestParse_Python (0.00s)
  --- PASS: TestParseImageRef (0.01s)
  --- PASS: TestParse_PURLVersionless (0.01s)
  --- PASS: TestParse_Dockerfile (0.00s)
  --- PASS: TestParse_UVLockTyper (0.07s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.235s
  ```
- `go list -f '{{join .Imports " "}}' ./internal/extract/` -> exit 0
  ```
  errors fmt github.com/Amrit-Code/upstreamwatch/internal/model github.com/git-pkgs/manifests go.yaml.in/yaml/v3 golang.org/x/mod/modfile log/slog path regexp strconv strings
  ```
- `git status --porcelain` -> exit 0
  ```
   M .claude/plans/m1/WO-02-extract-manifests.md
   A internal/extract/compose.go
   A internal/extract/compose_test.go
   A internal/extract/dockerfile.go
   A internal/extract/dockerfile_test.go
   A internal/extract/extract.go
   A internal/extract/extract_test.go
   A internal/extract/gomod.go
   A internal/extract/gomod_test.go
   A internal/extract/library.go
   A internal/extract/library_test.go
   A internal/extract/testdata/poetry.lock
   A internal/extract/testdata/typer-uv.lock
   A internal/extract/testdata/uv.lock
   A internal/extract/uvlock.go
   A internal/extract/uvlock_test.go
  ?? .claude/plans/m1/WO-02-extract-manifests.report.md
  ```
  Someone other than me staged the owned files between rounds; I ran no git writes. The index matches the working tree; nothing shows as `AM`. The work order modification is still the lead's Rulings edit.

Decisions:
- A heredoc is opened only by a completed logical RUN, COPY or ADD line (case-insensitive). Each whitespace-separated word on that line is checked against BuildKit's heredoc word form, `^[0-9]*<<(-?)(["']?)NAME(["']?)$`, where NAME is `[A-Za-z_][A-Za-z0-9_]*` and the quotes must match. So `<<EOF`, `<<-EOF`, `<<"EOF"`, `<<'EOF'` and `2<<EOF` open a heredoc. `$((1<<2))`, `<<<` here-strings and mismatched quotes do not.
- Bodies follow the line in the order of their markers. Every body line, the terminator included, is skipped before comment or continuation handling, so `# ...` inside a body is content, not a comment. A terminator is the raw line, minus a trailing `\r`, equal to the word. For `<<-` only, leading tabs are stripped before the comparison. An unterminated heredoc runs to the end of the file.
- A malformed FROM outside a heredoc is still an error ("malformed FROM instruction"). `TestParse_InvalidContent` covers a closed heredoc followed by `FROM alpine extra words`.
- The new tests are a RUN python heredoc with `from os import path`, `FROM x` and a `#` line; a `COPY <<EOF` body with FROM words; and one RUN line with `<<FIRST` and `<<-'SECOND'`, whose tab-indented body and terminator contain `FROM z`. Each parses to only the real FROM images. I did not run them against the old code. By reading it, `from os import path` and `FROM is a word here` are FROM lines with extra words, which the old code rejects as malformed.

Outside scope: none new.

Concerns: none.

## Gate (lead, 2026-11-19)

Round 0 concerns ruled on (docker:// skip accepted; lockfile local entries dropped; purl pinned at v0.2.1 for Go case). Fix rounds 1 and 2 implemented the rulings (poetry.lock path entries, uv.lock line scan with the typer fixture, mixed-case Go PURL test). Sonnet reviewer: ACCEPT with two minors: Dockerfile heredoc bodies failing the file (fixed in round 3) and a `<<` merge key under compose services (dismissed as rare). make check rerun after round 3: exit 0; extract tests ok; no I/O imports. Accepted.

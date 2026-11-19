Status: DONE
Work order: .claude/plans/m0/WO-11-doctor.md
Files changed:
- internal/gitsrc/doc.go (created)
- internal/gitsrc/cli.go (created)
- internal/gitsrc/cli_test.go (created)
- internal/vuln/osv/doc.go (created)
- internal/vuln/osv/client.go (created)
- internal/vuln/osv/client_test.go (created)
- internal/upstream/depsdev/doc.go (created)
- internal/upstream/depsdev/resolver.go (created)
- internal/upstream/depsdev/resolver_test.go (created)
- internal/issues/github/doc.go (created)
- internal/issues/github/source.go (created)
- internal/issues/github/source_test.go (created)
- cmd/upw/doctor.go (created)
- cmd/upw/doctor_test.go (created)
- cmd/upw/main.go (modified: `doctor` line in the usage text and the `case "doctor"` dispatch only)

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
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.472s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.185s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	1.180s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.721s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.420s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.381s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.182s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.182s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.190s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	2.096s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.166s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.157s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.385s
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
- `go test -race -count=1 ./internal/gitsrc/ ./internal/vuln/osv/ ./internal/upstream/depsdev/ ./internal/issues/github/ ./cmd/upw/` -> exit 0
  ```
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	1.098s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.117s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.113s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.113s
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.382s
  ```
- `make build && env -u UPW_GITHUB_TOKEN_TARGETS -u UPW_GITHUB_TOKEN_UPSTREAM ./bin/upw doctor -config upw.dev.yaml; echo "exit=$?"` -> exit=0
  ```
  go build -o bin/upw ./cmd/upw
  ok    config                  upw.dev.yaml
  warn  github targets token    not set: private targets cannot be fetched
  warn  github upstream token   not set: upstream polling limited to 60 requests an hour
  ok    git                     installed
  ok    osv                     api.osv.dev reachable
  ok    deps.dev                api.deps.dev reachable
  ok    github                  api.github.com reachable
  exit=0
  ```
- `UPW_WEBHOOK_URL=https://hooks.example.invalid/abcd1234wxyz ./bin/upw doctor -config <scratchpad>/wo11/upw.dev.yaml` (a copy of upw.dev.yaml with the discord channel uncommented; stdout and stderr captured together) -> exit 0
  ```
  ok    config                  <scratchpad>/wo11/upw.dev.yaml
  warn  github targets token    not set: private targets cannot be fetched
  warn  github upstream token   not set: upstream polling limited to 60 requests an hour
  ok    notify channel 2        discord_webhook ****wxyz
  ok    git                     installed
  ok    osv                     api.osv.dev reachable
  ok    deps.dev                api.deps.dev reachable
  ok    github                  api.github.com reachable
  ```
  `grep -c abcd1234wxyz` on the captured output -> 0 (the full URL appears nowhere).
- `sh scripts/check-imports.sh` -> exit 0 (no output)
- `rm -rf bin; git status --porcelain` -> before this report was written:
  ```
   M cmd/upw/main.go
  ?? cmd/upw/doctor.go
  ?? cmd/upw/doctor_test.go
  ?? internal/gitsrc/
  ?? internal/issues/
  ?? internal/upstream/
  ?? internal/vuln/
  ```
  Only owned files, plus this report file once written.

Decisions:
- OSV probe: `GET https://api.osv.dev/v1/vulns/GO-2020-0001` (documented "get vuln by ID" endpoint; long-published, reviewed Go advisory). Checked live on 2026-11-19: `HTTP/2 200`, `content-type: application/json`, `content-length: 1226`, `x-envoy-decorator-operation: ingress GetVulnById`, body starting `{"id":"GO-2020-0001","summary":"Arbitrary log line injection in github.com/gin-gonic/gin",...`.
- deps.dev probe: `GET https://api.deps.dev/v3/systems/go/packages/github.com%2Fgoogle%2Fuuid/versions/v1.6.0` (documented GetVersion endpoint, the same family docs/external-sources.md lists). Checked live on 2026-11-19: `HTTP/2 200`, `content-type: application/json; charset=utf-8`, `content-length: 481`, body `{"versionKey":{"system":"GO","name":"github.com/google/uuid","version":"v1.6.0"},"publishedAt":"2024-01-23T18:54:04Z",...,"links":[{"label":"SOURCE_REPO","url":"https://github.com/google/uuid"}],...}`.
- GitHub `GET https://api.github.com/rate_limit` with the Accept and API-version headers, unauthenticated, checked live: 200, 678 bytes.
- Check errors: every adapter prefixes the host (`<host>: <httpx error>`). GitHub with a token adds the redacted token (`<host> (token ****abcd): ...`); a 401 with a token is exactly `<host>: token ****abcd rejected (401)`, matching the work order's example line.
- Report column format is `%-5s %-23s %s`: byte-identical to the example (5-character status column plus a space; name and its separator space fill 24 characters). I chose 23+space over `%-24s` with no separator so a separator is always there; the two differ only for a 24-character name, and doctor has none.
- `notify channel N` numbers channels by their 1-based position in `notify.channels`, counting channels without a secret, so the operator can find the entry. In upw.dev.yaml with discord uncommented this prints `notify channel 2` (stdout is channel 1). "Has a secret" is decided by type: `discord_webhook`, `slack_webhook`, `ntfy` → `url`; `email` → `password`; `stdout` has no line.
- GitHub checks run upstream token first, then targets token, following the order of the example report.
- The config line's detail is the `-config` path exactly as given.
- `Checker` is declared in `cmd/upw/doctor.go` (per the Ruling; `cmd/upw` has no interfaces.go and only doctor.go is owned).
- `cmdDoctor` takes the check builder as its last parameter (`doctorChecks` in production, set in the dispatch line), so the tests drive the whole command, config file and stderr logger included, with fake checks. The report itself is built by `doctorReport(ctx, w, configPath, cfg, checks)`.
- A config that fails to load returns the `config.Load` error unwrapped, so `run` prints `upw: <problems>` to stderr, one per line, and exits 1, the same path `serve` and `run` use.
- `gitsrc.CLI.Check`: resolves git with `exec.LookPath` inside `Check`, not once at start-up, because constructors do no I/O and the not-installed test changes PATH. Runs `exec.CommandContext(ctx, path, "--version")` with an explicit `cmd.Env` of `PATH`, `LC_ALL=C`, `GIT_TERMINAL_PROMPT=0`, `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`; stdout and stderr are captured with a 4 KiB cap. `HOME` is not set: the rule calls for an empty directory under the data dir, `New(logger)` has no data dir, and `--version` reads no config. The per-invocation `-c` settings (protocol allowlist, credential helper, redirects, hooks, low-speed limits) are left out because they only affect network and repository commands, which come in M1.
- The not-installed error is an unexported sentinel with the text `git is not installed: no git executable on PATH`; it is not exported because Produced does not list it.
- Each adapter uses its logger for one Debug line on success (GitHub also logs whether it was authenticated and the remaining rate budget), so the logger parameter in the Produced signatures is not left unused.
- The adapter tests use `httptest.NewServer` (plain http), which `AllowPrivateHosts: ["127.0.0.1"]` permits, with `MaxWait: 1ms` so the server-down retries are fast. A 500 is not retried by httpx (only 502, 503 and 504 are), so that test makes a single request.
- Tests beyond the listed ones: `TestCLI_Check_CancelledContext`, `TestDoctor_UsageErrors`, `TestUsage_ListsDoctor` (put in doctor_test.go so main_test.go stays unchanged), `TestDoctorChecks_Names` (check names and order for each token combination), and `TestSecretLines` (secrets of 8 characters or fewer show `****`, llm disabled has no line, a channel without a secret field has no line, email/ntfy/slack lines).

Outside scope:
- docs rows for docs-sync: `docs/interfaces.md` implementations table (`Checker` now implemented by `gitsrc.CLI`, `osv.Client`, `depsdev.Resolver`, `github.Source`; `Checker` declared in `cmd/upw`); `docs/architecture.md` CLI table (`upw doctor` flags `-config`, `-log-level`; it does not open the database); `docs/external-sources.md` (the doctor probe endpoints: OSV `GET /v1/vulns/GO-2020-0001`, deps.dev `GET /v3/systems/go/packages/github.com%2Fgoogle%2Fuuid/versions/v1.6.0`, GitHub `GET /rate_limit`; the GitHub row may not list `/rate_limit` yet).
- `doctor` checks the LLM key only from config/env. A key saved on the settings page (in the database) is not visible to doctor, which does not open the database, so with `llm.enabled: true` and a key stored only in the UI, doctor warns `not set`. That matches requirement 6 and the warning text, but an operator who saved the key in the UI will still see it.
- The targets token is sent to `api.github.com/rate_limit` as the work order says. That proves GitHub accepts it, not that it can read the private targets (M1, as Out of scope notes).

Concerns: none

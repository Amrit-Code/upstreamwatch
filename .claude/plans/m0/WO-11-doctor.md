# WO-11: `upw doctor` and the first four reachability checks

Milestone: M0
Agent: go-mech   Model: opus
Depends on: WO-08, WO-10 (both merged)
Report file: `.claude/plans/m0/WO-11-doctor.report.md`
Ticket: UPW-18

## Objective

`upw doctor` tells the operator whether the box can do its job: the config loads, secrets are present (shown only as their last four characters), `git` is installed, and OSV, deps.dev and GitHub answer. The four adapter packages are created with their constructor and `Check` method only; later milestones add the real work to them. Serves M0 checklist items 6 and 17 (and 23 on the VM).

## Owned files

- `internal/gitsrc/` (new package)
- `internal/vuln/osv/` (new package)
- `internal/upstream/depsdev/` (new package)
- `internal/issues/github/` (new package)
- `cmd/upw/doctor.go`, `cmd/upw/doctor_test.go`
- `cmd/upw/main.go`: only the lines that add the `doctor` subcommand to the dispatch and to the usage text; and the existing usage test if its expected text changes

Lead-owned, read-only for you: everything else, including `internal/httpx/`, `internal/config/`, `internal/model/`, the other files in `cmd/upw/`, `.go-arch-lint.yml`, `go.mod`.

## Interfaces

### Consumed

```go
// internal/httpx (read the package for the full contract)
func New(cache CacheStore, opts Options, logger *slog.Logger) *Client
func (c *Client) Do(ctx context.Context, req Request) (Response, error)
type StatusError struct{ Method, Host, Path string; Status int }

// internal/config
func Load(path string, getenv func(string) string) (Config, error)

// internal/model
type Secret string // String() is redacted to the last four characters; Reveal() returns the value
```

`cmd/upw` already has the subcommand scaffolding, the `-config` and `-log-level` flags and the logger set-up from WO-08; follow it.

### Produced

```go
// internal/gitsrc
type CLI struct{ /* unexported */ }
func New(logger *slog.Logger) *CLI
func (g *CLI) Check(ctx context.Context) error

// internal/vuln/osv
type Options struct{ BaseURL string } // default https://api.osv.dev
type Client struct{ /* unexported */ }
func New(hc *httpx.Client, opts Options, logger *slog.Logger) *Client
func (c *Client) Check(ctx context.Context) error

// internal/upstream/depsdev
type Options struct{ BaseURL string } // default https://api.deps.dev
type Resolver struct{ /* unexported */ }
func New(hc *httpx.Client, opts Options, logger *slog.Logger) *Resolver
func (r *Resolver) Check(ctx context.Context) error

// internal/issues/github
type Options struct{ BaseURL string } // default https://api.github.com
type Source struct{ /* unexported */ }
func New(hc *httpx.Client, token model.Secret, opts Options, logger *slog.Logger) *Source
func (s *Source) Check(ctx context.Context) error
```

The CLI: `upw doctor [-config path] [-log-level level]`.

## Requirements

1. Every `Check` is a cheap, read-only probe with its own 10-second timeout derived from `ctx`. It returns nil when the service answered as expected, and otherwise an error an operator can act on: which host, what status or failure, and for an authenticated call the token's last four characters (through `model.Secret`'s redaction, never `Reveal()` in a message). No `Check` retries beyond what httpx does, and none writes anything.
2. `gitsrc.CLI.Check` runs `git --version` following every applicable rule in `.claude/skills/outbound-client/references/GIT-INVOCATION.md` (explicit argument list, no shell, minimal environment, no prompts). `git` not on `PATH` is an error saying git is not installed. `internal/gitsrc` is the only package that may import `os/exec`.
3. `osv.Client.Check` and `depsdev.Resolver.Check` each make one unauthenticated `GET` through the httpx client to a stable, small, documented endpoint of that API. Choose the endpoint by checking the live API once, and record the exact request and the response you saw under Decisions. A 2xx is success.
4. `github.Source.Check` makes one `GET {BaseURL}/rate_limit` (it does not count against the limit), with `Authorization: Bearer <token>` only when the token is non-empty, plus `Accept: application/vnd.github+json` and `X-GitHub-Api-Version: 2022-11-28`. 2xx is success. A 401 is an error saying the token (last four) was rejected. Without a token the call is unauthenticated and still proves reachability.
5. Adapters import only the standard library, `internal/httpx` and `internal/model`. They never build an `http.Client` or call `http.Get`.
6. `upw doctor` loads the config. A config that fails to load prints the problems to stderr and returns 1. Otherwise it prints a report to stdout, one line per check, in this order and format (`<status>` is `ok`, `warn` or `fail`, left-aligned in a 5-character column; then the check name in a 24-character column; then the detail):
   ```
   ok    config                  upw.dev.yaml
   warn  github targets token    not set: private targets cannot be fetched
   warn  github upstream token   not set: upstream polling limited to 60 requests an hour
   ok    llm api key             ****abcd
   ok    notify channel 1        discord_webhook ****wxyz
   ok    git                     installed
   ok    osv                     api.osv.dev reachable
   ok    deps.dev                api.deps.dev reachable
   ok    github (upstream token) api.github.com reachable
   fail  github (targets token)  api.github.com: token ****abcd rejected (401)
   ```
   - secret lines: one for each of the two GitHub tokens (`warn` with the texts above when empty, otherwise `ok` with the redacted value); one for `llm api key` only when `llm.enabled` is true (`warn` `not set: add it on the settings page or set UPW_LLM_API_KEY` when empty); one per notify channel that has a secret (`url` or `password`), showing the channel type and the redacted secret, `warn` `<type> secret not set` when empty;
   - network checks run one after another: `git`, `osv`, `deps.dev`, then GitHub once per configured token with that token (name `github (targets token)` or `github (upstream token)`), or once unauthenticated with the name `github` when neither is set;
   - a failed check prints `fail` with the error text and does not stop the later checks.
7. Exit code: 1 if any line is `fail`, otherwise 0. Warnings never change the exit code.
8. No secret value appears anywhere in doctor's stdout, stderr or logs beyond its last four characters. A secret of 8 characters or fewer shows as `****`.
9. `upw doctor` does not open the database. It builds its own httpx client with `httpx.New(nil, httpx.Options{}, logger)`; the checks use `ValidatorsNone`, so no cache store is needed. If `httpx.New` does not accept a nil store for that use, report BLOCKED rather than working around it.
10. The top-level usage lists `doctor`.

## Tests to include

- Per HTTP adapter, with an `httptest` server reached through `httpx.Options{AllowPrivateHosts: []string{"127.0.0.1"}}` and the adapter's `BaseURL` option: success; a 500 (after httpx's retries; keep the test fast with small `MaxWait`) → error naming the host; server down → error. For GitHub also: the `Authorization` header is sent exactly when a token is set; a 401 → error containing the redacted token and not the token.
- An external test file in each adapter package asserting the type has `Check(context.Context) error` (declare the one-method interface in the test).
- `gitsrc`: `Check` succeeds on this machine; with `PATH` emptied through `t.Setenv` it returns the not-installed error.
- `cmd/upw/doctor_test.go`: the report is built by a function that takes the config and a list of named checks, so tests inject fake checks instead of using the network:
  - `TestDoctor_ReportFormat`: golden-style comparison of the whole stdout for a config with one token set, one unset, `llm.enabled` true with a key, and a discord channel;
  - `TestDoctor_NoSecretLeak`: every secret value used in the test is absent from stdout and stderr, and its last four characters are present;
  - `TestDoctor_ExitCodes`: all ok → 0; warnings only → 0; one fail → 1 and later checks still printed;
  - `TestDoctor_BadConfig` → 1.

## Acceptance

Run from the repository root.

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race ./internal/gitsrc/ ./internal/vuln/osv/ ./internal/upstream/depsdev/ ./internal/issues/github/ ./cmd/upw/` | `ok` for all five |
| `make build && env -u UPW_GITHUB_TOKEN_TARGETS -u UPW_GITHUB_TOKEN_UPSTREAM ./bin/upw doctor -config upw.dev.yaml; echo "exit=$?"` | config ok; both token lines `warn`; git, osv, deps.dev and github `ok`; `exit=0` |
| `UPW_WEBHOOK_URL=https://hooks.example.invalid/abcd1234wxyz ./bin/upw doctor -config <a copy of upw.dev.yaml in a temp dir with the discord channel uncommented>` | the channel line shows `****wxyz`; the full URL appears nowhere in the output |
| `sh scripts/check-imports.sh` | exit 0 (`os/exec` only in `internal/gitsrc`) |
| `rm -rf bin; git status --porcelain` | only owned files and the report file |

## Out of scope

- Clone, fetch, log and cat-file in `gitsrc` (M1); OSV queries (M2); deps.dev resolution and GitHub issue listing (M3); notifier and LLM provider checks (M2, M4).
- Checking that the targets token can read a particular private repository (M1, with ingest).
- Changing `internal/httpx` or `.go-arch-lint.yml`. If the lint rejects one of the new packages, report BLOCKED with its output.

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `outbound-client`.
Read: `.claude/skills/outbound-client/references/GIT-INVOCATION.md`; `docs/interfaces.md` (`Checker`); `docs/external-sources.md`; `internal/httpx/` (exported API); `cmd/upw/` as merged.

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

- 2026-11-19: The `Checker` interface is declared in `cmd/upw`, the package that calls it, as the one-method interface from `docs/interfaces.md`.
- 2026-11-19: A missing GitHub token is a warning, not a failure: polling works unauthenticated, and a deployment may have only public targets.

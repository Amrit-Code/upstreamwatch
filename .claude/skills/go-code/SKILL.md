---
name: go-code
description: Go coding rules for upstreamwatch production code. Covers the standard-library-first stance and the approved dependency list, error wrapping and sentinels, context and goroutine lifetimes, slog logging through an injected logger, constructors, naming, doc comments, secrets, the cmd/upw CLI built on the standard flag package with subcommands, and config loaded from YAML plus environment variables. Triggers on writing, implementing, editing or refactoring any non-test .go file; adding a CLI subcommand or flag; adding a config key; adding or upgrading a dependency, go get, or go.mod changes; and on gofmt or go vet failures. Not for deciding which package code belongs in (use upw-architecture), _test.go files and fixtures (go-test), SQL and migrations (sqlite-store), HTTP or git adapter behaviour (outbound-client), or the LLM stage (llm-triage).
---

# Go code for upstreamwatch

Module `github.com/Amrit-Code/upstreamwatch`, Go 1.27. When in doubt, read the neighbouring file and match it. Load `upw-architecture` first if you have not decided where the code goes.

## Standard library first

The default stack is the standard library: `net/http`, `encoding/json`, `html/template`, `log/slog`, `flag`, `net/smtp`, `os/exec`, `go/parser`. No web framework, no third-party JSON or logging library, no CLI framework, no UUID library.

The approved modules are the libraries named in `mvp-plan/PLAN.md` section 2. Versions are pinned in `go.mod`.

| Module | Used for |
|---|---|
| `github.com/git-pkgs/manifests` | parsing manifests and lockfiles |
| `github.com/git-pkgs/vers` | version-range evaluation (pinned by commit until tagged) |
| `github.com/git-pkgs/purl` | package URLs |
| `github.com/git-pkgs/vulns/osv` | the OSV client (pinned by commit) |
| `modernc.org/sqlite` | the SQLite driver; imported only by `internal/store` |
| `github.com/robfig/cron/v3` | the in-process scheduler |
| `golang.org/x/mod/modfile` | only where `manifests` misses a go.mod detail |
| `github.com/anthropics/anthropic-sdk-go` | the Anthropic provider |
| `golang.org/x/vuln/scan` | post-MVP (M6) only |

These are explicitly not approved: Cobra, Viper, testify or any test framework, `openai-go` (the OpenAI-compatible provider is raw `net/http`), go-git (we shell out to `git`), web frameworks, and logging libraries.

The standard library has no YAML package. The approved YAML library is recorded in `docs/decisions.md`; use that module and no other.

Anything not in the table needs the owner's yes. Do not run `go get` for an unlisted module. Stop and report a one-line justification instead: what the standard library lacks, and what writing it ourselves would cost. Tools that run beside the build (staticcheck, go-arch-lint, and the `git-pkgs` and `osv-scanner` binaries used by referee tests) are not imported, so this table does not cover them.

## Errors

- **Wrap with context:** `return fmt.Errorf("poll %s issues: %w", repo.Slug, err)`. Say what this function was doing, in lowercase, without "failed to", so the chain reads as a sentence.
- **Expected outcomes are package-level sentinels,** checked with `errors.Is`. A sentinel that crosses a layer (`model.ErrNotFound`, for example) lives in `internal/model`, so callers do not import the package that produced it.
- **When the caller needs data from the error, use a typed error.** A rate-limit reset time is an example. Inspect it with `errors.AsType[*T](err)`.
- **Handle an error once:** either log it or return it. The Runner, HTTP handlers and `main` log. Everything below them returns.
- **Use `errors.Join`** for several independent failures: all config validation problems, or one result per enabled notifier.
- **Do not panic outside `main`.** Library code returns errors.
- **Do not discard errors.** If a failure is deliberately best-effort, say so in a one-line comment where it is ignored.

## Context and goroutines

- **`context.Context` is the first parameter** of every function that does I/O, calls something that does, or can be cancelled. Pass it through; never store it in a struct.
- **Every goroutine has a shutdown path,** usually context cancellation, and its creator waits for it to finish (`sync.WaitGroup.Go` plus `Wait`). Do not start a goroutine without knowing what stops it.
- **Long jobs check `ctx.Err()` between units of work** (per repo, per page, per candidate), so a SIGTERM during a backfill stops within one unit.
- **Jobs run one at a time.** Do not add concurrency inside a job unless the work order asks for it. The single-writer database and GitHub's request for serial calls both depend on it.

## Logging

- **Use `log/slog` only, through the injected `*slog.Logger`.** Never `slog.Default()`, the `log` package, or `fmt.Print*` for diagnostics.
- **Keep the message a stable lowercase phrase and put the variables in attributes:** `s.logger.Info("issues poll finished", "repo", slug, "fetched", n, "not_modified", nm)`. Attribute keys are snake_case. Add per-run context once with `logger.With("job", name)`.
- **Levels:**
  - `Debug` for tracing;
  - `Info` for lifecycle and run summaries;
  - `Warn` for recoverable trouble (a 5xx that will be retried next run, rate limit nearly spent);
  - `Error` for a failed operation.
- **Never log secrets.** That means tokens, the LLM API key, webhook URLs (the URL is the credential), the SMTP password, `Authorization` headers, or a URL with credentials in it. Config secrets use the redacting `Secret` type in `templates/config-loading.md`, so passing one to a logger prints only the last four characters.

## Constructors, interfaces and types

- **Constructors are `NewX(deps..., logger *slog.Logger) *X`.** Dependencies are the interfaces the package declares. A type with more than two settings takes an `Options` struct it declares. Return `(*X, error)` only when construction validates something.
- **Constructors do no I/O.** Connecting, fetching and checking happen in methods that take a context.
- **Accept interfaces, return structs.** Keep interfaces as small as the caller needs. The interface rules are in `upw-architecture`.
- **No package-level mutable state and no `init()` wiring.** `cmd/upw` constructs everything explicitly.
- **Values stored as TEXT in the database are typed string constants in `internal/model`.** For example `type Verdict string` with `VerdictAffects Verdict = "affects"`. A stored value is never renamed.
- **Time is UTC everywhere.** A service that needs the current time takes a `now func() time.Time` field set to `time.Now` by its constructor. A pure function takes `now time.Time` as a parameter. Tests then control the clock without sleeping.

## Naming and layout

- Package names are lowercase, one word, no underscores, matching the directory: `openaicompat`, not a name with an underscore. No `util`, `common` or `helpers` packages; put a helper next to its only caller.
- Do not stutter: `issues.Poller`, not `issues.IssuesPoller`.
- Interfaces a package consumes sit in its `interfaces.go`, which the lead session owns.

## Completeness and comments

- **Production quality only.** No `TODO`, `FIXME`, placeholder bodies, stub functions that return nil, commented-out code, or unused parameters kept for later. If the work order cannot be finished as written, report what is missing instead of leaving a stub.
- **Every exported identifier has a doc comment that starts with its name:** `// Poller fetches issues and comments updated since each upstream's cursor.`
- **Inside functions, comment only the non-obvious why:** a workaround, a constraint from an outside API, an invariant a reader would miss.

## The CLI (`cmd/upw`)

The command surface is in `docs/architecture.md` (CLI table) and `mvp-plan/PLAN.md` section 7. The scaffold is in `templates/cmd-upw.md`.

- **`main` is thin.** It builds the signal context with `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`, calls `run(ctx, os.Args[1:], os.Stdout, os.Stderr, os.Getenv)` and turns the result into an exit code. `os.Exit` appears once, in `main`. `run` takes its inputs as parameters so tests can call it.
- **Subcommands use the standard `flag` package.**
  - Dispatch on `args[0]`.
  - Each subcommand has its own `flag.NewFlagSet(name, flag.ContinueOnError)` with output set to stderr.
  - Every subcommand registers `-config`.
- **`flag` stops parsing at the first non-flag argument.** For `upw run <job> --repo x`, take the positional job name off the front before calling `Parse`. Otherwise `--repo` is silently left unparsed.
- **stdout carries the command's output only** (tables, or JSON with `--json`). Logs and error messages go to stderr. That keeps `upw findings --json | jq` working.
- **Exit codes:** 0 success, 1 failure, 2 usage error. `-h` prints usage and exits 0.
- **`upw serve` blocks until the context is cancelled, then shuts down in order:**
  1. the HTTP server, with `Shutdown` and a 10-second timeout;
  2. the scheduler (wait for the running job to return);
  3. the store, closed last.

## Configuration

`internal/config` loads YAML for structure and the environment for secrets. The field list is `mvp-plan/PLAN.md` section 7, plus the email notifier settings in `docs/external-sources.md`. The scaffold is in `templates/config-loading.md`.

- **Load is `config.Load(path string, getenv func(string) string) (Config, error)`.** Taking `getenv` as a parameter keeps tests off the real environment.
- **Defaults are set in code,** then the YAML is decoded over them.
- **`${NAME}` references are expanded in string values after decoding,** never in the raw file text. A value from the environment then cannot inject YAML structure. An unset variable expands to the empty string, and validation catches required fields left empty.
- **Secret fields (tokens, the LLM key, the webhook URL, the SMTP password) are accepted only as `${NAME}` references.** Load rejects a literal value there, because the plan keeps secrets out of YAML.
- **Validate everything at load and return every problem at once,** joined with `errors.Join`, so the operator fixes the file in one pass.
- **A new config key needs four things:**
  - a field with a default;
  - validation;
  - an entry in `upw.example.yaml`;
  - a mention in your report so `docs-sync` can pick it up.
- **Services never import `config`.** `cmd/upw` copies the relevant values into each service's `Options`.

## Before you report

Run `gofmt -l .` (it must print nothing), then `go vet ./...` and `go build ./...`, then `make check`. Put the real output in the report file `work-order` defines. If something cannot pass, say so; do not describe it as done. A missing module or an unapproved dependency makes the status BLOCKED.

## Not covered here

- Which package code belongs in, import rules, interfaces: `upw-architecture`
- Writing tests, fakes, golden files, fixture repos: `go-test`
- Migrations, SQL and transactions: `sqlite-store`
- `httpx`, ETags, rate limits, safe `git` invocation: `outbound-client`
- Provider adapters and prompts: `llm-triage`
- Handlers and templates: `web-ui`
- Image build and deployment: `upw-deploy`

---
name: upw-architecture
description: Structural rules for upstreamwatch. Says which layer and package a piece of code belongs in, which packages may import which, where interfaces are declared and who owns them, and the steps for adding a job, an adapter behind an existing interface, or a new interface. Points at the docs/ file that holds each fact. Triggers on "where does this go", "which package", new package, new job, new adapter, new interface, import cycle, wiring in cmd/upw, a go-arch-lint or layering failure from make check, editing .go-arch-lint.yml, or a work order that touches more than one package. Every builder loads it before writing code. Not for style inside a file (use go-code), tests (go-test), SQL, migrations and store internals (sqlite-store), HTTP and git adapter internals (outbound-client), or the LLM stage (llm-triage).
---

# upstreamwatch architecture rules

This skill holds the rules. The facts (packages, interface signatures, tables) live in `docs/`. Open the doc instead of guessing, and do not copy its tables into code comments or reports.

## Which doc answers which question

| Question | Open |
|---|---|
| What are the layers? Which service runs which job, what does it call, which tables does it write? HTTP routes, CLI commands, schedules | `docs/architecture.md` |
| Exact Go signature of an outbound interface; which adapters implement it | `docs/interfaces.md` |
| Which store interface covers a table; who may write it; transaction rules | `docs/data-access.md` |
| What a model object holds; which rules are methods on it | `docs/domain-model.md` |
| Columns of a table | `docs/database.md` |
| An outside service: auth, cadence, failure behaviour | `docs/external-sources.md` |
| Directory layout | `mvp-plan/PLAN.md` section 11. Where it disagrees with the layer diagram in `docs/architecture.md`, the diagram wins |
| Why a choice was made | `docs/decisions.md` |

Your work order is more specific than any of these. If the work order and a doc disagree, stop and report the conflict; do not pick one yourself.

## Where code goes

Ask these in order and stop at the first yes.

1. **Does it turn outside input into a call?** CLI arguments, an HTTP request, a cron tick. It is an entry point: `cmd/upw`, `internal/sched` or `internal/web`. It parses, calls one service method and formats the result. It makes no decisions, so a rule written here would differ between the CLI and the web UI.
2. **Does it touch something outside the process?** The network, the `git` binary, SMTP, files outside the database. It is an adapter, behind an interface declared by the service that uses it. HTTP adapters go through `internal/httpx`; ETags, rate limits, retries and the private-address check are written once there and never again in an adapter.
3. **Does it run SQL?** It goes in `internal/store`, behind a store interface declared by the service that uses it. See `sqlite-store`.
4. **Is it a decision you can compute from values alone?** No I/O, no clock (take `now time.Time` as a parameter), no logger. It is a pure function.
   - If it is a rule about one model object and more than one package needs the answer (ranking, state transitions, "was this pinned on date X"), make it a method on the type in `internal/model`. `docs/domain-model.md` lists the ones already planned.
   - If only one job makes the decision, write it as a function in that job's package, in a file that imports no I/O package. That keeps it testable with plain tables.
5. **Otherwise it is workflow:** load, decide, call out, save. That is an application service: a job (`docs/architecture.md`, job table) or one of the request services (findings, settings, eval, read services).

A type used by more than one package goes in `internal/model`. That includes sentinel errors that cross a layer, such as "not found", so a service can test `errors.Is(err, model.ErrNotFound)` without importing the store.

## Import rules

| Package | May import from `internal/` | Never imports |
|---|---|---|
| `cmd/upw` | anything; it is the wiring | (nothing forbidden) |
| `internal/sched`, `internal/web` (entry points) | the services they call, `model` | `store`, `config`, adapters, `httpx` |
| Services and jobs | `model` only, except `internal/triage`, which may also import `internal/llm` | another service, `store`, `config`, adapters, `httpx` |
| `internal/llm` (pure helpers: provider-neutral prompt builder, vocabulary constants, output schema, cost helpers) | `model` | everything else |
| Adapters | `model`, `httpx` | `store`, any service, any other adapter |
| Provider adapters `internal/llm/anthropic`, `internal/llm/openaicompat` | `llm`, `httpx`, `model` | `store`, any service, each other |
| `internal/httpx` | `model` | `store`, services, adapters |
| `internal/store` | `model` | everything else |
| `internal/model` | nothing | everything |
| `internal/config` | `model` | everything else |

What follows from the table:

- **Only `cmd/upw` imports `store`, `config` and adapter packages.** It constructs each adapter and the store and passes them to service constructors as interface values.
- **Only `internal/store` imports `database/sql` or the SQLite driver.** Only `internal/gitsrc` imports `os/exec`, apart from the test helpers under `internal/testutil`.
- **`internal/llm` is a pure helper package, not an adapter.** It holds no I/O. `internal/triage` and the two provider adapters import it; nothing else does. `eval` imports `model` only: it declares its own small "judge this candidate" interface, which `triage.Service` satisfies, and `cmd/upw` wires them.
- **Read services live in one package, `internal/query`,** which imports `model` and declares the store interfaces it needs.
- **Services get their settings as an `Options` struct they declare.** `cmd/upw` fills it from `config`. A service that imported `config` would drag YAML and env handling into every unit test.
- **`pipeline.Runner` reaches jobs only through the `Job` interface.** `Scope` and `Stats` live in `model`, so a job package never imports `pipeline`.
- **`httpx` reaches the `http_cache` table through an interface it declares.** `cmd/upw` passes the store in.

`make check` enforces these with `go-arch-lint` plus an import check for the standard-library rules. `references/ARCH-LINT.md` has the configuration sketch. When the lint fails, move the code. Do not add an exception to `.go-arch-lint.yml`; the lead session owns that file.

## Interfaces: declared by the consumer, owned by the lead

- **Every interface is declared in the package that calls it,** in that package's `interfaces.go`. That covers outbound interfaces (`IssueSource`, `Notifier` and the rest) and store interfaces.
- **A store interface holds only the methods its package calls.** Name it after the group in `docs/data-access.md` (`IssueStore`, `FindingStore`). The `vuln` package's `TimelineStore` will be a smaller set than `timeline`'s, and that is intended. `*store.DB` satisfies all of them. The check happens where `cmd/upw` passes the store to a constructor.
- **Interface methods take `context.Context` first and use only `model` types and standard-library types.** No `*http.Response`, no `sql.Null*`, no adapter- or library-specific types, so a fake can be written in a few lines.
- **The lead session writes every `interfaces.go`.** Those files, `.go-arch-lint.yml`, and any `internal/model` type your work order does not list are read-only to builders. Parallel builders depend on these contracts staying fixed while they work.

**If you need an interface changed, stop the part of the work that depends on it.** Finish the parts that do not, and report BLOCKED (the report format is in `work-order`) with:

- the file and the current signature;
- the signature you need;
- why the current one does not work;
- which implementers and callers would change.

Do not work around a missing method. A type assertion to a concrete type, a second look-alike interface, an `any` parameter, or importing the concrete package each break a rule above, and the reviewer will send the work back.

## Adding a job

1. Create `internal/<job>/` with a `Service` struct (the poller is `issues.Poller`; follow the name in `docs/architecture.md` if it gives one). The constructor is `NewService(deps..., opts Options, logger *slog.Logger) *Service` (see `go-code`).
2. Implement `Name() string` and `Run(ctx, scope) (model.Stats, error)`. `Name` returns the string used in the config `schedules` key and in `upw run <job>`.
3. Use the interfaces the lead put in `internal/<job>/interfaces.go`. If the file is missing or lacks a method you need, follow the stop-and-report rule above.
4. Put the job's decisions in pure functions (step 4 of "Where code goes") and keep `Run` to load, decide, call out, save. Writes that must land together go through `InTx` (see `sqlite-store`).
5. Wire it in `cmd/upw`: construct it with the store and adapters and add it to the jobs the Runner is built with. Add its schedule to config and `upw.example.yaml` only if the work order says so.
6. Test the service with hand-written fakes and the pure functions with tables (see `go-test`).
7. In your report, list the doc rows that are now out of date: the job table and schedule table in `docs/architecture.md`, and the store-interface users in `docs/data-access.md`. The `docs-sync` skill applies them.

## Adding an adapter behind an existing interface

1. Put it at the package path `docs/architecture.md` gives for that adapter family, for example `internal/notify/<name>` or `internal/issues/<host>`. One adapter per package.
2. Import only `model`, `httpx` (for anything over HTTP) and the approved library it wraps, if any. Never the store, a service, or a sibling adapter. Code two adapters share belongs in `httpx` (transport behaviour) or `model` (pure formatting of a model value).
3. Export a constructor that returns the concrete type. Prove it satisfies the interface in the adapter's external test file, for example `var _ report.Notifier = (*email.Notifier)(nil)` in `email_test.go`. Test files are outside the lint, so the import is allowed there and catches drift.
4. Give it the `Check(ctx context.Context) error` method of the `Checker` interface in `docs/interfaces.md`; `upw doctor` calls it on every outbound adapter.
5. Secrets come from the environment through config. Never log them (see `go-code`).
6. Test it against a fake server injected through its client (see `go-test`). For the HTTP and git specifics, load `outbound-client`.
7. Report the doc rows to update: the implementations table in `docs/interfaces.md`, and `docs/external-sources.md` if it talks to a new outside service.

## Adding a new interface

Only the lead session adds one. If your task needs one, propose it in your report:

- the consuming package;
- the method signatures (`context.Context` first, `model` types only);
- the adapter or adapters that will implement it;
- why an existing interface cannot carry the need.

Keep the method set to what the consumer calls today. A one- or two-method interface is normal here.

## Not covered here

- Go style, errors, logging, CLI and config code: `go-code`
- Tests, fakes, fixtures, golden files: `go-test`
- Migrations, SQL, transactions, the store's method conventions: `sqlite-store`
- `httpx`, ETags, rate limits, safe `git` invocation: `outbound-client`
- Provider adapters and the triage service: `llm-triage`
- Handlers and templates: `web-ui`
- How a work order is written and what a builder reports: `work-order`
- Updating `docs/` after a structural change: `docs-sync`

# Data access

This document describes how code reaches the database: the `store.DB` type, the thirteen store interfaces, which tables sit behind each one, and the rules for transactions and writers. Table and column details are in [database.md](database.md).

`store.DB` lives in `internal/store`. It is the only package that imports `database/sql`, holds the migrations, and opens transactions. Services never see SQL. Each service asks for the narrow slice it needs, so the prefilter cannot write findings by accident and a test can fake five methods instead of fifty.

## Store interfaces

```mermaid
classDiagram
  direction LR
  class store_DB {
    Open() Migrate() InTx()
    Backup() Export()
  }
  class RepoStore { <<interface>> }
  class TimelineStore { <<interface>> }
  class UsageStore { <<interface>> }
  class AdvisoryStore { <<interface>> }
  class UpstreamStore { <<interface>> }
  class IssueStore { <<interface>> }
  class CandidateStore { <<interface>> }
  class VerdictStore { <<interface>> }
  class FindingStore { <<interface>> }
  class EvalStore { <<interface>> }
  class RunStore { <<interface>> }
  class CacheStore { <<interface>> }
  class SettingsStore { <<interface>> }
  RepoStore <|.. store_DB
  TimelineStore <|.. store_DB
  UsageStore <|.. store_DB
  AdvisoryStore <|.. store_DB
  UpstreamStore <|.. store_DB
  IssueStore <|.. store_DB
  CandidateStore <|.. store_DB
  VerdictStore <|.. store_DB
  FindingStore <|.. store_DB
  EvalStore <|.. store_DB
  RunStore <|.. store_DB
  CacheStore <|.. store_DB
  SettingsStore <|.. store_DB
```

One concrete type, thirteen small interfaces. The interfaces group tables that change together.

## Table to interface map

| Store interface | Tables behind it | Used by |
|---|---|---|
| `RepoStore` | repos | ingest, timeline, query |
| `TimelineStore` | commits, manifests, packages, dep_events, dep_versions | timeline (write); vulns, prefilter, triage, query (read) |
| `UsageStore` | symbol_usage | usage (write); prefilter, triage (read) |
| `AdvisoryStore` | advisories, advisory_affected, advisory_matches | vulns |
| `UpstreamStore` | upstream_repos, package_upstream | upstream (write); issues, prefilter, coverage (read) |
| `IssueStore` | upstream_issues, upstream_issue_comments | issues (write); prefilter, triage (read) |
| `CandidateStore` | issue_candidates | prefilter (write); triage, eval (read) |
| `VerdictStore` | llm_verdicts | triage, eval |
| `FindingStore` | findings, finding_state, digests | vulns, triage, findings.Service, digest |
| `EvalStore` | eval_labels | eval, findings.Service on dismiss |
| `RunStore` | runs | pipeline.Runner, health, coverage |
| `CacheStore` | http_cache, kv | httpx, feed cursors |
| `SettingsStore` | settings | `settings.Service` only (read and write); the table holds ciphertext, and only `settings.Service` encrypts or decrypts. `triage` does not use it |

The `schema_migrations` table is owned by `store.DB` itself and has no interface.

### What is built

The table above is the design. After M0 these methods exist on `store.DB`:

| Interface | Methods on `store.DB` | Table |
|---|---|---|
| `RunStore` | `StartRun`, `FinishRun`, `LatestRuns` | runs |
| `FindingStore` | `UpsertFinding`, `ResolveFinding`, `ListOpenFindings` | findings, finding_state |
| `CacheStore` (http half) | `GetHTTPValidator`, `SaveHTTPValidator` | http_cache |

`store.DB` also has `Open`, `Close`, `Migrate` and `InTx`. `Backup` and `Export` and the other interfaces come with the milestones that need them. The `kv` half of `CacheStore` is still to come, in M2.

Each interface a service uses is declared in that service's package, so the sets differ by consumer: `pipeline.RunStore` (`StartRun`, `FinishRun`) and `pipeline.FindingStore` (`UpsertFinding`, `ResolveFinding`), `query.RunStore` (`LatestRuns`) and `query.FindingStore` (`ListOpenFindings`), and `httpx.CacheStore` (`GetHTTPValidator`, `SaveHTTPValidator`). `*store.DB` satisfies all of them. `UpsertFinding` inserts a finding in state `open` together with its `finding_state` row, in one transaction; for a fingerprint that exists it updates score, rank reason and last-changed time, clears `resolved_ts` and leaves the operator state alone. `ListOpenFindings` returns unresolved findings in state `open` or `acked`, highest score first.

## Rules

- Writes that must land together, such as the issues page and its cursor, go through `store.DB.InTx`, which hands the service the same interfaces bound to a transaction. `InTx` opens an `IMMEDIATE` write transaction, commits when the function returns nil and rolls back on an error or panic; called inside a transaction it joins it. `store.Bind` adapts `InTx` to a consumer-declared interface, so a service can run its own interface in a transaction without importing the store.
- `store.Open` sets WAL, a five-second busy timeout and foreign keys on, and refuses to continue if the pragmas did not take. It creates the database file with mode 0600 and a missing directory with mode 0700; an existing data directory that group or others can access is set to 0700, and `Open` fails if it cannot be.
- The scheduler is the single writer for pipeline tables. The web server writes only finding state, eval labels and settings. SQLite runs in WAL mode so page loads never wait on a job.
- Store methods take and return domain objects (see [domain-model.md](domain-model.md)), never rows or `sql.Null*` types.
- Migrations are numbered SQL files embedded in the binary and applied at startup, each in its own transaction with its `schema_migrations` row. They are forward-only and additive until 1.0. The binary refuses to start on a database newer than itself, and on one whose applied migration file has since been edited (the stored checksum differs).

## Settings and the LLM key

The settings page lets the operator paste the LLM key into the web UI. The key is stored in the `settings` table encrypted at rest. The encryption key is a file in the data directory with mode 600. The key is write-only in the UI: the page shows the last four characters and never the value.

`settings.Service` is the only code that encrypts and decrypts the key and the only user of `SettingsStore`; the store holds ciphertext only. `triage` does not use `SettingsStore` and never reads the key. Instead each provider adapter is constructed with a `KeySource` (see [interfaces.md](interfaces.md)) and calls it on every `Judge` and `Check`, so a newly saved key works without a restart. `cmd/upw` wires the `KeySource`: `UPW_LLM_API_KEY` if set, otherwise `settings.Service`. Until the LLM milestone (M4) the environment variable is the only route. Milestones are in [mvp-plan/PLAN.md](../mvp-plan/PLAN.md), section 10.

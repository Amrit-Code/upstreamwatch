# Data access

This document describes how code reaches the database: the `store.DB` type, the thirteen store interfaces, which tables sit behind each one, and the rules for transactions and writers. Table and column details are in [database.md](database.md).

`store.DB` lives in `internal/store`. It is the only package that imports `database/sql`, holds the migrations, and opens transactions. Services never see SQL. Each service asks for the narrow slice it needs, so the prefilter cannot write findings by accident and a test can fake five methods instead of fifty.

## Store interfaces

```mermaid
classDiagram
  direction LR
  class store_DB {
    Open() Migrate() InTx()
    BackupTo() Export()
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
| `AdvisoryStore` | advisories, advisory_affected, advisory_matches, osv_queries | vulns |
| `UpstreamStore` | upstream_repos, package_upstream | upstream (write); issues, prefilter, coverage (read) |
| `IssueStore` | upstream_issues, upstream_issue_comments | issues (write); prefilter, triage (read) |
| `CandidateStore` | issue_candidates | prefilter (write); triage, eval (read) |
| `VerdictStore` | llm_verdicts | triage, eval |
| `FindingStore` | findings, finding_state, digests | vulns, triage, findings.Service, digest |
| `EvalStore` | eval_labels | eval, findings.Service on dismiss |
| `RunStore` | runs | pipeline.Runner, health, coverage |
| `CacheStore` | http_cache, kv | httpx (`http_cache`); vulns (`kv`, for the feed cursors) |
| `SettingsStore` | settings | `settings.Service` only (read and write); the table holds ciphertext, and only `settings.Service` encrypts or decrypts. `triage` does not use it |

The `schema_migrations` table is owned by `store.DB` itself and has no interface.

### What is built

The table above is the design. So far these methods exist on `store.DB`:

| Interface | Methods on `store.DB` | Table |
|---|---|---|
| `RepoStore` | `UpsertRepo`, `RecordFetch`, `GetRepo`, `ListRepos` | repos |
| `TimelineStore` | `ListEvents`, `ListIntervals`, `ApplyTimeline` | commits, manifests, packages, dep_events, dep_versions (and `repos.last_indexed_sha`, `repos.timeline_settings`) |
| `RunStore` | `StartRun`, `FinishRun`, `LatestRuns` | runs |
| `FindingStore` | `UpsertFinding`, `ResolveFinding`, `ListOpenFindings`, `ListFindings`, `ListUnresolvedFindings`, `GetFinding`, `SaveFindingState`, `MarkReported`, `GetFindingDetail` (also reads repos, packages, dep_versions, advisories, advisory_affected and advisory_matches), `SaveDigest`, `FailedDigests`, `MarkDigestRetried`, `LastSentDigest` | findings, finding_state, digests |
| `AdvisoryStore` | `ListAllIntervals` (reads dep_versions with the repo slug and package URL), `QueriedVersions`, `MarkQueried`, `AdvisoryModified`, `SaveAdvisory`, `ListAdvisories`, `ApplySignals`, `ReplaceMatches` | advisories, advisory_affected, advisory_matches, osv_queries (`SaveAdvisory` also inserts packages not stored yet) |
| `CacheStore` | `GetHTTPValidator`, `SaveHTTPValidator` (http half); `GetKV`, `SetKV` (kv half) | http_cache, kv |

`store.DB` also has `Open`, `Close`, `Migrate`, `InTx` and `BackupTo`. `Export` and the other interfaces come with the milestones that need them.

`BackupTo(ctx, path)` writes a consistent, compacted copy of the database with `VACUUM INTO`, while readers and writers carry on. It refuses a path that already exists, creates a missing parent directory with mode 0700, gives the copy mode 0600, and cannot be called inside a transaction.

`ApplyTimeline` writes one timeline run for one repo in one `InTx` transaction. It appends the new commits, upserts manifests and packages, inserts the events, replaces the repo's intervals while keeping the row ID of every interval whose manifest, package and opening commit are unchanged, and sets `repos.last_indexed_sha` and `repos.timeline_settings` (NULL when the fingerprint is empty). With `Reset` set it first deletes the repo's commits, events and intervals, for a rebuild from the root: a branch whose history was rewritten, a changed settings fingerprint, or `upw run timeline -rebuild`. An existing `packages` row keeps the name it was first stored with; the upsert only fills in an ecosystem the row lacked.

Each interface a service uses is declared in that service's package, so the sets differ by consumer: `pipeline.RunStore` (`StartRun`, `FinishRun`) and `pipeline.FindingStore` (`UpsertFinding`, `ResolveFinding`), `query.RunStore` (`LatestRuns`) and `query.FindingStore` (`ListOpenFindings`), `httpx.CacheStore` (`GetHTTPValidator`, `SaveHTTPValidator`), `ingest.RepoStore` (`UpsertRepo`, `RecordFetch`), `timeline.RepoStore` (`GetRepo`), `timeline.TimelineStore` (`ListEvents`, `ApplyTimeline`) and `query.TimelineStore` (`GetRepo`, `ListRepos`, `ListIntervals`). The M2 interfaces are declared the same way: `vuln.AdvisoryStore`, `vuln.KVStore` (`GetKV`, `SetKV`) and `vuln.FindingStore` (`UpsertFinding`, `ResolveFinding`, `ListUnresolvedFindings`) for the vulns job; `report.Store` (`ListFindings`, `GetFindingDetail`, `MarkReported`, `LatestRuns`, `SaveDigest`, `FailedDigests`, `MarkDigestRetried`, `LastSentDigest`) for the digest job; `findings.Store` (`GetFinding`, `SaveFindingState`) for `findings.Service`; and `ops.Backuper` (`BackupTo`). `*store.DB` satisfies all of them. `UpsertFinding` inserts a finding in state `open` together with its `finding_state` row, in one transaction; for a fingerprint that exists it updates the `dep_version_id` link, score and rank reason, clears `resolved_ts` and leaves the operator state alone. Its last-changed time moves to the new value only when the score or rank reason differs from the stored one or the finding was resolved, so a job that sees the same finding again does not make it count as changed in the next digest. `ListOpenFindings` returns unresolved findings in state `open` or `acked`, highest score first. `ListFindings` takes a `model.FindingFilter` and returns unresolved findings, highest score first, filtering on the effective state (a snooze that has ended counts as open); the filter's `Now` is required. `ListAllIntervals` returns every repo's intervals, open and closed, with the repo and package IDs. `ReplaceMatches` makes a list the repo's complete set of advisory matches in one transaction, and refuses, writing nothing, a match whose interval is not one of that repo's. `SaveAdvisory` upserts an advisory and replaces its `advisory_affected` rows; it leaves the exploit-signal columns of an existing advisory alone, and `ApplySignals` writes them, matching a signal's CVE to an advisory id or alias.

## Rules

- Writes that must land together, such as the issues page and its cursor, or a timeline run's commits, events and intervals, go through `store.DB.InTx`, which hands the service the same interfaces bound to a transaction. `InTx` opens an `IMMEDIATE` write transaction, commits when the function returns nil and rolls back on an error or panic; called inside a transaction it joins it. Before `BEGIN`, an outermost `InTx` waits in a process-wide first-in, first-out write queue, so write transactions from one process take turns in arrival order and the busy timeout only covers writers in other processes. A single-statement write outside a transaction waits in the same queue, so it cannot meet an open transaction in SQLite's busy handler; inside `InTx` it runs directly, since the transaction holds the queue. The wait ends when the caller's context does, with an error and no transaction opened (or, for a single-statement write, with the statement not run). The function must use only the transaction it is given: an `InTx` on the outer `*DB` from inside it waits for its own transaction until the context ends. `store.Bind` adapts `InTx` to a consumer-declared interface, so a service can run its own interface in a transaction without importing the store.
- `store.Open` sets WAL, a five-second busy timeout and foreign keys on, and refuses to continue if the pragmas did not take. It creates the database file with mode 0600 and a missing directory with mode 0700; an existing data directory that group or others can access is set to 0700, and `Open` fails if it cannot be.
- The scheduler is the single writer for pipeline tables. The web server writes only finding state, eval labels and settings. SQLite runs in WAL mode so page loads never wait on a job.
- Store methods take and return domain objects (see [domain-model.md](domain-model.md)), never rows or `sql.Null*` types.
- Migrations are numbered SQL files embedded in the binary and applied at startup, each in its own transaction with its `schema_migrations` row. They are forward-only and additive until 1.0. The binary refuses to start on a database newer than itself, and on one whose applied migration file has since been edited (the stored checksum differs).

## Settings and the LLM key

The settings page lets the operator paste the LLM key into the web UI. The key is stored in the `settings` table encrypted at rest. The encryption key is a file in the data directory with mode 600. The key is write-only in the UI: the page shows the last four characters and never the value.

`settings.Service` is the only code that encrypts and decrypts the key and the only user of `SettingsStore`; the store holds ciphertext only. `triage` does not use `SettingsStore` and never reads the key. Instead each provider adapter is constructed with a `KeySource` (see [interfaces.md](interfaces.md)) and calls it on every `Judge` and `Check`, so a newly saved key works without a restart. `cmd/upw` wires the `KeySource`: `UPW_LLM_API_KEY` if set, otherwise `settings.Service`. Until the LLM milestone (M4) the environment variable is the only route. Milestones are in [mvp-plan/PLAN.md](../mvp-plan/PLAN.md), section 10.

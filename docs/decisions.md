# Decisions

This document records what has been decided about upstreamwatch and why: the owner's decisions of 2026-11-19, the eight changes to the plan that were accepted, and the fourteen open decisions from section 12 of [the plan](../mvp-plan/PLAN.md) with the option chosen. Where this file and the plan disagree, this file is newer and wins.

## Owner decisions, 2026-11-19

- The eight changes listed below are accepted and are now simply the design.
- The fourteen defaults in plan section 12 stand, except where the table below says otherwise.
- Builds and tests run on the Mac (Go 1.27.x from Homebrew). The homelab VM is for deployment only. See [development.md](development.md).
- The "digest" is the daily summary message. The notifier adapters in the MVP are email (SMTP, standard library), Discord webhook, Slack webhook, ntfy and stdout. Several can be enabled at once. The owner will use email.
- Email is a new adapter that is not in the plan. Its config has host, port, username, password (from an environment variable), from and to. It adds one external source: the operator's SMTP server. See [interfaces.md](interfaces.md) and [external-sources.md](external-sources.md).
- LLM key: in the finished product the operator pastes the key into a settings page in the web UI. The `UPW_LLM_API_KEY` variable still works and takes precedence. This adds a `GET /settings` and `POST /settings` page, a `settings` table that holds the key encrypted at rest (key file in the data directory, mode 600), a `SettingsStore` interface on `store.DB`, and a rule that the key is write-only in the UI (shown as its last four characters). It is scheduled with the LLM milestone (M4). Until then the environment variable is the only route.
- The GitHub repository is public from the first commit. The module path is `github.com/Amrit-Code/upstreamwatch`. Nothing private may appear in the repository.
- Git: the owner and the lead session are the two parties. The lead session commits and merges straight to `main` for now. Subagents never commit, push or merge.
- Review weight: `make check` on every task, a fresh reviewer on design-heavy tasks only, and a security review once per milestone. Reviewers and checkers run on a lower-tier model (Sonnet).
- Dependencies: the libraries named in the plan are approved. The CLI uses the standard library `flag`, with no Cobra or Viper. Anything else needs the owner's yes. There are no test frameworks such as testify; fakes are written by hand.
- Model routing: the lead session orchestrates. Opus builds design-heavy packages. Sonnet does fully specified work, tests, reviews, security review and docs upkeep.

## Owner decisions, 2026-11-19 (build start)

These replace the review-weight and model-routing lines above where they differ.

- The lead session orchestrates only. All code and tests are written by Opus agents: `go-builder`, `go-mech` and `test-writer` are all dispatched with `model: "opus"`. Sonnet remains for docs upkeep and the milestone security review.
- Review: every work order's diff is inspected by GPT-6 Astra (Codex CLI, read-only, through the claudex-loop runner's `inspect` mode) before commit. Astra never edits; findings go back to the building agent. This holds while Codex weekly usage is below 80%; at or above 80% the Sonnet `reviewer` agent takes over as described in the review-gate skill.
- YAML library: `go.yaml.in/yaml/v3` is approved (pinned at v3.0.6).
- The owner creates the public GitHub repository. Until it exists the lead commits locally only.

## Lead decisions during M0

Rulings the lead session made while building M0, in the work orders under `.claude/plans/m0/`.

- The whole documented schema is created in one migration, `0001_initial_schema.sql`, not table by table as each milestone needs it. With foreign keys on, SQLite rejects a write to a table whose `REFERENCES` names a table that does not exist yet, and constraints cannot be added later under the additive-only rule. Foreign keys to `dep_versions` have explicit delete rules because a reindex deletes and rebuilds a repo's `dep_versions`: `advisory_matches.dep_version_id` and `issue_candidates.dep_version_id` are `ON DELETE CASCADE`, and `findings.dep_version_id` is `ON DELETE SET NULL`, so a finding keeps its row. Every other foreign key is `NO ACTION`.
- One `job_failure` finding per job. Its fingerprint is `job_failure|<job>` (the plan's `kind|repo|package|advisory_or_issue` form has no repo, package or advisory to fill for a failed job). A failed run raises or refreshes the finding; the next successful run of the same job resolves it.
- `httpx`, the shared HTTP client, was built in M0 and not in M2 as the plan's milestones place it. It brings `model.HTTPValidator` and the `httpx.CacheStore` interface, implemented by `store.DB` over the `http_cache` table. Its address policy follows the rulings in the httpx work order: see [external-sources.md](external-sources.md).

## Owner decisions, 2026-11-19 (start of M1)

- M1 starts while one M0 item is still open: the owner will put the real GitHub tokens on the VM and `upw doctor` is then rerun (M0 checklist item 23). M0 is tagged `m0` after that.
- The low M0 security finding (job error text shown unredacted on `/health` and the findings page) is waived for M0 and fixed in M1 (M1 checklist item 16).
- The made-up `github_pat_...` test strings in early commits of the public history stay; the history is not rewritten.

## Lead decisions for M1

Rulings the lead session made while planning M1, in the work orders under `.claude/plans/m1/`.

- First sync creates the clone with `git init --bare` and fetches only the indexed branch (no tags), instead of `git clone --bare`. Every fetch lands in a staging ref (`+refs/heads/<b>:refs/upw/staging/<b>`), and `refs/heads/<b>` moves with `git update-ref` only after the size cap check passes, so a rejected fetch never moves the indexed branch. Every fetch passes the URL explicitly, so no remote URL is stored in the clone's config and a URL change in config takes effect on the next fetch.
- `go.mod` is parsed with `golang.org/x/mod/modfile`, not the `manifests` library, because the library (v0.14.0) ignores `replace` directives and the `go` and `toolchain` lines. Dockerfiles and compose files are parsed by upstreamwatch's own code, because the library reports build-stage names (`FROM builder`) as images and drops the tag of an image pinned by digest. The library is used for every other format.
- The manifest event diff compares version, requirement and the direct flag; a change in any of them emits `changed`. A change of scope alone emits no event. (Changed on 2026-11-19 after WO-08: comparing only version and requirement left a Go module that moved from `// indirect` to a direct require marked indirect for good, and issue matching uses direct dependencies only.)
- `uv.lock` and `poetry.lock` cannot tell direct from transitive dependencies, so their entries are stored with `direct` NULL; the matching manifest (pyproject.toml) records the same packages as direct with their requirement. Unity's `Packages/packages-lock.json` records each package's `depth`, so its entries are direct at depth 0 and transitive above it (NULL only when `depth` is absent).
- `github.com/git-pkgs/purl` is pinned at v0.2.1, the version the plan names, rather than the v0.1.24 that `manifests` v0.14.0 requires. v0.2.1 keeps the case of Go module paths (`pkg:golang/github.com/BurntSushi/toml`), so the Go package URLs upstreamwatch builds itself and the ones the library builds agree.
- A lockfile entry for a local-path or editable package (the project's own root in `uv.lock`, a path dependency) is not a dependency and is dropped, so a public package with the same name can never be matched against the project.
- The timeline skips manifests under any `testdata`, `vendor` or `node_modules` directory, at any depth, and under the directory prefixes a target lists in `targets[].exclude_paths`. Test fixtures, vendored copies and installed packages are not what the project ships, and would otherwise be matched against advisories.
- A package is identified by its version-less package URL. For PyPI the package URL normalises the name (lower case, runs of `-`, `_` and `.` as `-`, as PEP 503 and the purl spec say), so a lockfile that respells `Jinja2` as `jinja2` describes the same package: no event when the version is unchanged, `changed` when it moved. `packages.name` keeps the first spelling seen; the timeline shows the package URL.
- From the M1 security review: a target using `auth: targets` or `auth: upstream` must be hosted on `github.com`, because both tokens are GitHub tokens; any other host needs `auth: none`. Dependency names or package URLs containing control characters, whitespace, `://` or `@` are dropped (they are a URL or an injection attempt, not a package), and every extracted version and requirement passes through `model.Redact` before it is stored. The `@` rule must be relaxed to allow a leading `@scope/` when an npm extractor is added (scoped npm names start with `@`).
- When one file names the same package twice (two `FROM golang:` stages with different tags), the first occurrence in the file wins.
- A manifest that fails to parse at a commit produces no events at that commit; its previous state stands.
- `dep_versions` gains a `requirement` column (migration `0002_timeline.sql`), and a unique index on repo, manifest, package and opening commit. A timeline run keeps the row ID of every interval whose identity is unchanged, so advisory matches and findings keep pointing at it.
- If a fetched branch no longer contains the last indexed commit (history rewritten), the timeline for that repo is rebuilt from the root.
- `internal/gitsrc` uses `httpx.BlockedAddr` for the private-address check, so git remotes and HTTP adapters refuse the same ranges. `internal/extract` may use the YAML library, for compose files.
- `targets[].allow_private_hosts: true` exempts one target's host from the private-address check; the URL must still be https.

## The eight accepted changes

| Change | Why |
|---|---|
| `pipeline.Runner` and a `Job` interface | The plan has the scheduler call jobs directly. With a Runner, the scheduler, `upw run` and tests all start a job the same way, and run recording and failure findings are written once. |
| `httpx` shared HTTP client | ETag handling, rate-limit headers, retries and the private-address check appear in five adapters in the plan. One client means one place to get them right. |
| `store.DB` behind small interfaces | The plan has a `store/` package but does not say how services see it. The access layer is now one concrete type with a narrow interface per group of tables. See [data-access.md](data-access.md). |
| `internal/model` for shared objects | Without it, feature packages import each other for types and Go rejects the cycle. See [domain-model.md](domain-model.md). |
| `usage` as its own job, reading through git | The plan describes the symbol index but gives it no place in the run order, and bare clones mean it cannot read a directory. |
| `llm_verdicts.request_payload` column | The finding page promises to show exactly what was sent to the model. The schema stored only the response. |
| `digests` table | "Send only if something changed since the last digest" needs a record of the last digest, and a failed send needs somewhere to be retried from. |
| Prefilter runs straight after issues | The plan's schedule has no prefilter entry, so candidates would never be created on a timer. |

Schema additions accepted since: `llm_verdicts.evidence_verified` (integer, 1 when every evidence quote was found in the issue text) and `digests.error` (text). Together with `llm_verdicts.request_payload`, the `digests` table and the `settings` table, these are the accepted additions to the plan's schema, listed in [database.md](database.md).

Nothing here changes scope, milestones or the stack. Every non-goal in the plan stays a non-goal. Later additions to the owner's decisions (email, `SettingsStore`) are listed above and are not part of the eight.

## Plan section 12: the fourteen decisions

| # | Decision | Chosen option | Note |
|---|---|---|---|
| 1 | Timeline engine | Own walker on `git-pkgs/manifests`, with `git pkgs` as the test oracle | The indexer of `git-pkgs` is internal; we need first-parent semantics and our own tables. |
| 2 | Notification channel | Email (SMTP), Discord webhook, Slack webhook, ntfy and stdout; several can be enabled at once. The owner uses email. | Changed from the plan, which defaulted to a Discord or Slack webhook and left email out. Plane stays out of the MVP. |
| 3 | LLM default and data sent | `claude-opus-5-5`, effort `low`, `snippet_mode: symbols` | Symbols-only keeps private code at home. Switch to `snippets` per repo only if the eval set shows it is needed. The key comes from `UPW_LLM_API_KEY` or the settings page (M4). |
| 4 | Unity scope | Timeline for all UPM packages and the editor; issue watching for git-URL packages only | No API for Unity's tracker. |
| 5 | Issue source | GitHub REST directly with a public-only token; ecosyste.ms only for repo metadata | The ecosyste.ms list endpoint has no bodies. |
| 6 | Firehose upstreams (`golang/go`) | Do not watch issues (`watch_issues: false`) | Rely on OSV `GO-` advisories and the toolchain derivation; revisit with a label filter later. |
| 7 | History scope | First-parent of the default branch | "What was on main" is the question. |
| 8 | License | Apache-2.0 | Patent grant; compatible with the dependencies. |
| 9 | Plane integration and tailnet retag | Keep `tag:dev` for the MVP | Retag when a Plane adapter is built. |
| 10 | A local Ollama server as the LLM | Not configured by default; use only if the owner asks | Standing instruction from the owner. |
| 11 | Where private eval labels live | Exported JSON on the Mac, with the database as the working copy | Not reconstructible, and the labels name private usage, so not in the public repo. |
| 12 | Transitive dependencies in the prefilter and LLM path | Direct only for issues; all for advisories | Transitive upstream issues rarely map to our symbols; advisories apply regardless. |
| 13 | Daily LLM cap | $2 per day | The cap only matters during backfill and eval at this scale. |
| 14 | Token model for private targets | Fine-grained personal access token now; GitHub App later if limits or expiry become annoying | Less code. Check that the organisation allows fine-grained tokens. |

## Corrections to PLAN.md found while writing the skills

- Clones use `git clone --bare` with no `--filter` flag. PLAN.md 5.1 writes `--filter=none`, which git rejects as an invalid filter-spec (confirmed on git 2.52).
- The git token is passed through per-invocation environment config (`GIT_CONFIG_COUNT` and friends), not `-c http.extraHeader=...` as PLAN.md 5.1 has it, because command-line arguments are readable by other processes on the machine.
- Verdict reuse when the configured model changes: scheduled runs reuse an existing verdict for the same issue, body, package, version, usage, prompt version and snippet mode even if the model differs; only `upw llm rejudge` and `upw eval run` match on the model too. This resolves PLAN.md 5.8 saying both that the cache key includes the model and that re-judging on a model change is opt-in.
- The layering lint (`go-arch-lint`) cannot see standard-library imports, so `make check` also runs a `go list` based check that only `internal/store` imports `database/sql` and only `internal/gitsrc` imports `os/exec`. The lint tool is run with `go run <module>@<version>` so it does not enter `go.mod`.

- PLAN.md 5.2 lists `go.sum` among the manifest pathspecs, but its resolution table says `go.sum` is ignored because it lists modules outside the build. The table wins: `go.sum` is not walked or parsed (decided 2026-11-19 while syncing the docs after M1 wave 1).

## Open

Nothing is open. The YAML library question was settled on 2026-11-19: `go.yaml.in/yaml/v3`, see the build-start decisions above.

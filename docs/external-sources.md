# External sources

This document lists every outside service `upw` talks to: what it takes from each, how, with what credentials, how often, through which interface, and what happens when the service is down. It also lists the libraries that run inside the binary and the two outside tools used only in tests. The interfaces are in [interfaces.md](interfaces.md); the system context diagram is in [architecture.md](architecture.md).

All calls go out from the binary. Nothing calls in. Failures never fail silently: the Runner records the run and raises a `job_failure` finding, which reaches the next digest.

## Services

| Source | What we take | How | Auth | How often | Behind interface | If it fails |
|---|---|---|---|---|---|---|
| Target git remotes | Commit history and manifest file contents | `git` over https, bare clone | Read-only token for private repos, sent as a header, never stored | 6 h | `GitSource` | job_failure finding; timeline stays at last good commit |
| OSV API | Advisories for each package version | `POST /v1/querybatch`, `GET /v1/vulns/{id}` | None | 6 h | `AdvisorySource` | Keep stored advisories; retry next run |
| OSV bucket | Which advisories changed since last look | `modified_id.csv` per ecosystem | None | Daily | `AdvisorySource` | Fall back to querybatch |
| CISA KEV | Known-exploited flag per CVE | JSON download | None | Daily | `ExploitSignals` | Ranking uses yesterday's flags |
| EPSS | Exploit probability per CVE | Gzipped CSV, follows redirect | None | Daily | `ExploitSignals` | Ranking uses yesterday's scores |
| deps.dev | Package to source repo and issue tracker | `GET /v3/systems/.../versions/...` | None | Once per package, monthly refresh | `UpstreamResolver` | Next resolver in the chain |
| packages.ecosyste.ms | Fallback for the same mapping | REST | None | On miss | `UpstreamResolver` | Package listed as unmapped on Coverage |
| Go vanity hosts | Repo behind a non-GitHub module path | `GET https://path?go-get=1` | None | Once per module | `UpstreamResolver` | Unmapped on Coverage |
| GitHub REST | Issues and comments of public upstream repos | `/repos/{o}/{r}/issues` and `/issues/comments` with ETags | Token with no repository access | Hourly | `IssueSource` | Cursor does not advance; stops early near the rate limit |
| LLM provider | One verdict per candidate | Anthropic SDK, or OpenAI-compatible chat endpoint | Your API key: `UPW_LLM_API_KEY`, or pasted into the settings page (the variable wins) | Hourly, capped per run and per day | `Provider` | Candidate stays queued and shows as unreviewed |
| Your SMTP server | Nothing; we send the digest | SMTP from the standard library; config has host, port, username, password (from an environment variable), from and to | SMTP username and password | Daily, weekly heartbeat | `Notifier` | Digest row marked failed and retried; visible on Coverage |
| Webhook or ntfy | Nothing; we send the digest | JSON POST | The URL is the secret | Daily, weekly heartbeat | `Notifier` | Digest row marked failed and retried; visible on Coverage |

Several notifiers can be enabled at once. A failure in one does not stop the others, because each send is recorded as its own row in `digests` (see [database.md](database.md)).

## Two GitHub tokens

The private side and the public side use different tokens:

- The targets token reads the private target repos. It needs Contents and Metadata read access only.
- The upstream token has no repository access. It exists only to lift the GitHub rate limit on public repos from 60 to 5,000 requests an hour and to make 304 responses free. It never touches the private targets.

## Libraries

Libraries are not on the table above because they run inside the binary:

- `git-pkgs/manifests`, `git-pkgs/vers`, `git-pkgs/purl` and `git-pkgs/vulns/osv`
- `modernc.org/sqlite`
- `robfig/cron`
- the Anthropic SDK

The CLI uses the standard library `flag` package. Anything beyond the libraries named in [the plan](../mvp-plan/PLAN.md) needs the owner's approval. There are no test frameworks; tests use hand-written fakes.

## Test referees

Two outside tools are used only in tests, as referees:

- `git pkgs history` checks our timelines against an independent implementation.
- `osv-scanner` checks our advisory matches against the same lockfiles.

A difference between our output and a referee's is a bug in one of the two and is investigated, not papered over.

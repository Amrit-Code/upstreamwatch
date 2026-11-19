# External sources

This document lists every outside service `upw` talks to: what it takes from each, how, with what credentials, how often, through which interface, and what happens when the service is down. It also lists the libraries that run inside the binary and the two outside tools used only in tests. The interfaces are in [interfaces.md](interfaces.md); the system context diagram is in [architecture.md](architecture.md).

All calls go out from the binary. Nothing calls in. Failures never fail silently: the Runner records the run and raises a `job_failure` finding, which reaches the next digest.

## Services

| Source | What we take | How | Auth | How often | Behind interface | If it fails |
|---|---|---|---|---|---|---|
| Target git remotes | Commit history and manifest file contents | `git` over https: `git init --bare`, then a fetch of the indexed branch only, no tags | Read-only token for private repos, sent as a header, never stored | 6 h | `GitSource` | job_failure finding; timeline stays at last good commit |
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

In the config, `notify` is a mapping with two keys: `channels`, a list with one entry per notifier, and `heartbeat`, a five-field cron expression for the "all quiet" message. A channel has a `type` (`email`, `discord_webhook`, `slack_webhook`, `ntfy` or `stdout`) and the fields of its type: `url` for the webhook and ntfy types; `host`, `port` (587 by default), `username`, `password`, `from` and `to` for email. The secret fields (a channel's `url`, an email `password`, the tokens and the LLM key) take only a `${NAME}` reference to an environment variable, as in `deploy/upw.example.yaml`, which uses `UPW_SMTP_PASSWORD` for the email password. The config loader holds them as `model.Secret`.

Several notifiers can be enabled at once. A failure in one does not stop the others, because each send is recorded as its own row in `digests` (see [database.md](database.md)).

## The shared HTTP client and its policy

Every HTTP adapter sends its requests through `internal/httpx` (the email notifier, which speaks SMTP, is the exception). One client means one place for these rules, which come from `internal/httpx`:

- Only `https` URLs, with no userinfo and a host. At most five redirects, each checked against the same rules. Once a redirect chain has left the first request's host, every later hop (even one back to it) is sent with only `User-Agent`, `Accept`, `Accept-Encoding`, `Accept-Language` and `Content-Type`, so credentials and validators are not forwarded, cookie-jar cookies included. No proxy.
- A connection is refused, after the hostname is resolved, when the address is loopback, private, link-local, multicast or unspecified, or lies in the shared address space `100.64.0.0/10` (where tailnet addresses live), "this network" `0.0.0.0/8`, `192.0.0.0/24`, `198.18.0.0/15`, `240.0.0.0/4`, the IPv4 and IPv6 documentation ranges, local-use NAT64 `64:ff9b:1::/48`, Teredo `2001::/32` or deprecated site-local `fec0::/10`. For NAT64 `64:ff9b::/96`, 6to4 `2002::/16` and IPv4-compatible `::/96` addresses the embedded IPv4 address is judged by the same rules.
- A host the operator names in the client's `AllowPrivateHosts` option is exempt from the address check and may use `http`. Nothing in production code sets it; tests use it to reach a local fake server.
- Defaults: a two-minute timeout per request, a 64 MiB response body cap, three attempts for a GET or HEAD, a 30-second longest wait between attempts, and a 100 ms gap between requests to `api.github.com`. Other methods are sent once. A server asking for a longer wait than the maximum (`Retry-After` or the rate-limit reset header) ends the request with a rate-limit error that carries the reset time.
- `httpx.BlockedAddr` exposes the same address check. `internal/gitsrc` applies it to git remotes, because `git` dials for itself and this client never sees those connections (see the next section).
- Errors name the method, host and path, never the query string; a request marked `SecretPath` (webhooks) names the host only.
- Conditional requests use `ETag` and `Last-Modified`. The client either keeps them itself in the `http_cache` table, keyed by exact URL, or leaves them to the caller, which passes the ETag in and stores the one that comes back. A 304 counts as an answer to a conditional request only when the request that got it, after redirects, carried a validator.

## Git remotes and their policy

`internal/gitsrc` runs the `git` binary for every clone and fetch. It does not use `internal/httpx`, so it applies its own checks before each network operation:

- Only `https` URLs, with a host and no userinfo. A URL starting with `-`, an `scp`-style address, a local path and `<transport>::` remote-helper syntax are refused. Only tests allow `file://` URLs.
- The host is resolved and the connection refused when any address is one `httpx.BlockedAddr` refuses (the ranges listed above); git is then pinned to the address that was checked. A target marked `AllowPrivateHost` is exempt from the address check; the URL must still be https. The config key that sets it is `targets[].allow_private_hosts` (default false; see [decisions.md](decisions.md)).
- The first sync creates the clone with `git init --bare` and fetches only the indexed branch, with no tags. The fetch lands on a staging ref, which is moved onto the branch only after it succeeded and the clone is within the size cap. Every fetch passes the URL on the command line, so no remote URL is stored in the clone's config and a URL change in config takes effect on the next fetch. A failed first sync leaves no clone behind.
- The token is sent as an `Authorization` header scoped to the target's host, through environment config only: never on the command line, in the URL or in a file. Error text is scrubbed of it.
- git runs with an explicit argument list, a minimal environment and an empty home directory, and with every protocol but https, credential helpers, redirects, submodules and hooks refused.
- Limits: a clone directory over 2048 MiB stops the fetch and fails the sync; a first sync has 30 minutes and a later fetch or `ls-remote` 10 minutes; `ReadBlobs` skips a blob over 5 MiB (it comes back as `TooLarge`). The first three are the config keys `git.max_clone_mb`, `git.clone_timeout` and `git.fetch_timeout`, with those values as defaults (the timeouts are Go durations such as `30m`; all three must be positive); `cmd/upw` passes them to `gitsrc.Options`. The 5 MiB blob limit has no config key.

## What `upw doctor` probes

`upw doctor` calls each adapter's `Check` through one `httpx` client with no cache store. It does not open the database, so it cannot see an LLM key saved on the settings page. The probes are:

| Check | Request | Notes |
|---|---|---|
| git | `git --version` | Runs with a minimal environment: the secrets in the process environment are not passed to git. |
| OSV | `GET https://api.osv.dev/v1/vulns/GO-2020-0001` | A small, long-published advisory. |
| deps.dev | `GET https://api.deps.dev/v3/systems/go/packages/github.com%2Fgoogle%2Fuuid/versions/v1.6.0` | One small, published package version. |
| GitHub | `GET https://api.github.com/rate_limit` | Does not count against the rate limit. Run once for each configured token (upstream, targets), or once without a token when neither is set. Proves GitHub accepts the token, not that it can read the private targets. |

## Two GitHub tokens

The private side and the public side use different tokens:

- The targets token reads the private target repos. It needs Contents and Metadata read access only.
- The upstream token has no repository access. It exists only to lift the GitHub rate limit on public repos from 60 to 5,000 requests an hour and to make 304 responses free. It never touches the private targets.

## Libraries

Libraries are not on the table above because they run inside the binary:

- `git-pkgs/manifests`, `git-pkgs/vers`, `git-pkgs/purl` and `git-pkgs/vulns/osv`. `manifests` is pinned at v0.14.0 and parses the Python files and GitHub Actions workflows; `purl` is pinned at v0.2.1 (see [decisions.md](decisions.md))
- `golang.org/x/mod/modfile` (v0.43.0), for `go.mod`: the `manifests` library ignores `replace` directives and the `go` and `toolchain` lines (see [decisions.md](decisions.md))
- `modernc.org/sqlite`
- `robfig/cron/v3`
- `go.yaml.in/yaml/v3`, for the config file and, in `internal/extract`, for compose files; approved by the owner on 2026-11-19 (see [decisions.md](decisions.md))
- the Anthropic SDK

The CLI uses the standard library `flag` package. Anything beyond the libraries named in [the plan](../mvp-plan/PLAN.md) needs the owner's approval. There are no test frameworks; tests use hand-written fakes.

## Test referees

Two outside tools are used only in tests, as referees:

- `git pkgs history` checks our timelines against an independent implementation.
- `osv-scanner` checks our advisory matches against the same lockfiles.

A difference between our output and a referee's is a bug in one of the two and is investigated, not papered over.

The `git pkgs history` comparison runs as `make e2e` (the test `TestParity` in `cmd/upw/parity_e2e_test.go`, under the build tag `e2e`). It indexes two public repositories, each pinned to a commit, with upstreamwatch and with `git pkgs history`, and compares the dependency events. It needs the network and `git-pkgs` on `PATH`, so `make check` leaves it out. Each difference a recorded decision in [decisions.md](decisions.md) explains is listed in the test as an exception that names the event; an exception that no longer matches a difference fails the test. See [development.md](development.md) for the CI job.

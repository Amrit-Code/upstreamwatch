# Invoking git from internal/gitsrc

`internal/gitsrc` is the only package that starts a subprocess. Target URLs, branch names and everything inside a target repo are untrusted: a typo in config or one compromised target is enough to reach the code below. PLAN.md sections 5.1 and 11 are the source; this file is how to apply them.

## Building the command

- `exec.CommandContext(ctx, gitPath, args...)` with an argument slice. No shell, no `sh -c`, no joining values into one string. `gitPath` is resolved once at startup with `exec.LookPath`; input never chooses the binary.
- Put `--` before user-influenced URLs and paths in commands where it ends options: `git clone --bare -- <url> <dir>`, `git ls-remote --symref -- <url> HEAD`.
- In commands where `--` introduces pathspecs (`git log`), put `--end-of-options` before revisions and `--` before pathspecs: `git log --first-parent ... --end-of-options <range> -- <pathspecs>`. The same applies to `git fetch --end-of-options <remote> <refspec>`.
- Branch names, whether from config or from `ls-remote`, are rejected if they start with `-` or if `git check-ref-format --branch <name>` rejects them.
- Slugs become paths (`<data_dir>/repos/<slug>.git`). Validate the slug as lowercase letters, digits and hyphens before building the path, so `../` cannot appear.
- Parse output with NUL separators (`%x00` in `--format`, `-z` where offered). Paths and commit subjects are attacker-controlled and can contain newlines.

## Environment

Set `cmd.Env` explicitly instead of inheriting it. The process environment holds every upstreamwatch secret (both GitHub tokens, the LLM key, the webhook URL) and git needs none of them.

| Variable | Value | Why |
|---|---|---|
| `PATH` | the container's PATH | git finds its helpers |
| `HOME` | an empty directory under the data dir | no user config is found |
| `LC_ALL` | `C` | output you can parse |
| `GIT_TERMINAL_PROMPT` | `0` | fail instead of waiting for a password |
| `GIT_CONFIG_NOSYSTEM` | `1` | system config cannot add `url.insteadOf`, helpers or hooks |
| `GIT_CONFIG_GLOBAL` | `/dev/null` | the same for global config |

Per-invocation config with `-c` (none of it secret):

- `protocol.allow=never` and `protocol.https.allow=always`: `file://`, `ssh://`, `git://` and `ext::` fail even if a URL slips past validation.
- `credential.helper=` (empty): no helper stores or offers credentials.
- `http.followRedirects=false`: a redirect would send git to a host the URL check never saw. A renamed target then fails its fetch, which reaches the digest as a `job_failure` and the operator updates the URL.
- `fetch.recurseSubmodules=false` and `core.hooksPath=/dev/null`. Never pass `--recurse-submodules`.
- `http.lowSpeedLimit` and `http.lowSpeedTime`, so a stalled transfer aborts before the context deadline.

## The token

- Pass the targets token as an HTTP header through environment config, scoped to the target's host:
  `GIT_CONFIG_COUNT=1`, `GIT_CONFIG_KEY_0=http.https://<host>/.extraHeader`, `GIT_CONFIG_VALUE_0=Authorization: Basic <base64 of "x-access-token:" + token>`.
  This is the header form GitHub's git endpoint accepts for tokens (it is what `actions/checkout` sends). PLAN.md 5.1 writes `Bearer`; whichever form ships, prove it with `upw doctor` against a private target.
- Not in argv: arguments are visible to every process on the host through `ps` and `/proc`, and appear in errors that echo the command. The environment is visible only to the same user.
- Never in the URL: `git clone` writes the remote URL into the bare repo's config, so a token there lands on disk. Never in a credential file or the repo config.
- Only targets whose `auth` setting names a token get the header, carrying that token. `auth: none` gets none.
- git's stderr can echo URLs, which never contain tokens. Capture stderr with a size cap, and never put the environment in an error or log line.

## URL check before every network operation

git dials for itself, so the httpx dialer never sees its connections. Before every `clone`, `fetch` and `ls-remote`, not only at config load:

1. Parse the URL. Require `https`, no userinfo, no leading `-`. Reject scp-style `host:path`.
2. Resolve the host. If any resolved address falls in a blocked range (the same list as httpx: loopback, private, link-local, multicast, unspecified, `100.64.0.0/10`), refuse unless `allow_private_hosts` is set.
3. Pin git to the address you checked with `-c http.curloptResolve=<host>:443:<address>`, so a second DNS answer cannot point the connection somewhere else.

## Repository shape and limits

- Bare clones only: `git clone --bare -- <url> <dir>`, with no `--filter` (PLAN.md's `--filter=none` is not a valid filter spec; the intent is a full clone). Directory mode 700.
- No working tree and no checkout, ever. Source files for the usage index are read at HEAD with `ls-tree` and `cat-file` through `GitSource.ListFiles` and `ReadBlobs`.
- Every invocation runs under a context deadline from config; clone gets a longer one than fetch.
- Size cap `gitsrc.max_clone_mb` (default 2048). git has no flag for it: watch the destination directory's size while clone or fetch runs, cancel the context when it passes the cap, delete a partial clone, and return an error the job turns into a `job_failure`.
- `cat-file --batch`: one long-lived process per repo per job. Read each header's size and discard blobs over the manifest size cap instead of buffering them; treat `missing` as an absent file; close stdin and `Wait` at the end; the context kills it on cancel.

## Tests

The URL policy and protocol allowlist are constructor inputs to `gitsrc`, so tests can allow `file` for fixture repos built in a temp dir. The production wiring passes the strict policy, and a test asserts that it does. Fixture repo construction is in `go-test`.

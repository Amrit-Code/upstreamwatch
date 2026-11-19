# WO-01: gitsrc: bare clone, fetch, first-parent log and blob reads

Milestone: M1
Agent: go-builder   Model: opus
Depends on: none (lead groundwork commit for M1)
Report file: `.claude/plans/m1/WO-01-gitsrc.report.md`
Ticket: UPW-28

## Objective

`gitsrc.CLI` implements the git side of M1: it syncs a target into a bare clone under the data directory without ever writing a credential to disk, walks the first-parent history for commits that touched manifest paths, and reads blobs through one `git cat-file` process. It also provides the fixture git repository builder that this and later test suites use. Serves M1 checklist items 2, 3, 4, 5, 9 and 13.

## Owned files

- `internal/gitsrc/` (all files; `cli.go` and `cli_test.go` exist from M0 and may be changed)
- `internal/testutil/gitrepo/` (new package: the fixture repository builder)
- `cmd/upw/doctor.go`: only the one line that constructs `gitsrc.New`, to pass the new `Options` argument (`gitsrc.New(gitsrc.Options{}, logger)` is enough: doctor only calls `Check`)

Lead-owned, read-only for you (stop and report if one is wrong): `internal/model/` (in particular `timeline.go`), `internal/ingest/interfaces.go`, `internal/timeline/interfaces.go`, `internal/httpx/`, `.go-arch-lint.yml`, `go.mod`, `go.sum`.

## Interfaces

### Consumed

```go
// internal/ingest/interfaces.go
type GitSource interface {
	Sync(ctx context.Context, repo model.TargetRepo) (model.SyncResult, error)
}

// internal/timeline/interfaces.go
type GitSource interface {
	Head(ctx context.Context, repo model.TargetRepo) (string, error)
	IsAncestor(ctx context.Context, repo model.TargetRepo, ancestor, commit string) (bool, error)
	ManifestCommits(ctx context.Context, repo model.TargetRepo, from, to string, patterns []string) ([]model.Commit, error)
	ReadBlobs(ctx context.Context, repo model.TargetRepo, refs []model.BlobRef) ([]model.Blob, error)
}

// internal/httpx/policy.go
func BlockedAddr(a netip.Addr) bool
```

Read the doc comments on these interfaces in the files themselves; they are part of the contract. The model types (`TargetRepo`, `SyncResult`, `Commit`, `BlobRef`, `Blob`, `Secret`) are in `internal/model/timeline.go` and `internal/model/secret.go`.

### Produced

```go
// internal/gitsrc
type Options struct {
	ReposDir      string        // clones live at ReposDir/<slug>.git; created 0700
	HomeDir       string        // HOME for every git process: an empty directory, created 0700
	MaxCloneBytes int64         // size cap on one clone directory; zero means 2048 MiB
	CloneTimeout  time.Duration // deadline for a first sync; zero means 30 minutes
	FetchTimeout  time.Duration // deadline for a later sync and for ls-remote; zero means 10 minutes
	MaxBlobBytes  int64         // ReadBlobs cap per blob; zero means 5 MiB
	// AllowFileURLs lets Sync accept file:// URLs and skips the address
	// check for them. Only tests set it, to sync from fixture repositories.
	AllowFileURLs bool
	// LookupHost resolves a host for the address check; nil means
	// net.DefaultResolver.LookupNetIP with network "ip".
	LookupHost func(ctx context.Context, host string) ([]netip.Addr, error)
}

func New(opts Options, logger *slog.Logger) *CLI
func (g *CLI) Check(ctx context.Context) error // unchanged behaviour
func (g *CLI) Sync(ctx context.Context, repo model.TargetRepo) (model.SyncResult, error)
func (g *CLI) Head(ctx context.Context, repo model.TargetRepo) (string, error)
func (g *CLI) IsAncestor(ctx context.Context, repo model.TargetRepo, ancestor, commit string) (bool, error)
func (g *CLI) ManifestCommits(ctx context.Context, repo model.TargetRepo, from, to string, patterns []string) ([]model.Commit, error)
func (g *CLI) ReadBlobs(ctx context.Context, repo model.TargetRepo, refs []model.BlobRef) ([]model.Blob, error)

// internal/testutil/gitrepo: a builder for small non-bare repositories in a
// temp directory, with fixed author and committer identities and dates so
// SHAs and timestamps are deterministic. Design the API yourself; it must be
// able to: write and delete files and commit them with a given committer
// date; create a branch, commit on it, switch back; merge with --no-ff at a
// given committer date; and report each commit's SHA and the repo's path and
// file:// URL. It fails the test (t.Fatalf) on any git error.
```

## Requirements

Follow `.claude/skills/outbound-client/references/GIT-INVOCATION.md` in full. It is part of this work order. The points below add to it or settle what it leaves open.

1. `CLI` stays the only `os/exec` user in production code. Every git process gets the environment from that reference (`HOME` = `Options.HomeDir`), plus per-invocation `-c` config: `protocol.allow=never`, `protocol.https.allow=always` (and `protocol.file.allow=always` only when `AllowFileURLs`), `credential.helper=`, `http.followRedirects=false`, `fetch.recurseSubmodules=false`, `core.hooksPath=/dev/null`, `http.lowSpeedLimit=1000`, `http.lowSpeedTime=60`.
2. Before every network operation (`ls-remote`, `fetch`), the URL is checked: scheme `https` (or `file` only when `AllowFileURLs`), no userinfo, no leading `-`, not scp-style, non-empty host. Each refusal returns an error naming the reason (for example `scheme "ssh" not allowed, only https`) and starts no git process. `ext::` URLs and `http://` are refused by the same check.
3. Address check: unless `repo.AllowPrivateHost`, resolve the host with `Options.LookupHost`; if any address is blocked by `httpx.BlockedAddr`, or none is returned, refuse with an error naming the host and saying it resolves to a private or reserved address. Then pin git to the first checked address with `-c http.curloptResolve=<host>:443:<addr>` (IPv6 in brackets). When `repo.AllowPrivateHost` is true the check and the pin are skipped, but the scheme must still be https.
4. Credential: when `repo.Token` is set, pass it only through `GIT_CONFIG_COUNT`/`GIT_CONFIG_KEY_n`/`GIT_CONFIG_VALUE_n` as `http.https://<host>/.extraHeader` = `Authorization: Basic <base64("x-access-token:" + token)>`. Never in argv, the URL, a file, the repo config, an error or a log line. `repo.Token` zero means no header. Calls to `Reveal()` appear only where this header is built.
5. Slug is checked against `^[a-z0-9][a-z0-9-]*$` before any path is built; the clone directory is `ReposDir/<slug>.git`.
6. `Sync`: branch = `repo.Branch`, or when empty the remote's default from `git ls-remote --symref -- <url> HEAD` (parse `ref: refs/heads/<b>\tHEAD`). The branch is rejected if it starts with `-` or `git check-ref-format --branch` rejects it. If the clone directory does not exist: `git init --bare <dir>` (mode 0700), then fetch; `Cloned: true`. Otherwise fetch only. The fetch is `git -C <dir> fetch --no-tags --end-of-options <url> +refs/heads/<b>:refs/heads/<b>`. Never `git clone`, never a remote added to the config, so `git -C <dir> config --list` shows no URL and no credential (checklist item 3). Returns `Branch`, `Head` (the local branch's commit after the fetch), `ClonePath`, `Cloned`.
7. Timeouts: a first sync (init plus fetch) runs under `CloneTimeout`, a later fetch and `ls-remote` under `FetchTimeout`, each nested in the caller's context.
8. Size cap: while a fetch runs, check the clone directory's total size every second; past `MaxCloneBytes`, cancel the git process and return an error saying the clone exceeded the cap in MiB. A first sync that fails for any reason (cap, network, refusal after init) removes the clone directory, so a later sync starts clean. A failed later fetch leaves the existing clone as it was.
9. Errors carry git's stderr (capped at 4 KiB) and never the environment. URLs in config cannot carry userinfo, so an echoed URL is safe; do not add one to errors yourself beyond the host.
10. `Head`: the commit `refs/heads/<repo.DefaultBranch>` points at in `repo.ClonePath`; error if the branch or clone is missing.
11. `IsAncestor`: `git merge-base --is-ancestor`; exit 0 is true, exit 1 is false, anything else an error.
12. `ManifestCommits`: first-parent chain of `to` (from the root when `from` is empty, else commits after `from`), oldest first, keeping only commits that changed a path matching a pattern relative to their first parent. Patterns are passed as `:(glob)<pattern>` pathspecs after `--`, revisions after `--end-of-options`. Parse NUL-separated output (use `-z` and `%x00` in `--format`). `CommittedAt` is the committer date (`%ct`) in UTC; author name and email and subject as recorded. `Paths` lists every matching path the commit added, changed or deleted relative to its first parent, sorted. A merge commit whose second parent changed a manifest appears with that path and its own committer date; commits only on a merged branch never appear. Choose the git flags that achieve this (at least `--first-parent`; make sure merges get a first-parent diff and that history simplification cannot drop or replace them) and prove it with the tests below. Empty patterns is an error.
13. `ReadBlobs`: one `git cat-file --batch -z` process per call, fed `<commit>:<path>` entries; results in input order. A `missing` reply sets `Missing`. A blob larger than `MaxBlobBytes` is read and discarded without buffering and sets `TooLarge`. The process is closed and waited for on return; context cancellation kills it. Zero refs returns no blobs and starts no process.
14. The `internal/testutil/gitrepo` builder uses `os/exec` (allowed in `internal/testutil` by `scripts/check-imports.sh`) with the same isolated environment idea (no user or system config).

## Tests to include

All against fixture repositories built with your `gitrepo` builder and synced with `AllowFileURLs: true`, except where the case is about refusing a URL.

- `TestSync_FirstThenIncremental`: first sync clones (`Cloned: true`), the clone is bare (`git rev-parse --is-bare-repository` prints `true`), has no working tree, and `git config --list` in it contains no `url`, `extraheader` or `credential` entry; after a new upstream commit a second sync fetches it (`Cloned: false`, new `Head`).
- `TestSync_DefaultBranch`: fixture whose HEAD is `trunk`, `repo.Branch` empty → `Branch: "trunk"`.
- `TestSync_RejectsURL`: table over `http://`, `ssh://`, `git://`, `file://` (with `AllowFileURLs` false), `ext::sh -c x`, `-oProxyCommand=x`, `host:path`, a URL with userinfo → each an error naming the reason, and no clone directory created. Use a `LookupHost` that fails the test if called, to prove the URL check runs first.
- `TestSync_PrivateAddress`: `https://internal.example/x` with a `LookupHost` returning `127.0.0.1` → refused naming the host; with `10.0.0.5` → refused; with `repo.AllowPrivateHost` → the address check passes (the sync may then fail to connect; assert the error is not the policy error).
- `TestSync_TokenNotOnDisk`: sync a fixture with `repo.Token` set; grep every file under the clone directory and `HomeDir` for the token and for `x-access-token` → no match. Also assert the token is absent from any returned error and from captured log output.
- `TestSync_SizeCap`: `MaxCloneBytes` small enough to trip on a fixture with a large file → error mentioning the cap, clone directory removed.
- `TestManifestCommits_FirstParent`: a fixture where main bumps `go.mod`, a feature branch bumps `go.mod` then reverts it, and a second branch bumps `requirements.txt` and is merged with `--no-ff` at a known committer date → the result has main's commit and the merge commit (with `requirements.txt` in `Paths` and the merge's committer date), and none of the branch-only commits.
- `TestManifestCommits_FromAndDeletes`: `from` set returns only later commits; a commit deleting a manifest lists the deleted path.
- `TestReadBlobs`: order preserved; a path absent at that commit → `Missing`; a blob over `MaxBlobBytes` → `TooLarge` with empty content.
- `TestHeadAndIsAncestor`.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -run 'TestSync\|TestManifestCommits\|TestReadBlobs\|TestHeadAndIsAncestor' -v ./internal/gitsrc/` | `ok`, every test above listed as PASS |
| `grep -rn 'Reveal()' internal/gitsrc --include='*.go' \| grep -v _test.go` | one place: where the extraHeader value is built |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- `ListFiles` (M3, with the usage index).
- Config keys for these options and the wiring in `cmd/upw` beyond the one doctor line (WO-08).
- The ingest and timeline services (WO-05, WO-06).

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `outbound-client`, `go-test`.
Read: `.claude/skills/outbound-client/references/GIT-INVOCATION.md`, `docs/decisions.md` ("Lead decisions for M1"), PLAN.md section 5.1 and 5.2, `internal/httpx/policy.go` (for `BlockedAddr`).

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

None.

Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m1/WO-01-gitsrc.md
Files changed:
- internal/gitsrc/cli.go (modified: Options, New(opts, logger), shared command builder, base -c config, environment with HOME)
- internal/gitsrc/remote.go (created: URL policy, address check and pin, credential env)
- internal/gitsrc/sync.go (created: Sync, ls-remote default branch, branch check, fetch with size cap)
- internal/gitsrc/history.go (created: Head, IsAncestor, ManifestCommits, log parser)
- internal/gitsrc/blobs.go (created: ReadBlobs over one cat-file --batch -z process)
- internal/gitsrc/cli_test.go (modified: New takes Options)
- internal/gitsrc/sync_test.go (created)
- internal/gitsrc/history_test.go (created)
- internal/gitsrc/blobs_test.go (created)
- internal/gitsrc/remote_internal_test.go (created)
- internal/testutil/gitrepo/gitrepo.go (created: fixture repository builder)
- cmd/upw/doctor.go (modified: the one gitsrc.New line, now gitsrc.New(gitsrc.Options{}, logger))

Commands run:
- `make check` -> exit 0
  ```
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.824s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	1.893s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/ingest	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/timeline	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	(cached)
  ==> arch-lint
  go run github.com/fe3dback/go-arch-lint@v1.19.2 check --output-color=false
  module: github.com/Amrit-Code/upstreamwatch
  linters:
     On | Base: component imports # always on
     On | Advanced: vendor imports # switch 'allow.depOnAnyVendor = false' (or delete) to on
    Off | Advanced: method calls and dependency injections # switch 'allow.deepScan = true' (or delete) to on

  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
  ```
  (An earlier run in this session failed TestSync_RejectsSlugAndBranch because I had changed the check-ref-format rejection test to exit code 1; `git check-ref-format --branch` exits 128 on an invalid name. Restored to "any exit status is a rejection" and reran: exit 0, above.)
- `go test -race -count=1 -run 'TestSync|TestManifestCommits|TestReadBlobs|TestHeadAndIsAncestor' -v ./internal/gitsrc/` -> exit 0
  ```
  --- PASS: TestReadBlobs_NoRefsStartsNothing (0.00s)
  --- PASS: TestSync_DefaultBranch (0.25s)
  --- PASS: TestReadBlobs (0.30s)
  --- PASS: TestSync_RejectsURL (0.00s)
  --- PASS: TestSync_PrivateAddress (0.00s)
  --- PASS: TestSync_RejectsSlugAndBranch (0.09s)
  --- PASS: TestReadBlobs_Errors (0.22s)
  --- PASS: TestManifestCommits_Errors (0.22s)
  --- PASS: TestSync_LaterFetchFailureKeepsClone (0.25s)
  --- PASS: TestSync_FirstThenIncremental (0.39s)
  --- PASS: TestHeadAndIsAncestor (0.41s)
  --- PASS: TestManifestCommits_FromAndDeletes (0.41s)
  --- PASS: TestSync_TokenNotOnDisk (0.43s)
  --- PASS: TestSync_SizeCap (0.29s)
  --- PASS: TestManifestCommits_FirstParent (0.56s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	1.709s
  ```
  (the subtest lines are also all PASS: 10 rows in RejectsURL, 6 in PrivateAddress, 6 in RejectsSlugAndBranch)
- `grep -rn 'Reveal()' internal/gitsrc --include='*.go' | grep -v _test.go` -> exit 0
  ```
  internal/gitsrc/remote.go:137:	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token.Reveal()))
  ```
- `git status --porcelain` -> exit 0
  ```
   M cmd/upw/doctor.go
   M internal/gitsrc/cli.go
   M internal/gitsrc/cli_test.go
  ?? internal/gitsrc/blobs.go
  ?? internal/gitsrc/blobs_test.go
  ?? internal/gitsrc/history.go
  ?? internal/gitsrc/history_test.go
  ?? internal/gitsrc/remote.go
  ?? internal/gitsrc/remote_internal_test.go
  ?? internal/gitsrc/sync.go
  ?? internal/gitsrc/sync_test.go
  ?? internal/testutil/
  ```
  (taken before this report file was written; the report is the only addition since)
- `go test -race -count=3 ./internal/gitsrc/` -> exit 0: `ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	2.847s`
- `go test -race -coverprofile=... ./internal/gitsrc/` -> exit 0: `coverage: 83.5% of statements`
- Mutation check: with `--first-parent` taken out of the log command, TestManifestCommits_FirstParent failed (branch-only commits showed up). The flag was put back and the full suite reran green.

Decisions:
- ManifestCommits flags: `log --first-parent --full-history --sparse --diff-merges=first-parent --no-renames --name-only --reverse -z --no-color --format=...`. `--full-history --sparse` turns history simplification off, so every first-parent commit is printed and none can be dropped or replaced for being TREESAME to some parent. Commits with no matching path are filtered in Go. `--no-renames` makes a rename show as delete plus add, so the deleted path is listed.
- Log parsing: each record starts with a `\x01upw-commit` marker token followed by a full hex SHA. Fields and names are NUL-split. git puts one empty token between the header and the names and a newline before the first name; the parser relies on both, and I checked them against git 2.52 output.
- Every git process uses `--git-dir=<dir>` instead of `-C <dir>`, so git never searches parent directories. Without that, a half-created or non-repo clone directory under a checkout could make git act on the parent repository. The fetch arguments are otherwise exactly as specified.
- git is found with `exec.LookPath` on each invocation, as Check already did, because constructors do no I/O.
- HOME is set to HomeDir on every process when HomeDir is set. Check still runs with `Options{}` (no HOME), as doctor passes it. Sync, Head, IsAncestor, ManifestCommits and ReadBlobs create HomeDir (0700) and return "gitsrc HomeDir is not set" when it is empty. Sync also returns "gitsrc ReposDir is not set" when that is empty.
- The address pin uses the URL's port when one is given (`host:8443:addr`), otherwise 443. Pinning 443 alone would leave a URL with a port unpinned.
- An IP-literal host is checked directly with BlockedAddr. It gets no LookupHost call and no pin, because git does not resolve it.
- When LookupHost returns no address, the error is `host "<h>" resolves to no address` rather than the private-address wording. When LookupHost fails, the error is `resolve host "<h>": <err>`.
- URL refusal reasons never echo the URL, since it could carry userinfo: `target URL starts with "-"`, `remote helper syntax "<transport>::" not allowed, only https`, `scp-style address not allowed, only https`, `local path not allowed, only https`, `scheme "<s>" not allowed, only https`, `target URL with user information not allowed`, `target URL has no host`, `target URL does not parse`.
- The credential header is set whenever repo.Token is non-empty, also for file:// URLs (the key's host is then empty). This lets TestSync_TokenNotOnDisk run the real credential path against a fixture.
- Size cap: besides the 1-second ticker during the fetch, the clone is measured once more after git exits. A fixture fetch finishes in well under a second, so without that final check the cap would never trip in the test, or on a small fast fetch. The error reads `clone exceeded the <n> MiB size cap`, with n written as a decimal (0.25 for 256 KiB).
- A first sync that fails at any step removes the clone directory, including the final head lookup. A failed later fetch leaves the directory in place. "Leaves the existing clone as it was" is taken to mean "not removed": if the cap trips only after a successful later fetch, the branch ref has already moved.
- Branch check: anything that is not `-`-prefixed must pass `git check-ref-format --branch`, and its output must equal the input, so `@{-1}` is rejected. Any exit status counts as a rejection (git exits 128 here).
- IsAncestor, ManifestCommits and ReadBlobs refuse an empty revision or one starting with `-`, and pass `--end-of-options` where git takes revisions. ManifestCommits also refuses an empty pattern string, because `:(glob)` alone matches every path.
- ReadBlobs uses `-z` as specified. A `missing` reply echoes the input, which may contain a newline, so the reply is matched against `<input> missing\n` with Peek before it is read as a header line. A writer goroutine feeds all refs and closes stdin, so Peek cannot block. An object at the path that is not a blob (a directory or submodule) gives `Missing: true`, with its content discarded. On any error the blobs slice is nil.
- The gitrepo API is `New(t, initialBranch)`, `Path`, `URL` (file://), `Write`, `WriteBytes`, `Delete`, `Commit(msg, at) sha`, `Branch` (create and switch), `Switch`, `Merge(branch, msg, at) sha` (--no-ff), `Head`, `Git(args...)`, plus a package function `Git(t, dir, args...)` for inspecting directories such as a bare clone. It exports the fixed identity constants `AuthorName`, `AuthorEmail`, `CommitterName` and `CommitterEmail`. Dates are passed as `@<unix> +0000`, because git reads a bare small number as a calendar date.
- TestSync_PrivateAddress, "allow private host" row: I used `https://127.0.0.1:1/x`, not `https://internal.example/x`. With the check skipped, git would otherwise send a real DNS query for internal.example, and unit tests must not touch the network. The loopback connection is refused at once, and the test asserts the error comes from `git fetch` and is not the policy error. The policy rows still use internal.example with a fake LookupHost.
- Added tests beyond the list: TestSync_LaterFetchFailureKeepsClone, TestSync_RejectsSlugAndBranch, TestManifestCommits_Errors, TestReadBlobs_NoRefsStartsNothing, TestReadBlobs_Errors, plus internal tests for the pin format, the credential env, the no-token case, the AllowPrivateHost skip, the strict defaults (no file protocol, every base -c present) and parseSymref.

Outside scope:
- The `go-test` skill and `templates/fixture-git-repo.md` name the helper `internal/testutil/gitfixture` and show `gitsrc.New(gitsrc.Options{DataDir: ...})`. This work order names `internal/testutil/gitrepo` and Options without DataDir; I followed the work order. The skill template is now stale.
- `GIT-INVOCATION.md` still describes `git clone --bare -- <url> <dir>`. The M1 lead decision (init --bare plus fetch) supersedes it, and the doc is now stale on that point.
- Doc rows for docs-sync: the `gitsrc` row in `docs/interfaces.md` (it now implements `ingest.GitSource` and `timeline.GitSource`; compile-time assertions are in sync_test.go), and `internal/testutil/gitrepo` as a new test-helper package in `docs/architecture.md` if packages are listed there.
- The worktree started at 4dff154, one commit behind the groundwork. The lead fast-forwarded it during the run; nothing in it was edited.

Concerns:
- The 1-second ticker that cancels a running fetch has no test. The tests trip the cap through the final post-fetch check, because a local fixture fetch ends before the first tick. A test for the ticker would need a fetch that lasts more than a second, or an injectable interval, and the work order defines no such option.
- The log parser assumes git's `-z --name-only` layout: an empty token after the header and a newline before the first name. I checked this against git 2.52 locally. The container runs Alpine 3.22 git (2.49). If a git version changes the layout, ManifestCommits fails with a parse error rather than returning wrong data.
- When AllowPrivateHost is false, Sync resolves the host up to three times: the upfront refusal check, before ls-remote when the branch is unset, and before fetch. That follows "check before every network operation", at the cost of extra DNS lookups.

## Fix round 1

Status: DONE
Work order: .claude/plans/m1/WO-01-gitsrc.md (findings GITSRC-001, GITSRC-002, GITSRC-003)
Files changed:
- internal/gitsrc/cli.go (modified: absolute HomeDir/ReposDir computed once in `dirs()`; git always runs in the absolute HomeDir; `invocation.secrets`; `runError` scrubs stderr through `scrub` and `model.Redact`; `cappedBuffer.truncated`; the unused `invocation.dir` field removed)
- internal/gitsrc/remote.go (modified: `credentialEnv` also returns the strings that reveal the token; `remote.secrets`)
- internal/gitsrc/sync.go (modified: Sync builds the clone path from the absolute ReposDir; fetch goes into a staging ref; `refCommit`, `deleteRef`; ls-remote and fetch pass their secrets)
- internal/gitsrc/history.go (modified: `prepareClone` returns the absolute clone path, which Head, IsAncestor and ManifestCommits now use)
- internal/gitsrc/blobs.go (modified: ReadBlobs uses the absolute clone path)
- internal/gitsrc/sync_test.go (modified: TestSync_RelativeDirectories, TestSync_IncrementalCapKeepsHead, TestSync_RedactsCredentialEchoedByGit)
- internal/gitsrc/remote_internal_test.go (modified: TestRunError_RedactsReflectedCredentials)

Commands run:
- `make check` -> exit 0
  ```
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.615s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	2.756s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	(cached)
  ==> arch-lint
  go run github.com/fe3dback/go-arch-lint@v1.19.2 check --output-color=false
  ...
  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
  ```
  An earlier run in this round exited 2. TestRunError_RedactsReflectedCredentials/token_cut_by_the_cap failed with `... fake-tok[REDACTED]", contains "fake-"`. The truncation scrub stopped at the first secret with any tail match: the base64 value, matching one character. A longer partial token stayed in the text. Fixed by taking the longest tail match across all secrets, then reran: exit 0, above.
- `go test -race -count=1 -run 'TestSync|TestManifestCommits|TestReadBlobs|TestHeadAndIsAncestor' -v ./internal/gitsrc/` -> exit 0
  ```
  --- PASS: TestSync_RelativeDirectories (0.39s)
  --- PASS: TestSync_RedactsCredentialEchoedByGit (0.05s)
  --- PASS: TestReadBlobs_NoRefsStartsNothing (0.00s)
  --- PASS: TestSync_DefaultBranch (0.33s)
  --- PASS: TestReadBlobs (0.38s)
  --- PASS: TestSync_LaterFetchFailureKeepsClone (0.38s)
  --- PASS: TestSync_RejectsURL (0.01s)
  --- PASS: TestSync_RejectsSlugAndBranch (0.10s)
  --- PASS: TestReadBlobs_Errors (0.31s)
  --- PASS: TestManifestCommits_Errors (0.30s)
  --- PASS: TestSync_PrivateAddress (0.00s)
  --- PASS: TestSync_FirstThenIncremental (0.52s)
  --- PASS: TestSync_SizeCap (0.53s)
  --- PASS: TestHeadAndIsAncestor (0.53s)
  --- PASS: TestSync_IncrementalCapKeepsHead (0.66s)
  --- PASS: TestManifestCommits_FromAndDeletes (0.40s)
  --- PASS: TestManifestCommits_FirstParent (0.73s)
  --- PASS: TestSync_TokenNotOnDisk (0.42s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	2.317s
  ```
- `grep -rn 'Reveal()' internal/gitsrc --include='*.go' | grep -v _test.go` -> exit 0
  ```
  internal/gitsrc/remote.go:143:	userPass := "x-access-token:" + token.Reveal()
  ```
  This is still the one place, in `credentialEnv`, where the extraHeader value is built. The secrets list is derived there from the same value.
- `git status --porcelain` -> exit 0: the same owned files as round 0, plus `.claude/plans/m1/WO-01-gitsrc.report.md`.
- `go test -race -count=3 ./internal/gitsrc/` -> exit 0. Coverage is 83.5% of statements.
- Mutation checks. Each mutation was made, the named test run, and the change reverted; `grep -rn MUTATION internal/` printed nothing afterwards:
  - `absPath` returning its input unchanged: TestSync_RelativeDirectories fails. ClonePath came back as `data/repos/app.git`, and `rev-parse` in the expected absolute clone failed with exit 128.
  - fetch writing `refs/heads/<b>` directly (the round-0 behaviour): TestSync_IncrementalCapKeepsHead fails with `clone's main after an over-cap fetch = 7988..., want the previous head b00b...`.
  - scrub disabled entirely: TestSync_RedactsCredentialEchoedByGit and all seven TestRunError rows fail.
  - only the truncation tail check disabled: the two "cut by the cap" rows fail.

Decisions:
- GITSRC-001: HomeDir and ReposDir are made absolute with `filepath.Abs` on first use, guarded by a `sync.Once`, not in New, because New does no I/O. git always runs with the absolute HomeDir as its working directory and HOME. Sync builds `<abs ReposDir>/<slug>.git` once and uses that path for git, the size watch, cleanup and `SyncResult.ClonePath`. A relative `repo.ClonePath` given to Head, IsAncestor, ManifestCommits or ReadBlobs is made absolute before git sees it.
- GITSRC-002: the staging ref is `refs/upw/staging/<branch>`, used for first and later syncs alike, so there is one fetch path. A failed first sync still removes the whole directory.
  - On success: resolve the staging commit, delete the staging ref, then `update-ref --end-of-options refs/heads/<b> <sha>`. Deleting first means a failure there leaves the branch untouched.
  - On failure: the staging ref is deleted under `context.WithoutCancel` with a 30-second bound, so it also runs after the fetch deadline. A delete error is joined to the returned error.
- GITSRC-003: the secrets are `x-access-token:<token>`, the base64 Basic value and the raw token. Each occurrence is replaced with `[REDACTED]`, longest first, and the text then goes through `model.Redact`.
  - When stderr was cut at the 4 KiB cap, a tail that is the start of any secret is also replaced. The longest such tail across all secrets wins, because the cut can split a credential. This runs only on truncated output.
  - Error wrapping (`%w`) and the 4 KiB bound are unchanged.
  - All git stderr goes through `model.Redact`, including local commands that carry no token.
- The end-to-end redaction test puts a stand-in `git` shell script first on PATH (`t.Setenv`, so the test is not parallel). Its fetch echoes `$GIT_CONFIG_VALUE_0` and the raw token on stderr and exits 128. The test proves the real Sync path carries the secrets to `runError`.

Outside scope: none new.

## Gate (lead, 2026-11-19)

make check rerun: exit 0; acceptance tests pass; Reveal() in one place. Round 0 concerns accepted (untested ticker interval; git log layout to be exercised on git 2.49 on the VM; three resolutions per sync). Astra round 0: REVISE (GITSRC-001 high relative paths, GITSRC-002 medium head moved by an over-cap fetch, GITSRC-003 high credential reflected in stderr), all accepted. Fix round 1 rechecked: make check exit 0, four regression tests pass. Recheck by the Sonnet reviewer (Codex 5-hour window at the 80% cap): all three addressed, nothing broken. Carried note: objects of a rejected over-cap fetch stay in the clone until git collects them, so the next sync fails the cap again; acceptable for M1 (the failure reaches the digest as job_failure), revisit with a prune step if it bites. Accepted.

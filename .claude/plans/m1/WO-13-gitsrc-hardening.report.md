Status: DONE
Work order: .claude/plans/m1/WO-13-gitsrc-hardening.md
Files changed:
- internal/gitsrc/cli.go (modified): `MaxBlobBytes` default 1 MiB, new `Options.MaxLogBytes` (default 64 MiB), shared `formatMiB` helper
- internal/gitsrc/history.go (modified): `ManifestCommits` reads `git log` through `limitedBuffer` (capped at `MaxLogBytes`; the write that would go past the cap cancels the context, which kills git); the unbounded `bytes.Buffer` is gone
- internal/gitsrc/sync.go (modified): size watch every 250 ms; `fetchConfig` (`fetch.unpackLimit=1`, `transfer.unpackLimit=1`) on every fetch; typed `capExceededError`; `shrinkAfterCap` after a rejected later fetch
- internal/gitsrc/blobs_test.go (modified): `TestReadBlobs_DefaultCap`
- internal/gitsrc/history_test.go (modified): `TestManifestCommits_LogCap`
- internal/gitsrc/sync_test.go (modified): `TestSync_IncrementalCapKeepsHead` renamed to `TestSync_IncrementalCapRemovesObjects` and extended; `TestSync_FetchKeepsPacks`; `fakeGitPath` helper
- internal/gitsrc/sync_internal_test.go (created): `TestShrinkAfterCap_PrunesAndKeepsClone`, `TestShrinkAfterCap_RemovesCloneStillOverCap`, `TestCapError_Message`
- .claude/plans/m1/WO-13-gitsrc-hardening.report.md (created)

Commands run:
- `git log --oneline -1` -> exit 0
  a0df331 plans: WO-13 and WO-14 from the M1 security review; decision on token hosts and unsafe dependency names
- `go test -race -count=3 ./internal/...` -> exit 0 (gitsrc: `ok ... 22.617s`; every package ok)
- `make check` -> exit 0
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.901s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.181s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.306s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.111s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	8.312s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.523s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.709s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.157s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.101s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.479s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.606s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.390s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	4.341s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	2.734s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.163s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.125s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.352s
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
- `go test -race -count=1 -v ./internal/gitsrc/` -> exit 0 (top-level results)
  --- PASS: TestCLI_Check_NotInstalled (0.00s)
  --- PASS: TestManifestCommits_LogCap (3.58s)
  --- PASS: TestSync_RelativeDirectories (0.34s)
  --- PASS: TestSync_FetchKeepsPacks (0.61s)
  --- PASS: TestSync_RedactsCredentialEchoedByGit (0.05s)
  --- PASS: TestParseSymref (0.00s)
  --- PASS: TestCapError_Message (0.00s)
  --- PASS: TestCLI_Check_CancelledContext (0.00s)
  --- PASS: TestCLI_Check_Installed (0.01s)
  --- PASS: TestReadBlobs_NoRefsStartsNothing (0.00s)
  --- PASS: TestCheckRemote_PrivateHostSkipsCheckAndPin (0.00s)
  --- PASS: TestSync_DefaultBranch (0.31s)
  --- PASS: TestCheckRemote_NoTokenNoHeader (0.00s)
  --- PASS: TestStrictPolicyByDefault (0.00s)
  --- PASS: TestShrinkAfterCap_RemovesCloneStillOverCap (0.34s)
  --- PASS: TestSync_LaterFetchFailureKeepsClone (0.35s)
  --- PASS: TestReadBlobs (0.37s)
  --- PASS: TestShrinkAfterCap_PrunesAndKeepsClone (0.37s)
  --- PASS: TestCheckRemote_PinsAndCredential (0.00s)
  --- PASS: TestSync_SizeCap (0.39s)
  --- PASS: TestSync_RejectsURL (0.00s)
  --- PASS: TestSync_RejectsSlugAndBranch (0.10s)
  --- PASS: TestReadBlobs_Errors (0.30s)
  --- PASS: TestRunError_RedactsReflectedCredentials (0.00s)
  --- PASS: TestReadBlobs_DefaultCap (0.40s)
  --- PASS: TestSync_PrivateAddress (0.00s)
  --- PASS: TestManifestCommits_Errors (0.28s)
  --- PASS: TestSync_TokenNotOnDisk (0.60s)
  --- PASS: TestSync_FirstThenIncremental (0.41s)
  --- PASS: TestManifestCommits_FromAndDeletes (0.39s)
  --- PASS: TestHeadAndIsAncestor (0.42s)
  --- PASS: TestSync_IncrementalCapRemovesObjects (0.77s)
  --- PASS: TestManifestCommits_FirstParent (0.57s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	6.582s
- `git status --porcelain` -> exit 0 (run before this report file was written)
   M internal/gitsrc/blobs_test.go
   M internal/gitsrc/cli.go
   M internal/gitsrc/history.go
   M internal/gitsrc/history_test.go
   M internal/gitsrc/sync.go
   M internal/gitsrc/sync_test.go
  ?? internal/gitsrc/sync_internal_test.go

Decisions:
- Log cap error text: `manifest commits of <slug>: git log output exceeded the <n> MiB limit`, using the same MiB formatting as the existing clone cap error (moved to a shared `formatMiB`).
- A rejected over-cap fetch is told apart from other fetch errors by an unexported typed error, `*capExceededError`, found with `errors.AsType`. Its message is unchanged ("clone exceeded the N MiB size cap").
- `shrinkAfterCap` runs only on the later-fetch path in `Sync`. A first sync already removes its directory on any failure.
- The prune runs under `cleanupTimeout` (30 s) on `context.WithoutCancel(ctx)`, the same pattern the staging-ref deletion already uses, so the cleanup still happens when the fetch's own deadline has passed.
- If the clone cannot be measured after the prune, it is treated like a clone still over the cap and removed. Enforcing the cap is the safer default.
- A failed prune is joined into the returned error, and the clone is then measured anyway.
- Removal message: the cap error followed by `; clone removed, the next sync creates it again`.
- Prune and completed packs: `git prune` removes loose objects and `tmp_pack_*` files but not a completed pack. A fetch that finished before the cap check therefore always ends with the clone removed. A fetch the size watch stopped part-way is pruned and the clone is kept. `TestSync_IncrementalCapRemovesObjects` accepts both outcomes, because which one happens depends on timing. The two internal `TestShrinkAfterCap_*` tests cover each path deterministically.
- Leftover-process check in `TestManifestCommits_LogCap`: a stand-in git on PATH records each process ID before exec-ing the real git. Each recorded PID must give ESRCH, and the goroutine count must return to its starting value (yielding with `runtime.Gosched`, never sleeping). The test uses `t.Setenv`, so it is not parallel; `TestSync_FetchKeepsPacks` is the same.

Outside scope:
- docs/external-sources.md line 49 still says `ReadBlobs` skips a blob over 5 MiB and that "The 5 MiB blob limit has no config key". It is now 1 MiB. The new 64 MiB `git log` limit (no config key), the 250 ms size watch, the unpack-limit settings and the removal of an over-cap clone after a rejected later fetch are not documented yet either. These are for docs-sync.
- .claude/skills/outbound-client/references/GIT-INVOCATION.md ("Repository shape and limits") does not mention the unpack limits or the prune-then-remove step after a rejected fetch. Lead-owned.
- `cmd/upw` leaves `MaxBlobBytes` and `MaxLogBytes` at zero, so it gets the new defaults. No config key was added, because the work order asks for none.

Concerns: none

## Fix round 1

Status: DONE
Work order: .claude/plans/m1/WO-13-gitsrc-hardening.md (lead follow-up: IsAncestor on a pruned ancestor)
Files changed:
- internal/gitsrc/history.go (modified): when `merge-base --is-ancestor` exits with a status above 1, `IsAncestor` checks both commits with the new `commitExists` (`git cat-file -e --end-of-options <rev>^{commit}`, same isolated invocation). If `ancestor` is missing and `commit` exists, it returns `false, nil`. Every other case returns the original error, with any check error joined. The doc comment is updated.
- internal/gitsrc/history_test.go (modified): new `TestIsAncestor_PrunedAfterForcePush`; `TestHeadAndIsAncestor` updated (see Decisions)
- .claude/plans/m1/WO-13-gitsrc-hardening.report.md (modified)

Commands run:
- `gofmt -l .` -> exit 0 (no output)
- `make check` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	2.667s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	7.914s
  ...
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	2.825s
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
- `go test -race -count=1 -v ./internal/gitsrc/` -> exit 0
  --- PASS: TestHeadAndIsAncestor (0.68s)
  --- PASS: TestIsAncestor_PrunedAfterForcePush (0.71s)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	7.555s

Decisions:
- The existence check runs only after `merge-base` fails with a status above 1, so the common path still starts one process. The check covers both `ancestor` and `commit`: a missing `commit` (or both missing) stays an error, as the lead asked.
- `commitExists` treats any exit status from `cat-file -e` as "not present", including a SHA that names a non-commit object. Failing to run git, or a cancelled context, is an error.
- The fixture force-moves `main` with an orphan branch renamed over it, then syncs. In the test it runs `repack -a -d` and `prune --expire=now` on the clone, because fetches now keep packs whole (unpackLimit=1) and prune alone does not drop packed objects. The test then confirms the old commit is gone with `cat-file --batch-all-objects --batch-check` before calling `IsAncestor`. The same test checks that `IsAncestor(newHead, prunedOld)` is an error and that a missing clone is still an error.
- `TestHeadAndIsAncestor` asserted an error for an ancestor that was never fetched. Under the new contract that is `false, nil`, so the case now expects that. Two cases were added: an unknown `commit` is an error, and an unknown `ancestor` with an unknown `commit` is an error.

Outside scope:
- `internal/timeline/service.go:161` already treats `false` as "history rewritten, rebuild from root", so the timeline needs no change.
- docs/interfaces.md, if it describes `IsAncestor` errors, should now say that a missing ancestor gives `false`. That is for docs-sync.

Concerns: none

## Gate (lead, 2026-11-19)

make check rerun: exit 0; 33 gitsrc tests pass. Sonnet reviewer: ACCEPT (log cap kills and reaps git; a rejected fetch never moves the branch; prune and removal stay inside the clone). Minors: parse memory is a few times the 64 MiB log cap (accepted, bounded); prune can delete an unreachable last-indexed commit, which made IsAncestor error after a force-push: fixed in fix round 1. Lead proved TestIsAncestor_PrunedAfterForcePush fails with the fix disabled ("Not a valid commit name") and passes with it. Accepted.

## Fix round 2

Status: DONE
Work order: .claude/plans/m1/WO-13-gitsrc-hardening.md (lead follow-up: two tests failing on ubuntu-latest CI)
Files changed:
- internal/gitsrc/cli.go (modified): `command()`, which builds every git process (`run`/`output`, `cat-file` in blobs.go, `git log` in history.go, fetch, prune), now sets `SysProcAttr{Setpgid: true}` and `cmd.Cancel`. Cancel calls the new `killGroup(pid)`, which sends SIGKILL to `-pid` and maps ESRCH to `os.ErrProcessDone`. `WaitDelay` is unchanged.
- internal/gitsrc/sync_test.go (modified): new `TestSync_SizeCapKillsProcessGroup` and its `processRunning` helper
- internal/gitsrc/history_test.go (modified): `TestIsAncestor_PrunedAfterForcePush` setup rewritten; new `objectFiles` helper
- .claude/plans/m1/WO-13-gitsrc-hardening.report.md (modified)

What was wrong and why the change fixes it on Linux:
1. TestSync_IncrementalCapRemovesObjects, "TempDir RemoveAll cleanup: directory not empty"
   - Cause: `exec.CommandContext` killed only the git parent when the size watch cancelled a fetch. `git fetch` runs `index-pack` (and, for a file:// remote, `upload-pack`) as child processes. A surviving `index-pack` kept writing into `objects/pack` while `shrinkAfterCap` pruned or removed the clone, and while the test's TempDir cleanup ran.
   - Why only CI: the Mac usually finishes the 2 MiB fetch before the first 250 ms check, so the post-fetch measurement path runs and nothing is killed. The slower CI runner hits the mid-transfer kill path.
   - Fix: git now leads its own process group and cancellation SIGKILLs the whole group. After SIGKILL a process never returns to user space, so at most a write syscall already in progress completes; no new files are created after Wait returns. This is a production fix: the same orphans could write into a clone being removed in deployment.
   - Proof: with the two production lines temporarily replaced, `TestSync_SizeCapKillsProcessGroup` failed: `process 7572 from the cancelled fetch is still running`, `process 7573 ...` (6.30 s, the WaitDelay wait while the orphans held the pipes). With them restored it passes in 0.5 s.
   - Test design: a stand-in git on PATH execs the real git for everything except `fetch`. For `fetch` it records its own PID, starts two `sleep 60` children and records their PIDs, writes 1 MiB into `$dir/objects/pack`, and `wait`s. The test asserts the cap error, the removed clone directory, and that none of the three processes is running.
   - Liveness is checked with `ps -o stat= -p <pid>`. No output means gone, and a `Z` state counts as dead, because on Linux an orphan killed with its group stays a zombie until init reaps it and `kill(pid, 0)` succeeds on a zombie. ps is polled at most 200 times, with no sleeping.
   - A `t.Cleanup` SIGKILLs any recorded PID so a failure cannot leak processes. The script is POSIX sh (dash on Ubuntu) using `head -c` and `sleep`.
2. TestIsAncestor_PrunedAfterForcePush, "old commit ... still in the clone after repack and prune"
   - I could not establish why the Linux runner's git kept the old commit after `repack -a -d` plus `prune --expire=now`. A probe on the Mac shows the clone holds only two packs (no `.keep` files, no reflogs, no refs other than `refs/heads/main`), so nothing in our layout should keep it.
   - Possible version-dependent causes include automatic maintenance writing a multi-pack-index or commit-graph, and repack defaults. Rather than guess, the setup no longer depends on git's collection.
   - New setup: it records the object files (packs and loose objects, not `objects/info`) after the first sync. After the force-push sync it deletes exactly those files, plus any `objects/info/commit-graph*` and `objects/pack/multi-pack-index*`, which are optional caches that could still name the old commit or the removed packs.
   - This is valid because the fetches keep packs whole (unpackLimit=1, and `--fix-thin` makes the kept pack self-contained), and the rewritten history shares no object with the old one, so every object the new history needs is in files the later fetch wrote.
   - The test then confirms with `cat-file --batch-all-objects --batch-check` that the old commit is gone and the new head is present, before asserting `IsAncestor(old, newHead) == false, nil`. A comment in the test says the fixture removes the object files directly and why.

Commands run:
- `go test -count=1 -run SizeCapKillsProcessGroup ./internal/gitsrc/` with the Setpgid/Cancel lines temporarily removed -> exit 1
  --- FAIL: TestSync_SizeCapKillsProcessGroup (6.30s)
      sync_test.go:600: process 7572 from the cancelled fetch is still running
      sync_test.go:600: process 7573 from the cancelled fetch is still running
  FAIL
  FAIL	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	6.533s
  (lines restored afterwards; `pgrep -fl "sleep 60"` showed nothing left)
- `go test -race -count=3 -run 'IsAncestor|SizeCap|IncrementalCap' ./internal/gitsrc/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	4.244s
- `GOOS=linux go vet ./internal/gitsrc/` and `GOOS=linux go test -c -o /dev/null ./internal/gitsrc/` -> exit 0 (linux-compile-ok)
- `gofmt -l .` -> exit 0 (no output)
- `make check` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	9.109s
  ...
  ==> arch-lint
  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
- `go test -race -count=1 -v ./internal/gitsrc/` -> exit 0 (selected lines)
  --- PASS: TestManifestCommits_LogCap (3.92s)
  --- PASS: TestSync_SizeCapKillsProcessGroup (0.52s)
  --- PASS: TestSync_FetchKeepsPacks (0.63s)
  --- PASS: TestSync_SizeCap (0.40s)
  --- PASS: TestSync_IncrementalCapRemovesObjects (0.82s)
  --- PASS: TestHeadAndIsAncestor (0.57s)
  --- PASS: TestIsAncestor_PrunedAfterForcePush (0.52s)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	7.493s
- `git status --porcelain` -> exit 0
   M internal/gitsrc/cli.go
   M internal/gitsrc/history_test.go
   M internal/gitsrc/sync_test.go
  (plus this report file)

Decisions:
- The process-group setup sits in `command()`, the one constructor of every git `exec.Cmd` in gitsrc, so all invocations get it, the cat-file and git log processes included, without separate edits in blobs.go and history.go.
- `syscall.Kill` and `Setpgid` are Unix-only, so gitsrc no longer builds for Windows. Nothing in the Makefile or CI targets Windows, and the deployment is a Linux container. If Windows matters later, this would need build-tagged files.
- With its own process group, git no longer receives a terminal's Ctrl-C directly. upw turns SIGINT/SIGTERM into context cancellation, which now kills the whole group, so shutdown behaviour is unchanged apart from also taking the children with it.
- Cancellation is the only time the group is killed (the lead's spec). A child that outlives a git parent that exited on its own is not handled; git waits for its helpers before exiting.

Outside scope:
- Not established: why the Linux runner's `git repack -a -d` plus `prune --expire=now` kept the old commit. If production relies on git collecting unreachable history (it does not today: `shrinkAfterCap` only expects prune to remove loose objects and tmp packs, and removes the clone otherwise), this is worth a look with the CI git version.

Concerns: none

## Gate, fix round 2 (lead, 2026-11-19)

CI on ubuntu-latest failed TestSync_IncrementalCapRemovesObjects (temp dir not empty: surviving git children) and TestIsAncestor_PrunedAfterForcePush (old commit not pruned by the test setup). Round 2: every git process (all built by g.command) now leads its own process group and cancellation SIGKILLs the group; the test setup is made robust. make check rerun on the Mac: exit 0. CI result recorded after the push.

## Fix round 3

Status: DONE
Work order: .claude/plans/m1/WO-13-gitsrc-hardening.md (lead follow-up: two tests still failing on Linux CI)
Worktree note: HEAD here is c297efa, not e0d6dbb. e0d6dbb is the merge of c297efa with WO-16 (store), so every gitsrc file is identical between the two. I did not move HEAD, because that is a git write.
Files changed:
- internal/gitsrc/cli.go (modified): `baseConfig` adds `gc.auto=0`, `gc.autoDetach=false` and `maintenance.auto=false`. Every git invocation gets them, and so do the helpers git starts, because `-c` values reach child processes through GIT_CONFIG_PARAMETERS.
- internal/gitsrc/sync.go (modified): when the clone path exists and is a directory, `Sync` first runs the new `isBareRepo` (`rev-parse --is-bare-repository` on the clone). Any git exit status, or output other than `true`, means the directory is not a usable bare repository: `Sync` logs Warn `git clone damaged, recreating it` (attribute `repo`), removes the directory and does a first sync (`Cloned: true`). Failing to run git at all is still an error.
- internal/gitsrc/sync_test.go (modified): `TestSync_FetchKeepsPacks` now also asserts the three settings on every gitsrc invocation, and that at least 6 were recorded. New `TestSync_HealsDamagedClone` covers three kinds of damage: emptied with a stray `gc.pid`, `HEAD` missing, `objects` missing.
- internal/gitsrc/history_test.go (modified): the `TestIsAncestor_PrunedAfterForcePush` setup now uses `collectUnreachable`. If the old commit survives, the failure message lists refs and object files.
- .claude/plans/m1/WO-13-gitsrc-hardening.report.md (modified)

Reasoning about Linux git:
- Both CI failures fit one cause: automatic maintenance started by `git fetch` (`git maintenance run --auto`, detached by default). A detached child calls setsid, so the round-2 process-group kill cannot reach it, and it keeps running after gitsrc returns. The Linux runner's git evidently decided auto maintenance was due on this repository shape, and the Mac git (2.52) did not.
  - IncrementalCap failure: the fetch completes, we find the clone over the cap, prune it, and remove it. Meanwhile the detached maintenance writes into the path (`gc.pid`, a pack, logs) and recreates the directory without HEAD, so the next fetch fails with "not a git repository".
  - Force-push test, round 2: my setup deleted the files the first sync wrote, and ignored ErrNotExist. A background repack that had already merged both packs into a new one would explain both the missing files and the old commit surviving in a file I never recorded. Round 1's repack plus prune could likewise lose a race with a concurrent maintenance repack.
- With all three settings, fetch starts no auto gc or maintenance, so no process outlives the invocation and the pack layout after a fetch is fixed (one kept pack per fetch).
- `isBareRepo` makes a clone damaged by anything else (an interrupted removal, a crash) heal on the next run instead of failing every run.
- Force-push setup, as the lead specified, run through `gitrepo.Git` (the isolated environment: no system or global config) with the same three settings: `reflog expire --expire=now --expire-unreachable=now --all`; delete every ref outside `refs/heads/`; `repack -a -d` (drops unreachable objects from packs; `-a` without `-A`/`-k` does not loosen or keep them); then `prune --expire=now` for loose ones. The test then checks with `cat-file --batch-all-objects` that the old commit is gone and the new head is present, before calling `IsAncestor`.

Commands run:
- `go test -count=1 -run Heals ./internal/gitsrc/` with the heal branch temporarily disabled (`if !valid && false`) -> exit 1. All three subtests failed with CI's error:
  sync_test.go:407: Sync() of a damaged clone error = sync app: run git fetch: exit status 128: fatal: not a git repository: '.../003/repos/app.git'
      delete staging ref: run git update-ref: exit status 128: fatal: not a git repository: '.../003/repos/app.git'
  FAIL	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	0.413s
  (restored afterwards)
- `go test -race -count=3 -run 'IsAncestor|Heals|FetchKeepsPacks|IncrementalCap|SizeCap' -v ./internal/gitsrc/` -> exit 0. Every listed test passed in all three runs, including `TestIsAncestor_PrunedAfterForcePush`, `TestSync_HealsDamagedClone`, `TestSync_IncrementalCapRemovesObjects` and `TestSync_SizeCapKillsProcessGroup`.
  (The first run of this command failed in `TestSync_FetchKeepsPacks`: the fixture's own `git commit`, `add` and `rev-parse`, which run through the stand-in, lack gitsrc's config. The assertion is now limited to invocations that start with gitsrc's base config.)
- `gofmt -l .` -> exit 0 (no output)
- `make check` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	8.415s
  ...
  OK - No warnings found
  ==> import-check
- `go test -race -count=1 -v ./internal/gitsrc/` -> exit 0; 36 top-level PASS lines, no FAIL
- `GOOS=linux go test -c -o /dev/null ./internal/gitsrc/` -> exit 0 (linux-compile-ok)
- `git status --porcelain` -> exit 0
   M internal/gitsrc/cli.go
   M internal/gitsrc/history_test.go
   M internal/gitsrc/sync.go
   M internal/gitsrc/sync_test.go
  (plus this report file)

Decisions:
- "Not a valid bare repository" means `rev-parse --is-bare-repository` exits non-zero or prints anything other than `true`. git's own repository check requires HEAD, `objects/` and `refs/`, so it covers the cases the lead named.
- A Warn log line, `git clone damaged, recreating it` with attribute `repo`, records the heal, so a clone that keeps getting damaged is visible to the operator. It is recoverable trouble under the go-code levels. It is the only new log line.
- A clone path that is a regular file still returns the existing "is not a directory" error and is not removed. The lead asked about damaged clone directories only.
- I used the three settings exactly as specified. I did not add `maintenance.autoDetach`, because with `maintenance.auto=false` nothing is started for it to affect.

Outside scope:
- docs (`docs/external-sources.md` or the GIT-INVOCATION reference) should list the new per-invocation settings and the damaged-clone heal. That is for docs-sync and the lead.

Concerns: none

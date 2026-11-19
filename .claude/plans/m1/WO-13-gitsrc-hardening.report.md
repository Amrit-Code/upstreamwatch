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

# WO-17: First-parent chain check, rebuild, retroactive filters and gitsrc caps

Milestone: M1
Agent: go-builder   Model: opus
Depends on: everything through WO-16 (merged); lead commit adding `model.Scope.Rebuild`
Report file: `.claude/plans/m1/WO-17-timeline-first-parent-and-rebuild.report.md`
Ticket: UPW-41 (M1 acceptance; findings of the Fable review)

## Objective

Fix the correctness findings of the independent Fable review of M1 that sit in `internal/timeline` and `internal/gitsrc`. The high-severity one: an incremental timeline run tests whether the last indexed commit is an *ancestor* of the new head, but it must be on the head's *first-parent chain*; after a fast-forward to a branch that merged the old head through its second parent (for example with `-s ours`), the walk misses the manifest state change and the timeline stays silently stale.

## Owned files

- `internal/gitsrc/` (all files)
- `internal/timeline/` (all files, **including `interfaces.go`** for the one change in requirement 1)

Read-only: `internal/model/` (the lead added `Scope.Rebuild`), `cmd/upw/` and `internal/config/` (WO-18 runs in parallel and wires the config and the CLI flag), `cmd/upw/parity_e2e_test.go` (WO-19).

## Findings to fix (Fable review, in substance)

1. **[high]** `timeline/service.go` decides "rewritten history" with `IsAncestor(from, head)`. Repro: main indexed at M (`go.mod: a v2.0.0`); a feature branch forked before M merges main with `-s ours` (keeping `a v1.0.0`); main is fast-forwarded to the feature branch. M is an ancestor of HEAD through the second parent, so no reset; the walk `HEAD ^M` lists only feature commits and the merge's diff against its first parent; root `go.mod` never appears; stored state keeps `a v2.0.0` open while HEAD has `v1.0.0`; and M stays in `commits` with new positions after it.
2. **[medium]** Exclusions (`testdata`, `vendor`, `node_modules`, `exclude_paths`) and the `ecosystems` allowlist are applied only to newly walked paths, so tightening them never closes intervals already stored; there is no rebuild.
3. **[medium]** `defaultMaxBlobBytes` 1 MiB freezes lockfiles that grow past it, silently.
4. **[low]** Non-monotonic committer dates on the first-parent chain make `fold` produce intervals with `To` before `From`, which `Contains` never matches.
5. **[low]** `--sparse` makes `git log` output scale with every first-parent commit.
6. **[low]** git stderr from a hostile remote can carry terminal control characters into error text.

## Requirements

1. **Chain check.** In `internal/timeline/interfaces.go`, replace `IsAncestor` in `GitSource` with:
   ```go
   // OnFirstParentChain reports whether commit is on the first-parent chain
   // ending at head (head itself counts). A commit that does not exist in
   // the clone is not on the chain.
   OnFirstParentChain(ctx context.Context, repo model.TargetRepo, commit, head string) (bool, error)
   ```
   Implement it in `gitsrc.CLI` by streaming `git rev-list --first-parent --end-of-options <head>` and stopping at the first match (bounded by `MaxLogBytes`; kill git on early exit through the existing group kill). `IsAncestor` may stay in gitsrc but is no longer used by the timeline. The timeline resets (`Reset: true`, from the root, counted in `resets`) when `LastIndexedSHA` is set and not on the chain of `head`.
2. **Rebuild.** When `scope.Rebuild` is true, every target in scope is rebuilt from the root (`Reset: true`, stored events ignored), whether or not its head moved; the zero-work shortcut is skipped.
3. **Retroactive filters.** When folding intervals and rebuilding state from stored events, drop stored events whose manifest path is excluded by the current rules (`excludedPath` with the target's `ExcludePaths`) or whose package ecosystem is outside a non-empty `Ecosystems` allowlist. Intervals for those therefore disappear at the next run (the store's interval replace deletes them; `dep_events` rows stay). A run that changes nothing but this still calls `ApplyTimeline` (so the intervals are replaced): run it whenever filtering removed at least one stored event, even if `head == LastIndexedSHA`.
4. **Fold order and clamp.** Fold events strictly in their given order (commit position, then manifest path, then package URL), as today, and clamp each closed interval so `To` is never before `From` (set `To = From` when the closing commit's date is earlier). Keep the output sort.
5. **Blob cap and batches.** `defaultMaxBlobBytes` becomes 8 MiB; the timeline reads blobs 8 refs at a time (so at most 64 MiB of blob data at once). `gitsrc.Options.MaxBlobBytes` stays the override (WO-18 wires `git.max_blob_mb`).
6. **Drop `--sparse`** from the `ManifestCommits` log invocation, keeping `--full-history` and the first-parent diff of merges, so output scales with manifest-touching commits. Re-prove `TestManifestCommits_FirstParent` and the integration first-parent test still pass.
7. **Control characters in errors.** In `gitsrc`'s stderr scrub, replace every rune for which `unicode.IsControl` is true, other than `\n` and `\t`, with `?` (after the secret scrub), and invalid UTF-8 with `?`.

## Tests to include

- `TestTimeline_FastForwardAfterOursMerge` (integration, real gitsrc and store on a `gitrepo` fixture): reproduce finding 1 exactly; after the second run the open interval of `a` at HEAD is `v1.0.0` and the stored events equal those of a full rebuild of the same repo (compare with a second fresh database).
- `TestOnFirstParentChain`: head itself; a first-parent ancestor; a second-parent-only ancestor (false); a pruned/unknown commit (false, no error).
- `TestRun_Rebuild` (service with fakes): `scope.Rebuild` resets a target whose head did not move.
- `TestRun_RetroactiveExclusion`: stored events under `examples/go.mod`, settings now exclude `examples` → `ApplyTimeline` is called with no intervals for that path although `head == LastIndexedSHA`; same for an ecosystem removed from the allowlist.
- `TestFold_ClampsBackwardDates`: a closing commit dated before the opening one → `To == From`.
- `TestReadBlobs_DefaultCap` updated for 8 MiB (a 9 MiB blob is `TooLarge`, a 2 MiB blob is read); the chunk test updated for 8.
- A scrub test: stderr containing `\x1b[2J` and U+009B comes back with `?` in their place.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -count=1 ./internal/gitsrc/ ./internal/timeline/` | `ok` |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- The `-rebuild` CLI flag, `git.max_blob_mb`, per-job locks, terminal-safe printing in `cmd/upw`, the store's package-name update (WO-18).
- The parity test (WO-19).

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `outbound-client`, `go-test`.
Read: `docs/decisions.md` ("Lead decisions for M1", in particular the entries changed 2026-11-19 after the Fable review), `internal/timeline/service.go`, `events.go`, `exclude.go`, `internal/gitsrc/history.go`, `cli.go`.

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

None.

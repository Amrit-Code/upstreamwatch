# WO-10: Referee test: timeline parity with `git pkgs history`

Milestone: M1
Agent: go-mech   Model: opus
Depends on: WO-01 to WO-07 (merged); runs beside WO-08, which owns every other file in `cmd/upw/`
Report file: `.claude/plans/m1/WO-10-gitpkgs-parity.report.md`
Ticket: UPW-38

## Objective

`make e2e` runs a tagged Go test that indexes two public fixture repositories with upstreamwatch's own git source, extractors, timeline job and store, runs `git pkgs history --format json` on the same commits, and fails on any difference in first-parent-reachable events that is not an explained, listed exception. CI runs it too. Serves M1 checklist item 11.

## Owned files

- `cmd/upw/parity_e2e_test.go` (new; first line `//go:build e2e`)
- `cmd/upw/testdata/parity/` (new, if you need fixtures or an exceptions file)
- `Makefile`: add an `e2e` target only
- `.github/workflows/ci.yml`: add one `e2e` job only

Lead-owned, read-only for you: every production file, `go.mod`, `go.sum`. You do not fix production code: a difference you believe is an upstreamwatch bug is reported, not patched (see Requirement 9).

## Interfaces

### Consumed

Read the merged code for exact signatures: `gitsrc.New` and `gitsrc.Options` (`internal/gitsrc`), `extract.New` (`internal/extract`), `unity.New` (`internal/extract/unity`), `ingest.NewService` (`internal/ingest`), `timeline.NewService` and `timeline.TargetSettings` (`internal/timeline`), `store.Open`, `(*store.DB).Migrate`, `(*store.DB).ListEvents`, `(*store.DB).GetRepo` (`internal/store`), and the model types in `internal/model/timeline.go`.

`git pkgs history --format json` (git-pkgs v0.22.0) prints a JSON array of objects with keys `sha`, `message`, `author_name`, `author_email`, `committed_at`, `name`, `ecosystem`, `change_type` (`added`, `modified`, `removed`), `requirement`, `previous_requirement`, `manifest_path`, `manifest_kind`. It must be run after `git pkgs init` in a non-bare clone. It walks every commit, not only the first-parent chain, and knows more manifest formats than upstreamwatch indexes.

## Requirements

1. Fixtures: `glow` (`https://github.com/charmbracelet/glow`) and `cleo` (`https://github.com/python-poetry/cleo`), one subtest each, run sequentially.
2. Per fixture: `git clone` the https URL into a temp directory (non-bare), then `git pkgs init` and `git pkgs history --format json` there. Index the same clone with upstreamwatch by syncing from its `file://` URL: `gitsrc.Options{AllowFileURLs: true}` with temp `ReposDir` and `HomeDir`, a fresh store in a temp directory, the ingest service and then the timeline service with the production extractors (`extract.New` with `DeriveToolchainFromImages: false`, so no derived events confuse the comparison; Unity off). Both sides then see exactly the same commits.
3. First-parent set FP: `git rev-list --first-parent HEAD` in the clone; merges M: `git rev-list --first-parent --merges HEAD`.
4. Normalise git-pkgs events: keep those with `sha` in FP and not in M, whose `ecosystem` is one of `golang`, `pypi`, `docker`, `github-actions`, and whose `manifest_path` upstreamwatch handles, decided by asking `extract.New(...).Parse(path, nil)`: a path is handled unless the error wraps `model.ErrUnsupportedFile`. Map `modified` to `changed`.
5. Normalise upstreamwatch events (`ListEvents`): drop those whose commit is in M (a merge's events correspond to branch commits on the git-pkgs side) and those for `pkg:golang/toolchain` (git-pkgs has no toolchain package).
6. Compare as multisets of `(sha, manifest_path, ecosystem, name, change)`. For `added` and `changed`, also compare upstreamwatch's `Requirement` with git-pkgs' `requirement`. `removed` events are compared without requirement (git-pkgs leaves it empty).
7. Exceptions: a small table in the test (or `testdata/parity/exceptions.json`), each entry matching specific events by fields and carrying a one-line reason that points at a recorded decision in `docs/decisions.md` ("Lead decisions for M1"), for example go.mod `replace` handling or the Docker parser differences. An exception may only be added for a difference that a recorded decision explains. An exception that matches nothing fails the test, so stale ones are removed.
8. On failure the test prints, per fixture, the counts on each side and up to 50 differing events from each side, sorted.
9. If differences remain that no decision explains, do not add exceptions for them and do not change production code: report status DONE_WITH_CONCERNS with the differing events, your analysis of which side is wrong, and the smallest reproduction (a commit SHA and manifest path). The lead turns real bugs into fix work orders.
10. The test skips (with `t.Skip` and a clear message) when `git-pkgs` is not on PATH, and is excluded from `make check` by the build tag.
11. `make e2e` runs `go test -tags e2e -race -count=1 -run TestParity -v -timeout 20m ./cmd/upw/`.
12. CI: a job `e2e` in `.github/workflows/ci.yml` on push to `main` that sets up Go like the existing job, downloads the git-pkgs v0.22.0 linux amd64 release archive from its GitHub release, verifies it against a SHA-256 you take from the release's checksums file and write into the workflow as a literal, puts the binary on PATH, and runs `make e2e`. Pin every action as the existing workflow does.

## Tests to include

- `TestParity/glow`, `TestParity/cleo` as above.
- A small unit test in the same file (also under the tag) for the normalisation and multiset comparison, with hand-made events, including an exception that matches nothing → failure.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 (the tagged file is not compiled) |
| `go vet -tags e2e ./cmd/upw/` | exit 0 |
| `make e2e` (git-pkgs 0.22.0 is installed on the Mac at `/opt/homebrew/bin/git-pkgs`) | `ok`, both subtests PASS; or DONE_WITH_CONCERNS per requirement 9 |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Fixing any production code (lead, through fix work orders).
- Advisory parity with osv-scanner (M2).

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `go-test` (its section on the referee tests).
Read: PLAN.md section 2 ("Why not just shell out to git pkgs?") and section 9 (timeline parity), `docs/decisions.md` ("Lead decisions for M1"), `.github/workflows/ci.yml`.

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

2026-11-19, on the round-0 concerns:
- PyPI names: upw is right; a package is identified by its version-less package URL, and the PyPI package URL normalises the name (recorded in `docs/decisions.md`, "Lead decisions for M1"). In the comparison, key PyPI events by the PEP 503 normalised name (lower case, runs of `-`, `_`, `.` replaced by `-`) on both sides, and collapse a git-pkgs `removed` plus `added` pair at the same commit and manifest whose names normalise equal: equal versions → no event, different versions → one `changed`. Do this as a normalisation step justified by that decision, not as per-event exceptions. Requirements are compared as before.
- Pin the fixtures: after cloning, reset the clone's default branch to a fixed commit and index that, for both sides. Use the current default-branch HEAD SHA of each fixture at the time you make this change, recorded as a constant in the test with the date. A later bump of a pin is a deliberate edit.
- The go-test referee template (build tag `referee`) is older than this work order; keep the `e2e` tag and the skip.
- Expect more differences once WO-12 lands on main (a direct-flag change now emits `changed`; manifests under `testdata`, `vendor` and `node_modules` are skipped). The lead will fast-forward your worktree after WO-12 merges and send you a short round to rerun and add exceptions for those two decisions only.

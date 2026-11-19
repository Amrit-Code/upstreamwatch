# WO-03: Unity extractor for the editor version and UPM packages

Milestone: M1
Agent: go-mech   Model: opus
Depends on: none (lead groundwork commit for M1)
Report file: `.claude/plans/m1/WO-03-extract-unity.report.md`
Ticket: UPW-31

## Objective

`unity.Extractor` reads a Unity project's `ProjectSettings/ProjectVersion.txt`, `Packages/manifest.json` and `Packages/packages-lock.json` so the timeline shows the editor version history as `pkg:generic/unity-editor` and every UPM package. Serves M1 checklist item 8 and part of 15.

## Owned files

- `internal/extract/unity/` (new package, with `testdata/`)

Lead-owned, read-only for you: `internal/model/`, `internal/timeline/interfaces.go`, `.go-arch-lint.yml`, `go.mod`, `go.sum`. `internal/extract/` itself is built in parallel by WO-02; do not touch it.

## Interfaces

### Consumed

```go
// internal/timeline/interfaces.go
type Extractor interface {
	Patterns() []string
	Parse(path string, content []byte) (model.ParsedManifest, error)
}
```

Model types are in `internal/model/timeline.go`; read their doc comments.

### Produced

```go
// internal/extract/unity
func New() *Extractor
func (e *Extractor) Patterns() []string
func (e *Extractor) Parse(path string, content []byte) (model.ParsedManifest, error)
```

## Requirements

1. `Patterns()` returns exactly `**/ProjectSettings/ProjectVersion.txt`, `**/Packages/manifest.json`, `**/Packages/packages-lock.json`.
2. `Parse` dispatches on the last two path elements (`ProjectSettings/ProjectVersion.txt`, `Packages/manifest.json`, `Packages/packages-lock.json`); any other path returns an error wrapping `model.ErrUnsupportedFile`. Malformed content returns an error naming the path. Standard library only (`encoding/json`, `bufio`, `strings`); no I/O.
3. Every result has `ParsedManifest.Ecosystem` `upm`.
4. `ProjectVersion.txt`: one dependency, `Package{PURL: "pkg:generic/unity-editor", Ecosystem: "generic", Name: "unity-editor"}`, `Version` the `m_EditorVersion:` value (`6000.0.23f1`), `Requirement` the `m_EditorVersionWithRevision:` value when present (`6000.0.23f1 (1c4764c07fb4)`) else the version, `Direct: DirectYes`, `Scope: "build"`. Kind `manifest`. No `m_EditorVersion` line → no dependencies, no error.
5. `manifest.json`: each entry of the `dependencies` object, sorted by name. `Package{PURL: "pkg:upm/<name>", Ecosystem: "upm", Name: <name>}`, `Direct: DirectYes`, `Scope: "runtime"`. Value forms:
   - A plain version (`1.2.3`, `3.0.6-preview`): `Version` and `Requirement` that string.
   - A git URL (starts with `https://`, `http://`, `git+`, `git@`, `ssh://`, or ends in `.git` before an optional `#`): `Version` the part after `#` when present, else empty; `Requirement` the URL with any userinfo removed (`https://user:tok@host/x.git#v1` → `https://host/x.git#v1`; `git@host:org/x.git` has no userinfo to remove beyond the `git@` user, which is kept).
   - `file:` references: `Version` empty, `Requirement` the value.
   Kind `manifest`. Other top-level keys (`scopedRegistries`, `testables`) are ignored.
6. `packages-lock.json`: each entry of the `dependencies` object, sorted by name, same `Package` form as 5, Kind `lockfile`. `Direct`: `DirectYes` when `depth` is 0, `DirectNo` when it is above 0, `DirectUnknown` when absent. `Scope: "runtime"`. By `source`:
   - `git`: `Version` the `hash` field when present, else the part after `#` of `version`, else empty; `Requirement` the `version` value with userinfo removed as in 5.
   - `registry`, `builtin`: `Version` and `Requirement` the `version` value.
   - `embedded`, `local`, or anything else: `Version` empty, `Requirement` the `version` value (userinfo removed if it is a URL).
7. No returned string anywhere contains URL userinfo with a password or token (`user:secret@`).

## Tests to include

- `TestPatterns`.
- `TestParse_ProjectVersion`: with and without the revision line; no version line.
- `TestParse_Manifest`: registry version, git URL with `#ref`, git URL without ref, `https://user:token@...` (stripped), `file:../pkg`, ignored `scopedRegistries`; output sorted by name.
- `TestParse_PackagesLock`: registry depth 0, builtin depth 1, git with `hash`, git without `hash`, embedded, missing depth.
- `TestParse_Unsupported`: `Packages/other.json`, `go.mod` → `errors.Is(err, model.ErrUnsupportedFile)`.
- `TestParse_Malformed`: invalid JSON → error naming the path, not `ErrUnsupportedFile`.
- `TestParse_RealProject`: put real-world samples under `testdata/` copied from the MIT-licensed public project `github.com/dbrizov/NaughtyAttributes` (its `ProjectSettings/ProjectVersion.txt`, `Packages/manifest.json`, `Packages/packages-lock.json` at the current default branch; fetch them with `gh api repos/dbrizov/NaughtyAttributes/contents/<path> -q .content | base64 -d`) and assert the editor version and a few packages.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -v ./internal/extract/unity/` | `ok`, the tests above PASS |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Deciding when the Unity extractor runs (the target's `unity: true`, handled by the timeline service in WO-06).
- Every non-Unity format (WO-02).

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `go-test`.
Read: PLAN.md section 5.2 (Unity row), `docs/decisions.md` ("Lead decisions for M1").

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

None.

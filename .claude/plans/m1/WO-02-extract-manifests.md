# WO-02: Manifest extractor over git-pkgs/manifests, go.mod, Docker and compose

Milestone: M1
Agent: go-builder   Model: opus
Depends on: none (lead groundwork commit for M1)
Report file: `.claude/plans/m1/WO-02-extract-manifests.report.md`
Ticket: UPW-30, UPW-32

## Objective

`extract.Extractor` turns one manifest or lockfile at one commit into normalised dependencies for the timeline: Go modules and the Go toolchain from go.mod, PyPI requirements and lockfiles, Docker base images (with the derived Go toolchain), compose images and GitHub Actions. Serves M1 checklist items 6, 7 and 8.

## Owned files

- `internal/extract/` (new package; every file directly in it, plus `internal/extract/testdata/`)

Not owned: `internal/extract/unity/` (WO-03 builds it in parallel). Lead-owned, read-only for you: `internal/model/` (in particular `timeline.go`), `internal/timeline/interfaces.go`, `.go-arch-lint.yml`, `go.mod`, `go.sum`. The modules `github.com/git-pkgs/manifests` v0.14.0, `golang.org/x/mod` v0.43.0 and `go.yaml.in/yaml/v3` are already in `go.mod`; `go.mod` marks the first two `// indirect` until code imports them, which the lead tidies after merge. Do not run `go get` or `go mod tidy`.

## Interfaces

### Consumed

```go
// internal/timeline/interfaces.go
type Extractor interface {
	Patterns() []string
	Parse(path string, content []byte) (model.ParsedManifest, error)
}
```

Model types `ParsedManifest`, `Dependency`, `Package`, `Directness`, `ManifestKind` and `ErrUnsupportedFile` are in `internal/model/timeline.go`; read their doc comments.

### Produced

```go
// internal/extract
type Options struct {
	// DeriveToolchainFromImages adds pkg:golang/toolchain for golang base
	// images (config vulns.derive_toolchain_from_images, default true).
	DeriveToolchainFromImages bool
}

func New(opts Options, logger *slog.Logger) *Extractor
func (e *Extractor) Patterns() []string
func (e *Extractor) Parse(path string, content []byte) (model.ParsedManifest, error)
```

## Requirements

General

1. `Patterns()` returns exactly, in this order: `**/go.mod`, `**/requirements*.txt`, `**/pyproject.toml`, `**/uv.lock`, `**/poetry.lock`, `**/Dockerfile`, `**/Dockerfile.*`, `**/*.Dockerfile`, `**/docker-compose*.yml`, `**/docker-compose*.yaml`, `**/compose*.yml`, `**/compose*.yaml`, `.github/workflows/*.yml`, `.github/workflows/*.yaml`. These are git glob pathspecs; `**/` also matches the root.
2. `Parse` dispatches on the path's base name (and the workflows directory) to the rules below. Any other path, including `go.sum`, returns an error wrapping `model.ErrUnsupportedFile`. Content that fails to parse returns a different error naming the path. The package does no I/O: no filesystem, no network, no git; call `manifests.Parse` without `Options.FSRoot`.
3. `Package.PURL` is always version-less: strip any `@version`, `?qualifiers` and `#subpath` the library produced. `Package.Ecosystem` is the file's ecosystem name as the library spells it (`golang`, `pypi`, `docker`, `github-actions`), and `pkg:golang/toolchain` uses `golang`. `Package.Name` is the name as the library gives it, or the module path / image name for the formats parsed here.
4. Within one file a package appears once: the first occurrence in file order wins, later ones are dropped.
5. `ParsedManifest.Kind` is `model.ManifestKindLockfile` for `uv.lock` and `poetry.lock`, `model.ManifestKindManifest` for everything else. A dependency from a lockfile has `Direct: model.DirectUnknown`.

go.mod (parsed with `golang.org/x/mod/modfile`, not the manifests library)

6. Each `require` line: `pkg:golang/<module path>`, `Version` and `Requirement` the version as written (keep the leading `v`), `Direct: DirectNo` when the line has the `// indirect` comment else `DirectYes`, `Scope: "runtime"`.
7. `replace` directives apply to the required modules they match (a replace with an old version applies only to that version). A replacement by a module path and version makes the dependency that path at that version (`pkg:golang/<new path>`, `Version` the new version, `Requirement` `<old path> => <new path> <new version>`). A replacement by a local directory makes `Version` empty and `Requirement` `=> <dir>`.
8. Toolchain: when a `go` or `toolchain` directive is present, add `pkg:golang/toolchain` (Name `toolchain`): `Version` = the `toolchain` value without its `go` prefix (`go1.25.1` → `1.25.1`) when present, else the `go` value (`1.24.2`); `Requirement` = `go <go value>` (or `toolchain <value>` when only toolchain is set); `Direct: DirectYes`; `Scope: "build"`. `toolchain default` is ignored.

Dockerfile (own parser)

9. Read `FROM` instructions case-insensitively, joining backslash line continuations and skipping comment lines. Skip flags such as `--platform=...`. `FROM <image> AS <name>` records `<name>` as a stage. A `FROM` whose image is an earlier stage name (case-insensitive), `scratch`, or contains `$` produces nothing.
10. Image reference `[registry[:port]/]path[:tag][@digest]`: `Package.PURL` is what `manifests.Parse` produces for an untagged reference to the same image (check `pkg:docker/golang` and `pkg:docker/gcr.io/distroless/static` against the library in a test so both parsers agree). `Version` = the tag, or `latest` when there is neither tag nor digest, or empty when there is a digest and no tag. `Requirement` = the digest when present, else the tag (or `latest`). `Direct: DirectYes`, `Scope: "runtime"`.
11. Derived toolchain, only when `Options.DeriveToolchainFromImages`: for the first image named `golang`, `library/golang` or `docker.io/library/golang` whose tag starts with `<major>.<minor>` or `<major>.<minor>.<patch>`, also add `pkg:golang/toolchain` with `Version` that number (`1.26-alpine` → `1.26`, `1.26.3-bookworm` → `1.26.3`), `Requirement` `golang:<tag>`, `Direct: DirectYes`, `Scope: "build"`. Tags such as `latest` or `alpine` derive nothing.

compose (own parser, `go.yaml.in/yaml/v3`)

12. Each `services.<name>.image` string, parsed as in 10, ecosystem `docker`. A value containing `$` is skipped. No toolchain derivation from compose. A file without `services` yields no dependencies and no error.

Through the manifests library

13. `requirements*.txt` and `pyproject.toml`: an exact pin (`==X` with no `*`, no comma, not `===`) gives `Version` `X`; anything else gives `Version` empty. `Requirement` is the constraint as the library returns it (`==2.31.0`, `>=6`). `Direct` from the library, `Scope` from the library.
14. `uv.lock`, `poetry.lock`: `Version` and `Requirement` the locked version.
15. GitHub Actions workflows: `Version` and `Requirement` the ref (tag, branch or SHA). Entries the library returns without a PURL (local `./` actions, `docker://`) are skipped.

## Tests to include

Table tests with inline file contents; put larger samples under `testdata/`.

- `TestPatterns`: exact list from requirement 1.
- `TestParse_GoMod`: requires direct and indirect, a versioned replace, a directory replace, `go 1.24.2` with `toolchain go1.25.1` → toolchain `1.25.1`; `go 1.24.2` alone → `1.24.2`.
- `TestParse_Dockerfile`: multi-stage file with `--platform`, `AS builder`, `FROM builder`, `scratch`, `FROM ${BASE}`, a digest-pinned image, a registry with a port, two `FROM golang:` lines with different tags (first wins), continuation lines → expected dependencies exactly; derivation on gives `pkg:golang/toolchain@1.26`; derivation off gives none.
- `TestDockerPURLMatchesLibrary`: for `golang`, `gcr.io/distroless/static`, `ghcr.io/owner/img` the PURL equals the library's.
- `TestParse_Compose`.
- `TestParse_Python`: pinned and unpinned requirements; a `uv.lock` and a `poetry.lock` sample with `DirectUnknown` and lockfile kind.
- `TestParse_Actions`: tag, SHA and a local action (skipped).
- `TestParse_Unsupported`: `go.sum`, `package.json`, `Packages/manifest.json` → `errors.Is(err, model.ErrUnsupportedFile)`.
- `TestParse_PURLVersionless`: no PURL returned by any case contains `@`.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -run 'TestPatterns\|TestParse\|TestDockerPURL' -v ./internal/extract/` | `ok`, the tests above PASS |
| `go list -f '{{join .Imports " "}}' ./internal/extract/` | no `os`, `net`, `net/http`, `os/exec` |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Unity files (WO-03, `internal/extract/unity`).
- Ecosystem allowlists and choosing which extractor handles a path (the timeline service, WO-06).
- Normalising versions for advisory matching (M2).

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `go-test`.
Read: PLAN.md section 5.2 (resolution rules table), `docs/decisions.md` ("Lead decisions for M1"), `go doc github.com/git-pkgs/manifests`.

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

2026-11-19, on the concerns in round 0:
- Requirement 15: skipping steps whose `uses` starts with `docker://`, and any entry without a PURL, is accepted.
- Lockfiles: drop every `uv.lock` or `poetry.lock` entry that is a local-path, editable or virtual package (the project's own root, path dependencies), using whatever source information the library returns for it. If the library gives no way to tell, report that instead of guessing by name. Recorded in `docs/decisions.md`.
- Go PURL case: `go.mod` now pins `github.com/git-pkgs/purl` v0.2.1 (your worktree has it), with which the library keeps module-path case. Keep requirements 3 and 6 as written, and add a test that the PURL you build for a mixed-case module (`github.com/BurntSushi/toml`) equals what `manifests.Parse` returns for the same require line.
- `go.mod` tidying is the lead's after merge.

2026-11-19, on fix round 1:
- poetry.lock filtering by a path-like `RegistryURL` is accepted.
- uv.lock: no TOML library (it is not approved). uv writes the lockfile itself, one `[[package]]` table per package with `name = "..."` and an inline `source = { ... }` on one line each. Scan the content line by line with the standard library: within each `[[package]]` table, note the name, and mark the package local when its `source` inline table's first key is `editable`, `virtual`, `directory` or `path`. Drop marked names from the library's output. A package whose source line cannot be read stays (do not guess). Add a test on a real uv.lock: copy the current `uv.lock` of the MIT-licensed `github.com/fastapi/typer` into `testdata/` (`gh api repos/fastapi/typer/contents/uv.lock -q .content | base64 -d`), and assert that `typer` itself is dropped and a few registry packages remain; keep a small hand-written case with `editable`, `virtual`, `directory`, `path`, `git` and `registry` sources (only the git and registry ones remain).


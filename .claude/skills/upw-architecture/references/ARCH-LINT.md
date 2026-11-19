# Layering check: go-arch-lint sketch

Read this when a layering failure needs explaining or when the lead session is writing `.go-arch-lint.yml`. The lead session, not a builder, writes `.go-arch-lint.yml` and the `make check` target during M0.

**This is a sketch, not a working file.** The YAML below follows the go-arch-lint v3 schema as documented in the tool's `docs/syntax/README.md` at v1.19.2. At build time, check every key against the README of the version you install, run the tool on the real tree, and fix whatever it rejects. Package paths here come from the layer diagram in `docs/architecture.md` and `mvp-plan/PLAN.md` section 11. Reconcile them with the packages that actually exist before committing.

## Contents

- What the tool can and cannot check
- Behaviours to confirm at build time
- Components and what each may depend on
- Configuration sketch
- Standard-library import check
- Wiring into make check

## What the tool can and cannot check

go-arch-lint checks imports between your own packages (components) and imports of third-party modules (vendors). It treats every standard-library import as allowed; its checker has an explicit "stdlib always ok" case. Two rules in `SKILL.md` are about the standard library:

- only `internal/store` imports `database/sql`;
- only `internal/gitsrc` imports `os/exec`, apart from the test helpers under `internal/testutil` (the fixture git repo builder).

go-arch-lint cannot enforce those, so a short `go list` check runs next to it (see below).

## Behaviours to confirm at build time

The sketch depends on these. Check each one against the tool's behaviour before relying on it:

1. **Imports inside one component.** The sketch assumes imports between packages of the *same* component are allowed. If so, a component that groups several services would let them import each other. That is why every service and every adapter below is its own component. Keep it that way even though the file is longer.
2. **Glob semantics in `in:`.** The sketch assumes `internal/notify/*` matches direct child packages only and does not match `internal/notify` itself, and that `**` matches any depth.
3. **`excludeFiles` and test files.** The sketch assumes the regex `^.*_test\.go$` keeps test files out of the check. Tests may import a consumer package to assert an adapter satisfies its interface, so they must stay excluded.
4. **How to install the tool.** Options are `go install` at a pinned version, or a `tool` directive in `go.mod`. A `tool` directive adds the module to `go.mod`, so it is a dependency question for the owner; ask the lead before choosing it.

## Components and what each may depend on

| Component | Packages | May depend on | May use (vendors) |
|---|---|---|---|
| main | `cmd/upw` | any project package | none directly |
| config | `internal/config` | model | the YAML library, once the owner approves one |
| model | `internal/model` | none | purl, vers (pure value libraries; move them if `docs/domain-model.md` puts range evaluation elsewhere) |
| store | `internal/store` (migrations are embedded files, not a package) | model | sqlite |
| httpx | `internal/httpx` | model | none |
| sched | `internal/sched` | pipeline, model | cron |
| web | `internal/web` | findings, settings, eval, query, model | none |
| pipeline | `internal/pipeline` | model | none |
| llm | `internal/llm` (pure helpers: provider-neutral prompt builder, vocabulary constants, output schema, cost helpers; no I/O) | model | none |
| one component per job | `internal/ingest`, `internal/timeline`, `internal/usage`, `internal/vuln`, `internal/upstream`, `internal/issues`, `internal/prefilter`, `internal/triage`, `internal/report`, `internal/ops` | model; triage also llm | prefilter may use vers for version mentions; otherwise none |
| query | `internal/query` (read services behind the web pages and read-only CLI commands; declares the store interfaces it needs) | model | none |
| one component per request service | `internal/findings`, `internal/settings`, `internal/eval` | model | none. `eval` declares its own "judge this candidate" interface and does not import `triage` |
| one component per adapter | `internal/gitsrc`, `internal/extract`, each package under `internal/usage/`, `internal/vuln/`, `internal/upstream/`, `internal/issues/`, `internal/llm/` (the two provider adapters; `internal/llm` itself is the `llm` component), `internal/notify/` | model, httpx; the two LLM adapters also llm | its own library only: extract uses manifests and x/mod; vuln/osv uses the git-pkgs vulns module; llm/anthropic uses the Anthropic SDK |
| testutil | `internal/testutil/...` (test helpers such as the fixture git repo builder) | model | none. No production component lists it, so production code cannot import it |

Note on `sched`: it imports `pipeline` to hand job names to the Runner. That is the one place an entry point imports a non-request service. It is allowed because the Runner is the job entry API.

## Configuration sketch

```yaml
# .go-arch-lint.yml: SKETCH. Verify every key against the go-arch-lint README at build time.
version: 3
workdir: .

allow:
  depOnAnyVendor: false

excludeFiles:
  - "^.*_test\\.go$"

vendors:
  sqlite:     { in: [ modernc.org/sqlite, modernc.org/sqlite/** ] }
  cron:       { in: github.com/robfig/cron/v3 }
  manifests:  { in: [ github.com/git-pkgs/manifests, github.com/git-pkgs/manifests/** ] }
  vers:       { in: github.com/git-pkgs/vers }
  purl:       { in: github.com/git-pkgs/purl }
  osvlib:     { in: [ github.com/git-pkgs/vulns/osv, github.com/git-pkgs/vulns/osv/** ] }
  xmod:       { in: golang.org/x/mod/modfile }
  anthropic:  { in: [ github.com/anthropics/anthropic-sdk-go, github.com/anthropics/anthropic-sdk-go/** ] }
  # yaml:     { in: <module path of the YAML library the owner approves> }

components:
  main:        { in: cmd/upw }
  config:      { in: internal/config }
  model:       { in: internal/model }
  store:       { in: internal/store }
  httpx:       { in: internal/httpx }
  sched:       { in: internal/sched }
  web:         { in: internal/web }
  pipeline:    { in: internal/pipeline }
  # jobs, one component each
  ingest:      { in: internal/ingest }
  timeline:    { in: internal/timeline }
  usage:       { in: internal/usage }
  vuln:        { in: internal/vuln }
  upstream:    { in: internal/upstream }
  issues:      { in: internal/issues }
  prefilter:   { in: internal/prefilter }
  triage:      { in: internal/triage }
  report:      { in: internal/report }
  ops:         { in: internal/ops }
  # request services, one component each
  findings:    { in: internal/findings }
  settings:    { in: internal/settings }
  eval:        { in: internal/eval }
  query:       { in: internal/query }
  llm:         { in: internal/llm }
  # adapters, one component each; list the real package paths
  gitsrc:            { in: internal/gitsrc }
  extract:           { in: internal/extract/** }
  usage-golang:      { in: internal/usage/golang }
  usage-python:      { in: internal/usage/python }
  vuln-osv:          { in: internal/vuln/osv }
  vuln-kev:          { in: internal/vuln/kev }
  vuln-epss:         { in: internal/vuln/epss }
  upstream-resolve:  { in: internal/upstream/* }      # split per resolver if they become separate packages
  issues-github:     { in: internal/issues/github }
  llm-anthropic:     { in: internal/llm/anthropic }
  llm-openaicompat:  { in: internal/llm/openaicompat }
  notify-email:      { in: internal/notify/email }
  notify-discord:    { in: internal/notify/discord }
  notify-slack:      { in: internal/notify/slack }
  notify-ntfy:       { in: internal/notify/ntfy }
  notify-stdout:     { in: internal/notify/stdout }
  testutil:          { in: internal/testutil/** }

commonComponents:
  - model

deps:
  main:
    anyProjectDeps: true
  config:
    canUse: [ ]            # add yaml once approved
  model:
    canUse: [ purl, vers ]
  store:
    canUse: [ sqlite ]
  sched:
    mayDependOn: [ pipeline ]
    canUse: [ cron ]
  web:
    mayDependOn: [ findings, settings, eval, query ]
  prefilter:
    canUse: [ vers ]
  triage:
    mayDependOn: [ llm ]
  # Jobs, request services, pipeline, httpx and testutil need no entry beyond
  # commonComponents. If the tool requires every component to appear under deps,
  # give each an empty entry.
  extract:          { mayDependOn: [ ],      canUse: [ manifests, xmod ] }
  vuln-osv:         { mayDependOn: [ httpx ], canUse: [ osvlib ] }
  vuln-kev:         { mayDependOn: [ httpx ] }
  vuln-epss:        { mayDependOn: [ httpx ] }
  upstream-resolve: { mayDependOn: [ httpx ] }
  issues-github:    { mayDependOn: [ httpx ] }
  llm-anthropic:    { mayDependOn: [ httpx, llm ], canUse: [ anthropic ] }
  llm-openaicompat: { mayDependOn: [ httpx, llm ] }
  notify-discord:   { mayDependOn: [ httpx ] }
  notify-slack:     { mayDependOn: [ httpx ] }
  notify-ntfy:      { mayDependOn: [ httpx ] }
  # notify-email and notify-stdout use only the standard library and model.
  # gitsrc uses only the standard library (os/exec) and model.
```

## Standard-library import check

The rules go-arch-lint cannot see, as a shell sketch for the Makefile. `go list` without `-test` reports only non-test imports, so the referee tests may still use `os/exec`.

```sh
# check-imports: fail if database/sql or os/exec is imported outside its one allowed package.
mod=github.com/Amrit-Code/upstreamwatch
bad=$(go list -f '{{.ImportPath}} {{join .Imports " "}}' ./... | awk -v m="$mod" '
  { pkg=$1
    for (i=2; i<=NF; i++) {
      if (($i=="database/sql" || $i ~ /^database\/sql\//) && pkg != m"/internal/store") print pkg" imports "$i
      if ($i=="os/exec" && pkg != m"/internal/gitsrc" && index(pkg, m"/internal/testutil") != 1) print pkg" imports "$i
    } }')
[ -z "$bad" ] || { echo "$bad"; exit 1; }
```

## Wiring into make check

`make check` runs, in order: gofmt check, `go vet ./...`, `go build ./...`, `go test -race ./...`, `go-arch-lint check`, then the import check. Each step stops the target on failure. The same target runs in CI.

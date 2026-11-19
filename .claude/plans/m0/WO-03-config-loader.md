# WO-03: Config loader with env expansion and validation

Milestone: M0
Agent: go-builder   Model: opus
Depends on: WO-01, WO-02 (merged)
Report file: `.claude/plans/m0/WO-03-config-loader.report.md`
Ticket: UPW-13

## Objective

`internal/config` loads `upw.yaml` into a typed, validated `Config`: defaults in code, YAML decoded over them, `${NAME}` references expanded from the environment, secrets accepted only as references, every validation problem reported at once. `model.Secret` gives every credential a redacted printed form. A committed example config and a dev config exist. Serves M0 checklist items 6 and 7.

## Owned files

- `internal/config/` (all files; replace `doc.go`'s neighbours as needed, keep the package comment in `doc.go`)
- `internal/config/testdata/`
- `internal/model/secret.go`, `internal/model/secret_test.go`
- `deploy/upw.example.yaml`
- `upw.dev.yaml`

Lead-owned, read-only for you (stop and report if one is wrong):

- `go.mod`, `go.sum` (`go.yaml.in/yaml/v3 v3.0.6` is already required)
- `.go-arch-lint.yml`, `Makefile`, `scripts/`
- every other file under `internal/model/`

## Interfaces

### Consumed

None from this repository. YAML: `go.yaml.in/yaml/v3` (the only approved YAML module).

### Produced

```go
// internal/model/secret.go — exactly the Secret type in
// .claude/skills/go-code/templates/config-loading.md:
type Secret string
func (s Secret) String() string          // redacted
func (s Secret) GoString() string        // redacted
func (s Secret) LogValue() slog.Value    // redacted
func (s Secret) MarshalText() ([]byte, error) // redacted
func (s Secret) Reveal() string          // the value
// Redaction: "" -> "", length <= 8 -> "****", otherwise "****" + last four characters.

// internal/config
func Load(path string, getenv func(string) string) (Config, error)

type Config struct {
	DataDir   string
	HTTP      HTTP
	Auth      Auth
	Targets   []Target
	Schedules Schedules
	Vulns     Vulns
	Upstream  Upstream
	Prefilter Prefilter
	LLM       LLM
	Notify    Notify
	Retention Retention
}
type HTTP struct{ Listen, BaseURL string }
type Auth struct{ GitHubTargetsToken, GitHubUpstreamToken model.Secret }
type Target struct {
	Slug, URL, Branch string
	Auth       TargetAuth // "targets" | "upstream" | "none"
	Ecosystems []string
	Unity      bool
}
type TargetAuth string
type Schedules struct{ Ingest, Vulns, Issues, LLM, Digest, Backup string } // cron expressions
```

The remaining structs (`Vulns`, `Upstream`, `Prefilter`, `LLM`, `Notify`, `Retention`) are yours to shape; their YAML keys, types and defaults are fixed by the requirements below. Field names follow the YAML key in Go casing (`max_usd_per_day` → `MaxUSDPerDay`). Every secret field has type `model.Secret`.

## Requirements

1. `Load` follows the scaffold in `templates/config-loading.md`: read file, defaults in code, strict decode (unknown YAML keys are an error naming the key), expand `${NAME}` in decoded string values only (never in the raw file text), validate, return all problems joined with `errors.Join`. On any error it returns the zero `Config`.
2. Keys, types and defaults are those of `upw.example.yaml` in `mvp-plan/PLAN.md` section 7, with these corrections from `docs/decisions.md`:
   - `notify` is a mapping with two keys: `channels` (a list) and `heartbeat` (cron, default `"0 8 * * 1"`). The plan's YAML mixes a list and a key, which is not valid YAML.
   - a channel has `type`, one of `email`, `discord_webhook`, `slack_webhook`, `ntfy`, `stdout`. `discord_webhook`, `slack_webhook` and `ntfy` take `url` (secret). `email` takes `host`, `port` (default 587), `username`, `password` (secret), `from`, `to` (list of addresses). `stdout` takes nothing. Default channels: none.
   - `schedules` defaults are the cron strings in plan section 7 (`ingest: "5 */6 * * *"`, `vulns: "10 */6 * * *"`, `issues: "15 * * * *"`, `llm: "20 * * * *"`, `digest: "0 8 * * *"`, `backup: "30 3 * * *"`).
   - `http.listen` default `127.0.0.1:8080`; `data_dir` default `./data`.
   - `llm.enabled` default `false` (no key exists before M4); other `llm` defaults as in the plan.
3. Secret fields are `auth.github_targets_token`, `auth.github_upstream_token`, `llm.api_key`, every channel `url`, and the email `password`. Each accepts only an empty value or exactly one `${NAME}` reference; a literal is an error naming the field and never echoing the value. An unset variable yields an empty secret.
4. Validation, one error per problem, each starting with the YAML path of the field (`targets[0].url: ...`):
   - `data_dir` and `http.listen` required; `http.listen` must parse as host:port;
   - `http.base_url`, if set, must be an absolute `http` or `https` URL;
   - every target: `slug` required, matching `^[a-z0-9][a-z0-9-]*$`, unique; `url` required, must parse, scheme must be `https` (the error names the rejected scheme, for example `targets[0].url: scheme "file" not allowed, only https`), must have a host and no userinfo; `auth` one of `targets`, `upstream`, `none`, default `none`;
   - each `schedules` value and `notify.heartbeat` is a five-field cron expression. Validate with a five-field shape check you write (five whitespace-separated fields, each made of digits, `*`, `/`, `,`, `-`); `internal/config` must not import the cron library;
   - `vulns.exposure_basis` is `published` or `pin`; `llm.provider` is `anthropic` or `openai_compat`; `llm.snippet_mode` is `symbols`, `snippets` or `none`; numeric caps are non-negative; `prefilter.threshold` is positive;
   - a channel missing a field its type requires is an error; an unknown `type` is an error;
   - when `llm.enabled` is true, `llm.model` is required. An empty `llm.api_key` is not an error (the settings page can supply it in M4).
   The private-address and DNS checks on target URLs are not done here (M1 ingest does them before every fetch).
5. `Config` and its nested structs carry no `yaml` tags on the exported type if you use the two-struct approach of the scaffold (`fileConfig` with plain strings, `Config` typed). Only `Config` and its field types are exported.
6. `deploy/upw.example.yaml` is the plan section 7 example, corrected per requirement 2, with example values only (`example-org`, `example.ts.net`), every secret as a `${UPW_...}` reference, and one commented `email` channel. It must load without error when the referenced variables are unset.
7. `upw.dev.yaml` is a dev config with `data_dir: ./data-dev`, `http.listen: 127.0.0.1:8080`, no targets, `llm.enabled: false`, a `stdout` channel, and a commented-out `discord_webhook` channel with `url: ${UPW_WEBHOOK_URL}`. It contains nothing private.
8. `internal/config` imports only the standard library, `go.yaml.in/yaml/v3` and `internal/model`.

## Tests to include

- `TestLoad_Defaults`: a file containing only `data_dir: x` → every default in requirement 2 is set.
- `TestLoad_Example`: `deploy/upw.example.yaml` with an empty `getenv` → no error; with `UPW_GITHUB_TOKEN_UPSTREAM=github_pat_abcdefgh1234` → `cfg.Auth.GitHubUpstreamToken.Reveal()` is that value and `fmt.Sprint` of it is `****1234`.
- `TestLoad_DevConfig`: `upw.dev.yaml` loads.
- `TestLoad_RejectsLiteralSecret`: `auth.github_upstream_token: github_pat_literal` → error names the field and does not contain `github_pat_literal`.
- `TestLoad_TargetScheme`: first target `url: file:///tmp/x` → error contains `targets[0].url` and `"file"`. Also `ssh://`, `git://`, `http://`, and a URL with `user:pass@`.
- `TestLoad_ReportsAllProblems`: a file with three independent problems → the error contains all three.
- `TestLoad_UnknownKey`: `htpp: {}` → error.
- `TestLoad_ExpansionCannotInjectStructure`: `data_dir: ${D}` with `D` set to `"x\nhttp:\n  listen: evil"` → `DataDir` is that literal string; `HTTP.Listen` is the default.
- `TestSecret_Redaction`: `""`, 8-character and 12-character values through `String`, `%v`, `%#v`, `slog` with a text handler into a buffer, and `json.Marshal` → never the full value.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race ./internal/config/ ./internal/model/` | `ok` for both |
| `go list -f '{{join .Imports "\n"}}' ./internal/config \| grep -v '^[a-z/]*$'` | only `go.yaml.in/yaml/v3` and `github.com/Amrit-Code/upstreamwatch/internal/model` |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Reading the config in `cmd/upw`, the `-config` flag, `upw serve`, `upw doctor` (WO-07, WO-08).
- DNS and private-address checks on target URLs (M1).
- Service `Options` structs (each service's own work order).
- Notifier adapters (M2).

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: none.
Read: `.claude/skills/go-code/templates/config-loading.md`; `mvp-plan/PLAN.md` section 7; `docs/decisions.md` (owner decisions); `docs/external-sources.md` (SMTP row).

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

- 2026-11-19: YAML library is `go.yaml.in/yaml/v3` (owner approved).

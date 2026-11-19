Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m0/WO-03-config-loader.md
Files changed:
- internal/model/secret.go (created)
- internal/model/secret_test.go (created)
- internal/config/config.go (created)
- internal/config/file.go (created)
- internal/config/load.go (created)
- internal/config/validate.go (created)
- internal/config/load_test.go (created)
- deploy/upw.example.yaml (created)
- upw.dev.yaml (created)
- .claude/plans/m0/WO-03-config-loader.report.md (created)
(internal/config/doc.go unchanged; internal/config/testdata/ not needed, test configs are written to t.TempDir())

Commands run:
- `make check` -> exit 0
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.183s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.126s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.115s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/pipeline	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/query	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/sched	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/store	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/web	[no test files]
  ==> arch-lint
  go run github.com/fe3dback/go-arch-lint@v1.19.2 check --output-color=false
  module: github.com/Amrit-Code/upstreamwatch
  linters:
     On | Base: component imports # always on
     On | Advanced: vendor imports # switch 'allow.depOnAnyVendor = false' (or delete) to on
     On | Advanced: method calls and dependency injections # switch 'allow.deepScan = true' (or delete) to on

  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
- `go test -race ./internal/config/ ./internal/model/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
- `go list -f '{{join .Imports "\n"}}' ./internal/config | grep -v '^[a-z/]*$'` -> exit 0
  github.com/Amrit-Code/upstreamwatch/internal/model
  go.yaml.in/yaml/v3
- `git status --porcelain` -> exit 0 (run before this report file was written; the report file is the only addition since)
  ?? deploy/
  ?? internal/config/config.go
  ?? internal/config/file.go
  ?? internal/config/load.go
  ?? internal/config/load_test.go
  ?? internal/config/validate.go
  ?? internal/model/secret.go
  ?? internal/model/secret_test.go
  ?? upw.dev.yaml
- `go test -race -count=1 -coverprofile=<scratch>/cover.out ./internal/config/ ./internal/model/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.255s	coverage: 99.3% of statements
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.222s	coverage: 90.0% of statements
- `go mod tidy -diff` (read-only check) -> exit 1: it would make `go.yaml.in/yaml/v3 v3.0.6` a direct requirement and drop the other modules, which nothing imports yet. See Outside scope.

Decisions:
- Files: types in config.go, the YAML mirror and defaults in file.go, Load/expand/secret/convert in load.go, validation in validate.go.
- Enumerated values other than TargetAuth (vulns.exposure_basis, llm.provider, llm.snippet_mode, channel type) are plain `string` fields. No exported constants were added for TargetAuth either; validation uses unexported literal sets.
- Shapes: `Upstream.Overrides map[string]UpstreamOverride{Repo string; WatchIssues bool}`, `LLM.PricesPerMTok Prices{Input, Output float64}`, `Notify{Channels []Channel; Heartbeat string}`, and one flat `Channel{Type; URL Secret; Host; Port int; Username; Password Secret; From; To []string}`.
- `prefilter.threshold` is `int` and `prefilter.weights` is `map[string]int`, because the plan's threshold and weights (section 5.7) are all integers. Weight names are not validated; the prefilter package owns them.
- An override's `watch_issues` defaults to true when it is left out, which matches `upstream_repos.watch_issues DEFAULT 1` in docs/database.md.
- Email `port` defaults to 587 only for `type: email`; other types leave Port at 0. An explicit email port outside 1..65535 is an error.
- A channel's "required" secret fields (`url`, email `password`) are checked against the raw file value: the key must be set to a `${NAME}` reference, but an unset variable gives an empty secret without an error. This is how requirement 6 (the example, with its active discord_webhook channel, loads with variables unset) and requirement 4 (a channel missing a field its type requires is an error) both hold. Plain fields (host, username, from, to) are checked after expansion.
- Email requires host, username, password, from and a non-empty `to` with no empty entries. I read requirement 2's list of email fields literally: every field except `port`, which has a default. Address syntax is not checked.
- `http.listen` must pass `net.SplitHostPort` and have a numeric port from 0 to 65535, so service names such as `:http` are rejected.
- "Numeric caps are non-negative" is applied to upstream.backfill_days, upstream.backfill_max_issues, llm.max_snippets, llm.max_issue_chars, llm.max_calls_per_run, llm.max_usd_per_day, llm.prices_per_mtok.input/output and both retention values.
- Target URL problems are reported independently (scheme, host, user info), and so is the slug. Messages never repeat a URL. A url.Parse failure reports only the inner reason, because `*url.Error` includes the whole URL, which could contain a credential.
- `llm.provider` and `llm.snippet_mode` are validated even when `llm.enabled` is false; their defaults are valid.
- An empty config file (no YAML document) is not an error: the defaults apply. The defaults are valid.
- Every non-secret string value is expanded, including list elements and override `repo` values. Map keys are not expanded.
- In deploy/upw.example.yaml, `llm.enabled` is `false` with a comment to turn it on once a key exists. The plan's example has `true`; I changed it to match the requirement-2 default and the fact that no key exists before M4. The dogfood target `https://github.com/Amrit-Code/upstreamwatch` (this public repository) and the public golang/go and alpine overrides were kept from the plan. The commented email channel references `${UPW_SMTP_PASSWORD}`.
- Extra tests beyond the listed ones: empty file, null values keep defaults, missing file, secret references resolved from the environment, target conversion, a validation table (27 rows), an LLM enabled with no key, the override watch default, unknown keys in nested and list positions, expand, isCronShape, Secret inside a struct, and uncommenting the commented channels of both example files.

Outside scope:
- go.mod still lists `go.yaml.in/yaml/v3 v3.0.6 // indirect`. Now that internal/config imports it, `go mod tidy` would make it direct (and would drop the other modules, which nothing imports yet). The build is unaffected. The lead owns go.mod.
- `prefilter.max_issue_age_days` (default 540) appears in PLAN.md section 5.7 but not in the section 7 example, so it is not a config key here. The prefilter work order will need it added.
- The `.claude/skills/go-code/templates/config-loading.md` scaffold still says the YAML library is "not yet approved". docs/decisions.md records `go.yaml.in/yaml/v3`.
- For docs-sync: new config keys `notify.channels` (and channel fields `type`, `url`, `host`, `port`, `username`, `password`, `from`, `to`) and `notify.heartbeat`; the new env variable name `UPW_SMTP_PASSWORD` used in the example; new files deploy/upw.example.yaml and upw.dev.yaml; the new `model.Secret` type.

Concerns:
- The email-required-field set (username and password required) and the raw-presence rule for secret fields are readings of requirements 2, 4 and 6, not explicit rulings. If an unauthenticated SMTP relay should be allowed, `required("username")` and `required("password")` in validateChannel are the two lines to drop.
- The example config's `llm.enabled: false` departs from the plan's literal `true` (see Decisions).

# M3 Upstream issues, prefilter and eval scaffolding: acceptance checklist

Source: PLAN.md section 10 (M3) with sections 5.5 to 5.7 and 9, adjusted for the accepted design: `usage` as its own job reading source through `GitSource` (no working tree), and `prefilter` scheduled straight after `issues`.

Conventions: run from the repository root on the Mac unless Who says VM. `upw` is the binary built from `./cmd/upw` with the dev config `upw.dev.yaml`. `<fixture>` is a public fixture target slug; `<target>` is a private target configured only on the VM. `<db>` is the SQLite file in the data directory. Status is changed only by the lead (see SKILL.md).

| # | Item | Command or observation | Expected | Who | Status | Evidence |
|---|---|---|---|---|---|---|
| 1 | Checks | `make check` | exit 0 | lead | open | |
| 2 | Resolver chain | `go test -race ./internal/upstream/...` | ok; fake-server tests for go-get meta, deps.dev and ecosyste.ms; cases for Go module paths, `golang.org/x/*`, the curated Docker map, Actions, UPM git URLs and config overrides; first hit wins and provenance is recorded | lead | open | |
| 3 | Redirects followed | resolver test for a renamed GitHub repo (301 on the repo API) | canonical owner/name stored | lead | open | |
| 4 | Coverage, fixtures | `upw run upstream`, open `/coverage` | every direct dependency of each fixture is mapped or listed with a reason; `watch_issues=0` upstreams listed | lead | open | |
| 5 | Conditional polling | with `UPW_GITHUB_TOKEN_UPSTREAM` set, run `upw run issues` twice a few minutes apart | second run's requests are 304s; `issues_etag` and cursors stored per upstream; `/coverage` shows last poll status and rate-limit remaining | lead | open | |
| 6 | Byte-identical requests | `go test -race ./internal/issues/...` | ok; test asserts the second poll's URL and query match the first exactly and carry `If-None-Match` | lead | open | |
| 7 | Rate-limit handling | issues tests with a fake server returning `x-ratelimit-remaining: 150` and with `retry-after` | job stops early and logs when remaining < 200; honours `retry-after`; requests serialised with a gap | lead | open | |
| 8 | Unauthenticated mode | run with no upstream token | issues schedule drops to every 6 hours; `/coverage` shows the warning | lead | open | |
| 9 | Backfill bounds | issues test with a fake server holding more than `backfill_max_issues` items | backfill stops at `backfill_days` and `backfill_max_issues`; `state=all`; PRs kept with `is_pull=1` | lead | open | |
| 10 | Usage index via git | `upw run usage --repo <fixture>` on a Go and a Python fixture; `sqlite3 <db> "select count(*) from symbol_usage"` | symbols recorded; no working tree created; `indexed_sha` equals HEAD; a second run with no new commit does no work | lead | open | |
| 11 | Prefilter after issues | `upw serve` start-up log of schedules | prefilter runs straight after issues in the same tick | lead | open | |
| 12 | Features stored | `sqlite3 <db> "select features, score, passed from issue_candidates limit 5"` and a candidate on the UI | features JSON present; the UI shows why an issue passed or not | lead | open | |
| 13 | Unreviewed candidates | with `llm.enabled: false`, `upw run digest` and `/` | passed candidates appear in a separate, clearly marked "unreviewed candidates" section, never as findings | lead | open | |
| 14 | Labelling page | `/eval` label buttons on a candidate | an `eval_labels` row is written; POST passes the same-origin check | lead | open | |
| 15 | Export and import | `upw export labels > /tmp/labels.json` (`upw eval export` is an alias), import into a fresh data dir with `upw eval import` | same label count and content | lead | open | |
| 16 | Public fixture labels | `git ls-files eval/` | only `eval/fixtures/` content; no private labels committed | lead | open | |
| 17 | Coverage, private targets | on the VM `/coverage` | every direct dependency of every `<target>` mapped or listed with a reason | owner (VM) | open | |
| 18 | Steady-state budget | on the VM after a day of hourly polling, `/coverage` | fewer than 100 counted GitHub requests per day | owner (VM, upstream token) | open | |
| 19 | Backfill in one run | on the VM, first `upw run issues` for the ~20 mapped upstreams | 12 months backfilled in one run without hitting the rate-limit stop | owner (VM, upstream token) | open | |
| 20 | Prefilter targets | owner labels the first 150 candidates in `/eval`; `upw eval report` | prefilter recall at least 0.90 and pass rate at most 0.25 | owner (labelling) | open | |

Security review: open.

# M0 Skeleton: acceptance checklist

Source: PLAN.md section 10 (M0), adjusted for the owner's decisions of 2026-11-19: builds and tests on the Mac with Go 1.27 from Homebrew, the VM for deployment only, the repository public from the first commit, and the accepted design changes `pipeline.Runner` with the `Job` interface, `store.DB` behind store interfaces, and `internal/model`.

Conventions: run from the repository root on the Mac unless Who says VM. `upw` is `go build -o bin/upw ./cmd/upw` run with the dev config `upw.dev.yaml` (data in `data-dev/`). `<db>` is the SQLite file in the configured data directory. Status is changed only by the lead (see SKILL.md).

| # | Item | Command or observation | Expected | Who | Status | Evidence |
|---|---|---|---|---|---|---|
| 1 | Toolchain | `go version` | `go1.27.x darwin/arm64` (or amd64) | lead | open | |
| 2 | Module | `head -3 go.mod` | `module github.com/Amrit-Code/upstreamwatch` and `go 1.27` | lead | open | |
| 3 | One check command | `make check` | exit 0; output shows gofmt, vet, build, `go test -race ./...` and the layering lint each ran | lead | open | |
| 4 | Layering lint bites | add a throwaway file under `internal/model/` that imports `database/sql`, run `make check`, delete the file, run `make check` | first run fails naming the layering rule; second run exit 0 | lead | open | |
| 5 | Layout | `go list ./...` | packages match the package list in `docs/architecture.md` for M0 (at least `cmd/upw`, `internal/config`, `internal/store`, `internal/model`, `internal/pipeline`, `internal/sched`, `internal/web`) | lead | open | |
| 6 | Config env expansion | `upw doctor` with `UPW_WEBHOOK_URL=https://hooks.example.invalid/x` set and the dev config referencing `${UPW_WEBHOOK_URL}` | config loads; doctor output shows the value only as its last four characters | lead | open | |
| 7 | Config validation | `upw serve` with a config whose first target url is `file:///tmp/x` | exits non-zero before listening; error names the target field and the rejected scheme | lead | open | |
| 8 | Migrations apply | delete `data-dev/`, run `upw serve` for a few seconds, then `sqlite3 <db> 'select version from schema_migrations'` | one row per embedded migration | lead | open | |
| 9 | Pragmas | `sqlite3 <db> 'pragma journal_mode'` | `wal` | lead | open | |
| 10 | Restart is a no-op | start `upw serve` a second time on the same data dir | starts cleanly; no migration applied twice; `schema_migrations` unchanged | lead | open | |
| 11 | Newer DB refused | insert a `schema_migrations` row with a version above the newest embedded one, start `upw serve` | refuses to start with an error saying the database is newer than the binary | lead | open | |
| 12 | Health | with `upw serve` running: `curl -s http://127.0.0.1:8080/health` | JSON with `"ok":true` and a `last_runs` field | lead | open | |
| 13 | Empty findings page | `curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/` | `200`; page renders an empty findings state | lead | open | |
| 14 | Not reachable from outside the host | run directly: `lsof -nP -iTCP:8080 -sTCP:LISTEN` (config default `127.0.0.1:8080`). In the container (the config sets `0.0.0.0:8080`): `docker compose port upw 8080` and `curl` to the VM's tailnet or LAN address on port 8080 from the Mac | direct run listens on `127.0.0.1:8080` only; compose publishes `127.0.0.1:8080` and the remote `curl` is refused, so only `tailscale serve` reaches the service | lead | open | |
| 15 | Runner records runs | `go test -race ./internal/pipeline/... ./internal/sched/...` | ok; tests cover a successful run writing a `runs` row and a failing job writing a failed `runs` row plus a `job_failure` finding | lead | open | |
| 16 | Schedules wired | `upw serve` start-up log | one line per configured schedule with its cron expression | lead | open | |
| 17 | Doctor without tokens | `upw doctor` with no token env vars set | git found; OSV and deps.dev reachable; missing tokens reported as warnings, not crashes; no secret printed beyond its last four characters | lead | open | |
| 18 | Repo hygiene | `git grep -nE -e 'github_pat_[A-Za-z0-9]' -e 'sk-ant-[A-Za-z0-9]' -e 'discord(app)?\.com/api/webhooks/[0-9]'` and `git ls-files -- ':(glob)**/upw.yaml' ':(glob)**/.env' mvp-plan/TARGETS.local.md` | both print nothing; `.gitignore` covers `upw.yaml`, `.env`, `data/`, `data-dev/`, `mvp-plan/TARGETS.local.md` | lead | open | |
| 19 | CI green | `gh run list --branch main --limit 1` | latest run `completed success`; workflow runs vet, test and build | lead | open | |
| 20 | Image builds | the CI workflow's image build job on a test tag, or `docker build -f deploy/Dockerfile .` where Docker is available | image builds from `golang:1.27-alpine` to `alpine` runtime with `git` and `ca-certificates`, non-root user | lead | open | |
| 21 | VM provisioned | upw-deploy and homelab skills: VM created, Docker and compose installed, ufw allowing only `tailscale0` | `docker compose ps` on the VM shows `upw` healthy | owner (VM) | open | |
| 22 | Served over the tailnet | from a tailnet device: `curl -s https://upstreamwatch.<tailnet>.ts.net/health` | `"ok":true` | owner (VM) | open | |
| 23 | Doctor with real tokens | on the VM: `docker compose exec upw upw doctor` | every check passes with the real tokens; tokens shown only as last four characters | owner (VM, tokens) | open | |

Security review: open.

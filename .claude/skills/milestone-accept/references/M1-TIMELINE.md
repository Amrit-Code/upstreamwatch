# M1 Timeline: acceptance checklist

Source: PLAN.md section 10 (M1) with sections 5.1 to 5.3, adjusted for the owner's decisions of 2026-11-19 (Mac builds, VM for deployment) and the accepted design (`ingest` and `timeline` as separate jobs behind `pipeline.Runner`, git access only through `GitSource`).

Conventions: run from the repository root on the Mac unless Who says VM. `upw` is the binary built from `./cmd/upw` with the dev config `upw.dev.yaml`. `<fixture>` is a public fixture target slug; `<target>` is a private target configured only on the VM. `<db>` is the SQLite file in the data directory. Status is changed only by the lead (see SKILL.md).

| # | Item | Command or observation | Expected | Who | Status | Evidence |
|---|---|---|---|---|---|---|
| 1 | Checks | `make check` | exit 0 | lead | open | |
| 2 | Bare clone | `upw run ingest --repo <fixture>`, then `git -C data-dev/repos/<fixture>.git rev-parse --is-bare-repository` | `true`; no working tree anywhere under `data-dev/repos/` | lead | open | |
| 3 | No credential on disk | `git -C data-dev/repos/<fixture>.git config --list` and `grep -rl -e x-access-token -e Authorization data-dev/repos/` | no credential helper, no `http.extraHeader`, no token-bearing URL; grep prints nothing | lead | open | |
| 4 | URL policy | `upw run ingest` with a target url of each of `http://`, `ssh://`, `git://`, `file://`, `ext::` and an `https://` host resolving to `127.0.0.1` | each refused before any git process starts, error naming the reason; the private host is accepted only with `allow_private_hosts: true` | lead | open | |
| 5 | Default branch | a fixture target with no `branch:` in config | indexed on the branch `git ls-remote --symref <url> HEAD` reports | lead | open | |
| 6 | Timeline | `upw run timeline --repo <fixture>`, then `upw timeline <fixture>` | intervals listed with package, version, from, to, direct, manifest | lead | open | |
| 7 | Toolchain pseudo-package | `upw timeline <fixture> pkg:golang/toolchain` on a Go fixture | intervals from the `go` / `toolchain` directives and from `golang:<ver>` base images when derivation is on | lead | open | |
| 8 | Ecosystems | `upw timeline <fixture>` on fixtures covering go.mod, a Python lockfile, a Dockerfile, a GitHub Actions workflow and a Unity project | each ecosystem produces events; Unity editor version appears as `pkg:generic/unity-editor` | lead | open | |
| 9 | First-parent semantics | the timeline test fixture with a feature branch that bumps and reverts a dependency before merge | the branch contributes no events; a merged bump is dated at the merge commit's committer date | lead | open | |
| 10 | Incremental, zero work | run `upw run ingest` and `upw run timeline` twice with no new upstream commits; inspect the second run's `stats` in `runs` (`sqlite3 <db> "select job, stats from runs order by id desc limit 2"`) | second run reports zero commits walked and zero events written | lead | open | |
| 11 | Timeline parity | `make e2e` | the event stream for each public fixture matches `git pkgs history --format json` on first-parent-reachable events | lead | open | |
| 12 | Timeline page | `upw serve`, open `http://127.0.0.1:8080/repos/<fixture>` | interval table with a filter box, per-package history and a "pinned on date" selector; upstream text escaped | lead | open | |
| 13 | Fetch failure surfaces | point a fixture target at an unreachable https host and run `upw run ingest` | run recorded as failed and a `job_failure` finding created; the timeline stays at the last good commit | lead | open | |
| 14 | Private targets indexed | on the VM: `docker compose exec upw upw run ingest` then `upw run timeline` | every `<target>` indexed in one run with no errors | owner (VM, targets token) | open | |
| 15 | Real histories correct | on the VM UI, `/repos/<target>` for the Go server target, the Unity target and a Python target | the `golang:` base-image bumps, the Unity editor version history and the Python lock history appear as intervals with correct dates | owner (VM) | open | |

Security review: open.

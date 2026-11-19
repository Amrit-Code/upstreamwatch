# M2 Vulnerability feed and digest: acceptance checklist

Source: PLAN.md section 10 (M2) with sections 5.4 and 6, adjusted for the owner's decisions of 2026-11-19: the notifier adapters are email (SMTP, standard library), Discord webhook, Slack webhook, ntfy and stdout, several may be enabled at once, and the owner receives the digest by email. Also the accepted design: the `digests` table, the shared `httpx` client, `findings.Service` for ack, snooze and dismiss. ntfy is placed here with the other notifiers; PLAN.md gives it no milestone of its own.

Conventions: run from the repository root on the Mac unless Who says VM. `upw` is the binary built from `./cmd/upw` with the dev config `upw.dev.yaml`. `<fixture>` is a public fixture target slug; `<target>` is a private target configured only on the VM. `<db>` is the SQLite file in the data directory. Status is changed only by the lead (see SKILL.md).

| # | Item | Command or observation | Expected | Who | Status | Evidence |
|---|---|---|---|---|---|---|
| 1 | Checks | `make check` | exit 0 | lead | open | |
| 2 | Advisory parity, fixtures | `upw run vulns`, then compare advisory ids of open matches (`exposed_to_ts` null) for each fixture with `osv-scanner --format json -L <lockfile>` (v2.7.x) on the same HEAD lockfiles | identical id sets; any difference investigated and explained, not filtered | lead | open | |
| 3 | Delta, not re-query | run `upw run vulns` twice; read the second run's `stats` in `runs` | second run fetches only records newer than stored (`modified_id.csv` delta); querybatch only for new package versions | lead | open | |
| 4 | Range evaluation | `go test -race ./internal/vuln/...` | ok; cases cover SEMVER and ECOSYSTEM ranges, explicit `versions`, `last_affected`, and GIT ranges skipped with confidence low | lead | open | |
| 5 | Exposure and confidence | `/f/<id>` for an advisory finding | shows exposure window, fixed version if known, confidence label and reason; `exposure_basis: pin` changes the window start | lead | open | |
| 6 | Toolchain derivation | a fixture whose Dockerfile uses `golang:1.26-alpine`; `upw findings --json` | Go stdlib advisories reported against `pkg:golang/toolchain` 1.26 | lead | open | |
| 7 | KEV and EPSS | `sqlite3 <db> "select count(*) from advisories where epss is not null"` and the same for `kev = 1` after `upw run vulns` | EPSS populated for advisories with a CVE alias; KEV flag set where listed; EPSS fetched through the redirect | lead | open | |
| 8 | Ranking | `upw findings --json` | ordered by score, highest first; scores follow the PLAN.md section 6 advisory formula | lead | open | |
| 9 | Notifier adapters | `go test -race ./internal/notify/...` | ok; each of email, Discord, Slack, ntfy and stdout is tested against a local fake (SMTP or HTTP) with no network | lead | open | |
| 10 | Email config | dev config with `notify` type email: host, port, username, password from env, from, to | config loads; password read only from env; doctor shows the password only as its last four characters | lead | open | |
| 11 | Several notifiers | dev config with stdout and email (to a local fake SMTP server) both enabled; `upw run digest` | both receive the same digest | lead | open | |
| 12 | Digest shape | `upw run digest` with the stdout notifier | header line with new, changed and waiting counts; NEW and CHANGED sections; last successful run footer; links use `http.base_url` | lead | open | |
| 13 | Only when changed | run `upw run digest` twice with nothing new | second run sends nothing; a `digests` row records the first send | lead | open | |
| 14 | Heartbeat | `go test` for the digest job with a clock set to the heartbeat schedule and no changes | an "all quiet, last successful run" message is sent | lead | open | |
| 15 | Failed send retried | a notifier pointed at an unreachable host; `upw run digest`, then fix the host and run again | first run marks the `digests` row failed and shows it on the UI; second run sends it | lead | open | |
| 16 | Ack keeps it out | ack a finding at `/f/<id>`, add a new finding, run `upw run digest` | acked finding absent; new one present | lead | open | |
| 17 | Snooze and dismiss | snooze 1d and dismiss with a note from `/f/<id>` | snoozed hidden until expiry or pin change; dismissed hidden; both visible with the suppressed toggle | lead | open | |
| 18 | Same-origin on POST | `curl -s -o /dev/null -w '%{http_code}' -X POST -H 'Origin: https://evil.example' -H 'Sec-Fetch-Site: cross-site' http://127.0.0.1:8080/f/1/ack` | `403`; finding state unchanged | lead | open | |
| 19 | Backup | `upw run backup`; and the backup job's test, which runs it over eight days with an injected clock | `data-dev/backups/upw-YYYYMMDD.sqlite` created via `VACUUM INTO` and opens with `sqlite3`; the test shows only 7 kept | lead | open | |
| 20 | Parity on private targets | on the VM, the item 2 comparison for each `<target>` HEAD lockfile | identical id sets | owner (VM) | open | |
| 21 | Digest arrives | on the VM with the owner's SMTP settings in `.env`: `docker compose exec upw upw run digest` | the digest arrives in the owner's inbox and its links open the tailnet UI | owner (VM, SMTP) | open | |
| 22 | Ack on the real instance | owner acks a finding on the tailnet UI; next scheduled digest | the acked finding is not in it | owner (VM) | open | |

Security review: open.

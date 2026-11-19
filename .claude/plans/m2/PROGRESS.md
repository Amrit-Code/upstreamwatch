# M2 progress (lead's running log)

Read this first when resuming. Update it after every accepted work order.

## Plan

| WO | Title | Agent | Wave | Depends on | Status |
|---|---|---|---|---|---|
| WO-01 | Store: advisories, matches, kv, finding detail | go-builder | 1 | - | dispatched |
| WO-02 | Store: finding state, digests, backup, write queue | go-builder | 1 | - | dispatched |
| WO-03 | OSV adapter | go-builder | 1 | - | dispatched |
| WO-04 | KEV and EPSS adapters | go-mech | 1 | - | dispatched |
| WO-05 | Model rules: rank, transitions, digest text | go-builder | 1 | - | dispatched |
| WO-06 | Vuln matching rules | go-builder | 1 | - | dispatched |
| WO-07 | vuln.Service (vulns job) | go-builder | 2 | 01,03,04,05,06 | not written |
| WO-08 | Notifiers: email and stdout | go-mech | 2 | 05 | not written |
| WO-09 | Notifiers: Discord, Slack, ntfy | go-mech | 2 | 05 | not written |
| WO-10 | report.Service (digest job, heartbeat, retry) | go-builder | 2 | 02,05 | not written |
| WO-11 | findings.Service and ops.Backup | go-mech | 2 | 02,05 | not written |
| WO-12 | Web: findings list, /f/{id}, ack/snooze/dismiss POSTs | go-mech | 3 | 01,02,11 | not written |
| WO-13 | Wiring: cmd/upw jobs, notifiers config, doctor, upw findings, heartbeat | go-mech | 3 | all wave 2 | not written |
| WO-14 | Test suites (vuln, digest, notifiers) | test-writer | 4 | 13 | not written |
| WO-15 | Advisory parity referee vs osv-scanner | go-mech | 4 | 13 | not written |

Then: Fable review of M2, security review (Sonnet), docs-sync, checklist run, owner items (SMTP UPW-42, VM parity, digest arrives, ack on VM).

## Rules in force

- Every builder dispatch: `model: "opus"`, `isolation: "worktree"` for parallel waves; push to origin before dispatch.
- Review each WO: Astra while Codex 5h and weekly windows < 80% (`python3 ~/.claude/mods/usage-hud/bin/usage.py tokens`), one at a time; otherwise Sonnet `reviewer`.
- Lead reruns `make check` itself; merges worktrees one at a time; `go mod tidy` after WO-06 if `vers` becomes direct.

## Log

- 2026-11-19 22:20 M2 started at Amrit's request. Interface files, migration 0004 and WO-01..06 written. Resume scheduled 01:29 (session cron) for another 5h window.

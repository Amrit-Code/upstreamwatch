# M2 progress (lead's running log)

Read this first when resuming. Update it after every accepted work order.

## Plan

| WO | Title | Agent | Wave | Depends on | Status |
|---|---|---|---|---|---|
| WO-01 | Store: advisories, matches, kv, finding detail | go-builder | 1 | - | merged (1 fix round, Sonnet review) |
| WO-02 | Store: finding state, digests, backup, write queue | go-builder | 1 | - | merged (Sonnet review ACCEPT) |
| WO-03 | OSV adapter | go-builder | 1 | - | fix round 1 done (Maven); in review |
| WO-04 | KEV and EPSS adapters | go-mech | 1 | - | merged (Astra REVISE -> fixed) |
| WO-05 | Model rules: rank, transitions, digest text | go-builder | 1 | - | merged (Sonnet review ACCEPT) |
| WO-06 | Vuln matching rules | go-builder | 1 | - | merged (ruling on Actions tags; review ACCEPT) |
| WO-07 | vuln.Service (vulns job) | go-builder | 2 | 01,03,04,05,06 | written; waits for WO-03 merge |
| WO-08 | Notifiers: email and stdout | go-mech | 2 | 05 | dispatched |
| WO-09 | Notifiers: Discord, Slack, ntfy | go-mech | 2 | 05 | dispatched |
| WO-10 | report.Service (digest job, heartbeat, retry) | go-builder | 2 | 01,02,05 | dispatched |
| WO-11 | findings.Service and ops.Backup | go-mech | 2 | 02,05 | dispatched |
| WO-12 | Web: findings list, /f/{id}, ack/snooze/dismiss POSTs | go-mech | 3 | 01,02,05,11 | written |
| WO-13 | Wiring: cmd/upw jobs, notifiers config, doctor, upw findings, heartbeat | go-mech | 3 | all wave 2 | written |
| WO-14 | Test suites (vuln, digest, notifiers) | test-writer | 4 | 13 | not written |
| WO-15 | Advisory parity referee vs osv-scanner | go-mech | 4 | 13 | written (osv-scanner 2.7.1 at ~/go/bin, built with CGO_ENABLED=0) |

Then: Fable review of M2, security review (Sonnet), docs-sync, checklist run, owner items (SMTP UPW-42, VM parity, digest arrives, ack on VM).

## Rules in force

- Every builder dispatch: `model: "opus"`, `isolation: "worktree"` for parallel waves; push to origin before dispatch.
- Review each WO: Astra while Codex 5h and weekly windows < 80% (`python3 ~/.claude/mods/usage-hud/bin/usage.py tokens`), one at a time; otherwise Sonnet `reviewer`.
- Lead reruns `make check` itself; merges worktrees one at a time; `go mod tidy` after WO-06 if `vers` becomes direct.

## Log

- 2026-11-19 22:20 M2 started at Amrit's request. Interface files, migration 0004 and WO-01..06 written. Resume scheduled 01:29 (session cron) for another 5h window.
- 22:30 Wave 1 dispatched (base 4f99de3). Codex 5h window hit 85% after the WO-04 Astra run (resets 23:22); Sonnet reviewer used since.
- 22:50 make check's gofmt now skips .claude/ (agent worktrees). go.mod tidied (vers direct).
- Deferred follow-ups (not yet in a WO): rc toolchain versions (`go1.26rc1`) are not normalised to `1.26.0-rc1`, so they skip ranges (WO-06 review); `migrate.go` creates schema_migrations outside the write queue (startup only); `store/interfaces_test.go` should assert the M2 consumer interfaces (lead verified by a scratch compile at fc47d4c); docs-sync pending for wave 1.

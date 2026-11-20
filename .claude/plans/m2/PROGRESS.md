# M2 progress (lead's running log)

Read this first when resuming. Update it after every accepted work order.

## Plan

| WO | Title | Agent | Wave | Depends on | Status |
|---|---|---|---|---|---|
| WO-01 | Store: advisories, matches, kv, finding detail | go-builder | 1 | - | merged (1 fix round, Sonnet review) |
| WO-02 | Store: finding state, digests, backup, write queue | go-builder | 1 | - | merged (Sonnet review ACCEPT) |
| WO-03 | OSV adapter | go-builder | 1 | - | merged (Maven fix; review ACCEPT) |
| WO-04 | KEV and EPSS adapters | go-mech | 1 | - | merged (Astra REVISE -> fixed) |
| WO-05 | Model rules: rank, transitions, digest text | go-builder | 1 | - | merged (Sonnet review ACCEPT) |
| WO-06 | Vuln matching rules | go-builder | 1 | - | merged (ruling on Actions tags; review ACCEPT) |
| WO-07 | vuln.Service (vulns job) | go-builder | 2 | 01,03,04,05,06 | merged (fix: mark queried after fetch; review ACCEPT) |
| WO-08 | Notifiers: email and stdout | go-mech | 2 | 05 | merged (fix: race, QUIT, redaction test; review ACCEPT) |
| WO-09 | Notifiers: Discord, Slack, ntfy | go-mech | 2 | 05 | merged (ruling: no body in errors; small fixes; review ACCEPT) |
| WO-10 | report.Service (digest job, heartbeat, retry) | go-builder | 2 | 01,02,05 | merged (major fix: per-record context; review) |
| WO-11 | findings.Service and ops.Backup | go-mech | 2 | 02,05 | merged (review ACCEPT) |
| WO-12 | Web: findings list, /f/{id}, ack/snooze/dismiss POSTs | go-mech | 3 | 01,02,05,11 | merged (review ACCEPT) |
| WO-13 | Wiring: cmd/upw jobs, notifiers config, doctor, upw findings, heartbeat | go-mech | 3 | all wave 2 | merged (2 fix rounds: unblocked; Astra REVISE -> shared pipeline lock, LastFire cutoff, OSV fixture) |
| WO-14 | Test suites (vuln, digest, notifiers) | test-writer | 4 | 13 | merged (fix round: test tightening; review ACCEPT) |
| WO-15 | Advisory parity referee vs osv-scanner | go-mech | 4 | 13 | in progress (cut off by usage limit 23:40, resumed 01:30) |

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
- 23:02 Waves 1 and 2 and WO-12 merged; main green at 3e5ef4d. Docs synced through wave 1 (df0989a); next docs-sync after WO-13.
- Deferred follow-ups added: a repo whose intervals all vanished keeps its advisory findings unresolved (vuln scope comes from intervals; WO-07 review); undecodable failed digest payload fails every digest run (WO-10 review); `osv` modified_id.csv uses ValidatorsNone (full download each run).
- 23:30 All M2 code merged; main green at e00d8d2. Checklist items 1, 3, 4, 6-14, 19 passed (7d469f3). Docs synced to e00d8d2 (231000f). govulncheck v1.9.0 clean.
- 23:35 Security review (Sonnet): 1 medium (bare URLs from advisory text stay clickable in every channel; defang once in report), 2 low (Discord escape `<` + heartbeat allowed_mentions + Slack `*_~` backtick; OSV Fetch body cap ~2 MiB + id equality check + cap Details). Not yet fixed: goes into WO-16 with the Fable findings.
- 23:40 Usage limit hit; WO-15 and the Fable review were cut off mid-run. 01:29 resumed both via SendMessage.
- Follow-ups still open: silent wait on the shared pipeline lock (log a line); duplicated finding display rules in cmd/upw/findings.go and internal/web/findings.go; rc toolchain normalisation; repo with vanished intervals keeps findings; undecodable failed digest payload; outbound-client skill text needs the SMTP loopback exception.

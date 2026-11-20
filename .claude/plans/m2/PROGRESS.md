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
| WO-15 | Advisory parity referee vs osv-scanner | go-mech | 4 | 13 | merged (ruling: osv-scanner ScanGoModVersion; 2 fix rounds; review ACCEPT) |

| WO-16 | Security review fixes | go-mech | 5 | review | merged (Astra REVISE -> Now: line fixed) |
| WO-17 | Fable review fixes (PyPI PEP 503, delta corrections, cursor, resolution scope) | go-builder | 5 | WO-16 | merged (Astra REVISE -> cursor = run start, scoped resolution) |

Status 2026-11-20 02:15: all lead work for M2 is done. Checklist 19/22 passed; security review clean; Fable review majors fixed; docs synced to 9ff9085 (0a49310). M2 does NOT close until the three owner items pass (20 parity on private targets, 21 digest arrives by email (needs UPW-42 SMTP details), 22 ack on the real instance). Then tag m2.

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
- 01:30-02:10 WO-15 parity: osv-scanner skips the go.mod Go version by default; ruled ScanGoModVersion=true -> glow 18=18, cleo 34=34 (lead run). WO-16 security fixes and WO-17 Fable fixes merged after Astra reviews (both REVISE, fixed). make e2e (both referees) passed on the WO-17 tree.
- 02:10 VM upgraded to image m2-9ff9085 (built on the VM from /root/src/upstreamwatch at 9ff9085; DB copied to data/pre-m2/ first). Migration 0004 applied; doctor all ok; run all -once on the 8 private targets ok (timelines rebuilt for extractor v2; 157 advisories, 283 findings; digest to stdout; backup written). UI checks (items 5, 16-18) passed on the VM; the three findings used were reopened afterwards.
- 02:15 Deferred follow-ups filed in Plane under M5 (UPW-127..132).
- Next: owner items 20-22; then milestone-accept closing steps (make check on main, tag m2). M3 not started: ask Amrit first.

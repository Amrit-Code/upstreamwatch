# M5 Eval, hardening and docs: acceptance checklist

Source: PLAN.md section 10 (M5) with sections 6 (ack/snooze semantics), 8 (upgrades, backups, promotion), 9 and 11, adjusted for the owner's decisions of 2026-11-19 (repository public, Mac builds, VM deployment, email digest).

Conventions: run from the repository root on the Mac unless Who says VM. `upw` is the binary built from `./cmd/upw` with the dev config `upw.dev.yaml`. `<db>` is the SQLite file in the data directory. Status is changed only by the lead (see SKILL.md).

| # | Item | Command or observation | Expected | Who | Status | Evidence |
|---|---|---|---|---|---|---|
| 1 | Checks | `make check` | exit 0 | lead | open | |
| 2 | CI hardening | `gh run list --branch main --limit 1` and the workflow file | green; workflow runs vet, staticcheck, `go test`, the fixture e2e, `govulncheck`, image build, push to `ghcr.io` on tags | lead | open | |
| 3 | Dogfood in CI | the scheduled weekly workflow run | `upw run all --once` on the tool's own repository with `llm.enabled: false` succeeds and posts a summary to the job log | lead | open | |
| 4 | Threshold sweep | `/eval` | prefilter recall and pass rate shown for thresholds 1 to 8; shipped default meets both targets on the fixture set | lead | open | |
| 5 | Fixture eval | `upw eval run` and `upw eval report` on the public fixture set (fake provider in CI; real key for the committed report) | every PLAN.md section 9 threshold met; report committed under `eval/reports/` | lead | open | |
| 6 | Dismissal labels | dismiss an issue finding with the box left ticked | an `eval_labels` row with `does_not_affect`; unticked writes none | lead | open | |
| 7 | Auto-resolve | findings tests | advisory finding resolves when the pin leaves the affected range; issue finding resolves when the issue closes as completed and the pin is at least `fixed_in`, or the package leaves the repo | lead | open | |
| 8 | Ack and snooze rules | findings tests | acked finding re-sent only if score rises by 20 or more or a new fixed version appears; snooze ends at its time or on pin change | lead | open | |
| 9 | Retention | retention job test | closed upstream issues older than `retention.issues_closed_keep_days` removed; non-reconstructible tables untouched | lead | open | |
| 10 | Export | `upw export verdicts > v.json`, `upw export labels > l.json`, `upw export state > s.json` (one kind per invocation, written to stdout); import the labels into a fresh data dir with `upw eval import` | each file is non-empty valid JSON; labels round-trip without loss | lead | open | |
| 11 | README | read `README.md` | what it does, 5-minute quickstart with a public repo and no tokens, config reference, privacy table by `snippet_mode`, supported ecosystems, how the prefilter and verdicts work, fixture eval numbers, self-hosting, roadmap, credits | lead | open | |
| 12 | Quickstart works | follow the README quickstart on a clean checkout | timeline and advisories shown for the public repo with no tokens | lead | open | |
| 13 | Security notes | `docs/` and README security sections | token scopes, untrusted upstream text, nothing executed, git URL policy, outbound surface list, settings key storage | lead | open | |
| 14 | Repo hygiene | the M0 item 18 commands, and a read of `eval/`, `docs/` and `.claude/plans/` | nothing names a private repository, hostname, tailnet or token | lead | open | |
| 15 | Release image | tag `v0.1.0`; `gh run list --workflow <ci workflow> --limit 1` | `ghcr.io/amrit-code/upstreamwatch:v0.1.0` published | lead | open | |
| 16 | Full label set | owner labels 300 to 400 candidates in `/eval`; `upw export labels` (alias `upw eval export`) kept on the Mac outside the repo | label count in range | owner (labelling) | open | |
| 17 | Private eval | with the owner's key, `upw eval run` and `upw eval report` on the private set (prompt v2 only with its report attached) | every PLAN.md section 9 threshold met | owner (key) | open | |
| 18 | No silent failures | two consecutive weeks of daily digests and Monday heartbeats on the VM | every expected message arrived; every failed job appeared as a `job_failure` finding | owner (VM, inbox) | open | |
| 19 | Upgrade path | on the VM: `UPW_VERSION=v0.1.0 docker compose pull && docker compose up -d` | migrations apply at start; service healthy; data intact | owner (VM) | open | |
| 20 | Fresh VM from README | owner stands up a new VM following only the README and upw-deploy | serving `/health` over the tailnet in under an hour | owner (VM) | open | |
| 21 | Promotion | homelab skill promotion checklist: `lab` tag, `health_check_url`, snapshot, fleet docs | done and recorded | owner (VM) | open | |

Security review: open.

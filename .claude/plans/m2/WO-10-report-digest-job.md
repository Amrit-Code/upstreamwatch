# WO-10: report.Service, the digest job

Milestone: M2
Agent: go-builder   Model: opus
Depends on: WO-01, WO-02, WO-05 (merged)
Report file: `.claude/plans/m2/WO-10-report-digest-job.report.md`
Ticket: UPW-53

## Objective

`report.Service` implements `pipeline.Job` as `digest`: it resends failed digests, builds one `model.Digest` from the open findings that are new or changed since they were last reported, falls back to the weekly heartbeat when there is nothing, marks the findings reported, sends the digest through every channel, and records each send in `digests`. Serves M2 checklist items 12 to 16.

## Owned files

- `internal/report/service.go`, `internal/report/render.go`, and their `_test.go` files, `internal/report/fakes_test.go`, `internal/report/testdata/` (new)
- `internal/report/integration_test.go` (new; real store in a temp dir, fake notifiers)

Read-only: `internal/report/interfaces.go`, `internal/report/doc.go`, `internal/model/`, `internal/store/`, `go.mod`, `go.sum`.

## Interfaces

### Consumed

`internal/report/interfaces.go`: `Notifier`, `Store` (read the file). `internal/model`: `Digest`, `DigestItem`, `DigestRecord`, `DigestStatus` constants, `FindingFilter`, `Finding`, `FindingDetail`, `Exposure`, `FindingState.Effective`, `Run`, `Redact`, `ErrNotFound`, `KindAdvisory`, `KindIssue`, `KindJobFailure`.

### Produced

```go
// internal/report/service.go
type Channel struct {
	Name     string   // unique; recorded in digests.channel
	Notifier Notifier
}
type Options struct {
	BaseURL  string // http.base_url without a trailing slash; empty means no links
	MaxItems int    // per section; default 25
	// HeartbeatLastFire returns the most recent heartbeat fire time at or
	// before now; ok is false when no heartbeat is configured.
	HeartbeatLastFire func(now time.Time) (t time.Time, ok bool)
	Now               func() time.Time // default time.Now; the digest uses its location
}
func NewService(store Store, channels []Channel, opts Options, logger *slog.Logger) *Service
func (s *Service) Name() string // "digest"
func (s *Service) Run(ctx context.Context, scope model.Scope) (model.Stats, error) // scope is ignored
```

## Requirements

1. Retry first: for each `FailedDigests` row whose `Channel` names a configured channel, decode the payload into a `model.Digest` and send it unchanged; on success `SaveDigest` a new `sent` row with the same payload and `MarkDigestRetried` the old row; on failure leave the row as it is and collect the error. Rows for channels no longer configured are left alone (logged once each).
2. Build: `ListFindings(FindingFilter{Suppressed: true, Now: now})` gives every unresolved finding. Counts `Open`, `Acked`, `Snoozed` by `State.Effective(now)` (dismissed counts nowhere). Candidates are findings whose effective state is `open`: NEW when `ReportedInDigestAt` is zero, CHANGED when `LastChangedAt` is after `ReportedInDigestAt`. Each list keeps the store's order (score, then ID). The first `MaxItems` of each become items; the rest are counted in `MoreNew` / `MoreChanged`.
3. Item rendering (`render.go`), from `GetFindingDetail`:
   - advisory: `Tag` `[A]`; `Headline` `<severity or "unknown">  <advisory id>  <package URL> <interval version>  (repo: <slug>)`; `Lines`: the summary (or the first sentence of details when there is no summary), max 200 runes; then the facts that are known, joined in one line: `Fixed in <v>.`, `KEV: yes.` or `KEV: no.`, `EPSS <score to 2 places> (p<percentile×100 rounded>).`; then one exposure line: for an open linked match `Exposed since <YYYY-MM-DD> (<manifest path>) — confidence <c>` with ` (pinned since <date>)` appended when the interval start differs from the exposure start, for historical only `Exposed <from> to <to> (<manifest path>, no longer pinned) — confidence <c>`; for a CHANGED item a last line `Now: <rank reason>`.
   - job_failure: `Tag` `[J]`; `Headline` `job <name> failed` (the job name is the part of the fingerprint after `job_failure|`); `Lines`: the rank reason.
   - issue (not produced before M4): `Tag` `[I]`, `Headline` the rank reason.
   - `URL` is `BaseURL + "/f/<id>"` when `BaseURL` is set.
   - Every Headline and Line is sanitised: control characters (including CR, LF and tab) become a space, runs of spaces collapse inside upstream text (the summary), invalid UTF-8 is replaced, and each line is capped at 300 runes with `…`.
4. Digest: `GeneratedAt` = now (local), `LastRuns` from `LatestRuns`, `FindingsURL` = `BaseURL + "/"` when set, `Waiting` 0.
5. Nothing to report (no items and no "more" counts): if `HeartbeatLastFire` is set and returns a time `t` with `t` not after now and `t` after the `SentAt` of `LastSentDigest` (or no digest was ever sent), send a heartbeat digest (`Heartbeat: true`, no items, same counts and runs). Otherwise send nothing and record nothing (stat `skipped` 1).
6. Before sending a non-heartbeat digest, `MarkReported` every candidate (shown or counted in "more") at now. Then send to each channel in order; for each, `SaveDigest` a row with `Status` sent or failed, `SentAt` now, the JSON payload, and the error text through `model.Redact`. One channel failing does not stop the others. If any send (including retries) failed, `Run` returns an error naming the failed channels after all channels were tried.
7. With no channels configured, `Run` returns nil, sends nothing, marks nothing, and logs a warning (stat `no_channels` 1).
8. Stats: `new`, `changed`, `more`, `heartbeat`, `sent`, `failed`, `retried`, `skipped`, `no_channels`.

## Tests to include

With a fake store and fake notifiers unless noted:

- `TestRun_NewAndChanged`: findings never reported, reported-then-changed, reported-unchanged, acked, snoozed (running), snoozed (expired, counts as open and is NEW), dismissed → the right items in New and Changed, counts open/acked/snoozed, MarkReported called with exactly the candidates.
- `TestRun_NothingThenSkip` (integration, real store): run twice with nothing new → second run sends nothing; one `digests` row from the first (checklist item 13).
- `TestRun_Heartbeat`: no candidates, heartbeat last fire after the last sent digest → heartbeat sent; a second run at the same time → skipped (checklist item 14).
- `TestRun_FailedSendRetried`: channel A fails, B succeeds → run returns an error naming A, rows failed and sent; next run with A healthy resends the same payload to A only and marks the old row retried (checklist item 15).
- `TestRun_AckedLeftOut` (integration): an acked finding and a new one → digest has only the new one (checklist item 16).
- `TestRender_Advisory_Golden`: an advisory finding detail → items golden JSON; a summary with `\x1b[31m` and newlines is sanitised.
- `TestRun_MaxItems`: 30 new findings with `MaxItems` 25 → 25 items, `MoreNew` 5, all 30 marked reported.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race ./internal/report/` | `ok` |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Notifier adapters (WO-08, WO-09). Wiring, channel naming from config, the heartbeat fire-time function (WO-13). Ack re-send rules (M5). Issue findings (M4).

## Skills and docs

No domain skill needed.
Read: PLAN.md section 6 (Daily digest, Ack/snooze semantics); `docs/decisions.md` "Lead decisions for M2"; `docs/database.md` (digests).

## Report

Write the full report to the report file named at the top, in the form given in your agent definition, then reply with only the short form. If a question's answer would change behaviour and nothing here or in the docs answers it, stop and report NEEDS_CONTEXT rather than guessing.

## Rulings


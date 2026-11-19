# WO-05: Finding rules in the model: advisory ranking, state transitions, digest text

Milestone: M2
Agent: go-builder   Model: opus
Depends on: none (interface files committed)
Report file: `.claude/plans/m2/WO-05-model-rules.report.md`
Ticket: UPW-49

## Objective

The pure rules that the vulns job, the digest job, `findings.Service`, the web handlers and the CLI share live in `internal/model`: the advisory score formula from PLAN.md section 6, the finding state machine with snooze expiry, and the plain-text layout of a digest. Serves M2 checklist items 8, 12, 14, 16 and 17.

## Owned files

- `internal/model/rank.go`, `internal/model/rank_test.go` (new)
- `internal/model/state.go`, `internal/model/state_test.go` (new)
- `internal/model/digest_text.go`, `internal/model/digest_text_test.go` (new)
- `internal/model/testdata/` (new; golden files)

Read-only: every other file in `internal/model/` (lead-owned: `finding.go`, `digest.go`, `advisory.go`, `finding_detail.go`, `timeline.go`, `run.go`), `go.mod`, `go.sum`. `internal/model` imports only the standard library, `purl` and `vers`; you need neither.

## Interfaces

### Consumed

`internal/model/finding.go` (`Finding`, `FindingState`, `FindingStateValue` and its constants), `internal/model/digest.go` (`Digest`, `DigestItem`), `internal/model/advisory.go` (`SeverityLabel`, `Confidence`), `internal/model/timeline.go` (`Directness`), `internal/model/run.go` (`Run`, `RunStatus`).

### Produced

```go
// internal/model/rank.go
// AdvisoryRankInput is what the advisory score depends on.
type AdvisoryRankInput struct {
	Severity         SeverityLabel
	KEV              bool
	HasEPSS          bool
	EPSSPercentile   float64 // 0..1
	Direct           Directness
	CurrentlyExposed bool // at least one match is still open
	Confidence       Confidence
}
// RankAdvisory returns the score of an advisory finding and a one-line
// explanation of its parts.
func RankAdvisory(in AdvisoryRankInput) (score float64, reason string)
// JobFailureScore is the fixed score of a job_failure finding.
const JobFailureScore = 50

// internal/model/state.go
// StateChange is one operator action on a finding.
type StateChange struct {
	To          FindingStateValue
	Now         time.Time
	SnoozeUntil time.Time // required, and after Now, when To is StateSnoozed
	Note        string
	By          string
}
var ErrInvalidTransition = errors.New("invalid finding state change")
// Transition returns the state f moves to under c, or an error wrapping
// ErrInvalidTransition.
func (f Finding) Transition(c StateChange) (FindingState, error)
// Effective returns the state as the operator sees it at now: a snooze that
// has ended counts as open.
func (s FindingState) Effective(now time.Time) FindingStateValue
// MaxNoteRunes is the longest note accepted.
const MaxNoteRunes = 2000

// internal/model/digest_text.go
// PlainText lays the digest out as plain text for email, ntfy and stdout.
func (d Digest) PlainText() string
// Subject is the one-line summary used as an email subject and ntfy title.
func (d Digest) Subject() string
```

## Requirements

1. `RankAdvisory`: base by severity critical 90, high 70, medium 40, low 20, unknown 30; plus 30 when `KEV`; plus `round(EPSSPercentile × 20)` when `HasEPSS` (an integer, so daily EPSS drift rarely changes the score); plus 10 when `Direct == DirectYes`; plus 20 when `CurrentlyExposed`; minus 10 for `ConfidenceMedium`, 25 for `ConfidenceLow`, 0 for high or empty. The score is an integer-valued float64.
2. The reason lists the parts that apply, in that order, separated by `; `, with fixed formatting so equal inputs always give byte-equal reasons, for example `high 70; KEV +30; EPSS p71 +14; direct +10; exposed now +20; confidence medium -10`. The EPSS part shows the percentile as `p<round(percentile×100)>`. Parts that contribute nothing (no KEV, no EPSS, not direct, not exposed, high confidence) are left out; unknown severity shows `unknown 30`.
3. `Transition`: an unresolved finding may move from any state to any of `open`, `acked`, `snoozed`, `dismissed`, including its current state (re-acking updates the note; re-snoozing changes the end). Errors wrapping `ErrInvalidTransition`: the finding is resolved (`ResolvedAt` set); `To` is not one of the four values; `To` is snoozed and `SnoozeUntil` is zero or not after `Now`; the note has more than `MaxNoteRunes` runes or contains a control character other than newline and tab, or is not valid UTF-8. The returned state has `State = To`, `SnoozeUntil` set only for snoozed (zero otherwise), `Note` (trimmed of surrounding white space), `ChangedAt = Now` (UTC), `ChangedBy = By`.
4. `Effective(now)`: `snoozed` with `SnoozeUntil` at or before `now` → `open`; otherwise the stored state.
5. `PlainText` for a digest with items follows PLAN.md section 6:

   ```
   upstreamwatch — 2026-11-18 — 3 new, 1 changed, 0 waiting for LLM budget

   NEW
   [A] high  GO-2026-4866  pkg:golang/toolchain 1.26.0  (repo: server)
        net/http: request smuggling ...
        Exposed since 2026-09-14 ... — confidence high
        https://upstreamwatch.example.ts.net/f/1842

   CHANGED
   [A] medium  PYSEC-2026-2275  ...
        ...

   Last runs: digest 08:00 ok, ingest 07:05 ok, vulns 07:10 failed.
   Open: 11, acked: 4, snoozed: 2.  https://upstreamwatch.example.ts.net/
   ```

   The header counts are `len(New)+MoreNew`, `len(Changed)+MoreChanged` and `Waiting`. An item is `Tag + " " + Headline`, then each of `Lines` and then `URL` (when not empty) on its own line indented by five spaces. Sections with no items are left out. After a section's items, when `MoreNew` (or `MoreChanged`) is positive, a line `     … and N more` follows (with the findings URL appended after a space when set). A `WAITING  N issue candidates over today's LLM budget.` line appears only when `Waiting > 0`. The `Last runs:` line lists every run in `LastRuns` as `<job> HH:MM ok|failed|running` in `GeneratedAt`'s location (the run's `FinishedAt`, or `StartedAt` while running), or `Last runs: none.` when empty. The counts line ends with the findings URL when set. Dates and times use `GeneratedAt.Location()`.
6. `PlainText` for a heartbeat: `upstreamwatch — <date> — all quiet`, a blank line, `No new or changed findings since the last digest.`, a blank line, then the same `Last runs:` and counts lines.
7. `Subject`: `upstreamwatch — <date> — N new, M changed` or, for a heartbeat, `upstreamwatch — <date> — all quiet`.
8. `PlainText` does not escape or alter `Headline`, `Lines` or `URL` (the digest job sanitises them); it never panics on empty fields.

## Tests to include

- `TestRankAdvisory` (table): the PLAN.md example shape (high, no KEV, EPSS p71, direct, exposed, high confidence → 70+14+10+20 = 114 and the exact reason), critical KEV low confidence historical transitive, unknown severity, EPSS percentile 0.874 → +17 and `p87`.
- `TestTransition` (table): every allowed move; resolved → error; snooze without end, end in the past, end equal to now → error; note too long, note with `\x1b` → error; unknown state → error; returned fields as specified.
- `TestEffective`: snoozed until past/now/future, other states.
- `TestDigestPlainText_Golden`, `TestDigestPlainText_Heartbeat_Golden`, `TestDigestPlainText_More` with golden files under `internal/model/testdata/` and an `-update` flag as the go-test skill describes; a fixed `GeneratedAt` in `Europe/London`.
- `TestDigestSubject`.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -run 'TestRankAdvisory\|TestTransition\|TestEffective\|TestDigest' ./internal/model/` | `ok` |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Ack and snooze re-send rules, snooze "until version change" and auto-resolve (M5).
- Issue ranking (M4).
- Building digest items (the digest job) and per-channel formats (the notifier work orders).

## Skills and docs

No domain skill needed.
Read: PLAN.md sections 6 (Ranking, Daily digest, Ack/snooze semantics); `docs/domain-model.md`.

## Report

Write the full report to the report file named at the top, in the form given in your agent definition, then reply with only the short form. If a question's answer would change behaviour and nothing here or in the docs answers it, stop and report NEEDS_CONTEXT rather than guessing.

## Rulings


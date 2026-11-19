# WO-11: findings.Service (ack, snooze, dismiss, reopen) and the backup job

Milestone: M2
Agent: go-mech   Model: opus
Depends on: WO-02, WO-05 (merged)
Report file: `.claude/plans/m2/WO-11-findings-service-backup.report.md`
Ticket: UPW-55 (service half), UPW-56

## Objective

`findings.Service` applies operator actions to a finding through the model's state rules, and `ops.Backup` is the nightly `backup` job that writes `data/backups/upw-YYYYMMDD.sqlite` with `VACUUM INTO` and keeps the newest seven. Serves M2 checklist items 16, 17 and 19.

## Owned files

- `internal/findings/service.go`, `internal/findings/service_test.go` (new)
- `internal/ops/backup.go`, `internal/ops/backup_test.go` (new)

Read-only: `internal/findings/interfaces.go`, `internal/findings/doc.go`, `internal/ops/interfaces.go`, `internal/ops/doc.go`, `internal/model/`, `internal/store/`, `go.mod`, `go.sum`.

## Interfaces

### Consumed

```go
// internal/findings/interfaces.go
type Store interface {
	GetFinding(ctx context.Context, id int64) (model.Finding, error)
	SaveFindingState(ctx context.Context, id int64, st model.FindingState) error
}
// internal/ops/interfaces.go
type Backuper interface {
	BackupTo(ctx context.Context, path string) error
}
// internal/model/state.go (WO-05)
type StateChange struct { To FindingStateValue; Now time.Time; SnoozeUntil time.Time; Note string; By string }
func (f Finding) Transition(c StateChange) (FindingState, error)
var ErrInvalidTransition error
```

### Produced

```go
// internal/findings/service.go
func NewService(store Store, now func() time.Time) *Service // now nil means time.Now
func (s *Service) Ack(ctx context.Context, id int64, note, by string) (model.FindingState, error)
func (s *Service) Snooze(ctx context.Context, id int64, d time.Duration, note, by string) (model.FindingState, error)
func (s *Service) Dismiss(ctx context.Context, id int64, note, by string) (model.FindingState, error)
func (s *Service) Reopen(ctx context.Context, id int64, by string) (model.FindingState, error)

// internal/ops/backup.go
type BackupOptions struct {
	Dir  string           // <data_dir>/backups
	Keep int              // retention.backups_keep; default 7
	Now  func() time.Time // default time.Now; the file name uses its local date
}
func NewBackup(db Backuper, opts BackupOptions, logger *slog.Logger) *Backup
func (b *Backup) Name() string // "backup"
func (b *Backup) Run(ctx context.Context, scope model.Scope) (model.Stats, error) // scope is ignored
```

## Requirements

1. Each `findings.Service` method loads the finding with `GetFinding`, calls `Transition` with `To` = acked, snoozed (`SnoozeUntil = now + d`), dismissed or open (Reopen, empty note), stores the result with `SaveFindingState`, and returns it. `Snooze` accepts only `d` of 24h, 7×24h or 30×24h (the UI's 1d, 7d, 30d); anything else is an error wrapping `model.ErrInvalidTransition`. Errors from the store (`model.ErrNotFound`) and from `Transition` are returned wrapped so `errors.Is` works.
2. `Backup.Run`: the file is `Dir/upw-<YYYYMMDD>.sqlite` using `Now()`'s local date. When that file already exists, nothing is written (stat `exists` 1) and retention still runs. Otherwise `BackupTo(path)` (stat `written` 1). The directory is created with mode 0700 if missing.
3. Retention: list the regular files in `Dir` whose names match `^upw-\d{8}\.sqlite$`, sort by name, and delete the oldest until at most `Keep` remain (stat `deleted` = count). Other files in the directory are never touched. A deletion error is returned after the others were tried.

## Tests to include

- `TestAck`, `TestSnooze` (1d sets `SnoozeUntil` to now+24h; 2h → error), `TestDismiss`, `TestReopen`, with a fake store; `TestAck_NotFound` → `errors.Is(err, model.ErrNotFound)`; `TestAck_Resolved` → `errors.Is(err, model.ErrInvalidTransition)` and nothing saved.
- `TestBackup_EightDays`: a fake `Backuper` that creates the file, an injected clock advanced one day per run for eight runs → seven files remain, the oldest gone, an unrelated `notes.txt` in the directory untouched (checklist item 19).
- `TestBackup_SameDayTwice` → second run writes nothing.
- `TestBackup_RealStore`: with a real store (`store.Open` in a temp dir, migrated, a row inserted) → the backup file opens with `store.Open` and has the row.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race ./internal/findings/ ./internal/ops/` | `ok` |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Web handlers (WO-12), wiring (WO-13), writing an eval label on dismiss (M5), snooze until version change (M5).

## Skills and docs

No domain skill needed.
Read: PLAN.md section 6 (Ack/snooze semantics) and 8 (Backups); `docs/architecture.md` (acknowledge sequence).

## Report

Write the full report to the report file named at the top, in the form given in your agent definition, then reply with only the short form. If a question's answer would change behaviour and nothing here or in the docs answers it, stop and report NEEDS_CONTEXT rather than guessing.

## Rulings


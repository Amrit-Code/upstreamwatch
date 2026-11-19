# WO-12: Findings list, finding page and the ack, snooze, dismiss and reopen forms

Milestone: M2
Agent: go-mech   Model: opus
Depends on: WO-01, WO-02, WO-05, WO-11 (merged)
Report file: `.claude/plans/m2/WO-12-web-findings-pages.report.md`
Ticket: UPW-54, UPW-55

## Objective

`GET /` lists findings with repo and kind filters and a "suppressed" toggle, `GET /f/{id}` shows one finding with everything known about it, and `POST /f/{id}/ack|snooze|dismiss|reopen` change its state through `findings.Service`, protected by the existing cross-origin check. Serves M2 checklist items 5, 16, 17 and 18.

## Owned files

- `internal/web/` (all files: `interfaces.go`, `handlers.go`, `server.go`, new `findings.go`, templates, `static/style.css`, tests, `testdata/`)
- `internal/query/` (all files: `interfaces.go`, `service.go`, new `findings.go`, tests)
- `cmd/upw/serve.go` (only the `web.NewServer` call, to pass the new dependency)

Read-only: `internal/model/`, `internal/store/`, `internal/findings/`, every other file in `cmd/upw/`, `go.mod`, `go.sum`.

## Interfaces

### Consumed

```go
// store methods (WO-01, WO-02) that query may declare in its FindingStore:
ListFindings(ctx context.Context, f model.FindingFilter) ([]model.Finding, error)
GetFindingDetail(ctx context.Context, id int64) (model.FindingDetail, error)
ListRepos(ctx context.Context) ([]model.TargetRepo, error) // already in query.TimelineStore

// internal/findings/service.go (WO-11)
func (s *Service) Ack(ctx context.Context, id int64, note, by string) (model.FindingState, error)
func (s *Service) Snooze(ctx context.Context, id int64, d time.Duration, note, by string) (model.FindingState, error)
func (s *Service) Dismiss(ctx context.Context, id int64, note, by string) (model.FindingState, error)
func (s *Service) Reopen(ctx context.Context, id int64, by string) (model.FindingState, error)
```

`model.FindingDetail`, `model.Exposure`, `model.FindingFilter`, `FindingState.Effective`, `model.ErrNotFound`, `model.ErrInvalidTransition`.

### Produced

```go
// internal/query
func (s *Service) Findings(ctx context.Context, f model.FindingFilter) ([]model.FindingDetail, error)
func (s *Service) FindingDetail(ctx context.Context, id int64) (model.FindingDetail, error)

// internal/web/interfaces.go
type FindingsReader interface {
	Findings(ctx context.Context, f model.FindingFilter) ([]model.FindingDetail, error)
	FindingDetail(ctx context.Context, id int64) (model.FindingDetail, error)
}
type FindingActions interface {
	Ack(ctx context.Context, id int64, note, by string) (model.FindingState, error)
	Snooze(ctx context.Context, id int64, d time.Duration, note, by string) (model.FindingState, error)
	Dismiss(ctx context.Context, id int64, note, by string) (model.FindingState, error)
	Reopen(ctx context.Context, id int64, by string) (model.FindingState, error)
}
func NewServer(health HealthReader, findings FindingsReader, actions FindingActions, timeline TimelineReader, opts Options, logger *slog.Logger) (*Server, error)
```

`query.Service.OpenFindings` and `query.FindingStore.ListOpenFindings` are removed once nothing uses them (keep `store.DB.ListOpenFindings`; the lead removes it later if unused).

## Requirements

1. `query.Service.Findings(f)`: `ListFindings(f)` then `GetFindingDetail` for each, in the same order. `FindingDetail(id)` passes through, `model.ErrNotFound` included. Both set `f.Now` to the service clock when zero (add a `now func() time.Time` defaulting to `time.Now`, set through an unexported field for tests).
2. `GET /` query parameters: `repo` (a slug; unknown slug → 400), `kind` (`advisory`, `issue`, `job_failure`; other → 400), `suppressed` (`1` to include snoozed and dismissed). Table columns: score, kind, repo, package and version, the advisory id (or job name for job_failure), severity, confidence of the linked match, effective state, first seen (UTC). Each row links to `/f/{id}`. A filter form (GET) with the repo select, kind select and a suppressed checkbox. Empty list: "No open findings." (or "No findings." when suppressed is on).
3. `GET /f/{id}`: 404 for a non-numeric or unknown id. Shows: kind, score and rank reason; effective state with note, changed time and who; repo slug (linking to `/repos/{slug}`), package URL, version and manifest of the linked interval; for advisories: id, aliases, summary, details in a `<pre>` (plain text, never rendered as markdown or HTML), severity and CVSS, published and modified dates, KEV (with date) and EPSS (score and percentile), and an exposures table (version, manifest, exposed from, exposed to or "still exposed", fixed version, confidence and its reason; a note when `vulns.exposure_basis` makes the exposure start differ from the pin start: show both the pin start and the exposure start). A sentence states that exposure windows are estimates. All upstream text goes through `html/template` escaping; nothing uses `template.HTML`.
4. Forms on `/f/{id}`: Ack (with optional note), Snooze with a select of 1d, 7d, 30d and an optional note, Dismiss with an optional note, and Reopen (shown when the effective state is not open). Each is `POST /f/{id}/<action>` with `application/x-www-form-urlencoded` fields `note` and, for snooze, `for` (`1d`, `7d`, `30d`). The handler normalises `\r\n` and lone `\r` in the note to `\n` before calling the service (browsers send CRLF), passes `by` = `"web"`, and answers 303 to `/f/{id}` on success; 400 with the error page for an invalid transition, bad `for` or a note the model rejects; 404 for an unknown id. Bodies over 16 KiB are refused (`http.MaxBytesReader`).
5. Cross-origin POSTs are refused by the existing `http.CrossOriginProtection` (the routes sit behind it): `curl -X POST -H 'Origin: https://evil.example' -H 'Sec-Fetch-Site: cross-site' http://127.0.0.1:8080/f/1/ack` → 403 and the state unchanged. Add a test for it through `Server.Handler()`.
6. Every page keeps the security headers and the host check already in place. The layout nav is unchanged apart from styling.
7. `cmd/upw/serve.go` builds `findings.NewService(a.db, nil)` and passes it to `web.NewServer`.

## Tests to include

- Golden pages (go-test skill, `-update` flag): findings list with an advisory, a job_failure and a dismissed finding hidden; the same with `suppressed=1`; finding page for an advisory with two exposures (one open, one closed), a summary containing `<script>alert(1)</script>` rendered escaped, KEV and EPSS; finding page for a job_failure.
- `TestFindings_BadFilter` (unknown repo, bad kind → 400).
- `TestAck_PostRedirect`, `TestSnooze_Post` (`for=7d` → the fake service got 168h), `TestDismiss_Post` with a CRLF note (fake receives LF), `TestReopen_Post`, `TestPost_InvalidTransition` (fake returns an error wrapping `model.ErrInvalidTransition` → 400), `TestPost_NotFound` → 404, `TestPost_CrossOrigin` → 403 and the fake service not called.
- query: `TestFindings_PassesFilterAndNow`, `TestFindingDetail_NotFound`.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race ./internal/web/ ./internal/query/ ./cmd/upw/` | `ok` |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- `POST /f/{id}/label` and the eval set (M3), snooze until version change (M5), the "what was sent to the model" section (M4), `/coverage` (M3).

## Skills and docs

Load with the Skill tool: `web-ui`.
Read: PLAN.md section 6 (Web UI, Ack/snooze semantics); `docs/architecture.md` (HTTP routes, acknowledge sequence).

## Report

Write the full report to the report file named at the top, in the form given in your agent definition, then reply with only the short form. If a question's answer would change behaviour and nothing here or in the docs answers it, stop and report NEEDS_CONTEXT rather than guessing.

## Rulings


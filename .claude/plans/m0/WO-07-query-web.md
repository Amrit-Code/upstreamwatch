# WO-07: Read service and web server with /health and the findings page

Milestone: M0
Agent: go-mech   Model: opus
Depends on: WO-01, WO-02 (merged)
Report file: `.claude/plans/m0/WO-07-query-web.report.md`
Ticket: UPW-17 (the `internal/query` and `internal/web` half; `upw serve` wiring is WO-08)

## Objective

`internal/query` gains the first read service, and `internal/web` gains the HTTP server: `GET /health` as JSON, `GET /` as the findings page (empty in M0 unless a job has failed), the shared layout, one stylesheet, the security headers and graceful shutdown. Serves M0 checklist items 12, 13 and 14.

## Owned files

- `internal/query/` (all files except `interfaces.go`; keep `doc.go`)
- `internal/web/` (all files except `interfaces.go`; keep the package comment in `doc.go`), including `templates/`, `static/` and `testdata/`

Lead-owned, read-only for you (stop and report if one is wrong):

- `internal/query/interfaces.go`, `internal/web/interfaces.go`
- `internal/model/`
- `go.mod`, `go.sum`, `.go-arch-lint.yml`, `Makefile`, `scripts/`

## Interfaces

### Consumed

```go
// internal/query/interfaces.go
type RunStore interface {
	LatestRuns(ctx context.Context) ([]model.Run, error)
}
type FindingStore interface {
	ListOpenFindings(ctx context.Context) ([]model.Finding, error)
}

// internal/web/interfaces.go
type HealthReader interface {
	LastRuns(ctx context.Context) ([]model.Run, error)
}
type FindingsReader interface {
	OpenFindings(ctx context.Context) ([]model.Finding, error)
}
```

Plus `model.Run`, `model.Finding` and their constants from the committed `internal/model` files.

### Produced

```go
// internal/query
func NewService(runs RunStore, findings FindingStore, logger *slog.Logger) *Service
func (s *Service) LastRuns(ctx context.Context) ([]model.Run, error)
func (s *Service) OpenFindings(ctx context.Context) ([]model.Finding, error)

// internal/web
type Options struct {
	Listen  string // host:port, for example 127.0.0.1:8080
	BaseURL string // the configured http.base_url; may be empty
}
func NewServer(health HealthReader, findings FindingsReader, opts Options, logger *slog.Logger) (*Server, error)
func (s *Server) Handler() http.Handler
func (s *Server) Serve(ctx context.Context) error
```

`*query.Service` must satisfy both `web.HealthReader` and `web.FindingsReader`; `cmd/upw` passes it in (WO-08).

## Requirements

1. `query.Service.LastRuns` and `OpenFindings` return what the store returns, wrapping errors with what was being read. They add no logic.
2. `web.NewServer` parses every embedded template once and returns an error if one fails to parse. It does not listen.
3. `Handler` serves exactly these routes, registered with `net/http` method patterns:
   - `GET /{$}`: the findings page;
   - `GET /health`: JSON;
   - `GET /static/style.css`: the embedded stylesheet, `Content-Type: text/css; charset=utf-8`;
   - anything else: 404; a known path with another method: 405.
4. `GET /health` responds `200` with `Content-Type: application/json` and a body of exactly this shape:
   ```json
   {"ok":true,"last_runs":[{"job":"ingest","status":"succeeded","started_at":"2026-03-01T12:00:00Z","finished_at":"2026-03-01T12:00:05Z","error":""}]}
   ```
   `last_runs` is `[]`, never `null`, when there are no runs. `finished_at` is `null` for a run that is still running. Times are RFC 3339 UTC, whole seconds. If `LastRuns` returns an error the response is `503` with `{"ok":false,"last_runs":[]}` and the error is logged, not sent.
5. `GET /` responds `200` with the findings page rendered from the shared layout. With no findings it shows the text `No open findings.`. With findings it shows one table row per finding, in the order given: kind, rank reason, score (one decimal), first seen (UTC, `2006-01-02 15:04`), state. If `OpenFindings` returns an error the response is `500` with a generic message and the detail is only logged.
6. The layout has the page title, a header with the product name `upstreamwatch`, a nav containing only links to pages that exist (in M0: Findings), and a `<link>` to `/static/style.css`. No inline styles, no scripts, no external assets.
7. Every response carries `Content-Security-Policy: default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; frame-ancestors 'none'` and `X-Content-Type-Options: nosniff`, set by one middleware.
8. The mux is wrapped with `http.NewCrossOriginProtection()`. When `opts.BaseURL` is non-empty its origin is added with `AddTrustedOrigin`; a malformed `BaseURL` makes `NewServer` return an error.
9. Pages are executed into a buffer before anything is written, so a template failure yields a clean `500`.
10. `Serve` listens on `opts.Listen`, logs one `Info` line `http server listening` with `addr` set to the actual listening address, and serves until `ctx` is cancelled. It then calls `Shutdown` with a 10-second timeout and returns nil after a clean shutdown, or the listen, serve or shutdown error wrapped. The `http.Server` sets `ReadHeaderTimeout` 10 s, `ReadTimeout` 30 s, `WriteTimeout` 60 s and `IdleTimeout` 120 s.
11. `internal/web` imports only the standard library and `internal/model`. `internal/query` imports only the standard library and `internal/model`.
12. The stylesheet is one small hand-written file: readable type, a plain table, an empty-state style. Do not load a design skill for this work order.

## Tests to include

Hand-written fakes for the four consumed interfaces.

- `TestService_LastRuns` and `TestService_OpenFindings`, including error wrapping (`errors.Is` against the fake's error).
- An external test file asserting `var _ web.HealthReader = (*query.Service)(nil)` and the same for `web.FindingsReader`.
- `TestHealth_Shape`: no runs → body is exactly `{"ok":true,"last_runs":[]}` (after trimming the trailing newline); two runs, one still running → `finished_at` null for it; decoded with `encoding/json` and compared field by field.
- `TestHealth_StoreError`: 503, `ok` false, the fake's error text absent from the body.
- `TestFindingsPage_Golden`: empty state and a three-finding state compared with `testdata/findings_empty.golden` and `testdata/findings_list.golden` (`-update` flag per the go-test golden template). One finding's rank reason contains `<script>alert(1)</script>` and `</pre>` and must appear escaped.
- `TestFindingsPage_StoreError`: 500, generic body.
- `TestSecurityHeaders`: both headers on `/`, `/health`, `/static/style.css` and a 404.
- `TestRoutes_NotFoundAndMethod`: `GET /nope` → 404; `POST /health` with `Sec-Fetch-Site: same-origin` → 405; `POST /` with `Sec-Fetch-Site: cross-site` → 403.
- `TestNewServer_BadBaseURL`.
- `TestServe_ShutsDownOnCancel`: `Listen: "127.0.0.1:0"`; cancel the context; `Serve` returns nil within 2 s. Capture the logged `addr` to make one real request to `/health` before cancelling.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race ./internal/web/ ./internal/query/` | `ok` for both |
| `go list -f '{{join .Imports "\n"}}' ./internal/web ./internal/query \| grep upstreamwatch \| sort -u` | only `.../internal/model` |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- `upw serve`, reading config, opening the store, the listen-address default (WO-08).
- `/f/{id}`, ack, snooze and dismiss, filters, the suppressed toggle (M2); `/repos`, `/coverage`, `/eval`, `/settings` (later milestones).
- Finding ranking or state rules; the page shows what it is given.

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `web-ui`.
Read: `docs/architecture.md` ("HTTP routes", "Layers"); `.claude/skills/go-test/templates/golden-files.md`.

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

- 2026-11-19: `ok` in `/health` means the process is serving and the database answered. It does not turn false because a job's last run failed; `last_runs` carries that, and a failed job is a `job_failure` finding.

package ingest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// t0 is the fake clock's time, in a non-UTC zone so tests see the conversion.
var t0 = time.Date(2026, 3, 1, 13, 0, 0, 0, time.FixedZone("plus1", 3600))

type fakeGit struct {
	results map[string]model.SyncResult // keyed by slug
	errs    map[string]error            // keyed by slug
	onSync  func(repo model.TargetRepo) // called before returning, if set
	calls   []model.TargetRepo
}

func (g *fakeGit) Sync(ctx context.Context, repo model.TargetRepo) (model.SyncResult, error) {
	g.calls = append(g.calls, repo)
	if g.onSync != nil {
		g.onSync(repo)
	}
	if err := g.errs[repo.Slug]; err != nil {
		return model.SyncResult{}, err
	}
	return g.results[repo.Slug], nil
}

type upsertCall struct{ slug, url string }

type fakeRepos struct {
	ids        map[string]int64 // stored ID per slug
	upsertErrs map[string]error // keyed by slug
	recordErr  error
	// ctxAware makes RecordFetch fail with ctx.Err() on a done context, as a
	// real store would.
	ctxAware        bool
	upserts         []upsertCall
	fetches         []model.FetchResult // every RecordFetch call, failed or not
	recordDeadlines []bool              // whether each RecordFetch context had a deadline
}

func (r *fakeRepos) UpsertRepo(ctx context.Context, slug, url string) (model.TargetRepo, error) {
	r.upserts = append(r.upserts, upsertCall{slug, url})
	if err := r.upsertErrs[slug]; err != nil {
		return model.TargetRepo{}, err
	}
	return model.TargetRepo{ID: r.ids[slug], Slug: slug, URL: url, ClonePath: "/stored/" + slug + ".git"}, nil
}

func (r *fakeRepos) RecordFetch(ctx context.Context, f model.FetchResult) error {
	r.fetches = append(r.fetches, f)
	_, ok := ctx.Deadline()
	r.recordDeadlines = append(r.recordDeadlines, ok)
	if r.ctxAware {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return r.recordErr
}

var (
	alpha = model.TargetRepo{
		Slug:   "alpha",
		URL:    "https://git.example/example-org/alpha",
		Branch: "main",
		Token:  model.Secret("token-for-alpha-0001"),
	}
	beta = model.TargetRepo{
		Slug:             "beta",
		URL:              "https://git.example/example-org/beta",
		AllowPrivateHost: true,
	}
)

func newTestService(git GitSource, repos RepoStore, targets []model.TargetRepo, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return NewService(git, repos, Options{Targets: targets, Now: func() time.Time { return t0 }}, logger)
}

func newRepos() *fakeRepos {
	return &fakeRepos{ids: map[string]int64{"alpha": 11, "beta": 12}}
}

func wantStats(targets, fetched, cloned, failed int64) model.Stats {
	return model.Stats{"targets": targets, "fetched": fetched, "cloned": cloned, "failed": failed}
}

func TestName(t *testing.T) {
	t.Parallel()
	if got := newTestService(&fakeGit{}, newRepos(), nil, nil).Name(); got != "ingest" {
		t.Errorf("Name() = %q, want %q", got, "ingest")
	}
}

func TestRun_AllTargetsSucceed(t *testing.T) {
	t.Parallel()
	git := &fakeGit{results: map[string]model.SyncResult{
		"alpha": {Branch: "main", Head: "aaaa", ClonePath: "/repos/alpha.git", Cloned: true},
		"beta":  {Branch: "trunk", Head: "bbbb", ClonePath: "/repos/beta.git"},
	}}
	repos := newRepos()
	svc := newTestService(git, repos, []model.TargetRepo{alpha, beta}, nil)

	stats, err := svc.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if want := wantStats(2, 2, 1, 0); !reflect.DeepEqual(stats, want) {
		t.Errorf("Run() stats = %v, want %v", stats, want)
	}

	wantUpserts := []upsertCall{{"alpha", alpha.URL}, {"beta", beta.URL}}
	if !reflect.DeepEqual(repos.upserts, wantUpserts) {
		t.Errorf("upserts = %v, want %v", repos.upserts, wantUpserts)
	}

	wantSyncs := []model.TargetRepo{
		{ID: 11, Slug: "alpha", URL: alpha.URL, Branch: "main", Token: alpha.Token},
		{ID: 12, Slug: "beta", URL: beta.URL, AllowPrivateHost: true},
	}
	if !reflect.DeepEqual(git.calls, wantSyncs) {
		t.Errorf("Sync calls = %+v, want %+v", git.calls, wantSyncs)
	}

	at := t0.UTC()
	wantFetches := []model.FetchResult{
		{RepoID: 11, At: at, Branch: "main", ClonePath: "/repos/alpha.git"},
		{RepoID: 12, At: at, Branch: "trunk", ClonePath: "/repos/beta.git"},
	}
	if !reflect.DeepEqual(repos.fetches, wantFetches) {
		t.Errorf("RecordFetch calls = %+v, want %+v", repos.fetches, wantFetches)
	}
	if loc := repos.fetches[0].At.Location(); loc != time.UTC {
		t.Errorf("RecordFetch At location = %v, want UTC", loc)
	}
}

func TestRun_OneTargetFails(t *testing.T) {
	t.Parallel()
	syncErr := errors.New("fetch https://git.example: exit status 128")
	git := &fakeGit{
		results: map[string]model.SyncResult{"beta": {Branch: "trunk", ClonePath: "/repos/beta.git"}},
		errs:    map[string]error{"alpha": syncErr},
	}
	repos := newRepos()
	svc := newTestService(git, repos, []model.TargetRepo{alpha, beta}, nil)

	stats, err := svc.Run(t.Context(), model.Scope{})

	const wantErr = "ingest: 1 of 2 targets failed: alpha: fetch https://git.example: exit status 128"
	if err == nil || err.Error() != wantErr {
		t.Fatalf("Run() error = %v, want %q", err, wantErr)
	}
	if !errors.Is(err, syncErr) {
		t.Errorf("errors.Is(Run() error, syncErr) = false, want true")
	}
	if want := wantStats(2, 1, 0, 1); !reflect.DeepEqual(stats, want) {
		t.Errorf("Run() stats = %v, want %v", stats, want)
	}
	if len(git.calls) != 2 {
		t.Errorf("Sync calls = %d, want 2", len(git.calls))
	}
	wantFetches := []model.FetchResult{
		{RepoID: 11, At: t0.UTC(), Error: syncErr.Error()},
		{RepoID: 12, At: t0.UTC(), Branch: "trunk", ClonePath: "/repos/beta.git"},
	}
	if !reflect.DeepEqual(repos.fetches, wantFetches) {
		t.Errorf("RecordFetch calls = %+v, want %+v", repos.fetches, wantFetches)
	}
}

func TestRun_AllTargetsFailListsEachInOrder(t *testing.T) {
	t.Parallel()
	git := &fakeGit{errs: map[string]error{"alpha": errors.New("boom a"), "beta": errors.New("boom b")}}
	svc := newTestService(git, newRepos(), []model.TargetRepo{alpha, beta}, nil)

	stats, err := svc.Run(t.Context(), model.Scope{})

	const wantErr = "ingest: 2 of 2 targets failed: alpha: boom a; beta: boom b"
	if err == nil || err.Error() != wantErr {
		t.Fatalf("Run() error = %v, want %q", err, wantErr)
	}
	if want := wantStats(2, 0, 0, 2); !reflect.DeepEqual(stats, want) {
		t.Errorf("Run() stats = %v, want %v", stats, want)
	}
}

func TestRun_UpsertFails(t *testing.T) {
	t.Parallel()
	storeErr := errors.New("database is locked")
	git := &fakeGit{results: map[string]model.SyncResult{"beta": {Branch: "trunk"}}}
	repos := newRepos()
	repos.upsertErrs = map[string]error{"alpha": storeErr}
	svc := newTestService(git, repos, []model.TargetRepo{alpha, beta}, nil)

	stats, err := svc.Run(t.Context(), model.Scope{})

	const wantErr = "ingest: 1 of 2 targets failed: alpha: upsert repo: database is locked"
	if err == nil || err.Error() != wantErr {
		t.Fatalf("Run() error = %v, want %q", err, wantErr)
	}
	if !errors.Is(err, storeErr) {
		t.Errorf("errors.Is(Run() error, storeErr) = false, want true")
	}
	if want := wantStats(2, 1, 0, 1); !reflect.DeepEqual(stats, want) {
		t.Errorf("Run() stats = %v, want %v", stats, want)
	}
	if len(git.calls) != 1 || git.calls[0].Slug != "beta" {
		t.Errorf("Sync calls = %+v, want only beta", git.calls)
	}
	if len(repos.fetches) != 1 || repos.fetches[0].RepoID != 12 {
		t.Errorf("RecordFetch calls = %+v, want only beta's", repos.fetches)
	}
}

func TestRun_RecordFetchFails(t *testing.T) {
	t.Parallel()
	recordErr := errors.New("disk full")
	tests := []struct {
		name      string
		syncErr   error
		wantErr   string
		wantStats model.Stats
	}{
		{
			// The clone was made and fetched, so the target counts as
			// fetched and cloned as well as failed.
			name:      "after successful sync",
			wantErr:   "ingest: 1 of 1 targets failed: alpha: record fetch: disk full",
			wantStats: wantStats(1, 1, 1, 1),
		},
		{
			name:      "after failed sync",
			syncErr:   errors.New("refused"),
			wantErr:   "ingest: 1 of 1 targets failed: alpha: refused (record fetch: disk full)",
			wantStats: wantStats(1, 0, 0, 1),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			git := &fakeGit{
				results: map[string]model.SyncResult{"alpha": {Branch: "main", Cloned: true}},
				errs:    map[string]error{"alpha": tt.syncErr},
			}
			repos := newRepos()
			repos.recordErr = recordErr
			svc := newTestService(git, repos, []model.TargetRepo{alpha}, nil)

			stats, err := svc.Run(t.Context(), model.Scope{})

			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("Run() error = %v, want %q", err, tt.wantErr)
			}
			if !errors.Is(err, recordErr) {
				t.Errorf("errors.Is(Run() error, recordErr) = false, want true")
			}
			if !reflect.DeepEqual(stats, tt.wantStats) {
				t.Errorf("Run() stats = %v, want %v", stats, tt.wantStats)
			}
		})
	}
}

func TestRun_RedactsCredentialsInSyncError(t *testing.T) {
	t.Parallel()
	const (
		password = "hunter2-very-secret"
		pat      = "github_pat_11ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	)
	syncErr := errors.New("fetch https://deploy:" + password + "@git.example/example-org/alpha: server rejected " + pat)
	var buf bytes.Buffer
	git := &fakeGit{errs: map[string]error{"alpha": syncErr}}
	repos := newRepos()
	svc := newTestService(git, repos, []model.TargetRepo{alpha}, slog.New(slog.NewTextHandler(&buf, nil)))

	_, err := svc.Run(t.Context(), model.Scope{})
	if err == nil {
		t.Fatal("Run() error = nil, want the sync failure")
	}

	sinks := map[string]string{
		"log output":          buf.String(),
		"RecordFetch Error":   repos.fetches[0].Error,
		"returned error text": err.Error(),
	}
	for name, got := range sinks {
		for _, secret := range []string{password, pat} {
			if strings.Contains(got, secret) {
				t.Errorf("%s contains %q: %q", name, secret, got)
			}
		}
		if !strings.Contains(got, "[REDACTED]") {
			t.Errorf("%s = %q, want the redaction marker", name, got)
		}
	}
	const wantFetchErr = "fetch https://[REDACTED]@git.example/example-org/alpha: server rejected [REDACTED]"
	if got := repos.fetches[0].Error; got != wantFetchErr {
		t.Errorf("RecordFetch Error = %q, want %q", got, wantFetchErr)
	}
}

func TestRun_CancelledDuringSyncStillRecords(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	git := &fakeGit{
		errs:   map[string]error{"alpha": fmt.Errorf("fetch: %w", context.Canceled)},
		onSync: func(model.TargetRepo) { cancel() },
	}
	repos := newRepos()
	repos.ctxAware = true
	svc := newTestService(git, repos, []model.TargetRepo{alpha, beta}, nil)

	stats, err := svc.Run(ctx, model.Scope{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if !strings.HasPrefix(err.Error(), "ingest stopped") {
		t.Errorf("Run() error = %q, want the stop error", err)
	}
	wantFetches := []model.FetchResult{{RepoID: 11, At: t0.UTC(), Error: "fetch: context canceled"}}
	if !reflect.DeepEqual(repos.fetches, wantFetches) {
		t.Errorf("RecordFetch calls = %+v, want %+v", repos.fetches, wantFetches)
	}
	if want := []bool{true}; !reflect.DeepEqual(repos.recordDeadlines, want) {
		t.Errorf("RecordFetch context had deadline = %v, want %v", repos.recordDeadlines, want)
	}
	if len(git.calls) != 1 {
		t.Errorf("Sync calls = %d, want 1 (beta not attempted)", len(git.calls))
	}
	if want := wantStats(1, 0, 0, 1); !reflect.DeepEqual(stats, want) {
		t.Errorf("Run() stats = %v, want %v", stats, want)
	}
}

func TestRun_CancelledDuringLastTarget(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	git := &fakeGit{
		results: map[string]model.SyncResult{"alpha": {Branch: "main", ClonePath: "/repos/alpha.git", Cloned: true}},
		onSync:  func(model.TargetRepo) { cancel() },
	}
	repos := newRepos()
	repos.ctxAware = true
	svc := newTestService(git, repos, []model.TargetRepo{alpha}, nil)

	stats, err := svc.Run(ctx, model.Scope{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	wantFetches := []model.FetchResult{{RepoID: 11, At: t0.UTC(), Branch: "main", ClonePath: "/repos/alpha.git"}}
	if !reflect.DeepEqual(repos.fetches, wantFetches) {
		t.Errorf("RecordFetch calls = %+v, want %+v", repos.fetches, wantFetches)
	}
	if want := wantStats(1, 1, 1, 0); !reflect.DeepEqual(stats, want) {
		t.Errorf("Run() stats = %v, want %v", stats, want)
	}
}

func TestRun_ScopeOneRepo(t *testing.T) {
	t.Parallel()
	git := &fakeGit{results: map[string]model.SyncResult{"beta": {Branch: "trunk", ClonePath: "/repos/beta.git"}}}
	repos := newRepos()
	svc := newTestService(git, repos, []model.TargetRepo{alpha, beta}, nil)

	stats, err := svc.Run(t.Context(), model.Scope{RepoSlug: "beta"})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if want := wantStats(1, 1, 0, 0); !reflect.DeepEqual(stats, want) {
		t.Errorf("Run() stats = %v, want %v", stats, want)
	}
	if want := []upsertCall{{"beta", beta.URL}}; !reflect.DeepEqual(repos.upserts, want) {
		t.Errorf("upserts = %v, want %v", repos.upserts, want)
	}
	if len(git.calls) != 1 || git.calls[0].Slug != "beta" {
		t.Errorf("Sync calls = %+v, want only beta", git.calls)
	}
}

func TestRun_ScopeUnknownRepo(t *testing.T) {
	t.Parallel()
	git := &fakeGit{}
	repos := newRepos()
	svc := newTestService(git, repos, []model.TargetRepo{alpha, beta}, nil)

	stats, err := svc.Run(t.Context(), model.Scope{RepoSlug: "gamma"})
	if err == nil || !strings.Contains(err.Error(), `"gamma"`) {
		t.Fatalf("Run() error = %v, want an error naming \"gamma\"", err)
	}
	if want := wantStats(0, 0, 0, 0); !reflect.DeepEqual(stats, want) {
		t.Errorf("Run() stats = %v, want %v", stats, want)
	}
	if len(repos.upserts) != 0 || len(git.calls) != 0 || len(repos.fetches) != 0 {
		t.Errorf("work done for unknown scope: upserts %v, syncs %v, fetches %v", repos.upserts, git.calls, repos.fetches)
	}
}

func TestRun_NoTargets(t *testing.T) {
	t.Parallel()
	stats, err := newTestService(&fakeGit{}, newRepos(), nil, nil).Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if want := wantStats(0, 0, 0, 0); !reflect.DeepEqual(stats, want) {
		t.Errorf("Run() stats = %v, want %v", stats, want)
	}
}

func TestRun_ContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	git := &fakeGit{
		results: map[string]model.SyncResult{"alpha": {Branch: "main"}},
		onSync:  func(model.TargetRepo) { cancel() },
	}
	repos := newRepos()
	svc := newTestService(git, repos, []model.TargetRepo{alpha, beta}, nil)

	stats, err := svc.Run(ctx, model.Scope{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if len(git.calls) != 1 || git.calls[0].Slug != "alpha" {
		t.Errorf("Sync calls = %+v, want only alpha", git.calls)
	}
	if want := []upsertCall{{"alpha", alpha.URL}}; !reflect.DeepEqual(repos.upserts, want) {
		t.Errorf("upserts = %v, want %v", repos.upserts, want)
	}
	if want := wantStats(1, 1, 0, 0); !reflect.DeepEqual(stats, want) {
		t.Errorf("Run() stats = %v, want %v", stats, want)
	}
}

func TestRun_CancelledBeforeStart(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	git := &fakeGit{}
	repos := newRepos()

	_, err := newTestService(git, repos, []model.TargetRepo{alpha}, nil).Run(ctx, model.Scope{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if len(repos.upserts) != 0 || len(git.calls) != 0 {
		t.Errorf("work done after cancellation: upserts %v, syncs %v", repos.upserts, git.calls)
	}
}

func TestRun_TokenNotLogged(t *testing.T) {
	t.Parallel()
	token := "github_pat_" + strings.Repeat("Ab3", 10)
	target := alpha
	target.Token = model.Secret(token)

	tests := []struct {
		name    string
		handler func(w io.Writer) slog.Handler
	}{
		{"text", func(w io.Writer) slog.Handler {
			return slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug})
		}},
		{"json", func(w io.Writer) slog.Handler {
			return slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			git := &fakeGit{errs: map[string]error{"alpha": errors.New("fetch: authentication failed")}}
			svc := newTestService(git, newRepos(), []model.TargetRepo{target}, slog.New(tt.handler(&buf)))

			_, err := svc.Run(t.Context(), model.Scope{})
			if err == nil {
				t.Fatal("Run() error = nil, want the sync failure")
			}
			out := buf.String()
			if !strings.Contains(out, "alpha") {
				t.Errorf("log output = %q, want the slug", out)
			}
			if strings.Contains(out, token) {
				t.Errorf("log output contains the token: %q", out)
			}
			if strings.Contains(err.Error(), token) {
				t.Errorf("Run() error contains the token: %q", err)
			}
			if got := git.calls[0].Token.Reveal(); got != token {
				t.Errorf("Sync got token %q, want the configured one", got)
			}
		})
	}
}

func TestNewService_DefaultClock(t *testing.T) {
	t.Parallel()
	repos := newRepos()
	git := &fakeGit{results: map[string]model.SyncResult{"alpha": {Branch: "main"}}}
	svc := NewService(git, repos, Options{Targets: []model.TargetRepo{alpha}}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := svc.Run(t.Context(), model.Scope{}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if at := repos.fetches[0].At; at.IsZero() || at.Location() != time.UTC {
		t.Errorf("RecordFetch At = %v, want a non-zero time in UTC", at)
	}
}

func TestNewService_CopiesTargets(t *testing.T) {
	t.Parallel()
	targets := []model.TargetRepo{alpha}
	git := &fakeGit{}
	svc := newTestService(git, newRepos(), targets, nil)
	targets[0].Slug = "changed"

	if _, err := svc.Run(t.Context(), model.Scope{}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if len(git.calls) != 1 || git.calls[0].Slug != "alpha" {
		t.Errorf("Sync calls = %+v, want alpha", git.calls)
	}
}

package pipeline_test

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/pipeline"
	"github.com/Amrit-Code/upstreamwatch/internal/store"
)

// These tests run the real pipeline.Runner over a real SQLite file, opened and
// migrated as start-up does it. Each runs inside a synctest bubble so the
// Runner's clock (time.Now) is the bubble's fake clock: times are whole
// seconds, and advance moves the clock without waiting.

// openDB opens and migrates a fresh database file. Call it inside the bubble,
// so the pool's goroutines belong to the bubble and are closed at cleanup.
func openDB(t *testing.T) *store.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "upw.sqlite")
	db, err := store.Open(t.Context(), path, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("store.Open(%s) error = %v", path, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return db
}

// advance moves the bubble's fake clock forward by d. Inside synctest.Test a
// sleep returns as soon as every goroutine in the bubble is blocked, without
// real waiting.
func advance(d time.Duration) { time.Sleep(d) }

// funcJob is a pipeline.Job whose Run is fn.
type funcJob struct {
	name string
	fn   func(ctx context.Context, scope model.Scope) (model.Stats, error)
}

func (j *funcJob) Name() string { return j.name }

func (j *funcJob) Run(ctx context.Context, scope model.Scope) (model.Stats, error) {
	return j.fn(ctx, scope)
}

func newRunner(t *testing.T, db *store.DB, jobs ...pipeline.Job) *pipeline.Runner {
	t.Helper()
	r, err := pipeline.NewRunner(jobs, db, db, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	return r
}

func latestRun(t *testing.T, db *store.DB, job string) model.Run {
	t.Helper()
	runs, err := db.LatestRuns(t.Context())
	if err != nil {
		t.Fatalf("LatestRuns() error = %v", err)
	}
	for _, r := range runs {
		if r.Job == job {
			return r
		}
	}
	t.Fatalf("LatestRuns() = %+v, no run of job %q", runs, job)
	return model.Run{}
}

func openFindings(t *testing.T, db *store.DB) []model.Finding {
	t.Helper()
	got, err := db.ListOpenFindings(t.Context())
	if err != nil {
		t.Fatalf("ListOpenFindings() error = %v", err)
	}
	return got
}

func TestIntegration_SuccessfulRunIsRecorded(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		db := openDB(t)
		stats := model.Stats{"fetched": 3, "not_modified": 2}
		r := newRunner(t, db, &funcJob{name: "issues", fn: func(context.Context, model.Scope) (model.Stats, error) {
			advance(2 * time.Second)
			return stats, nil
		}})

		start := time.Now().UTC()
		got, err := r.Run(t.Context(), "issues", model.Scope{})
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}

		runs, err := db.LatestRuns(t.Context())
		if err != nil {
			t.Fatalf("LatestRuns() error = %v", err)
		}
		want := model.Run{
			ID:         got.ID,
			Job:        "issues",
			StartedAt:  start,
			FinishedAt: start.Add(2 * time.Second),
			Status:     model.RunSucceeded,
			Stats:      stats,
		}
		if got.ID == 0 {
			t.Errorf("Run() returned run ID 0, want the stored ID")
		}
		if len(runs) != 1 || !reflect.DeepEqual(runs[0], want) {
			t.Errorf("LatestRuns() = %+v, want [%+v]", runs, want)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Run() = %+v, want the stored run %+v", got, want)
		}
		if f := openFindings(t, db); f != nil {
			t.Errorf("ListOpenFindings() = %+v, want none", f)
		}
	})
}

func TestIntegration_FailedRunRaisesJobFailure(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		db := openDB(t)
		jobErr := errors.New("poll example-org/example-server issues: 502 bad gateway")
		r := newRunner(t, db, &funcJob{name: "issues", fn: func(context.Context, model.Scope) (model.Stats, error) {
			return model.Stats{"fetched": 1}, jobErr
		}})

		now := time.Now().UTC()
		got, err := r.Run(t.Context(), "issues", model.Scope{})
		if !errors.Is(err, jobErr) {
			t.Fatalf("Run() error = %v, want errors.Is %v", err, jobErr)
		}

		run := latestRun(t, db, "issues")
		wantRun := model.Run{
			ID: got.ID, Job: "issues", StartedAt: now, FinishedAt: now,
			Status: model.RunFailed, Stats: model.Stats{"fetched": 1}, Error: jobErr.Error(),
		}
		if !reflect.DeepEqual(run, wantRun) {
			t.Errorf("latest run = %+v, want %+v", run, wantRun)
		}

		findings := openFindings(t, db)
		if len(findings) != 1 {
			t.Fatalf("ListOpenFindings() = %+v, want exactly one finding", findings)
		}
		f := findings[0]
		if f.Kind != model.KindJobFailure {
			t.Errorf("finding Kind = %q, want %q", f.Kind, model.KindJobFailure)
		}
		if f.Fingerprint != "job_failure|issues" {
			t.Errorf("finding Fingerprint = %q, want %q", f.Fingerprint, "job_failure|issues")
		}
		if f.Score != 50 {
			t.Errorf("finding Score = %v, want 50", f.Score)
		}
		if f.State.State != model.StateOpen {
			t.Errorf("finding State = %q, want %q", f.State.State, model.StateOpen)
		}
		if !strings.Contains(f.RankReason, jobErr.Error()) {
			t.Errorf("finding RankReason = %q, want it to contain %q", f.RankReason, jobErr.Error())
		}
		if !f.FirstSeenAt.Equal(now) || !f.LastChangedAt.Equal(now) || !f.ResolvedAt.IsZero() {
			t.Errorf("finding times first=%v last=%v resolved=%v, want %v, %v, zero", f.FirstSeenAt, f.LastChangedAt, f.ResolvedAt, now, now)
		}
		if f.RepoID != 0 || f.PackageID != 0 || f.DepVersionID != 0 || f.AdvisoryID != "" || f.IssueID != 0 || f.VerdictID != 0 {
			t.Errorf("finding refs = %+v, want all zero for a job_failure", f)
		}
	})
}

func TestIntegration_RepeatedFailureUpdatesOneFinding(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		db := openDB(t)
		errs := []error{errors.New("first failure: timeout"), errors.New("second failure: 503")}
		var calls atomic.Int64
		r := newRunner(t, db, &funcJob{name: "vulns", fn: func(context.Context, model.Scope) (model.Stats, error) {
			return nil, errs[calls.Add(1)-1]
		}})

		first := time.Now().UTC()
		if _, err := r.Run(t.Context(), "vulns", model.Scope{}); !errors.Is(err, errs[0]) {
			t.Fatalf("first Run() error = %v, want %v", err, errs[0])
		}
		before := openFindings(t, db)
		if len(before) != 1 {
			t.Fatalf("after first failure ListOpenFindings() = %+v, want one", before)
		}

		advance(time.Hour)
		second := time.Now().UTC()
		if _, err := r.Run(t.Context(), "vulns", model.Scope{}); !errors.Is(err, errs[1]) {
			t.Fatalf("second Run() error = %v, want %v", err, errs[1])
		}

		after := openFindings(t, db)
		if len(after) != 1 {
			t.Fatalf("after second failure ListOpenFindings() = %+v, want still one", after)
		}
		f := after[0]
		if f.ID != before[0].ID {
			t.Errorf("finding ID = %d, want the same finding %d", f.ID, before[0].ID)
		}
		if !f.FirstSeenAt.Equal(first) {
			t.Errorf("FirstSeenAt = %v, want unchanged %v", f.FirstSeenAt, first)
		}
		if !f.LastChangedAt.Equal(second) || !f.LastChangedAt.After(before[0].LastChangedAt) {
			t.Errorf("LastChangedAt = %v, want advanced to %v (was %v)", f.LastChangedAt, second, before[0].LastChangedAt)
		}
		if !strings.Contains(f.RankReason, errs[1].Error()) || strings.Contains(f.RankReason, errs[0].Error()) {
			t.Errorf("RankReason = %q, want it from the second error %q only", f.RankReason, errs[1].Error())
		}
	})
}

func TestIntegration_SuccessResolvesFailure(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		db := openDB(t)
		results := []error{errors.New("boom 1"), nil, errors.New("boom 2")}
		var calls atomic.Int64
		r := newRunner(t, db, &funcJob{name: "ingest", fn: func(context.Context, model.Scope) (model.Stats, error) {
			return nil, results[calls.Add(1)-1]
		}})

		if _, err := r.Run(t.Context(), "ingest", model.Scope{}); err == nil {
			t.Fatal("first Run() error = nil, want the job failure")
		}
		raised := openFindings(t, db)
		if len(raised) != 1 {
			t.Fatalf("after failure ListOpenFindings() = %+v, want one", raised)
		}

		advance(time.Minute)
		if _, err := r.Run(t.Context(), "ingest", model.Scope{}); err != nil {
			t.Fatalf("second Run() error = %v, want nil", err)
		}
		if f := openFindings(t, db); f != nil {
			t.Errorf("after success ListOpenFindings() = %+v, want none", f)
		}
		if run := latestRun(t, db, "ingest"); run.Status != model.RunSucceeded {
			t.Errorf("latest run status = %q, want %q", run.Status, model.RunSucceeded)
		}

		advance(time.Minute)
		if _, err := r.Run(t.Context(), "ingest", model.Scope{}); err == nil {
			t.Fatal("third Run() error = nil, want the job failure")
		}
		again := openFindings(t, db)
		if len(again) != 1 {
			t.Fatalf("after second failure ListOpenFindings() = %+v, want one", again)
		}
		if again[0].ID != raised[0].ID {
			t.Errorf("reopened finding ID = %d, want the same finding %d", again[0].ID, raised[0].ID)
		}
		if !again[0].ResolvedAt.IsZero() || again[0].State.State != model.StateOpen {
			t.Errorf("reopened finding resolved=%v state=%q, want unresolved and open", again[0].ResolvedAt, again[0].State.State)
		}
		if !strings.Contains(again[0].RankReason, "boom 2") {
			t.Errorf("reopened finding RankReason = %q, want the latest error", again[0].RankReason)
		}
	})
}

func TestIntegration_PanickingJob(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		db := openDB(t)
		var calls atomic.Int64
		r := newRunner(t, db, &funcJob{name: "timeline", fn: func(context.Context, model.Scope) (model.Stats, error) {
			if calls.Add(1) == 1 {
				panic("index out of range")
			}
			return model.Stats{"commits": 4}, nil
		}})

		_, err := r.Run(t.Context(), "timeline", model.Scope{})
		if err == nil {
			t.Fatal("Run(panicking job) error = nil, want an error")
		}
		run := latestRun(t, db, "timeline")
		if run.Status != model.RunFailed || !strings.HasPrefix(run.Error, "panic: ") || run.FinishedAt.IsZero() {
			t.Errorf("latest run = %+v, want failed, finished, error starting %q", run, "panic: ")
		}
		findings := openFindings(t, db)
		if len(findings) != 1 || findings[0].Fingerprint != "job_failure|timeline" ||
			!strings.Contains(findings[0].RankReason, "panic: index out of range") {
			t.Fatalf("ListOpenFindings() = %+v, want one job_failure for the panic", findings)
		}

		// The Runner is still usable: the lock was released and the next run
		// is recorded and clears the finding.
		advance(time.Second)
		got, err := r.Run(t.Context(), "timeline", model.Scope{})
		if err != nil {
			t.Fatalf("Run() after panic error = %v, want nil", err)
		}
		if got.ID == run.ID {
			t.Errorf("Run() after panic reused run ID %d, want a new run", got.ID)
		}
		if run := latestRun(t, db, "timeline"); run.ID != got.ID || run.Status != model.RunSucceeded {
			t.Errorf("latest run after recovery = %+v, want run %d succeeded", run, got.ID)
		}
		if f := openFindings(t, db); f != nil {
			t.Errorf("ListOpenFindings() after recovery = %+v, want none", f)
		}
	})
}

func TestIntegration_CancelledRunIsStillRecorded(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		db := openDB(t)
		started := make(chan struct{})
		r := newRunner(t, db, &funcJob{name: "issues", fn: func(ctx context.Context, _ model.Scope) (model.Stats, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		}})

		ctx, cancel := context.WithCancel(t.Context())
		type result struct {
			run model.Run
			err error
		}
		done := make(chan result, 1)
		go func() {
			run, err := r.Run(ctx, "issues", model.Scope{})
			done <- result{run, err}
		}()
		<-started
		advance(3 * time.Second)
		cancelledAt := time.Now().UTC()
		cancel()
		res := <-done

		if !errors.Is(res.err, context.Canceled) {
			t.Errorf("Run() error = %v, want errors.Is context.Canceled", res.err)
		}
		run := latestRun(t, db, "issues")
		if run.ID != res.run.ID {
			t.Errorf("latest run ID = %d, want %d", run.ID, res.run.ID)
		}
		if run.Status != model.RunFailed {
			t.Errorf("latest run status = %q, want %q", run.Status, model.RunFailed)
		}
		if !run.FinishedAt.Equal(cancelledAt) {
			t.Errorf("latest run FinishedAt = %v, want %v", run.FinishedAt, cancelledAt)
		}
		if run.Error != context.Canceled.Error() {
			t.Errorf("latest run Error = %q, want %q", run.Error, context.Canceled.Error())
		}
		if f := openFindings(t, db); len(f) != 1 || f[0].Fingerprint != "job_failure|issues" {
			t.Errorf("ListOpenFindings() = %+v, want one job_failure for the cancelled run", f)
		}
	})
}

func TestIntegration_TwoJobsLatestRuns(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		db := openDB(t)
		// zeta is registered first, so the order of the result comes from the
		// job name, not from registration or from which job ran last.
		outcomes := map[string][]error{
			"zeta":  {errors.New("zeta 1"), nil, errors.New("zeta 3")},
			"alpha": {nil, errors.New("alpha 2"), nil},
		}
		calls := map[string]int{}
		job := func(name string) pipeline.Job {
			return &funcJob{name: name, fn: func(context.Context, model.Scope) (model.Stats, error) {
				err := outcomes[name][calls[name]]
				calls[name]++
				return model.Stats{"call": int64(calls[name])}, err
			}}
		}
		r := newRunner(t, db, job("zeta"), job("alpha"))

		last := map[string]model.Run{}
		for _, name := range []string{"zeta", "alpha", "alpha", "zeta", "zeta", "alpha"} {
			advance(time.Minute)
			run, _ := r.Run(t.Context(), name, model.Scope{}) // failures are expected here
			last[name] = run
		}

		got, err := db.LatestRuns(t.Context())
		if err != nil {
			t.Fatalf("LatestRuns() error = %v", err)
		}
		want := []model.Run{last["alpha"], last["zeta"]}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("LatestRuns() =\n%+v\nwant\n%+v", got, want)
		}
		if len(got) == 2 {
			if got[0].Status != model.RunSucceeded || got[0].Stats["call"] != 3 {
				t.Errorf("alpha latest = %+v, want its third run, succeeded", got[0])
			}
			if got[1].Status != model.RunFailed || got[1].Error != "zeta 3" {
				t.Errorf("zeta latest = %+v, want its third run, failed with %q", got[1], "zeta 3")
			}
		}
	})
}

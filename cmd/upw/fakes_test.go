package main

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/pipeline"
)

// fakeJob is a pipeline job that records the scope of every run and returns
// what run returns, or nothing when run is nil.
type fakeJob struct {
	name   string
	run    func(ctx context.Context, scope model.Scope) (model.Stats, error)
	mu     sync.Mutex
	scopes []model.Scope
}

func (j *fakeJob) Name() string { return j.name }

func (j *fakeJob) Run(ctx context.Context, scope model.Scope) (model.Stats, error) {
	j.mu.Lock()
	j.scopes = append(j.scopes, scope)
	j.mu.Unlock()
	if j.run == nil {
		return nil, nil
	}
	return j.run(ctx, scope)
}

// runScopes returns the scopes the job has been run with.
func (j *fakeJob) runScopes() []model.Scope {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]model.Scope(nil), j.scopes...)
}

// fakeRunStore hands out run IDs and stores nothing.
type fakeRunStore struct {
	mu     sync.Mutex
	nextID int64
}

func (s *fakeRunStore) StartRun(_ context.Context, job string, startedAt time.Time) (model.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	return model.Run{ID: s.nextID, Job: job, StartedAt: startedAt, Status: model.RunRunning}, nil
}

func (s *fakeRunStore) FinishRun(context.Context, model.Run) error { return nil }

// fakeFindingStore accepts every finding and stores nothing.
type fakeFindingStore struct{}

func (fakeFindingStore) UpsertFinding(_ context.Context, f model.Finding) (model.Finding, error) {
	return f, nil
}

func (fakeFindingStore) ResolveFinding(context.Context, string, time.Time) error { return nil }

// newFakeRunner returns a Runner over jobs with stores that keep nothing.
func newFakeRunner(t *testing.T, jobs ...pipeline.Job) *pipeline.Runner {
	t.Helper()
	r, err := pipeline.NewRunner(jobs, &fakeRunStore{}, fakeFindingStore{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	return r
}

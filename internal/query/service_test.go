package query

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

type fakeRunStore struct {
	runs  []model.Run
	err   error
	calls int
}

func (f *fakeRunStore) LatestRuns(ctx context.Context) ([]model.Run, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.runs, nil
}

type fakeFindingStore struct {
	findings []model.Finding
	err      error
	calls    int
}

func (f *fakeFindingStore) ListOpenFindings(ctx context.Context) ([]model.Finding, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.findings, nil
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

var t0 = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func TestService_LastRuns(t *testing.T) {
	t.Parallel()
	errStore := errors.New("database is closed")
	twoRuns := []model.Run{
		{ID: 1, Job: "ingest", StartedAt: t0, FinishedAt: t0.Add(5 * time.Second), Status: model.RunSucceeded},
		{ID: 2, Job: "vulns", StartedAt: t0, Status: model.RunRunning},
	}
	tests := []struct {
		name    string
		store   *fakeRunStore
		want    []model.Run
		wantErr error
	}{
		{name: "two runs", store: &fakeRunStore{runs: twoRuns}, want: twoRuns},
		{name: "no runs", store: &fakeRunStore{}, want: nil},
		{name: "store error", store: &fakeRunStore{err: errStore}, wantErr: errStore},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := NewService(tt.store, &fakeFindingStore{}, &fakeTimelineStore{}, discardLogger())
			got, err := s.LastRuns(t.Context())
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("LastRuns() error = %v, want wrapping %v", err, tt.wantErr)
				}
				if err.Error() == tt.wantErr.Error() {
					t.Errorf("LastRuns() error = %q, want context added", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("LastRuns() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("LastRuns() = %v, want %v", got, tt.want)
			}
			if tt.store.calls != 1 {
				t.Errorf("LatestRuns calls = %d, want 1", tt.store.calls)
			}
		})
	}
}

func TestService_OpenFindings(t *testing.T) {
	t.Parallel()
	errStore := errors.New("database is closed")
	findings := []model.Finding{
		{ID: 3, Kind: model.KindJobFailure, Score: 9.5, RankReason: "job ingest failed", FirstSeenAt: t0,
			State: model.FindingState{State: model.StateOpen}},
		{ID: 1, Kind: model.KindAdvisory, Score: 7.25, RankReason: "advisory affects pinned version", FirstSeenAt: t0,
			State: model.FindingState{State: model.StateAcked}},
	}
	tests := []struct {
		name    string
		store   *fakeFindingStore
		want    []model.Finding
		wantErr error
	}{
		{name: "two findings in store order", store: &fakeFindingStore{findings: findings}, want: findings},
		{name: "no findings", store: &fakeFindingStore{}, want: nil},
		{name: "store error", store: &fakeFindingStore{err: errStore}, wantErr: errStore},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := NewService(&fakeRunStore{}, tt.store, &fakeTimelineStore{}, discardLogger())
			got, err := s.OpenFindings(t.Context())
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("OpenFindings() error = %v, want wrapping %v", err, tt.wantErr)
				}
				if err.Error() == tt.wantErr.Error() {
					t.Errorf("OpenFindings() error = %q, want context added", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("OpenFindings() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("OpenFindings() = %v, want %v", got, tt.want)
			}
			if tt.store.calls != 1 {
				t.Errorf("ListOpenFindings calls = %d, want 1", tt.store.calls)
			}
		})
	}
}

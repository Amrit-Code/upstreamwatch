package query

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// fakeFindingStore lists canned findings and serves details by ID, recording
// the filter it was given and the IDs whose detail was read.
type fakeFindingStore struct {
	findings  []model.Finding
	details   map[int64]model.FindingDetail
	listErr   error
	detailErr error

	gotFilters   []model.FindingFilter
	gotDetailIDs []int64
}

func (f *fakeFindingStore) ListFindings(ctx context.Context, flt model.FindingFilter) ([]model.Finding, error) {
	f.gotFilters = append(f.gotFilters, flt)
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.findings, nil
}

func (f *fakeFindingStore) GetFindingDetail(ctx context.Context, id int64) (model.FindingDetail, error) {
	f.gotDetailIDs = append(f.gotDetailIDs, id)
	if f.detailErr != nil {
		return model.FindingDetail{}, f.detailErr
	}
	d, ok := f.details[id]
	if !ok {
		return model.FindingDetail{}, fmt.Errorf("finding %d: %w", id, model.ErrNotFound)
	}
	return d, nil
}

// twoFindings returns findings 3 and 1, in that (score) order, and their
// details.
func twoFindings() ([]model.Finding, map[int64]model.FindingDetail) {
	f3 := model.Finding{ID: 3, Kind: model.KindJobFailure, Score: 50, RankReason: "job ingest failed",
		Fingerprint: "job_failure|ingest", FirstSeenAt: t0, State: model.FindingState{State: model.StateOpen}}
	f1 := model.Finding{ID: 1, Kind: model.KindAdvisory, RepoID: 2, AdvisoryID: "GHSA-aaaa-bbbb-cccc", Score: 7.25,
		FirstSeenAt: t0, State: model.FindingState{State: model.StateAcked}}
	details := map[int64]model.FindingDetail{
		3: {Finding: f3},
		1: {Finding: f1, RepoSlug: "example-server", Advisory: model.Advisory{ID: "GHSA-aaaa-bbbb-cccc"}},
	}
	return []model.Finding{f3, f1}, details
}

func TestFindings_PassesFilterAndNow(t *testing.T) {
	t.Parallel()
	clock := time.Date(2026, 3, 2, 8, 30, 0, 0, time.FixedZone("X", 3600))
	given := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		filter  model.FindingFilter
		wantNow time.Time
	}{
		{name: "zero now set from clock", filter: model.FindingFilter{RepoID: 2, Kind: model.KindAdvisory, Suppressed: true},
			wantNow: clock.UTC()},
		{name: "given now kept", filter: model.FindingFilter{Kind: model.KindJobFailure, Now: given}, wantNow: given},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			findings, details := twoFindings()
			store := &fakeFindingStore{findings: findings, details: details}
			s := NewService(&fakeRunStore{}, store, &fakeTimelineStore{}, discardLogger())
			s.now = func() time.Time { return clock }

			got, err := s.Findings(t.Context(), tt.filter)
			if err != nil {
				t.Fatalf("Findings() error = %v", err)
			}
			want := tt.filter
			want.Now = tt.wantNow
			if len(store.gotFilters) != 1 || store.gotFilters[0] != want {
				t.Errorf("ListFindings filters = %+v, want [%+v]", store.gotFilters, want)
			}
			if !store.gotFilters[0].Now.Equal(tt.wantNow) || store.gotFilters[0].Now.Location() != time.UTC {
				t.Errorf("ListFindings Now = %v, want %v in UTC", store.gotFilters[0].Now, tt.wantNow)
			}
			wantDetails := []model.FindingDetail{details[3], details[1]}
			if !reflect.DeepEqual(got, wantDetails) {
				t.Errorf("Findings() =\n%+v\nwant\n%+v", got, wantDetails)
			}
			if wantIDs := []int64{3, 1}; !reflect.DeepEqual(store.gotDetailIDs, wantIDs) {
				t.Errorf("GetFindingDetail IDs = %v, want %v", store.gotDetailIDs, wantIDs)
			}
		})
	}
}

func TestFindings_Empty(t *testing.T) {
	t.Parallel()
	store := &fakeFindingStore{}
	s := NewService(&fakeRunStore{}, store, &fakeTimelineStore{}, discardLogger())
	got, err := s.Findings(t.Context(), model.FindingFilter{})
	if err != nil || got != nil {
		t.Errorf("Findings() = %+v, %v; want nil, nil", got, err)
	}
	if len(store.gotDetailIDs) != 0 {
		t.Errorf("GetFindingDetail IDs = %v, want none", store.gotDetailIDs)
	}
}

func TestFindings_StoreErrors(t *testing.T) {
	t.Parallel()
	errStore := errors.New("database is closed")
	tests := []struct {
		name  string
		store func() *fakeFindingStore
	}{
		{name: "list fails", store: func() *fakeFindingStore { return &fakeFindingStore{listErr: errStore} }},
		{name: "detail fails", store: func() *fakeFindingStore {
			findings, details := twoFindings()
			return &fakeFindingStore{findings: findings, details: details, detailErr: errStore}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := NewService(&fakeRunStore{}, tt.store(), &fakeTimelineStore{}, discardLogger())
			got, err := s.Findings(t.Context(), model.FindingFilter{})
			if !errors.Is(err, errStore) {
				t.Fatalf("Findings() error = %v, want wrapping %v", err, errStore)
			}
			if err.Error() == errStore.Error() {
				t.Errorf("Findings() error = %q, want context added", err)
			}
			if got != nil {
				t.Errorf("Findings() = %+v, want nil on error", got)
			}
		})
	}
}

func TestFindingDetail_Found(t *testing.T) {
	t.Parallel()
	findings, details := twoFindings()
	store := &fakeFindingStore{findings: findings, details: details}
	s := NewService(&fakeRunStore{}, store, &fakeTimelineStore{}, discardLogger())
	got, err := s.FindingDetail(t.Context(), 1)
	if err != nil {
		t.Fatalf("FindingDetail(1) error = %v", err)
	}
	if !reflect.DeepEqual(got, details[1]) {
		t.Errorf("FindingDetail(1) = %+v, want %+v", got, details[1])
	}
}

func TestFindingDetail_NotFound(t *testing.T) {
	t.Parallel()
	errStore := errors.New("database is closed")
	tests := []struct {
		name    string
		store   *fakeFindingStore
		wantErr error
	}{
		{name: "unknown id", store: &fakeFindingStore{}, wantErr: model.ErrNotFound},
		{name: "store error", store: &fakeFindingStore{detailErr: errStore}, wantErr: errStore},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := NewService(&fakeRunStore{}, tt.store, &fakeTimelineStore{}, discardLogger())
			got, err := s.FindingDetail(t.Context(), 99)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("FindingDetail(99) error = %v, want wrapping %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, model.FindingDetail{}) {
				t.Errorf("FindingDetail(99) = %+v, want zero on error", got)
			}
		})
	}
}

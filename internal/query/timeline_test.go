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

// fakeTimelineStore serves repos by slug and intervals by repo ID.
type fakeTimelineStore struct {
	repos     map[string]model.TargetRepo
	intervals map[int64][]model.DepInterval
	list      []model.TargetRepo
	getErr    error
	listErr   error
	reposErr  error
	// gotRepoIDs records the repo IDs ListIntervals was called with.
	gotRepoIDs []int64
}

func (f *fakeTimelineStore) GetRepo(ctx context.Context, slug string) (model.TargetRepo, error) {
	if f.getErr != nil {
		return model.TargetRepo{}, f.getErr
	}
	r, ok := f.repos[slug]
	if !ok {
		return model.TargetRepo{}, fmt.Errorf("repo %q: %w", slug, model.ErrNotFound)
	}
	return r, nil
}

func (f *fakeTimelineStore) ListRepos(ctx context.Context) ([]model.TargetRepo, error) {
	if f.reposErr != nil {
		return nil, f.reposErr
	}
	return f.list, nil
}

func (f *fakeTimelineStore) ListIntervals(ctx context.Context, repoID int64) ([]model.DepInterval, error) {
	f.gotRepoIDs = append(f.gotRepoIDs, repoID)
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.intervals[repoID], nil
}

var (
	pkgGin   = model.Package{PURL: "pkg:golang/github.com/gin-gonic/gin", Ecosystem: "golang", Name: "github.com/gin-gonic/gin"}
	pkgToml  = model.Package{PURL: "pkg:golang/github.com/BurntSushi/toml", Ecosystem: "golang", Name: "github.com/BurntSushi/toml"}
	pkgAlpin = model.Package{PURL: "pkg:docker/alpine", Ecosystem: "docker", Name: "alpine"}

	// ivGinOld covers days 0 to 10, ivGinNew is open from day 10, ivToml
	// covers days 5 to 20, ivAlpine is open from day 0.
	ivGinOld = model.DepInterval{ID: 1, ManifestPath: "go.mod", Package: pkgGin, Version: "v1.9.0", Direct: model.DirectYes,
		FromSHA: "a1", From: day(0), ToSHA: "b2", To: day(10)}
	ivGinNew = model.DepInterval{ID: 2, ManifestPath: "go.mod", Package: pkgGin, Version: "v1.10.0", Direct: model.DirectYes,
		FromSHA: "b2", From: day(10)}
	ivToml = model.DepInterval{ID: 3, ManifestPath: "go.mod", Package: pkgToml, Version: "v1.4.0", Direct: model.DirectNo,
		FromSHA: "c3", From: day(5), ToSHA: "d4", To: day(20)}
	ivAlpine = model.DepInterval{ID: 4, ManifestPath: "deploy/Dockerfile", Package: pkgAlpin, Version: "3.22",
		FromSHA: "a1", From: day(0)}

	storeOrder = []model.DepInterval{ivAlpine, ivToml, ivGinOld, ivGinNew}
	repoServer = model.TargetRepo{ID: 7, Slug: "server", URL: "https://github.com/example-org/example-server"}
)

func day(n int) time.Time { return t0.Add(time.Duration(n) * 24 * time.Hour) }

func newTimelineFake() *fakeTimelineStore {
	return &fakeTimelineStore{
		repos:     map[string]model.TargetRepo{"server": repoServer},
		intervals: map[int64][]model.DepInterval{7: storeOrder},
	}
}

func TestService_Timeline(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		filter TimelineFilter
		want   []model.DepInterval
	}{
		{name: "no filter keeps store order", filter: TimelineFilter{}, want: storeOrder},
		{name: "package exact", filter: TimelineFilter{Package: pkgGin.PURL}, want: []model.DepInterval{ivGinOld, ivGinNew}},
		{name: "package is not a prefix match", filter: TimelineFilter{Package: "pkg:golang/github.com/gin-gonic"}, want: nil},
		{name: "package is case sensitive", filter: TimelineFilter{Package: "pkg:golang/github.com/burntsushi/toml"}, want: nil},
		{name: "contains ignores case", filter: TimelineFilter{Contains: "BURNTsushi"}, want: []model.DepInterval{ivToml}},
		{name: "contains matches several", filter: TimelineFilter{Contains: "pkg:golang/"}, want: []model.DepInterval{ivToml, ivGinOld, ivGinNew}},
		{name: "on at an opening instant", filter: TimelineFilter{On: day(10)}, want: []model.DepInterval{ivAlpine, ivToml, ivGinNew}},
		{name: "on before every interval", filter: TimelineFilter{On: day(-1)}, want: nil},
		{name: "on after closed intervals", filter: TimelineFilter{On: day(30)}, want: []model.DepInterval{ivAlpine, ivGinNew}},
		{name: "package and on", filter: TimelineFilter{Package: pkgGin.PURL, On: day(3)}, want: []model.DepInterval{ivGinOld}},
		{name: "contains and on", filter: TimelineFilter{Contains: "golang", On: day(15)}, want: []model.DepInterval{ivToml, ivGinNew}},
		{name: "all three", filter: TimelineFilter{Package: pkgGin.PURL, Contains: "GIN", On: day(12)}, want: []model.DepInterval{ivGinNew}},
		{name: "all three disagree", filter: TimelineFilter{Package: pkgGin.PURL, Contains: "toml", On: day(12)}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := newTimelineFake()
			s := NewService(&fakeRunStore{}, &fakeFindingStore{}, fake, discardLogger())
			repo, got, err := s.Timeline(t.Context(), "server", tt.filter)
			if err != nil {
				t.Fatalf("Timeline(server, %+v) error = %v", tt.filter, err)
			}
			if repo != repoServer {
				t.Errorf("Timeline(server, %+v) repo = %+v, want %+v", tt.filter, repo, repoServer)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Timeline(server, %+v) intervals =\n%+v\nwant\n%+v", tt.filter, got, tt.want)
			}
			if want := []int64{7}; !reflect.DeepEqual(fake.gotRepoIDs, want) {
				t.Errorf("ListIntervals repo IDs = %v, want %v", fake.gotRepoIDs, want)
			}
		})
	}
}

func TestService_Timeline_UnknownSlug(t *testing.T) {
	t.Parallel()
	fake := newTimelineFake()
	s := NewService(&fakeRunStore{}, &fakeFindingStore{}, fake, discardLogger())
	repo, got, err := s.Timeline(t.Context(), "missing", TimelineFilter{})
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("Timeline(missing) error = %v, want wrapping model.ErrNotFound", err)
	}
	if repo != (model.TargetRepo{}) || got != nil {
		t.Errorf("Timeline(missing) = %+v, %+v; want zero values", repo, got)
	}
	if len(fake.gotRepoIDs) != 0 {
		t.Errorf("ListIntervals called %v times, want none", fake.gotRepoIDs)
	}
}

func TestService_Timeline_StoreErrors(t *testing.T) {
	t.Parallel()
	errStore := errors.New("database is closed")
	tests := []struct {
		name string
		fake *fakeTimelineStore
	}{
		{name: "get repo fails", fake: &fakeTimelineStore{getErr: errStore}},
		{name: "list intervals fails", fake: &fakeTimelineStore{repos: map[string]model.TargetRepo{"server": repoServer}, listErr: errStore}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := NewService(&fakeRunStore{}, &fakeFindingStore{}, tt.fake, discardLogger())
			repo, got, err := s.Timeline(t.Context(), "server", TimelineFilter{})
			if !errors.Is(err, errStore) {
				t.Fatalf("Timeline() error = %v, want wrapping %v", err, errStore)
			}
			if err.Error() == errStore.Error() {
				t.Errorf("Timeline() error = %q, want context added", err)
			}
			if repo != (model.TargetRepo{}) || got != nil {
				t.Errorf("Timeline() = %+v, %+v; want zero values", repo, got)
			}
		})
	}
}

func TestService_Repos(t *testing.T) {
	t.Parallel()
	errStore := errors.New("database is closed")
	two := []model.TargetRepo{
		{ID: 2, Slug: "game", URL: "https://github.com/example-org/example-game"},
		{ID: 7, Slug: "server", URL: "https://github.com/example-org/example-server"},
	}
	tests := []struct {
		name    string
		fake    *fakeTimelineStore
		want    []model.TargetRepo
		wantErr error
	}{
		{name: "two repos in store order", fake: &fakeTimelineStore{list: two}, want: two},
		{name: "no repos", fake: &fakeTimelineStore{}, want: nil},
		{name: "store error", fake: &fakeTimelineStore{reposErr: errStore}, wantErr: errStore},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := NewService(&fakeRunStore{}, &fakeFindingStore{}, tt.fake, discardLogger())
			got, err := s.Repos(t.Context())
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) || err.Error() == tt.wantErr.Error() {
					t.Fatalf("Repos() error = %v, want wrapping %v with context", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Repos() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Repos() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

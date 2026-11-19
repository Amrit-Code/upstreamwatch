package store

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const (
	repoURL      = "https://github.com/example-org/example-server"
	repoURLMoved = "https://git.example.com/example-org/example-server"
)

func TestDB_UpsertRepo_InsertThenUpdate(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()

	first, err := db.UpsertRepo(ctx, "example-server", repoURL)
	if err != nil {
		t.Fatalf("first UpsertRepo() error = %v", err)
	}
	want := model.TargetRepo{ID: first.ID, Slug: "example-server", URL: repoURL}
	if first.ID == 0 || !reflect.DeepEqual(first, want) {
		t.Errorf("first UpsertRepo() = %+v, want %+v", first, want)
	}

	if err := db.RecordFetch(ctx, model.FetchResult{RepoID: first.ID, At: t0, Branch: "main", ClonePath: "/data/clones/example-server"}); err != nil {
		t.Fatalf("RecordFetch() error = %v", err)
	}
	exec(t, db, `UPDATE repos SET last_indexed_sha = 'abc123' WHERE id = ?`, first.ID)

	later := t0.Add(time.Hour)
	db.now = func() time.Time { return later }
	second, err := db.UpsertRepo(ctx, "example-server", repoURLMoved)
	if err != nil {
		t.Fatalf("second UpsertRepo() error = %v", err)
	}
	want = model.TargetRepo{
		ID:             first.ID,
		Slug:           "example-server",
		URL:            repoURLMoved,
		DefaultBranch:  "main",
		ClonePath:      "/data/clones/example-server",
		LastIndexedSHA: "abc123",
		LastFetchAt:    t0,
	}
	if !reflect.DeepEqual(second, want) {
		t.Errorf("second UpsertRepo() = %+v, want %+v", second, want)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM repos`); n != 1 {
		t.Errorf("repos rows = %d, want 1", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM repos WHERE created_ts = ? AND updated_ts = ?`,
		ts0, "2026-03-01T13:00:00Z"); n != 1 {
		t.Errorf("repos with created_ts kept and updated_ts moved = %d, want 1", n)
	}
}

func TestDB_RecordFetch_SuccessAndFailure(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	repo, err := db.UpsertRepo(ctx, "example-server", repoURL)
	if err != nil {
		t.Fatalf("UpsertRepo() error = %v", err)
	}

	steps := []struct {
		name string
		in   model.FetchResult
		want model.TargetRepo
	}{
		{
			name: "success stores branch and clone path",
			in:   model.FetchResult{RepoID: repo.ID, At: t0, Branch: "main", ClonePath: "/data/clones/example-server"},
			want: model.TargetRepo{ID: repo.ID, Slug: "example-server", URL: repoURL, DefaultBranch: "main",
				ClonePath: "/data/clones/example-server", LastFetchAt: t0},
		},
		{
			name: "failure keeps earlier branch and clone path",
			in: model.FetchResult{RepoID: repo.ID, At: t0.Add(time.Hour), Branch: "other", ClonePath: "/elsewhere",
				Error: "fetch: exit status 128"},
			want: model.TargetRepo{ID: repo.ID, Slug: "example-server", URL: repoURL, DefaultBranch: "main",
				ClonePath: "/data/clones/example-server", LastFetchAt: t0.Add(time.Hour), LastFetchError: "fetch: exit status 128"},
		},
		{
			name: "next success clears the error",
			in:   model.FetchResult{RepoID: repo.ID, At: t0.Add(2 * time.Hour), Branch: "trunk", ClonePath: "/data/clones/example-server"},
			want: model.TargetRepo{ID: repo.ID, Slug: "example-server", URL: repoURL, DefaultBranch: "trunk",
				ClonePath: "/data/clones/example-server", LastFetchAt: t0.Add(2 * time.Hour)},
		},
	}
	// The steps build on each other, so they run in order.
	for _, s := range steps {
		if err := db.RecordFetch(ctx, s.in); err != nil {
			t.Fatalf("%s: RecordFetch() error = %v", s.name, err)
		}
		got, err := db.GetRepo(ctx, "example-server")
		if err != nil {
			t.Fatalf("%s: GetRepo() error = %v", s.name, err)
		}
		if !reflect.DeepEqual(got, s.want) {
			t.Errorf("%s: GetRepo() = %+v, want %+v", s.name, got, s.want)
		}
	}
	if n := count(t, db, `SELECT COUNT(*) FROM repos WHERE last_fetch_error IS NULL`); n != 1 {
		t.Errorf("repos with NULL last_fetch_error = %d, want 1", n)
	}
}

func TestDB_RecordFetch_UnknownRepo(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	for _, in := range []model.FetchResult{
		{RepoID: 42, At: t0, Branch: "main", ClonePath: "/data/clones/x"},
		{RepoID: 42, At: t0, Error: "boom"},
	} {
		if err := db.RecordFetch(t.Context(), in); !errors.Is(err, model.ErrNotFound) {
			t.Errorf("RecordFetch(%+v) error = %v, want model.ErrNotFound", in, err)
		}
	}
}

func TestDB_GetRepo_NotFound(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if _, err := db.UpsertRepo(t.Context(), "example-server", repoURL); err != nil {
		t.Fatalf("UpsertRepo() error = %v", err)
	}
	_, err := db.GetRepo(t.Context(), "example-client")
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("GetRepo(example-client) error = %v, want model.ErrNotFound", err)
	}
}

func TestDB_ListRepos_Order(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()

	got, err := db.ListRepos(ctx)
	if err != nil || got != nil {
		t.Fatalf("ListRepos() on empty database = %v, %v; want nil, nil", got, err)
	}

	ids := map[string]int64{}
	for _, slug := range []string{"zeta", "alpha", "mid"} {
		r, err := db.UpsertRepo(ctx, slug, "https://github.com/example-org/"+slug)
		if err != nil {
			t.Fatalf("UpsertRepo(%s) error = %v", slug, err)
		}
		ids[slug] = r.ID
	}
	if err := db.RecordFetch(ctx, model.FetchResult{RepoID: ids["mid"], At: t0, Branch: "main", ClonePath: "/data/clones/mid"}); err != nil {
		t.Fatalf("RecordFetch() error = %v", err)
	}

	got, err = db.ListRepos(ctx)
	if err != nil {
		t.Fatalf("ListRepos() error = %v", err)
	}
	want := []model.TargetRepo{
		{ID: ids["alpha"], Slug: "alpha", URL: "https://github.com/example-org/alpha"},
		{ID: ids["mid"], Slug: "mid", URL: "https://github.com/example-org/mid", DefaultBranch: "main",
			ClonePath: "/data/clones/mid", LastFetchAt: t0},
		{ID: ids["zeta"], Slug: "zeta", URL: "https://github.com/example-org/zeta"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListRepos() = %+v, want %+v", got, want)
	}
}

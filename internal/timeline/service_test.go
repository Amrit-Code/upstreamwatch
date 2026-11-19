package timeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// fakeExtractor handles paths ending in suffix. Content is whitespace-separated
// "purl@version" entries; the content "!error" is a parse error.
type fakeExtractor struct {
	patterns  []string
	suffix    string
	ecosystem string
	kind      model.ManifestKind
	offered   []string
}

func (f *fakeExtractor) Patterns() []string { return f.patterns }

func (f *fakeExtractor) Parse(path string, content []byte) (model.ParsedManifest, error) {
	f.offered = append(f.offered, path)
	if f.suffix == "" || !strings.HasSuffix(path, f.suffix) {
		return model.ParsedManifest{}, fmt.Errorf("parse %s: %w", path, model.ErrUnsupportedFile)
	}
	text := string(content)
	if text == "!error" {
		return model.ParsedManifest{}, errors.New("syntax error")
	}
	pm := model.ParsedManifest{Ecosystem: f.ecosystem, Kind: f.kind}
	for _, entry := range strings.Fields(text) {
		purl, version, _ := strings.Cut(entry, "@")
		pm.Dependencies = append(pm.Dependencies, fdep(purl, f.ecosystem, version))
	}
	return pm, nil
}

// fdep is the dependency fakeExtractor produces for one entry.
func fdep(purl, eco, version string) model.Dependency {
	return model.Dependency{
		Package:     model.Package{PURL: purl, Ecosystem: eco, Name: purl},
		Version:     version,
		Requirement: version,
		Direct:      model.DirectYes,
	}
}

func goExtractor() *fakeExtractor {
	return &fakeExtractor{patterns: []string{"**/go.mod"}, suffix: "go.mod", ecosystem: "golang", kind: model.ManifestKindManifest}
}

func dockerExtractor() *fakeExtractor {
	return &fakeExtractor{patterns: []string{"**/Dockerfile"}, suffix: "Dockerfile", ecosystem: "docker", kind: model.ManifestKindManifest}
}

type commitsCall struct {
	slug, from, to string
	patterns       []string
}

type fakeGit struct {
	heads       map[string]string // by slug
	headErr     error
	notAncestor bool
	shortRead   bool                      // ReadBlobs drops the last blob of every call
	readErrOn   int                       // ReadBlobs call number (1-based) that fails; 0 never
	commits     map[string][]model.Commit // by slug
	blobs       map[model.BlobRef]model.Blob

	headCalls     []string
	ancestorCalls []string
	commitCalls   []commitsCall
	readCalls     [][]model.BlobRef
}

func (f *fakeGit) Head(ctx context.Context, repo model.TargetRepo) (string, error) {
	f.headCalls = append(f.headCalls, repo.Slug)
	if f.headErr != nil {
		return "", f.headErr
	}
	return f.heads[repo.Slug], nil
}

func (f *fakeGit) IsAncestor(ctx context.Context, repo model.TargetRepo, ancestor, commit string) (bool, error) {
	f.ancestorCalls = append(f.ancestorCalls, ancestor+".."+commit)
	return !f.notAncestor, nil
}

func (f *fakeGit) ManifestCommits(ctx context.Context, repo model.TargetRepo, from, to string, patterns []string) ([]model.Commit, error) {
	f.commitCalls = append(f.commitCalls, commitsCall{slug: repo.Slug, from: from, to: to, patterns: patterns})
	return f.commits[repo.Slug], nil
}

func (f *fakeGit) ReadBlobs(ctx context.Context, repo model.TargetRepo, refs []model.BlobRef) ([]model.Blob, error) {
	f.readCalls = append(f.readCalls, refs)
	if len(f.readCalls) == f.readErrOn {
		return nil, errors.New("cat-file died")
	}
	out := make([]model.Blob, 0, len(refs))
	for _, ref := range refs {
		b, ok := f.blobs[ref]
		if !ok {
			return nil, fmt.Errorf("no blob for %s:%s", ref.Commit, ref.Path)
		}
		b.Ref = ref
		out = append(out, b)
	}
	if f.shortRead {
		out = out[:len(out)-1]
	}
	return out, nil
}

// file stores text as the content of path at commit c.
func (f *fakeGit) file(c model.Commit, path, text string) {
	if f.blobs == nil {
		f.blobs = map[model.BlobRef]model.Blob{}
	}
	f.blobs[model.BlobRef{Commit: c.SHA, Path: path}] = model.Blob{Content: []byte(text)}
}

func (f *fakeGit) blob(c model.Commit, path string, b model.Blob) {
	if f.blobs == nil {
		f.blobs = map[model.BlobRef]model.Blob{}
	}
	f.blobs[model.BlobRef{Commit: c.SHA, Path: path}] = b
}

type fakeRepoStore struct {
	repos map[string]model.TargetRepo
	err   error
}

func (f *fakeRepoStore) GetRepo(ctx context.Context, slug string) (model.TargetRepo, error) {
	if f.err != nil {
		return model.TargetRepo{}, f.err
	}
	r, ok := f.repos[slug]
	if !ok {
		return model.TargetRepo{}, fmt.Errorf("get repo %s: %w", slug, model.ErrNotFound)
	}
	return r, nil
}

type fakeTimelineStore struct {
	events   map[int64][]model.DepEvent
	applyErr map[int64]error
	// applyHook, when set, runs on every ApplyTimeline call and its error
	// is returned.
	applyHook func(ctx context.Context, u model.TimelineUpdate) error

	listCalls []int64
	applied   []model.TimelineUpdate
}

func (f *fakeTimelineStore) ListEvents(ctx context.Context, repoID int64) ([]model.DepEvent, error) {
	f.listCalls = append(f.listCalls, repoID)
	return f.events[repoID], nil
}

func (f *fakeTimelineStore) ApplyTimeline(ctx context.Context, u model.TimelineUpdate) error {
	f.applied = append(f.applied, u)
	if f.applyHook != nil {
		return f.applyHook(ctx, u)
	}
	return f.applyErr[u.RepoID]
}

func testRepo(id int64, slug string) model.TargetRepo {
	return model.TargetRepo{
		ID:            id,
		Slug:          slug,
		URL:           "https://github.com/example-org/" + slug,
		DefaultBranch: "main",
		ClonePath:     "/data/repos/" + slug + ".git",
	}
}

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

// stats returns a Stats with every key zero, then the given values.
func stats(kv map[string]int64) model.Stats {
	s := model.Stats{}
	for _, k := range statKeys {
		s[k] = 0
	}
	for k, v := range kv {
		s[k] = v
	}
	return s
}

func checkStats(t *testing.T, got model.Stats, want map[string]int64) {
	t.Helper()
	if w := stats(want); !reflect.DeepEqual(got, w) {
		t.Errorf("Run() stats = %v, want %v", got, w)
	}
}

func onlyApply(t *testing.T, ts *fakeTimelineStore) model.TimelineUpdate {
	t.Helper()
	if len(ts.applied) != 1 {
		t.Fatalf("ApplyTimeline called %d times, want 1", len(ts.applied))
	}
	return ts.applied[0]
}

func TestService_Name(t *testing.T) {
	t.Parallel()
	s := NewService(&fakeGit{}, &fakeRepoStore{}, &fakeTimelineStore{}, nil, nil, Options{}, discard())
	if got := s.Name(); got != "timeline" {
		t.Errorf("Name() = %q, want %q", got, "timeline")
	}
}

func TestRun_FirstRun(t *testing.T) {
	t.Parallel()
	c1 := commit("c1", 1, "go.mod", "Dockerfile")
	c2 := commit("c2", 2, "go.mod")
	c3 := commit("c3", 3, "Dockerfile")
	git := &fakeGit{
		heads:   map[string]string{"alpha": "c3"},
		commits: map[string][]model.Commit{"alpha": {c1, c2, c3}},
	}
	git.file(c1, "go.mod", "pkg:golang/a@v1.0.0 pkg:golang/b@v1.0.0")
	git.file(c1, "Dockerfile", "pkg:docker/golang@1.26")
	git.file(c2, "go.mod", "pkg:golang/a@v1.1.0 pkg:golang/b@v1.0.0")
	git.file(c3, "Dockerfile", "pkg:docker/golang@1.27")
	repos := &fakeRepoStore{repos: map[string]model.TargetRepo{"alpha": testRepo(1, "alpha")}}
	ts := &fakeTimelineStore{}
	s := NewService(git, repos, ts, []Extractor{goExtractor(), dockerExtractor()}, nil,
		Options{Targets: []TargetSettings{{Slug: "alpha"}}}, discard())

	got, err := s.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantCall := []commitsCall{{slug: "alpha", from: "", to: "c3", patterns: []string{"**/go.mod", "**/Dockerfile"}}}
	if !reflect.DeepEqual(git.commitCalls, wantCall) {
		t.Errorf("ManifestCommits calls = %+v, want %+v", git.commitCalls, wantCall)
	}
	a1 := fdep("pkg:golang/a", "golang", "v1.0.0")
	a2 := fdep("pkg:golang/a", "golang", "v1.1.0")
	b1 := fdep("pkg:golang/b", "golang", "v1.0.0")
	img1 := fdep("pkg:docker/golang", "docker", "1.26")
	img2 := fdep("pkg:docker/golang", "docker", "1.27")
	want := model.TimelineUpdate{
		RepoID:  1,
		Commits: []model.Commit{c1, c2, c3},
		Manifests: map[string]model.ManifestInfo{
			"go.mod":     {Ecosystem: "golang", Kind: model.ManifestKindManifest},
			"Dockerfile": {Ecosystem: "docker", Kind: model.ManifestKindManifest},
		},
		Events: []model.DepEvent{
			event(c1, "Dockerfile", img1, model.ChangeAdded, ""),
			event(c1, "go.mod", a1, model.ChangeAdded, ""),
			event(c1, "go.mod", b1, model.ChangeAdded, ""),
			event(c2, "go.mod", a2, model.ChangeChanged, "v1.0.0"),
			event(c3, "Dockerfile", img2, model.ChangeChanged, "1.26"),
		},
		Intervals: []model.DepInterval{
			interval("Dockerfile", img1, c1, &c3),
			interval("Dockerfile", img2, c3, nil),
			interval("go.mod", a1, c1, &c2),
			interval("go.mod", a2, c2, nil),
			interval("go.mod", b1, c1, nil),
		},
		IndexedSHA: "c3",
	}
	if u := onlyApply(t, ts); !reflect.DeepEqual(u, want) {
		t.Errorf("ApplyTimeline update =\n%+v\nwant\n%+v", u, want)
	}
	checkStats(t, got, map[string]int64{
		"targets": 1, "commits_walked": 3, "files_read": 4, "events_written": 5, "intervals": 5,
	})
}

func TestRun_NoNewCommits(t *testing.T) {
	t.Parallel()
	repo := testRepo(1, "alpha")
	repo.LastIndexedSHA = "c3"
	git := &fakeGit{heads: map[string]string{"alpha": "c3"}}
	ts := &fakeTimelineStore{}
	s := NewService(git, &fakeRepoStore{repos: map[string]model.TargetRepo{"alpha": repo}}, ts,
		[]Extractor{goExtractor()}, nil, Options{Targets: []TargetSettings{{Slug: "alpha"}}}, discard())

	got, err := s.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(git.commitCalls) != 0 || len(git.readCalls) != 0 || len(git.ancestorCalls) != 0 {
		t.Errorf("git calls: ManifestCommits %d, ReadBlobs %d, IsAncestor %d, want 0 each",
			len(git.commitCalls), len(git.readCalls), len(git.ancestorCalls))
	}
	if len(ts.applied) != 0 || len(ts.listCalls) != 0 {
		t.Errorf("store calls: ApplyTimeline %d, ListEvents %d, want 0 each", len(ts.applied), len(ts.listCalls))
	}
	checkStats(t, got, map[string]int64{"targets": 1})
}

func TestRun_Incremental(t *testing.T) {
	t.Parallel()
	c1 := commit("c1", 1, "go.mod")
	c2 := commit("c2", 2, "go.mod")
	a1 := fdep("pkg:golang/a", "golang", "v1.0.0")
	a2 := fdep("pkg:golang/a", "golang", "v1.1.0")
	b1 := fdep("pkg:golang/b", "golang", "v1.0.0")
	repo := testRepo(7, "alpha")
	repo.LastIndexedSHA = "c1"
	git := &fakeGit{
		heads:   map[string]string{"alpha": "c2"},
		commits: map[string][]model.Commit{"alpha": {c2}},
	}
	git.file(c2, "go.mod", "pkg:golang/a@v1.1.0 pkg:golang/b@v1.0.0")
	stored := []model.DepEvent{
		event(c1, "go.mod", a1, model.ChangeAdded, ""),
		event(c1, "go.mod", b1, model.ChangeAdded, ""),
	}
	ts := &fakeTimelineStore{events: map[int64][]model.DepEvent{7: stored}}
	s := NewService(git, &fakeRepoStore{repos: map[string]model.TargetRepo{"alpha": repo}}, ts,
		[]Extractor{goExtractor()}, nil, Options{Targets: []TargetSettings{{Slug: "alpha"}}}, discard())

	got, err := s.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if want := []string{"c1..c2"}; !reflect.DeepEqual(git.ancestorCalls, want) {
		t.Errorf("IsAncestor calls = %v, want %v", git.ancestorCalls, want)
	}
	if len(git.commitCalls) != 1 || git.commitCalls[0].from != "c1" || git.commitCalls[0].to != "c2" {
		t.Errorf("ManifestCommits calls = %+v, want one from c1 to c2", git.commitCalls)
	}
	u := onlyApply(t, ts)
	if u.Reset {
		t.Errorf("Reset = true, want false")
	}
	wantEvents := []model.DepEvent{event(c2, "go.mod", a2, model.ChangeChanged, "v1.0.0")}
	if !reflect.DeepEqual(u.Events, wantEvents) {
		t.Errorf("Events = %+v, want %+v", u.Events, wantEvents)
	}
	wantIntervals := []model.DepInterval{
		interval("go.mod", a1, c1, &c2),
		interval("go.mod", a2, c2, nil),
		interval("go.mod", b1, c1, nil),
	}
	if !reflect.DeepEqual(u.Intervals, wantIntervals) {
		t.Errorf("Intervals = %+v, want %+v", u.Intervals, wantIntervals)
	}
	if u.IndexedSHA != "c2" || u.RepoID != 7 {
		t.Errorf("IndexedSHA, RepoID = %q, %d, want c2, 7", u.IndexedSHA, u.RepoID)
	}
	checkStats(t, got, map[string]int64{
		"targets": 1, "commits_walked": 1, "files_read": 1, "events_written": 1, "intervals": 3,
	})
}

func TestRun_DeletedManifest(t *testing.T) {
	t.Parallel()
	c1 := commit("c1", 1, "go.mod", "Dockerfile")
	c2 := commit("c2", 2, "go.mod")
	a1 := fdep("pkg:golang/a", "golang", "v1.0.0")
	b1 := fdep("pkg:golang/b", "golang", "v1.0.0")
	img := fdep("pkg:docker/golang", "docker", "1.26")
	repo := testRepo(1, "alpha")
	repo.LastIndexedSHA = "c1"
	git := &fakeGit{
		heads:   map[string]string{"alpha": "c2"},
		commits: map[string][]model.Commit{"alpha": {c2}},
	}
	git.blob(c2, "go.mod", model.Blob{Missing: true})
	ts := &fakeTimelineStore{events: map[int64][]model.DepEvent{1: {
		event(c1, "Dockerfile", img, model.ChangeAdded, ""),
		event(c1, "go.mod", a1, model.ChangeAdded, ""),
		event(c1, "go.mod", b1, model.ChangeAdded, ""),
	}}}
	s := NewService(git, &fakeRepoStore{repos: map[string]model.TargetRepo{"alpha": repo}}, ts,
		[]Extractor{goExtractor(), dockerExtractor()}, nil, Options{Targets: []TargetSettings{{Slug: "alpha"}}}, discard())

	got, err := s.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	u := onlyApply(t, ts)
	wantEvents := []model.DepEvent{
		event(c2, "go.mod", a1, model.ChangeRemoved, "v1.0.0"),
		event(c2, "go.mod", b1, model.ChangeRemoved, "v1.0.0"),
	}
	if !reflect.DeepEqual(u.Events, wantEvents) {
		t.Errorf("Events = %+v, want %+v", u.Events, wantEvents)
	}
	wantIntervals := []model.DepInterval{
		interval("Dockerfile", img, c1, nil),
		interval("go.mod", a1, c1, &c2),
		interval("go.mod", b1, c1, &c2),
	}
	if !reflect.DeepEqual(u.Intervals, wantIntervals) {
		t.Errorf("Intervals = %+v, want %+v", u.Intervals, wantIntervals)
	}
	if len(u.Manifests) != 0 {
		t.Errorf("Manifests = %v, want empty: nothing was parsed", u.Manifests)
	}
	checkStats(t, got, map[string]int64{
		"targets": 1, "commits_walked": 1, "events_written": 2, "intervals": 3,
	})
}

func TestRun_RewrittenHistory(t *testing.T) {
	t.Parallel()
	old := commit("old", 1, "go.mod")
	n1 := commit("n1", 2, "go.mod")
	a1 := fdep("pkg:golang/a", "golang", "v1.0.0")
	a2 := fdep("pkg:golang/a", "golang", "v2.0.0")
	repo := testRepo(1, "alpha")
	repo.LastIndexedSHA = "old"
	git := &fakeGit{
		heads:       map[string]string{"alpha": "n1"},
		notAncestor: true,
		commits:     map[string][]model.Commit{"alpha": {n1}},
	}
	git.file(n1, "go.mod", "pkg:golang/a@v2.0.0")
	ts := &fakeTimelineStore{events: map[int64][]model.DepEvent{1: {
		event(old, "go.mod", a1, model.ChangeAdded, ""),
	}}}
	s := NewService(git, &fakeRepoStore{repos: map[string]model.TargetRepo{"alpha": repo}}, ts,
		[]Extractor{goExtractor()}, nil, Options{Targets: []TargetSettings{{Slug: "alpha"}}}, discard())

	got, err := s.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(git.commitCalls) != 1 || git.commitCalls[0].from != "" {
		t.Errorf("ManifestCommits calls = %+v, want one with from empty", git.commitCalls)
	}
	if len(ts.listCalls) != 0 {
		t.Errorf("ListEvents called %d times, want 0 after a rewrite", len(ts.listCalls))
	}
	u := onlyApply(t, ts)
	want := model.TimelineUpdate{
		RepoID:     1,
		Reset:      true,
		Commits:    []model.Commit{n1},
		Manifests:  map[string]model.ManifestInfo{"go.mod": {Ecosystem: "golang", Kind: model.ManifestKindManifest}},
		Events:     []model.DepEvent{event(n1, "go.mod", a2, model.ChangeAdded, "")},
		Intervals:  []model.DepInterval{interval("go.mod", a2, n1, nil)},
		IndexedSHA: "n1",
	}
	if !reflect.DeepEqual(u, want) {
		t.Errorf("ApplyTimeline update =\n%+v\nwant\n%+v", u, want)
	}
	checkStats(t, got, map[string]int64{
		"targets": 1, "resets": 1, "commits_walked": 1, "files_read": 1, "events_written": 1, "intervals": 1,
	})
}

func TestRun_UnityAndAllowlist(t *testing.T) {
	t.Parallel()
	c1 := commit("c1", 1, "Packages/manifest.json", "go.mod", "Dockerfile")
	git := &fakeGit{
		heads:   map[string]string{"game": "c1"},
		commits: map[string][]model.Commit{"game": {c1}},
	}
	git.file(c1, "Packages/manifest.json", "pkg:upm/com.example.tool@1.0.0")
	git.file(c1, "go.mod", "pkg:golang/a@v1.0.0")
	git.file(c1, "Dockerfile", "pkg:docker/golang@1.26")
	unity := &fakeExtractor{
		patterns: []string{"Packages/manifest.json", "ProjectSettings/ProjectVersion.txt"},
		suffix:   "manifest.json", ecosystem: "upm", kind: model.ManifestKindManifest,
	}
	goExt := goExtractor()
	ts := &fakeTimelineStore{}
	s := NewService(git, &fakeRepoStore{repos: map[string]model.TargetRepo{"game": testRepo(3, "game")}}, ts,
		[]Extractor{goExt, dockerExtractor()}, unity,
		Options{Targets: []TargetSettings{{Slug: "game", Unity: true, Ecosystems: []string{"golang"}}}}, discard())

	got, err := s.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	wantPatterns := []string{"Packages/manifest.json", "ProjectSettings/ProjectVersion.txt", "**/go.mod", "**/Dockerfile"}
	if len(git.commitCalls) != 1 || !reflect.DeepEqual(git.commitCalls[0].patterns, wantPatterns) {
		t.Errorf("ManifestCommits calls = %+v, want patterns %v", git.commitCalls, wantPatterns)
	}
	if want := []string{"Packages/manifest.json", "go.mod", "Dockerfile"}; !reflect.DeepEqual(unity.offered, want) {
		t.Errorf("Unity extractor offered %v, want %v", unity.offered, want)
	}
	if want := []string{"go.mod", "Dockerfile"}; !reflect.DeepEqual(goExt.offered, want) {
		t.Errorf("go extractor offered %v, want %v (Unity file handled first)", goExt.offered, want)
	}
	u := onlyApply(t, ts)
	wantManifests := map[string]model.ManifestInfo{
		"Packages/manifest.json": {Ecosystem: "upm", Kind: model.ManifestKindManifest},
		"go.mod":                 {Ecosystem: "golang", Kind: model.ManifestKindManifest},
		"Dockerfile":             {Ecosystem: "docker", Kind: model.ManifestKindManifest},
	}
	if !reflect.DeepEqual(u.Manifests, wantManifests) {
		t.Errorf("Manifests = %v, want %v", u.Manifests, wantManifests)
	}
	a1 := fdep("pkg:golang/a", "golang", "v1.0.0")
	if want := []model.DepEvent{event(c1, "go.mod", a1, model.ChangeAdded, "")}; !reflect.DeepEqual(u.Events, want) {
		t.Errorf("Events = %+v, want %+v (only golang allowed)", u.Events, want)
	}
	checkStats(t, got, map[string]int64{
		"targets": 1, "commits_walked": 1, "files_read": 3, "events_written": 1, "intervals": 1,
	})
}

func TestRun_ParseErrorKeepsState(t *testing.T) {
	t.Parallel()
	c1 := commit("c1", 1, "go.mod")
	c2 := commit("c2", 2, "go.mod")
	c3 := commit("c3", 3, "go.mod")
	a1 := fdep("pkg:golang/a", "golang", "v1.0.0")
	repo := testRepo(1, "alpha")
	repo.LastIndexedSHA = "c1"
	git := &fakeGit{
		heads:   map[string]string{"alpha": "c3"},
		commits: map[string][]model.Commit{"alpha": {c2, c3}},
	}
	git.file(c2, "go.mod", "!error")
	git.file(c3, "go.mod", "pkg:golang/a@v1.0.0")
	ts := &fakeTimelineStore{events: map[int64][]model.DepEvent{1: {event(c1, "go.mod", a1, model.ChangeAdded, "")}}}
	s := NewService(git, &fakeRepoStore{repos: map[string]model.TargetRepo{"alpha": repo}}, ts,
		[]Extractor{goExtractor()}, nil, Options{Targets: []TargetSettings{{Slug: "alpha"}}}, discard())

	got, err := s.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	u := onlyApply(t, ts)
	if len(u.Events) != 0 {
		t.Errorf("Events = %+v, want none: the failed parse keeps the state", u.Events)
	}
	if want := []model.DepInterval{interval("go.mod", a1, c1, nil)}; !reflect.DeepEqual(u.Intervals, want) {
		t.Errorf("Intervals = %+v, want %+v", u.Intervals, want)
	}
	checkStats(t, got, map[string]int64{
		"targets": 1, "commits_walked": 2, "files_read": 1, "parse_errors": 1, "intervals": 1,
	})
}

func TestRun_TooLargeSkipped(t *testing.T) {
	t.Parallel()
	c1 := commit("c1", 1, "go.mod")
	c2 := commit("c2", 2, "go.mod")
	a1 := fdep("pkg:golang/a", "golang", "v1.0.0")
	git := &fakeGit{
		heads:   map[string]string{"alpha": "c2"},
		commits: map[string][]model.Commit{"alpha": {c1, c2}},
	}
	git.file(c1, "go.mod", "pkg:golang/a@v1.0.0")
	git.blob(c2, "go.mod", model.Blob{TooLarge: true})
	ts := &fakeTimelineStore{}
	s := NewService(git, &fakeRepoStore{repos: map[string]model.TargetRepo{"alpha": testRepo(1, "alpha")}}, ts,
		[]Extractor{goExtractor()}, nil, Options{Targets: []TargetSettings{{Slug: "alpha"}}}, discard())

	got, err := s.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	u := onlyApply(t, ts)
	if want := []model.DepEvent{event(c1, "go.mod", a1, model.ChangeAdded, "")}; !reflect.DeepEqual(u.Events, want) {
		t.Errorf("Events = %+v, want %+v", u.Events, want)
	}
	if want := []model.Commit{c1, c2}; !reflect.DeepEqual(u.Commits, want) {
		t.Errorf("Commits = %+v, want both walked commits", u.Commits)
	}
	checkStats(t, got, map[string]int64{
		"targets": 1, "commits_walked": 2, "files_read": 1, "files_skipped": 1, "events_written": 1, "intervals": 1,
	})
}

func TestRun_UnhandledFileSkipped(t *testing.T) {
	t.Parallel()
	c1 := commit("c1", 1, "go.mod", "go.sum")
	git := &fakeGit{
		heads:   map[string]string{"alpha": "c1"},
		commits: map[string][]model.Commit{"alpha": {c1}},
	}
	git.file(c1, "go.mod", "pkg:golang/a@v1.0.0")
	git.file(c1, "go.sum", "pkg:golang/z@v9.9.9")
	ts := &fakeTimelineStore{}
	s := NewService(git, &fakeRepoStore{repos: map[string]model.TargetRepo{"alpha": testRepo(1, "alpha")}}, ts,
		[]Extractor{goExtractor()}, nil, Options{Targets: []TargetSettings{{Slug: "alpha"}}}, discard())

	got, err := s.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	u := onlyApply(t, ts)
	if _, ok := u.Manifests["go.sum"]; ok || len(u.Events) != 1 {
		t.Errorf("Manifests = %v, Events = %+v, want go.sum ignored", u.Manifests, u.Events)
	}
	checkStats(t, got, map[string]int64{
		"targets": 1, "commits_walked": 1, "files_read": 1, "events_written": 1, "intervals": 1,
	})
}

func TestRun_NoManifestCommitsStillMovesIndexedSHA(t *testing.T) {
	t.Parallel()
	c1 := commit("c1", 1, "go.mod")
	a1 := fdep("pkg:golang/a", "golang", "v1.0.0")
	repo := testRepo(1, "alpha")
	repo.LastIndexedSHA = "c1"
	git := &fakeGit{heads: map[string]string{"alpha": "c2"}}
	ts := &fakeTimelineStore{events: map[int64][]model.DepEvent{1: {event(c1, "go.mod", a1, model.ChangeAdded, "")}}}
	s := NewService(git, &fakeRepoStore{repos: map[string]model.TargetRepo{"alpha": repo}}, ts,
		[]Extractor{goExtractor()}, nil, Options{Targets: []TargetSettings{{Slug: "alpha"}}}, discard())

	got, err := s.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	u := onlyApply(t, ts)
	if u.IndexedSHA != "c2" || len(u.Commits) != 0 || len(u.Events) != 0 {
		t.Errorf("update = %+v, want IndexedSHA c2 with no commits or events", u)
	}
	if want := []model.DepInterval{interval("go.mod", a1, c1, nil)}; !reflect.DeepEqual(u.Intervals, want) {
		t.Errorf("Intervals = %+v, want %+v", u.Intervals, want)
	}
	if len(git.readCalls) != 0 {
		t.Errorf("ReadBlobs called %d times, want 0", len(git.readCalls))
	}
	checkStats(t, got, map[string]int64{"targets": 1, "intervals": 1})
}

func TestRun_OneTargetFails(t *testing.T) {
	t.Parallel()
	c1 := commit("c1", 1, "go.mod")
	c2 := commit("c2", 2, "go.mod")
	c3 := commit("c3", 3, "go.mod")
	git := &fakeGit{
		heads:   map[string]string{"alpha": "c3", "beta": "c1"},
		commits: map[string][]model.Commit{"alpha": {c1, c2, c3}, "beta": {c1}},
	}
	git.file(c1, "go.mod", "pkg:golang/a@v1.0.0")
	git.file(c2, "go.mod", "!error")
	git.blob(c3, "go.mod", model.Blob{TooLarge: true})
	repos := &fakeRepoStore{repos: map[string]model.TargetRepo{
		"alpha": testRepo(1, "alpha"),
		"beta":  testRepo(2, "beta"),
	}}
	ts := &fakeTimelineStore{applyErr: map[int64]error{1: errors.New("disk full")}}
	s := NewService(git, repos, ts, []Extractor{goExtractor()}, nil,
		Options{Targets: []TargetSettings{{Slug: "alpha"}, {Slug: "beta"}}}, discard())

	got, err := s.Run(t.Context(), model.Scope{})
	want := "timeline: 1 of 2 targets failed: alpha: apply timeline: disk full"
	if err == nil || err.Error() != want {
		t.Fatalf("Run() error = %v, want %q", err, want)
	}
	if len(ts.applied) != 2 || ts.applied[1].RepoID != 2 {
		t.Fatalf("ApplyTimeline calls = %d, want the second for repo 2", len(ts.applied))
	}
	// alpha's work before its failed update counts: 3 commits, 1 parsed,
	// 1 parse error, 1 too large, 1 event and 1 interval passed to the store.
	checkStats(t, got, map[string]int64{
		"targets": 2, "failed": 1, "commits_walked": 4, "files_read": 2, "parse_errors": 1,
		"files_skipped": 1, "events_written": 2, "intervals": 2,
	})
}

func TestRun_FailureInLaterBlobChunkKeepsCounters(t *testing.T) {
	t.Parallel()
	var commits []model.Commit
	git := &fakeGit{heads: map[string]string{"alpha": "c500"}, readErrOn: 2}
	for i := range 501 {
		c := commit(fmt.Sprintf("c%d", i), 1, "go.mod")
		commits = append(commits, c)
		git.file(c, "go.mod", fmt.Sprintf("pkg:golang/a@v1.0.%d", i))
	}
	git.commits = map[string][]model.Commit{"alpha": commits}
	ts := &fakeTimelineStore{}
	s := NewService(git, &fakeRepoStore{repos: map[string]model.TargetRepo{"alpha": testRepo(1, "alpha")}}, ts,
		[]Extractor{goExtractor()}, nil, Options{Targets: []TargetSettings{{Slug: "alpha"}}}, discard())

	got, err := s.Run(t.Context(), model.Scope{})
	want := "timeline: 1 of 1 targets failed: alpha: read blobs: cat-file died"
	if err == nil || err.Error() != want {
		t.Fatalf("Run() error = %v, want %q", err, want)
	}
	if len(ts.applied) != 0 {
		t.Errorf("ApplyTimeline called %d times, want 0", len(ts.applied))
	}
	checkStats(t, got, map[string]int64{
		"targets": 1, "failed": 1, "commits_walked": 501, "files_read": 500,
	})
}

func TestRun_GitAndStoreErrorsFailTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		git       *fakeGit
		repos     *fakeRepoStore
		want      string
		wantStats map[string]int64
	}{
		{
			name:      "head",
			git:       &fakeGit{headErr: errors.New("bad ref")},
			repos:     &fakeRepoStore{repos: map[string]model.TargetRepo{"alpha": testRepo(1, "alpha")}},
			want:      "timeline: 1 of 1 targets failed: alpha: read head: bad ref",
			wantStats: map[string]int64{"targets": 1, "failed": 1},
		},
		{
			name:      "get repo",
			git:       &fakeGit{},
			repos:     &fakeRepoStore{err: errors.New("database is locked")},
			want:      "timeline: 1 of 1 targets failed: alpha: read repo: database is locked",
			wantStats: map[string]int64{"targets": 1, "failed": 1},
		},
		{
			name:      "read blobs",
			git:       &fakeGit{heads: map[string]string{"alpha": "c1"}, commits: map[string][]model.Commit{"alpha": {commit("c1", 1, "go.mod")}}},
			repos:     &fakeRepoStore{repos: map[string]model.TargetRepo{"alpha": testRepo(1, "alpha")}},
			want:      "timeline: 1 of 1 targets failed: alpha: read blobs: no blob for c1:go.mod",
			wantStats: map[string]int64{"targets": 1, "failed": 1, "commits_walked": 1},
		},
		{
			name: "short blob read",
			git: &fakeGit{
				heads:     map[string]string{"alpha": "c1"},
				commits:   map[string][]model.Commit{"alpha": {commit("c1", 1, "go.mod")}},
				blobs:     map[model.BlobRef]model.Blob{{Commit: "c1", Path: "go.mod"}: {}},
				shortRead: true,
			},
			repos:     &fakeRepoStore{repos: map[string]model.TargetRepo{"alpha": testRepo(1, "alpha")}},
			want:      "timeline: 1 of 1 targets failed: alpha: read blobs: got 0 blobs for 1 refs",
			wantStats: map[string]int64{"targets": 1, "failed": 1, "commits_walked": 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ts := &fakeTimelineStore{}
			s := NewService(tt.git, tt.repos, ts, []Extractor{goExtractor()}, nil,
				Options{Targets: []TargetSettings{{Slug: "alpha"}}}, discard())
			got, err := s.Run(t.Context(), model.Scope{})
			if err == nil || err.Error() != tt.want {
				t.Fatalf("Run() error = %v, want %q", err, tt.want)
			}
			if len(ts.applied) != 0 {
				t.Errorf("ApplyTimeline called %d times, want 0", len(ts.applied))
			}
			checkStats(t, got, tt.wantStats)
		})
	}
}

func TestRun_NeverFetched(t *testing.T) {
	t.Parallel()
	unfetched := testRepo(1, "alpha")
	unfetched.ClonePath = ""
	noBranch := testRepo(2, "beta")
	noBranch.DefaultBranch = ""
	git := &fakeGit{}
	ts := &fakeTimelineStore{}
	s := NewService(git, &fakeRepoStore{repos: map[string]model.TargetRepo{"alpha": unfetched, "beta": noBranch}}, ts,
		[]Extractor{goExtractor()}, nil,
		Options{Targets: []TargetSettings{{Slug: "alpha"}, {Slug: "beta"}, {Slug: "gamma"}}}, discard())

	got, err := s.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if len(git.headCalls) != 0 || len(ts.applied) != 0 {
		t.Errorf("Head calls %d, ApplyTimeline calls %d, want 0 each", len(git.headCalls), len(ts.applied))
	}
	checkStats(t, got, map[string]int64{"targets": 3, "skipped": 3})
}

func TestRun_ScopeOneRepo(t *testing.T) {
	t.Parallel()
	alpha, beta := testRepo(1, "alpha"), testRepo(2, "beta")
	alpha.LastIndexedSHA, beta.LastIndexedSHA = "h", "h"
	git := &fakeGit{heads: map[string]string{"alpha": "h", "beta": "h"}}
	s := NewService(git, &fakeRepoStore{repos: map[string]model.TargetRepo{"alpha": alpha, "beta": beta}},
		&fakeTimelineStore{}, nil, nil,
		Options{Targets: []TargetSettings{{Slug: "alpha"}, {Slug: "beta"}}}, discard())

	got, err := s.Run(t.Context(), model.Scope{RepoSlug: "beta"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if want := []string{"beta"}; !reflect.DeepEqual(git.headCalls, want) {
		t.Errorf("Head calls = %v, want %v", git.headCalls, want)
	}
	checkStats(t, got, map[string]int64{"targets": 1})
}

func TestRun_ScopeUnknownRepo(t *testing.T) {
	t.Parallel()
	git := &fakeGit{}
	s := NewService(git, &fakeRepoStore{}, &fakeTimelineStore{}, nil, nil,
		Options{Targets: []TargetSettings{{Slug: "alpha"}}}, discard())

	got, err := s.Run(t.Context(), model.Scope{RepoSlug: "nope"})
	if err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Fatalf("Run() error = %v, want one naming \"nope\"", err)
	}
	if len(git.headCalls) != 0 {
		t.Errorf("Head calls = %v, want none", git.headCalls)
	}
	checkStats(t, got, nil)
}

func TestRun_ContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	alpha, beta := testRepo(1, "alpha"), testRepo(2, "beta")
	alpha.LastIndexedSHA = "h"
	git := &cancellingGit{fakeGit: fakeGit{heads: map[string]string{"alpha": "h", "beta": "h"}}, cancel: cancel}
	s := NewService(git, &fakeRepoStore{repos: map[string]model.TargetRepo{"alpha": alpha, "beta": beta}},
		&fakeTimelineStore{}, nil, nil,
		Options{Targets: []TargetSettings{{Slug: "alpha"}, {Slug: "beta"}}}, discard())

	_, err := s.Run(ctx, model.Scope{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if want := []string{"alpha"}; !reflect.DeepEqual(git.headCalls, want) {
		t.Errorf("Head calls = %v, want %v", git.headCalls, want)
	}
}

func TestRun_ContextCancelledDuringOnlyTarget(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	git := &cancellingGit{fakeGit: fakeGit{heads: map[string]string{"alpha": "h"}}, cancel: cancel, fail: true}
	ts := &fakeTimelineStore{}
	s := NewService(git, &fakeRepoStore{repos: map[string]model.TargetRepo{"alpha": testRepo(1, "alpha")}},
		ts, nil, nil, Options{Targets: []TargetSettings{{Slug: "alpha"}}}, discard())

	got, err := s.Run(ctx, model.Scope{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if len(ts.applied) != 0 {
		t.Errorf("ApplyTimeline called %d times, want 0", len(ts.applied))
	}
	checkStats(t, got, map[string]int64{"targets": 1, "failed": 1})
}

func TestRun_ContextCancelledDuringFinalTargetApply(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	c1 := commit("c1", 1, "go.mod")
	git := &fakeGit{
		heads:   map[string]string{"alpha": "c1", "beta": "c1"},
		commits: map[string][]model.Commit{"alpha": {c1}, "beta": {c1}},
	}
	git.file(c1, "go.mod", "pkg:golang/a@v1.0.0")
	repos := &fakeRepoStore{repos: map[string]model.TargetRepo{
		"alpha": testRepo(1, "alpha"),
		"beta":  testRepo(2, "beta"),
	}}
	ts := &fakeTimelineStore{applyHook: func(ctx context.Context, u model.TimelineUpdate) error {
		if u.RepoID != 2 {
			return nil
		}
		cancel()
		return fmt.Errorf("begin transaction: %w", ctx.Err())
	}}
	s := NewService(git, repos, ts, []Extractor{goExtractor()}, nil,
		Options{Targets: []TargetSettings{{Slug: "alpha"}, {Slug: "beta"}}}, discard())

	got, err := s.Run(ctx, model.Scope{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if len(ts.applied) != 2 {
		t.Errorf("ApplyTimeline called %d times, want 2", len(ts.applied))
	}
	checkStats(t, got, map[string]int64{
		"targets": 2, "failed": 1, "commits_walked": 2, "files_read": 2, "events_written": 2, "intervals": 2,
	})
}

func TestRun_ContextCancelledAfterFinalTargetSucceeds(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	repo := testRepo(1, "alpha")
	repo.LastIndexedSHA = "h"
	git := &cancellingGit{fakeGit: fakeGit{heads: map[string]string{"alpha": "h"}}, cancel: cancel}
	s := NewService(git, &fakeRepoStore{repos: map[string]model.TargetRepo{"alpha": repo}},
		&fakeTimelineStore{}, nil, nil, Options{Targets: []TargetSettings{{Slug: "alpha"}}}, discard())

	got, err := s.Run(ctx, model.Scope{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	checkStats(t, got, map[string]int64{"targets": 1})
}

// cancellingGit cancels the run's context during every Head call. With fail
// set, Head then returns the context's error, as a git process killed by the
// cancellation would.
type cancellingGit struct {
	fakeGit
	cancel context.CancelFunc
	fail   bool
}

func (g *cancellingGit) Head(ctx context.Context, repo model.TargetRepo) (string, error) {
	g.cancel()
	if g.fail {
		g.headCalls = append(g.headCalls, repo.Slug)
		return "", fmt.Errorf("git rev-parse: %w", ctx.Err())
	}
	return g.fakeGit.Head(ctx, repo)
}

func TestRun_ReadsBlobsInChunks(t *testing.T) {
	t.Parallel()
	var commits []model.Commit
	git := &fakeGit{heads: map[string]string{"alpha": "c500"}}
	for i := range 501 {
		c := commit(fmt.Sprintf("c%d", i), 1, "go.mod")
		commits = append(commits, c)
		git.file(c, "go.mod", fmt.Sprintf("pkg:golang/a@v1.0.%d", i))
	}
	git.commits = map[string][]model.Commit{"alpha": commits}
	ts := &fakeTimelineStore{}
	s := NewService(git, &fakeRepoStore{repos: map[string]model.TargetRepo{"alpha": testRepo(1, "alpha")}}, ts,
		[]Extractor{goExtractor()}, nil, Options{Targets: []TargetSettings{{Slug: "alpha"}}}, discard())

	got, err := s.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(git.readCalls) != 2 || len(git.readCalls[0]) != 500 || len(git.readCalls[1]) != 1 {
		sizes := make([]int, len(git.readCalls))
		for i, c := range git.readCalls {
			sizes[i] = len(c)
		}
		t.Errorf("ReadBlobs call sizes = %v, want [500 1]", sizes)
	}
	if got["files_read"] != 501 || got["events_written"] != 501 {
		t.Errorf("files_read, events_written = %d, %d, want 501, 501", got["files_read"], got["events_written"])
	}
	if u := onlyApply(t, ts); len(u.Intervals) != 501 {
		t.Errorf("Intervals = %d, want 501", len(u.Intervals))
	}
}

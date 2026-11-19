package timeline

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

func day(n int) time.Time { return time.Date(2026, 3, n, 12, 0, 0, 0, time.UTC) }

func commit(sha string, n int, paths ...string) model.Commit {
	return model.Commit{SHA: sha, CommittedAt: day(n), Subject: "commit " + sha, Paths: paths}
}

func pkg(purl, eco string) model.Package {
	return model.Package{PURL: purl, Ecosystem: eco, Name: purl}
}

func dep(purl, version, req string, direct model.Directness, scope string) model.Dependency {
	return model.Dependency{Package: pkg(purl, "golang"), Version: version, Requirement: req, Direct: direct, Scope: scope}
}

func event(c model.Commit, path string, d model.Dependency, change model.Change, prev string) model.DepEvent {
	return model.DepEvent{
		CommitSHA: c.SHA, CommittedAt: c.CommittedAt, ManifestPath: path, Package: d.Package,
		Change: change, Version: d.Version, Requirement: d.Requirement, Direct: d.Direct,
		Scope: d.Scope, PrevVersion: prev,
	}
}

func interval(path string, d model.Dependency, from model.Commit, to *model.Commit) model.DepInterval {
	iv := model.DepInterval{
		ManifestPath: path, Package: d.Package, Version: d.Version, Requirement: d.Requirement,
		Direct: d.Direct, FromSHA: from.SHA, From: from.CommittedAt,
	}
	if to != nil {
		iv.ToSHA = to.SHA
		iv.To = to.CommittedAt
	}
	return iv
}

func TestDiff(t *testing.T) {
	t.Parallel()
	c := commit("c2", 2, "go.mod")
	aV1 := dep("pkg:golang/a", "v1.0.0", "v1.0.0", model.DirectYes, "runtime")
	aV2 := dep("pkg:golang/a", "v1.1.0", "v1.1.0", model.DirectYes, "runtime")
	bV1 := dep("pkg:golang/b", "v1.0.0", "v1.0.0", model.DirectNo, "")
	reqOld := dep("pkg:pypi/c", "", ">=1.0", model.DirectYes, "")
	reqNew := dep("pkg:pypi/c", "", ">=2.0", model.DirectYes, "")
	aIndirect := dep("pkg:golang/a", "v1.0.0", "v1.0.0", model.DirectNo, "runtime")
	aUnknown := dep("pkg:golang/a", "v1.0.0", "v1.0.0", model.DirectUnknown, "runtime")
	aTestScope := dep("pkg:golang/a", "v1.0.0", "v1.0.0", model.DirectYes, "test")
	aIndirectTest := dep("pkg:golang/a", "v1.0.0", "v1.0.0", model.DirectNo, "test")

	tests := []struct {
		name      string
		prev      repoState
		changes   []manifestChange
		want      []model.DepEvent
		wantState repoState
	}{
		{
			name:      "added",
			prev:      repoState{},
			changes:   []manifestChange{{Path: "go.mod", Deps: []model.Dependency{aV1}}},
			want:      []model.DepEvent{event(c, "go.mod", aV1, model.ChangeAdded, "")},
			wantState: repoState{"go.mod": {"pkg:golang/a": aV1}},
		},
		{
			name:      "removed carries previous values",
			prev:      repoState{"go.mod": {"pkg:golang/a": aV1, "pkg:golang/b": bV1}},
			changes:   []manifestChange{{Path: "go.mod", Deps: []model.Dependency{aV1}}},
			want:      []model.DepEvent{event(c, "go.mod", bV1, model.ChangeRemoved, "v1.0.0")},
			wantState: repoState{"go.mod": {"pkg:golang/a": aV1}},
		},
		{
			name:      "changed version",
			prev:      repoState{"go.mod": {"pkg:golang/a": aV1}},
			changes:   []manifestChange{{Path: "go.mod", Deps: []model.Dependency{aV2}}},
			want:      []model.DepEvent{event(c, "go.mod", aV2, model.ChangeChanged, "v1.0.0")},
			wantState: repoState{"go.mod": {"pkg:golang/a": aV2}},
		},
		{
			name:      "changed requirement only",
			prev:      repoState{"requirements.txt": {"pkg:pypi/c": reqOld}},
			changes:   []manifestChange{{Path: "requirements.txt", Deps: []model.Dependency{reqNew}}},
			want:      []model.DepEvent{event(c, "requirements.txt", reqNew, model.ChangeChanged, "")},
			wantState: repoState{"requirements.txt": {"pkg:pypi/c": reqNew}},
		},
		{
			name:      "direct only change emits changed",
			prev:      repoState{"go.mod": {"pkg:golang/a": aIndirect}},
			changes:   []manifestChange{{Path: "go.mod", Deps: []model.Dependency{aV1}}},
			want:      []model.DepEvent{event(c, "go.mod", aV1, model.ChangeChanged, "v1.0.0")},
			wantState: repoState{"go.mod": {"pkg:golang/a": aV1}},
		},
		{
			name:      "direct to unknown emits changed",
			prev:      repoState{"go.mod": {"pkg:golang/a": aV1}},
			changes:   []manifestChange{{Path: "go.mod", Deps: []model.Dependency{aUnknown}}},
			want:      []model.DepEvent{event(c, "go.mod", aUnknown, model.ChangeChanged, "v1.0.0")},
			wantState: repoState{"go.mod": {"pkg:golang/a": aUnknown}},
		},
		{
			name:      "scope only change emits nothing and keeps state",
			prev:      repoState{"go.mod": {"pkg:golang/a": aV1}},
			changes:   []manifestChange{{Path: "go.mod", Deps: []model.Dependency{aTestScope}}},
			want:      nil,
			wantState: repoState{"go.mod": {"pkg:golang/a": aV1}},
		},
		{
			name:      "direct and scope change emits changed with the new scope",
			prev:      repoState{"go.mod": {"pkg:golang/a": aV1}},
			changes:   []manifestChange{{Path: "go.mod", Deps: []model.Dependency{aIndirectTest}}},
			want:      []model.DepEvent{event(c, "go.mod", aIndirectTest, model.ChangeChanged, "v1.0.0")},
			wantState: repoState{"go.mod": {"pkg:golang/a": aIndirectTest}},
		},
		{
			name: "deleted manifest removes every package",
			prev: repoState{"go.mod": {"pkg:golang/a": aV1, "pkg:golang/b": bV1}},
			changes: []manifestChange{
				{Path: "go.mod", Deleted: true},
			},
			want: []model.DepEvent{
				event(c, "go.mod", aV1, model.ChangeRemoved, "v1.0.0"),
				event(c, "go.mod", bV1, model.ChangeRemoved, "v1.0.0"),
			},
			wantState: repoState{},
		},
		{
			name: "ordering within a commit by path then package",
			prev: repoState{"z/go.mod": {"pkg:golang/b": bV1}},
			changes: []manifestChange{
				{Path: "z/go.mod", Deps: []model.Dependency{aV1}},
				{Path: "go.mod", Deps: []model.Dependency{bV1, aV1}},
			},
			want: []model.DepEvent{
				event(c, "go.mod", aV1, model.ChangeAdded, ""),
				event(c, "go.mod", bV1, model.ChangeAdded, ""),
				event(c, "z/go.mod", aV1, model.ChangeAdded, ""),
				event(c, "z/go.mod", bV1, model.ChangeRemoved, "v1.0.0"),
			},
			wantState: repoState{
				"go.mod":   {"pkg:golang/a": aV1, "pkg:golang/b": bV1},
				"z/go.mod": {"pkg:golang/a": aV1},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := tt.prev
			got := diffCommit(c, st, tt.changes)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("diffCommit events = %+v, want %+v", got, tt.want)
			}
			if !reflect.DeepEqual(st, tt.wantState) {
				t.Errorf("diffCommit state = %+v, want %+v", st, tt.wantState)
			}
		})
	}
}

func TestFold(t *testing.T) {
	t.Parallel()
	c1, c2, c3, c4 := commit("c1", 1), commit("c2", 2), commit("c3", 3), commit("c4", 4)
	v1 := dep("pkg:golang/a", "v1.0.0", "v1.0.0", model.DirectYes, "")
	v2 := dep("pkg:golang/a", "v1.1.0", "v1.1.0", model.DirectNo, "")
	v3 := dep("pkg:golang/a", "v1.2.0", "v1.2.0", model.DirectYes, "")

	tests := []struct {
		name   string
		events []model.DepEvent
		want   []model.DepInterval
	}{
		{
			name: "add change change remove",
			events: []model.DepEvent{
				event(c1, "go.mod", v1, model.ChangeAdded, ""),
				event(c2, "go.mod", v2, model.ChangeChanged, "v1.0.0"),
				event(c3, "go.mod", v3, model.ChangeChanged, "v1.1.0"),
				event(c4, "go.mod", v3, model.ChangeRemoved, "v1.2.0"),
			},
			want: []model.DepInterval{
				interval("go.mod", v1, c1, &c2),
				interval("go.mod", v2, c2, &c3),
				interval("go.mod", v3, c3, &c4),
			},
		},
		{
			name: "two manifests pinning the same package are independent",
			events: []model.DepEvent{
				event(c1, "svc/go.mod", v1, model.ChangeAdded, ""),
				event(c1, "go.mod", v2, model.ChangeAdded, ""),
				event(c2, "svc/go.mod", v3, model.ChangeChanged, "v1.0.0"),
			},
			want: []model.DepInterval{
				interval("go.mod", v2, c1, nil),
				interval("svc/go.mod", v1, c1, &c2),
				interval("svc/go.mod", v3, c2, nil),
			},
		},
		{
			name: "remove then re-add",
			events: []model.DepEvent{
				event(c1, "go.mod", v1, model.ChangeAdded, ""),
				event(c2, "go.mod", v1, model.ChangeRemoved, "v1.0.0"),
				event(c3, "go.mod", v1, model.ChangeAdded, ""),
			},
			want: []model.DepInterval{
				interval("go.mod", v1, c1, &c2),
				interval("go.mod", v1, c3, nil),
			},
		},
		{
			name:   "changed without prior added opens one interval",
			events: []model.DepEvent{event(c2, "go.mod", v2, model.ChangeChanged, "v1.0.0")},
			want:   []model.DepInterval{interval("go.mod", v2, c2, nil)},
		},
		{
			name: "added while open closes then opens",
			events: []model.DepEvent{
				event(c1, "go.mod", v1, model.ChangeAdded, ""),
				event(c2, "go.mod", v2, model.ChangeAdded, ""),
			},
			want: []model.DepInterval{
				interval("go.mod", v1, c1, &c2),
				interval("go.mod", v2, c2, nil),
			},
		},
		{
			name:   "removed without open interval is ignored",
			events: []model.DepEvent{event(c1, "go.mod", v1, model.ChangeRemoved, "v1.0.0")},
			want:   nil,
		},
		{
			name: "ordered by package then manifest then from",
			events: []model.DepEvent{
				event(c1, "go.mod", dep("pkg:golang/z", "v1", "v1", 0, ""), model.ChangeAdded, ""),
				event(c1, "b/go.mod", v1, model.ChangeAdded, ""),
				event(c2, "a/go.mod", v1, model.ChangeAdded, ""),
			},
			want: []model.DepInterval{
				interval("a/go.mod", v1, c2, nil),
				interval("b/go.mod", v1, c1, nil),
				interval("go.mod", dep("pkg:golang/z", "v1", "v1", 0, ""), c1, nil),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := fold(tt.events)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fold() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestStateFromEvents(t *testing.T) {
	t.Parallel()
	c1, c2, c3 := commit("c1", 1), commit("c2", 2), commit("c3", 3)
	a1 := dep("pkg:golang/a", "v1.0.0", "v1.0.0", model.DirectYes, "runtime")
	a2 := dep("pkg:golang/a", "v1.1.0", "v1.1.0", model.DirectNo, "")
	b1 := dep("pkg:golang/b", "v1.0.0", "v1.0.0", model.DirectUnknown, "")

	tests := []struct {
		name   string
		events []model.DepEvent
		want   repoState
	}{
		{name: "no events", events: nil, want: repoState{}},
		{
			name: "latest event wins",
			events: []model.DepEvent{
				event(c1, "go.mod", a1, model.ChangeAdded, ""),
				event(c2, "go.mod", a2, model.ChangeChanged, "v1.0.0"),
			},
			want: repoState{"go.mod": {"pkg:golang/a": a2}},
		},
		{
			name: "removed drops the package and an emptied manifest",
			events: []model.DepEvent{
				event(c1, "go.mod", a1, model.ChangeAdded, ""),
				event(c1, "Dockerfile", b1, model.ChangeAdded, ""),
				event(c2, "go.mod", a1, model.ChangeRemoved, "v1.0.0"),
			},
			want: repoState{"Dockerfile": {"pkg:golang/b": b1}},
		},
		{
			name: "re-added after removal",
			events: []model.DepEvent{
				event(c1, "go.mod", a1, model.ChangeAdded, ""),
				event(c2, "go.mod", a1, model.ChangeRemoved, "v1.0.0"),
				event(c3, "go.mod", a2, model.ChangeAdded, ""),
			},
			want: repoState{"go.mod": {"pkg:golang/a": a2}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := stateFromEvents(tt.events)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("stateFromEvents() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestFilterDeps(t *testing.T) {
	t.Parallel()
	goA := dep("pkg:golang/a", "v1", "v1", 0, "")
	goA2 := dep("pkg:golang/a", "v2", "v2", 0, "")
	img := model.Dependency{Package: pkg("pkg:docker/golang", "docker"), Version: "1.26"}

	tests := []struct {
		name       string
		deps       []model.Dependency
		ecosystems []string
		want       []model.Dependency
	}{
		{name: "no allowlist keeps all", deps: []model.Dependency{goA, img}, want: []model.Dependency{goA, img}},
		{name: "allowlist drops others", deps: []model.Dependency{goA, img}, ecosystems: []string{"golang"}, want: []model.Dependency{goA}},
		{name: "duplicate keeps first", deps: []model.Dependency{goA, goA2}, want: []model.Dependency{goA}},
		{name: "nothing allowed", deps: []model.Dependency{goA}, ecosystems: []string{"pypi"}, want: []model.Dependency{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := filterDeps(tt.deps, tt.ecosystems)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("filterDeps() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDiffThenFold_IndirectBecomesDirect(t *testing.T) {
	t.Parallel()
	c1, c2 := commit("c1", 1, "go.mod"), commit("c2", 2, "go.mod")
	aIndirect := dep("pkg:golang/a", "v1.0.0", "v1.0.0", model.DirectNo, "")
	aDirect := dep("pkg:golang/a", "v1.0.0", "v1.0.0", model.DirectYes, "")

	st := repoState{}
	var events []model.DepEvent
	events = append(events, diffCommit(c1, st, []manifestChange{{Path: "go.mod", Deps: []model.Dependency{aIndirect}}})...)
	events = append(events, diffCommit(c2, st, []manifestChange{{Path: "go.mod", Deps: []model.Dependency{aDirect}}})...)

	got := fold(events)
	want := []model.DepInterval{
		interval("go.mod", aIndirect, c1, &c2),
		interval("go.mod", aDirect, c2, nil),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fold(diffCommit(...)) = %+v, want %+v", got, want)
	}
	if got[1].Direct != model.DirectYes {
		t.Errorf("second interval Direct = %v, want DirectYes", got[1].Direct)
	}
}

func TestExcludedPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path     string
		prefixes []string
		want     bool
	}{
		{path: "go.mod", want: false},
		{path: "internal/x/testdata/uv.lock", want: true},
		{path: "testdata/go.mod", want: true},
		{path: "vendor/github.com/y/go.mod", want: true},
		{path: "web/node_modules/z/package-lock.json", want: true},
		{path: "a/b/vendor/c/go.mod", want: true},
		// Only directory segments count, not the file name.
		{path: "testdata", want: false},
		{path: "src/vendor", want: false},
		// Names that merely contain a skipped name are not skipped.
		{path: "mytestdata/go.mod", want: false},
		{path: "vendored/go.mod", want: false},
		{path: "Vendor/go.mod", want: false},
		{path: "examples/demo/go.mod", prefixes: []string{"examples"}, want: true},
		{path: "examples/go.mod", prefixes: []string{"examples"}, want: true},
		{path: "examples/go.mod", prefixes: []string{"examples/"}, want: true},
		{path: "examples/a/go.mod", prefixes: []string{"examples/"}, want: true},
		{path: "examples2/go.mod", prefixes: []string{"examples"}, want: false},
		{path: "examples2/go.mod", prefixes: []string{"examples/"}, want: false},
		{path: "tools/legacy/go.mod", prefixes: []string{"tools/legacy"}, want: true},
		{path: "tools/go.mod", prefixes: []string{"tools/legacy"}, want: false},
		{path: "tools/legacy", prefixes: []string{"tools/legacy"}, want: true},
		{path: "docs/go.mod", prefixes: []string{"examples", "docs"}, want: true},
		{path: "go.mod", prefixes: []string{"examples"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.path+" "+strings.Join(tt.prefixes, ","), func(t *testing.T) {
			t.Parallel()
			if got := excludedPath(tt.path, tt.prefixes); got != tt.want {
				t.Errorf("excludedPath(%q, %q) = %v, want %v", tt.path, tt.prefixes, got, tt.want)
			}
		})
	}
}

func TestDropExcluded(t *testing.T) {
	t.Parallel()
	c1 := commit("c1", 1, "go.mod", "vendor/x/go.mod", "examples/go.mod")
	c2 := commit("c2", 2, "testdata/go.mod")
	c3 := commit("c3", 3, "Dockerfile")
	in := []model.Commit{c1, c2, c3}

	got, n := dropExcluded(in, []string{"examples"})
	want := []model.Commit{
		commit("c1", 1, "go.mod"),
		{SHA: "c2", CommittedAt: day(2), Subject: "commit c2", Paths: []string{}},
		c3,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dropExcluded() commits = %+v, want %+v", got, want)
	}
	if n != 3 {
		t.Errorf("dropExcluded() dropped = %d, want 3", n)
	}
	if want := []string{"go.mod", "vendor/x/go.mod", "examples/go.mod"}; !reflect.DeepEqual(in[0].Paths, want) {
		t.Errorf("input commit Paths = %v, want unchanged %v", in[0].Paths, want)
	}
	if got, n := dropExcluded(nil, nil); len(got) != 0 || n != 0 {
		t.Errorf("dropExcluded(nil) = %v, %d, want empty, 0", got, n)
	}
}

func TestFold_ClampsBackwardDates(t *testing.T) {
	t.Parallel()
	// c2 follows c1 on the chain but its committer clock was behind.
	c1, c2, c3 := commit("c1", 5), commit("c2", 3), commit("c3", 4)
	v1 := dep("pkg:golang/a", "v1.0.0", "v1.0.0", model.DirectYes, "")
	v2 := dep("pkg:golang/a", "v1.1.0", "v1.1.0", model.DirectYes, "")
	b1 := dep("pkg:golang/b", "v1.0.0", "v1.0.0", model.DirectYes, "")

	got := fold([]model.DepEvent{
		event(c1, "go.mod", v1, model.ChangeAdded, ""),
		event(c1, "go.mod", b1, model.ChangeAdded, ""),
		event(c2, "go.mod", v2, model.ChangeChanged, "v1.0.0"),
		event(c2, "go.mod", b1, model.ChangeRemoved, "v1.0.0"),
		event(c3, "go.mod", v2, model.ChangeRemoved, "v1.1.0"),
	})
	closedAtOpen := func(d model.Dependency, from, to model.Commit) model.DepInterval {
		iv := interval("go.mod", d, from, &to)
		iv.To = from.CommittedAt
		return iv
	}
	// Folded in the given order, never by date; the output is sorted by
	// opening time, so v1.1.0 (opened day 3) comes before v1.0.0 (day 5).
	want := []model.DepInterval{
		interval("go.mod", v2, c2, &c3),
		closedAtOpen(v1, c1, c2),
		closedAtOpen(b1, c1, c2),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fold() =\n%+v\nwant\n%+v", got, want)
	}
	for _, iv := range got {
		if iv.To.Before(iv.From) {
			t.Errorf("interval %+v ends before it starts", iv)
		}
	}
}

func TestUnionPatterns(t *testing.T) {
	t.Parallel()
	exts := []Extractor{
		&fakeExtractor{patterns: []string{"Packages/manifest.json", "**/go.mod"}},
		&fakeExtractor{patterns: []string{"**/go.mod", "**/Dockerfile"}},
	}
	got := unionPatterns(exts)
	want := []string{"Packages/manifest.json", "**/go.mod", "**/Dockerfile"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unionPatterns() = %v, want %v", got, want)
	}
}

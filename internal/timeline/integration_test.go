package timeline_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/extract"
	"github.com/Amrit-Code/upstreamwatch/internal/extract/unity"
	"github.com/Amrit-Code/upstreamwatch/internal/gitsrc"
	"github.com/Amrit-Code/upstreamwatch/internal/ingest"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/pipeline"
	"github.com/Amrit-Code/upstreamwatch/internal/store"
	"github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo"
	"github.com/Amrit-Code/upstreamwatch/internal/timeline"
)

// These tests run the ingest and timeline jobs end to end in-process: the
// real gitsrc.CLI syncing fixture repositories over file:// URLs, the real
// extractors, a real SQLite file opened and migrated as start-up does it, and
// both services run through pipeline.Runner so the runs table and job_failure
// findings are written as in production. Fixture commits carry fixed dates,
// so intervals are asserted exactly.

// day returns a fixed time on day n of March 2026.
func day(n int) time.Time { return time.Date(2026, 3, n, 12, 0, 0, 0, time.UTC) }

// fetchTime is the ingest clock: every fetch attempt is recorded at it.
var fetchTime = time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)

// target is one configured target of a stack.
type target struct {
	slug   string
	url    string
	branch string
	unity  bool
}

// stack is the whole ingest and timeline stack over one temp data directory.
type stack struct {
	t       *testing.T
	db      *store.DB
	git     *gitsrc.CLI
	derive  bool
	runner  *pipeline.Runner
	dataDir string
}

// newStack builds the stack for targets: a temp data directory, the store
// opened and migrated, one gitsrc.CLI allowing file:// URLs, the ingest and
// timeline services and a Runner with both jobs. derive turns on the
// golang base-image toolchain derivation.
func newStack(t *testing.T, derive bool, targets ...target) *stack {
	t.Helper()
	dataDir := t.TempDir()
	logger := slog.New(slog.DiscardHandler)
	db, err := store.Open(t.Context(), filepath.Join(dataDir, "upw.sqlite"), logger)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	s := &stack{
		t:      t,
		db:     db,
		derive: derive,
		git: gitsrc.New(gitsrc.Options{
			ReposDir:      filepath.Join(dataDir, "repos"),
			HomeDir:       filepath.Join(dataDir, "git-home"),
			AllowFileURLs: true,
		}, logger),
		dataDir: dataDir,
	}
	s.retarget(targets...)
	return s
}

// retarget rebuilds both services and the Runner for targets, keeping the
// store, the gitsrc.CLI and so the clones, as a restart with an edited
// config would.
func (s *stack) retarget(targets ...target) {
	s.t.Helper()
	logger := slog.New(slog.DiscardHandler)
	var repos []model.TargetRepo
	var settings []timeline.TargetSettings
	for _, tg := range targets {
		repos = append(repos, model.TargetRepo{Slug: tg.slug, URL: tg.url, Branch: tg.branch})
		settings = append(settings, timeline.TargetSettings{Slug: tg.slug, Unity: tg.unity})
	}
	ing := ingest.NewService(s.git, s.db, ingest.Options{
		Targets: repos,
		Now:     func() time.Time { return fetchTime },
	}, logger)
	tl := timeline.NewService(s.git, s.db, s.db,
		[]timeline.Extractor{extract.New(extract.Options{DeriveToolchainFromImages: s.derive}, logger)},
		unity.New(), timeline.Options{Targets: settings}, logger)
	r, err := pipeline.NewRunner([]pipeline.Job{ing, tl}, s.db, s.db, logger)
	if err != nil {
		s.t.Fatalf("NewRunner() error = %v", err)
	}
	s.runner = r
}

// run runs job for every target through the Runner and fails the test on
// any error.
func (s *stack) run(job string) model.Run {
	s.t.Helper()
	run, err := s.runner.Run(s.t.Context(), job, model.Scope{})
	if err != nil {
		s.t.Fatalf("Run(%q) error = %v", job, err)
	}
	if run.Status != model.RunSucceeded {
		s.t.Fatalf("Run(%q) status = %q, want %q", job, run.Status, model.RunSucceeded)
	}
	return run
}

// sync runs ingest then timeline and returns the timeline run.
func (s *stack) sync() model.Run {
	s.t.Helper()
	s.run("ingest")
	return s.run("timeline")
}

func (s *stack) repo(slug string) model.TargetRepo {
	s.t.Helper()
	r, err := s.db.GetRepo(s.t.Context(), slug)
	if err != nil {
		s.t.Fatalf("GetRepo(%q) error = %v", slug, err)
	}
	return r
}

func (s *stack) events(slug string) []model.DepEvent {
	s.t.Helper()
	got, err := s.db.ListEvents(s.t.Context(), s.repo(slug).ID)
	if err != nil {
		s.t.Fatalf("ListEvents(%q) error = %v", slug, err)
	}
	return got
}

func (s *stack) intervals(slug string) []model.DepInterval {
	s.t.Helper()
	got, err := s.db.ListIntervals(s.t.Context(), s.repo(slug).ID)
	if err != nil {
		s.t.Fatalf("ListIntervals(%q) error = %v", slug, err)
	}
	return got
}

// latestRun returns the stored latest run of job.
func (s *stack) latestRun(job string) model.Run {
	s.t.Helper()
	runs, err := s.db.LatestRuns(s.t.Context())
	if err != nil {
		s.t.Fatalf("LatestRuns() error = %v", err)
	}
	for _, r := range runs {
		if r.Job == job {
			return r
		}
	}
	s.t.Fatalf("LatestRuns() = %+v, no run of job %q", runs, job)
	return model.Run{}
}

func (s *stack) openFindings() []model.Finding {
	s.t.Helper()
	got, err := s.db.ListOpenFindings(s.t.Context())
	if err != nil {
		s.t.Fatalf("ListOpenFindings() error = %v", err)
	}
	return got
}

// ev is the part of a stored event the tests compare.
type ev struct {
	SHA     string
	At      time.Time
	Path    string
	PURL    string
	Change  model.Change
	Version string
	Prev    string
}

func evs(events []model.DepEvent) []ev {
	var out []ev
	for _, e := range events {
		out = append(out, ev{e.CommitSHA, e.CommittedAt, e.ManifestPath, e.Package.PURL, e.Change, e.Version, e.PrevVersion})
	}
	return out
}

// iv is the part of a stored interval the tests compare.
type iv struct {
	Path    string
	PURL    string
	Version string
	FromSHA string
	From    time.Time
	ToSHA   string
	To      time.Time
}

func ivs(intervals []model.DepInterval) []iv {
	var out []iv
	for _, i := range intervals {
		out = append(out, iv{i.ManifestPath, i.Package.PURL, i.Version, i.FromSHA, i.From, i.ToSHA, i.To})
	}
	return out
}

// ofPURL keeps the intervals of one package.
func ofPURL(intervals []model.DepInterval, purl string) []model.DepInterval {
	var out []model.DepInterval
	for _, i := range intervals {
		if i.Package.PURL == purl {
			out = append(out, i)
		}
	}
	return out
}

// checkStats checks the named counters of a stored run's stats; each must be
// present.
func checkStats(t *testing.T, run model.Run, want map[string]int64) {
	t.Helper()
	for k, w := range want {
		got, ok := run.Stats[k]
		if !ok {
			t.Errorf("run %d (%s) stats = %v, missing %q", run.ID, run.Job, run.Stats, k)
			continue
		}
		if got != w {
			t.Errorf("run %d (%s) stats[%q] = %d, want %d", run.ID, run.Job, k, got, w)
		}
	}
}

func goMod(requires ...string) string {
	var b strings.Builder
	b.WriteString("module example.com/app\n")
	if len(requires) > 0 {
		b.WriteString("\nrequire (\n")
		for _, r := range requires {
			b.WriteString("\t" + r + "\n")
		}
		b.WriteString(")\n")
	}
	return b.String()
}

const purlA = "pkg:golang/example.com/a"

func TestTimeline_FirstParentSemantics(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "main")
	up.Write("go.mod", goMod("example.com/a v1.0.0"))
	c1 := up.Commit("add a", day(1))

	// A branch that bumps a and reverts the bump before it is merged.
	up.Branch("bump-and-revert")
	up.Write("go.mod", goMod("example.com/a v1.1.0"))
	bump := up.Commit("bump a", day(2))
	up.Write("go.mod", goMod("example.com/a v1.0.0"))
	revert := up.Commit("revert a", day(2).Add(time.Hour))
	up.Switch("main")
	merge1 := up.Merge("bump-and-revert", "merge bump-and-revert", day(3))

	// A branch whose bump lands: the change is dated at the merge.
	up.Branch("bump")
	up.Write("go.mod", goMod("example.com/a v1.2.0"))
	bump2 := up.Commit("bump a to v1.2.0", day(4))
	up.Switch("main")
	merge2 := up.Merge("bump", "merge bump", day(5))

	s := newStack(t, true, target{slug: "app", url: up.URL(), branch: "main"})
	run := s.sync()
	checkStats(t, run, map[string]int64{"failed": 0, "resets": 0, "events_written": 2})

	gotEvents := s.events("app")
	wantEvents := []ev{
		{c1, day(1), "go.mod", purlA, model.ChangeAdded, "v1.0.0", ""},
		{merge2, day(5), "go.mod", purlA, model.ChangeChanged, "v1.2.0", "v1.0.0"},
	}
	if got := evs(gotEvents); !reflect.DeepEqual(got, wantEvents) {
		t.Errorf("events =\n%+v\nwant\n%+v", got, wantEvents)
	}
	for _, e := range gotEvents {
		switch e.CommitSHA {
		case bump, revert, merge1, bump2:
			t.Errorf("event at branch-side or no-op merge commit %s: %+v", e.CommitSHA, e)
		}
	}

	wantIntervals := []iv{
		{"go.mod", purlA, "v1.0.0", c1, day(1), merge2, day(5)},
		{"go.mod", purlA, "v1.2.0", merge2, day(5), "", time.Time{}},
	}
	if got := ivs(s.intervals("app")); !reflect.DeepEqual(got, wantIntervals) {
		t.Errorf("intervals =\n%+v\nwant\n%+v", got, wantIntervals)
	}
	if got := s.repo("app").LastIndexedSHA; got != merge2 {
		t.Errorf("LastIndexedSHA = %s, want the branch head %s", got, merge2)
	}
}

func TestTimeline_SecondRunDoesZeroWork(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "main")
	up.Write("go.mod", goMod("example.com/a v1.0.0", "example.com/b v1.4.0"))
	up.Commit("first", day(1))
	up.Write("go.mod", goMod("example.com/a v1.1.0", "example.com/b v1.4.0"))
	head := up.Commit("bump a", day(2))

	s := newStack(t, true, target{slug: "app", url: up.URL(), branch: "main"})
	first := s.sync()
	checkStats(t, first, map[string]int64{"commits_walked": 2, "events_written": 3})
	events := s.events("app")
	intervals := s.intervals("app")
	if len(events) != 3 || len(intervals) != 3 {
		t.Fatalf("after first run: %d events, %d intervals, want 3 and 3", len(events), len(intervals))
	}

	ingestRun := s.run("ingest")
	checkStats(t, ingestRun, map[string]int64{"targets": 1, "fetched": 1, "cloned": 0, "failed": 0})
	second := s.run("timeline")
	if second.ID == first.ID {
		t.Fatalf("second timeline run has the first run's ID %d", first.ID)
	}

	stored := s.latestRun("timeline")
	if stored.ID != second.ID || stored.Status != model.RunSucceeded {
		t.Errorf("latest timeline run = %+v, want run %d succeeded", stored, second.ID)
	}
	checkStats(t, stored, map[string]int64{"commits_walked": 0, "events_written": 0, "resets": 0, "failed": 0})
	if got := s.events("app"); !reflect.DeepEqual(got, events) {
		t.Errorf("events after second run =\n%+v\nwant unchanged\n%+v", got, events)
	}
	if got := s.intervals("app"); !reflect.DeepEqual(got, intervals) {
		t.Errorf("intervals after second run =\n%+v\nwant unchanged (same IDs)\n%+v", got, intervals)
	}
	if got := s.repo("app").LastIndexedSHA; got != head {
		t.Errorf("LastIndexedSHA = %s, want %s", got, head)
	}
}

func TestTimeline_IncrementalKeepsIntervalIDs(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "main")
	up.Write("go.mod", goMod("example.com/a v1.0.0", "example.com/b v1.4.0"))
	c1 := up.Commit("first", day(1))
	up.Write("requirements.txt", "requests==2.31.0\n")
	c2 := up.Commit("add requirements", day(2))

	s := newStack(t, true, target{slug: "app", url: up.URL(), branch: "main"})
	s.sync()
	beforeEvents := s.events("app")
	before := s.intervals("app")
	if len(before) != 3 {
		t.Fatalf("after first run intervals = %+v, want 3", before)
	}

	up.Write("go.mod", goMod("example.com/a v1.0.0", "example.com/b v1.5.0"))
	c3 := up.Commit("bump b", day(3))
	run := s.sync()
	checkStats(t, run, map[string]int64{"commits_walked": 1, "events_written": 1, "resets": 0})

	gotEvents := s.events("app")
	if len(gotEvents) != len(beforeEvents)+1 {
		t.Fatalf("events after bump = %+v, want the %d earlier ones and one more", gotEvents, len(beforeEvents))
	}
	if !reflect.DeepEqual(gotEvents[:len(beforeEvents)], beforeEvents) {
		t.Errorf("earlier events changed:\n%+v\nwant\n%+v", gotEvents[:len(beforeEvents)], beforeEvents)
	}
	wantNew := ev{c3, day(3), "go.mod", "pkg:golang/example.com/b", model.ChangeChanged, "v1.5.0", "v1.4.0"}
	if got := evs(gotEvents[len(beforeEvents):])[0]; got != wantNew {
		t.Errorf("new event = %+v, want %+v", got, wantNew)
	}

	after := s.intervals("app")
	byKey := func(list []model.DepInterval, purl, from string) (model.DepInterval, bool) {
		for _, i := range list {
			if i.Package.PURL == purl && i.FromSHA == from {
				return i, true
			}
		}
		return model.DepInterval{}, false
	}
	// The unchanged packages keep their rows exactly.
	for _, k := range []struct{ purl, from string }{
		{purlA, c1},
		{"pkg:pypi/requests", c2},
	} {
		old, ok1 := byKey(before, k.purl, k.from)
		cur, ok2 := byKey(after, k.purl, k.from)
		if !ok1 || !ok2 {
			t.Errorf("interval of %s from %s: before found=%v, after found=%v, want both", k.purl, k.from, ok1, ok2)
			continue
		}
		if !reflect.DeepEqual(cur, old) {
			t.Errorf("interval of %s = %+v, want unchanged %+v", k.purl, cur, old)
		}
	}
	// The bumped package's old interval keeps its ID and is closed at c3.
	oldB, ok1 := byKey(before, "pkg:golang/example.com/b", c1)
	curB, ok2 := byKey(after, "pkg:golang/example.com/b", c1)
	if !ok1 || !ok2 {
		t.Fatalf("old interval of b: before found=%v, after found=%v, want both", ok1, ok2)
	}
	if curB.ID != oldB.ID {
		t.Errorf("old interval of b ID = %d, want kept %d", curB.ID, oldB.ID)
	}
	if !oldB.IsOpen() {
		t.Errorf("old interval of b before the bump = %+v, want open", oldB)
	}
	if curB.ToSHA != c3 || !curB.To.Equal(day(3)) || curB.Version != "v1.4.0" {
		t.Errorf("old interval of b after the bump = %+v, want v1.4.0 closed at %s (%v)", curB, c3, day(3))
	}
	newB, ok := byKey(after, "pkg:golang/example.com/b", c3)
	if !ok || !newB.IsOpen() || newB.Version != "v1.5.0" || !newB.From.Equal(day(3)) {
		t.Errorf("new interval of b = %+v (found %v), want open v1.5.0 from %v", newB, ok, day(3))
	}
	if newB.ID == oldB.ID {
		t.Errorf("new interval of b reuses ID %d of the closed one", newB.ID)
	}
	if len(after) != 4 {
		t.Errorf("intervals after bump = %+v, want 4", ivs(after))
	}
}

func TestTimeline_FetchFailureKeepsLastGoodCommit(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "main")
	up.Write("go.mod", goMod("example.com/a v1.0.0"))
	up.Commit("first", day(1))
	up.Write("go.mod", goMod("example.com/a v1.1.0"))
	good := up.Commit("bump a", day(2))

	s := newStack(t, true, target{slug: "app", url: up.URL(), branch: "main"})
	s.sync()
	events := s.events("app")
	intervals := s.intervals("app")
	if s.repo("app").LastIndexedSHA != good {
		t.Fatalf("LastIndexedSHA after the good run = %s, want %s", s.repo("app").LastIndexedSHA, good)
	}

	// The upstream gains a commit, then the target points at a path that
	// does not exist.
	up.Write("go.mod", goMod("example.com/a v1.2.0"))
	unseen := up.Commit("bump a again", day(3))
	missing := "file://" + filepath.ToSlash(filepath.Join(t.TempDir(), "no-such-repo"))
	s.retarget(target{slug: "app", url: missing, branch: "main"})

	failed, err := s.runner.Run(t.Context(), "ingest", model.Scope{})
	if err == nil {
		t.Fatal("Run(ingest) on a missing repository: error = nil, want the fetch failure")
	}
	stored := s.latestRun("ingest")
	if stored.ID != failed.ID || stored.Status != model.RunFailed || stored.Error == "" {
		t.Errorf("latest ingest run = %+v, want run %d failed with an error", stored, failed.ID)
	}
	checkStats(t, stored, map[string]int64{"targets": 1, "fetched": 0, "failed": 1})

	findings := s.openFindings()
	var jf []model.Finding
	for _, f := range findings {
		if f.Kind == model.KindJobFailure {
			jf = append(jf, f)
		}
	}
	if len(jf) != 1 || jf[0].Fingerprint != pipeline.JobFailureFingerprint("ingest") || jf[0].State.State != model.StateOpen {
		t.Errorf("open job_failure findings = %+v, want one open finding for ingest", jf)
	}

	repo := s.repo("app")
	if repo.LastFetchError == "" {
		t.Errorf("repo LastFetchError is empty, want the fetch failure")
	}
	if !repo.LastFetchAt.Equal(fetchTime) {
		t.Errorf("repo LastFetchAt = %v, want %v", repo.LastFetchAt, fetchTime)
	}
	if repo.ClonePath == "" || repo.DefaultBranch != "main" {
		t.Errorf("repo clone path %q, branch %q: want the last good values kept", repo.ClonePath, repo.DefaultBranch)
	}

	tl := s.run("timeline")
	checkStats(t, tl, map[string]int64{"failed": 0, "commits_walked": 0, "events_written": 0})
	repo = s.repo("app")
	if repo.LastIndexedSHA != good {
		t.Errorf("LastIndexedSHA after the failed fetch = %s, want the last good %s (not %s)", repo.LastIndexedSHA, good, unseen)
	}
	if got := s.events("app"); !reflect.DeepEqual(got, events) {
		t.Errorf("events after the failed fetch =\n%+v\nwant unchanged\n%+v", got, events)
	}
	if got := s.intervals("app"); !reflect.DeepEqual(got, intervals) {
		t.Errorf("intervals after the failed fetch =\n%+v\nwant unchanged\n%+v", got, intervals)
	}

	// Pointing the target back at the repository recovers: the finding is
	// resolved, the error cleared and the new commit indexed.
	s.retarget(target{slug: "app", url: up.URL(), branch: "main"})
	s.sync()
	for _, f := range s.openFindings() {
		if f.Kind == model.KindJobFailure {
			t.Errorf("open finding after recovery: %+v, want the ingest job_failure resolved", f)
		}
	}
	repo = s.repo("app")
	if repo.LastFetchError != "" || repo.LastIndexedSHA != unseen {
		t.Errorf("after recovery LastFetchError = %q, LastIndexedSHA = %s; want empty and %s", repo.LastFetchError, repo.LastIndexedSHA, unseen)
	}
}

func TestTimeline_ManifestDeletedAndReadded(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "main")
	up.Write("go.mod", goMod("example.com/a v1.0.0"))
	up.Write("requirements.txt", "requests==2.31.0\nflask==3.0.0\n")
	c1 := up.Commit("first", day(1))
	up.Delete("requirements.txt")
	c2 := up.Commit("drop requirements", day(2))

	s := newStack(t, true, target{slug: "app", url: up.URL(), branch: "main"})
	run := s.sync()
	checkStats(t, run, map[string]int64{"commits_walked": 2, "events_written": 5})

	const flask, requests = "pkg:pypi/flask", "pkg:pypi/requests"
	wantEvents := []ev{
		{c1, day(1), "go.mod", purlA, model.ChangeAdded, "v1.0.0", ""},
		{c1, day(1), "requirements.txt", flask, model.ChangeAdded, "3.0.0", ""},
		{c1, day(1), "requirements.txt", requests, model.ChangeAdded, "2.31.0", ""},
		{c2, day(2), "requirements.txt", flask, model.ChangeRemoved, "3.0.0", "3.0.0"},
		{c2, day(2), "requirements.txt", requests, model.ChangeRemoved, "2.31.0", "2.31.0"},
	}
	if got := evs(s.events("app")); !reflect.DeepEqual(got, wantEvents) {
		t.Errorf("events after deletion =\n%+v\nwant\n%+v", got, wantEvents)
	}
	wantIntervals := []iv{
		{"go.mod", purlA, "v1.0.0", c1, day(1), "", time.Time{}},
		{"requirements.txt", flask, "3.0.0", c1, day(1), c2, day(2)},
		{"requirements.txt", requests, "2.31.0", c1, day(1), c2, day(2)},
	}
	if got := ivs(s.intervals("app")); !reflect.DeepEqual(got, wantIntervals) {
		t.Errorf("intervals after deletion =\n%+v\nwant\n%+v", got, wantIntervals)
	}

	up.Write("requirements.txt", "requests==2.32.0\nflask==3.0.0\n")
	c3 := up.Commit("restore requirements", day(3))
	run = s.sync()
	checkStats(t, run, map[string]int64{"commits_walked": 1, "events_written": 2})

	wantEvents = append(wantEvents,
		ev{c3, day(3), "requirements.txt", flask, model.ChangeAdded, "3.0.0", ""},
		ev{c3, day(3), "requirements.txt", requests, model.ChangeAdded, "2.32.0", ""},
	)
	if got := evs(s.events("app")); !reflect.DeepEqual(got, wantEvents) {
		t.Errorf("events after re-adding =\n%+v\nwant\n%+v", got, wantEvents)
	}
	wantIntervals = []iv{
		{"go.mod", purlA, "v1.0.0", c1, day(1), "", time.Time{}},
		{"requirements.txt", flask, "3.0.0", c1, day(1), c2, day(2)},
		{"requirements.txt", flask, "3.0.0", c3, day(3), "", time.Time{}},
		{"requirements.txt", requests, "2.31.0", c1, day(1), c2, day(2)},
		{"requirements.txt", requests, "2.32.0", c3, day(3), "", time.Time{}},
	}
	if got := ivs(s.intervals("app")); !reflect.DeepEqual(got, wantIntervals) {
		t.Errorf("intervals after re-adding =\n%+v\nwant\n%+v", got, wantIntervals)
	}
}

// ecosystemFiles is a fixture covering every non-Unity extractor.
var ecosystemFiles = map[string]string{
	"go.mod": "module example.com/app\n\ngo 1.26.1\n\ntoolchain go1.26.1\n\nrequire github.com/example/lib v1.2.0\n",
	// The third stage starts from the first; "build" is a stage, not an image.
	"Dockerfile": "FROM golang:1.26-alpine AS build\nRUN go build -o /out/app .\n\n" +
		"FROM build AS check\nRUN go vet ./...\n\n" +
		"FROM alpine:3.22\nCOPY --from=build /out/app /app\n",
	"docker-compose.yml": "services:\n  db:\n    image: postgres:16.4\n  cache:\n    image: redis:7.2-alpine\n",
	".github/workflows/ci.yml": "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - uses: actions/checkout@v4\n      - uses: actions/setup-go@v5\n",
	"pyproject.toml": "[project]\nname = \"example-app\"\nversion = \"0.1.0\"\ndependencies = [\"requests==2.31.0\", \"click>=8\"]\n",
	"poetry.lock": "[[package]]\nname = \"click\"\nversion = \"8.1.7\"\ndescription = \"\"\noptional = false\npython-versions = \">=3.7\"\nfiles = []\n\n" +
		"[[package]]\nname = \"requests\"\nversion = \"2.31.0\"\ndescription = \"\"\noptional = false\npython-versions = \">=3.7\"\nfiles = []\n\n" +
		"[metadata]\nlock-version = \"2.0\"\npython-versions = \"^3.11\"\ncontent-hash = \"0\"\n",
}

func TestTimeline_Ecosystems(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "main")
	for path, content := range ecosystemFiles {
		up.Write(path, content)
	}
	c1 := up.Commit("all ecosystems", day(1))

	type pkgAt struct{ eco, name, path, version string }
	common := []pkgAt{
		{"golang", "github.com/example/lib", "go.mod", "v1.2.0"},
		{"golang", "toolchain", "go.mod", "1.26.1"},
		{"docker", "golang", "Dockerfile", "1.26-alpine"},
		{"docker", "alpine", "Dockerfile", "3.22"},
		{"docker", "postgres", "docker-compose.yml", "16.4"},
		{"docker", "redis", "docker-compose.yml", "7.2-alpine"},
		{"github-actions", "actions/checkout", ".github/workflows/ci.yml", "v4"},
		{"github-actions", "actions/setup-go", ".github/workflows/ci.yml", "v5"},
		{"pypi", "requests", "pyproject.toml", "2.31.0"},
		{"pypi", "click", "poetry.lock", "8.1.7"},
		{"pypi", "requests", "poetry.lock", "2.31.0"},
	}
	tests := []struct {
		name   string
		derive bool
		// toolchainPaths are the manifests pkg:golang/toolchain has an
		// interval in.
		toolchainPaths []string
	}{
		{"derivation on", true, []string{"Dockerfile", "go.mod"}},
		{"derivation off", false, []string{"go.mod"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newStack(t, tt.derive, target{slug: "eco", url: up.URL(), branch: "main"})
			run := s.sync()
			checkStats(t, run, map[string]int64{"parse_errors": 0, "files_skipped": 0, "files_read": int64(len(ecosystemFiles))})
			intervals := s.intervals("eco")

			for _, w := range common {
				found := false
				for _, i := range intervals {
					if i.Package.Ecosystem == w.eco && i.Package.Name == w.name && i.ManifestPath == w.path {
						found = true
						if i.Version != w.version || i.FromSHA != c1 || !i.From.Equal(day(1)) || !i.IsOpen() {
							t.Errorf("interval of %s %s in %s = %+v, want open %s from %s", w.eco, w.name, w.path, i, w.version, c1)
						}
					}
				}
				if !found {
					t.Errorf("no interval for %s package %s in %s", w.eco, w.name, w.path)
				}
			}

			toolchain := ofPURL(intervals, "pkg:golang/toolchain")
			var paths []string
			for _, i := range toolchain {
				paths = append(paths, i.ManifestPath)
				if i.ManifestPath == "Dockerfile" && i.Version != "1.26" {
					t.Errorf("Dockerfile toolchain interval version = %q, want %q", i.Version, "1.26")
				}
			}
			slices.Sort(paths)
			if !reflect.DeepEqual(paths, tt.toolchainPaths) {
				t.Errorf("pkg:golang/toolchain intervals in %q, want one each in %q", paths, tt.toolchainPaths)
			}

			for _, i := range intervals {
				if i.Package.PURL == "pkg:docker/build" || (i.Package.Ecosystem == "docker" && i.Package.Name == "build") {
					t.Errorf("build stage stored as a package: %+v", i)
				}
			}
			for _, e := range s.events("eco") {
				if e.Package.PURL == "pkg:docker/build" || (e.Package.Ecosystem == "docker" && e.Package.Name == "build") {
					t.Errorf("event for the build stage: %+v", e)
				}
			}
		})
	}
}

func TestTimeline_Unity(t *testing.T) {
	t.Parallel()
	const manifest = `{"dependencies": {"com.unity.textmeshpro": "3.0.6", "com.unity.ugui": "1.0.0"}}`
	const lock = `{"dependencies": {
  "com.unity.textmeshpro": {"version": "3.0.6", "depth": 0, "source": "registry", "dependencies": {"com.unity.ugui": "1.0.0"}},
  "com.unity.ugui": {"version": "1.0.0", "depth": 0, "source": "builtin", "dependencies": {}}
}}`
	up := gitrepo.New(t, "main")
	up.Write("ProjectSettings/ProjectVersion.txt", "m_EditorVersion: 2022.3.10f1\n")
	up.Write("Packages/manifest.json", manifest)
	up.Write("Packages/packages-lock.json", lock)
	up.Write(".github/workflows/ci.yml", "on: push\njobs:\n  b:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n")
	c1 := up.Commit("unity project", day(1))
	// A change to the file that leaves the editor version alone.
	up.Write("ProjectSettings/ProjectVersion.txt", "m_EditorVersion: 2022.3.10f1\nm_OtherSetting: 1\n")
	up.Commit("touch project version", day(2))
	up.Write("ProjectSettings/ProjectVersion.txt", "m_EditorVersion: 6000.0.23f1\nm_OtherSetting: 1\n")
	c3 := up.Commit("upgrade editor", day(3))

	const editor = "pkg:generic/unity-editor"
	const path = "ProjectSettings/ProjectVersion.txt"

	t.Run("unity on", func(t *testing.T) {
		t.Parallel()
		s := newStack(t, true, target{slug: "game", url: up.URL(), branch: "main", unity: true})
		s.sync()
		intervals := s.intervals("game")
		want := []iv{
			{path, editor, "2022.3.10f1", c1, day(1), c3, day(3)},
			{path, editor, "6000.0.23f1", c3, day(3), "", time.Time{}},
		}
		if got := ivs(ofPURL(intervals, editor)); !reflect.DeepEqual(got, want) {
			t.Errorf("unity-editor intervals =\n%+v\nwant\n%+v", got, want)
		}
		for _, k := range []struct{ path, purl string }{
			{"Packages/manifest.json", "pkg:upm/com.unity.textmeshpro"},
			{"Packages/packages-lock.json", "pkg:upm/com.unity.textmeshpro"},
			{"Packages/packages-lock.json", "pkg:upm/com.unity.ugui"},
		} {
			found := false
			for _, i := range ofPURL(intervals, k.purl) {
				if i.ManifestPath == k.path {
					found = true
				}
			}
			if !found {
				t.Errorf("no interval for %s in %s", k.purl, k.path)
			}
		}
	})

	t.Run("unity off", func(t *testing.T) {
		t.Parallel()
		s := newStack(t, true, target{slug: "game", url: up.URL(), branch: "main", unity: false})
		s.sync()
		intervals := s.intervals("game")
		indexed := false
		for _, i := range intervals {
			if i.ManifestPath == ".github/workflows/ci.yml" {
				indexed = true
			}
		}
		if !indexed {
			t.Errorf("intervals = %+v, want the workflow indexed", ivs(intervals))
		}
		for _, i := range intervals {
			if i.Package.PURL == editor || i.Package.Ecosystem == "upm" {
				t.Errorf("Unity interval with Unity off: %+v", i)
			}
		}
		for _, e := range s.events("game") {
			if e.Package.PURL == editor || e.Package.Ecosystem == "upm" {
				t.Errorf("Unity event with Unity off: %+v", e)
			}
		}
	})
}

func TestTimeline_RewrittenHistory(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "main")
	up.Write("go.mod", goMod("example.com/a v1.0.0"))
	old1 := up.Commit("first", day(1))
	up.Write("go.mod", goMod("example.com/a v1.1.0"))
	old2 := up.Commit("bump a", day(2))

	s := newStack(t, true, target{slug: "app", url: up.URL(), branch: "main"})
	s.sync()
	if n := len(s.events("app")); n != 2 {
		t.Fatalf("events before the rewrite = %d, want 2", n)
	}

	// Force main onto an unrelated history.
	up.Git("switch", "--quiet", "--orphan", "rewrite")
	up.Write("go.mod", goMod("example.com/c v0.3.0"))
	new1 := up.Commit("new root", day(10))
	up.Write("go.mod", goMod("example.com/c v0.4.0"))
	new2 := up.Commit("bump c", day(11))
	up.Git("branch", "--force", "main", "rewrite")
	up.Switch("main")
	up.Git("branch", "--delete", "--force", "rewrite")

	run := s.sync()
	checkStats(t, run, map[string]int64{"resets": 1, "failed": 0, "commits_walked": 2, "events_written": 2})

	const purlC = "pkg:golang/example.com/c"
	wantEvents := []ev{
		{new1, day(10), "go.mod", purlC, model.ChangeAdded, "v0.3.0", ""},
		{new2, day(11), "go.mod", purlC, model.ChangeChanged, "v0.4.0", "v0.3.0"},
	}
	gotEvents := s.events("app")
	if got := evs(gotEvents); !reflect.DeepEqual(got, wantEvents) {
		t.Errorf("events after the rewrite =\n%+v\nwant\n%+v", got, wantEvents)
	}
	for _, e := range gotEvents {
		if e.CommitSHA == old1 || e.CommitSHA == old2 {
			t.Errorf("event from the old history kept: %+v", e)
		}
	}
	wantIntervals := []iv{
		{"go.mod", purlC, "v0.3.0", new1, day(10), new2, day(11)},
		{"go.mod", purlC, "v0.4.0", new2, day(11), "", time.Time{}},
	}
	if got := ivs(s.intervals("app")); !reflect.DeepEqual(got, wantIntervals) {
		t.Errorf("intervals after the rewrite =\n%+v\nwant\n%+v", got, wantIntervals)
	}
	if got := s.repo("app").LastIndexedSHA; got != new2 {
		t.Errorf("LastIndexedSHA = %s, want %s", got, new2)
	}

	// The rebuilt timeline is incremental again on the next run.
	next := s.sync()
	checkStats(t, next, map[string]int64{"resets": 0, "commits_walked": 0, "events_written": 0})
}

// The clone directory is never the fixture: a sanity check that the stack
// reads history from its own bare clone, so deleting the fixture after a
// sync leaves the timeline buildable.
func TestTimeline_ReadsFromClone(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "main")
	up.Write("go.mod", goMod("example.com/a v1.0.0"))
	c1 := up.Commit("first", day(1))

	s := newStack(t, true, target{slug: "app", url: up.URL(), branch: "main"})
	s.run("ingest")
	if err := os.RemoveAll(up.Path()); err != nil {
		t.Fatalf("remove fixture: %v", err)
	}
	s.run("timeline")
	want := []iv{{"go.mod", purlA, "v1.0.0", c1, day(1), "", time.Time{}}}
	if got := ivs(s.intervals("app")); !reflect.DeepEqual(got, want) {
		t.Errorf("intervals = %+v, want %+v", got, want)
	}
	if clone := s.repo("app").ClonePath; !strings.HasPrefix(clone, s.dataDir) {
		t.Errorf("ClonePath = %q, want it under the data directory %q", clone, s.dataDir)
	}
}

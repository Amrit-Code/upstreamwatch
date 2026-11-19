package ingest_test

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
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

// These tests run the ingest job end to end in-process: the real gitsrc.CLI
// syncing fixture repositories over file:// URLs into a temp data directory,
// a real SQLite file opened and migrated as start-up does it, and the ingest
// and timeline services run through pipeline.Runner.

func day(n int) time.Time { return time.Date(2026, 3, n, 12, 0, 0, 0, time.UTC) }

var fetchTime = time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)

// fakeToken is a placeholder credential: never a real token.
const fakeToken = "example-token-not-a-secret-0123456789"

// stack is the ingest and timeline stack over one temp data directory.
type stack struct {
	t        *testing.T
	db       *store.DB
	runner   *pipeline.Runner
	reposDir string
}

// newStack builds the whole stack for targets: a temp data directory, the
// store opened and migrated, one gitsrc.CLI allowing file:// URLs, the
// ingest and timeline services and a Runner with both jobs.
func newStack(t *testing.T, targets ...model.TargetRepo) *stack {
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
	reposDir := filepath.Join(dataDir, "repos")
	git := gitsrc.New(gitsrc.Options{
		ReposDir:      reposDir,
		HomeDir:       filepath.Join(dataDir, "git-home"),
		AllowFileURLs: true,
	}, logger)

	var settings []timeline.TargetSettings
	for _, tg := range targets {
		settings = append(settings, timeline.TargetSettings{Slug: tg.Slug})
	}
	ing := ingest.NewService(git, db, ingest.Options{
		Targets: targets,
		Now:     func() time.Time { return fetchTime },
	}, logger)
	tl := timeline.NewService(git, db, db,
		[]timeline.Extractor{extract.New(extract.Options{DeriveToolchainFromImages: true}, logger)},
		unity.New(), timeline.Options{Targets: settings}, logger)
	r, err := pipeline.NewRunner([]pipeline.Job{ing, tl}, db, db, logger)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	return &stack{t: t, db: db, runner: r, reposDir: reposDir}
}

func (s *stack) repo(slug string) model.TargetRepo {
	s.t.Helper()
	r, err := s.db.GetRepo(s.t.Context(), slug)
	if err != nil {
		s.t.Fatalf("GetRepo(%q) error = %v", slug, err)
	}
	return r
}

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

func TestIngest_DefaultBranchAndBareClone(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "trunk")
	up.Write("go.mod", "module example.com/app\n\nrequire example.com/a v1.0.0\n")
	trunkHead := up.Commit("first", day(1))
	// Another branch with newer history: it must not be what gets indexed.
	up.Branch("main")
	up.Write("go.mod", "module example.com/app\n\nrequire example.com/a v1.9.0\n")
	mainHead := up.Commit("other branch", day(2))
	up.Switch("trunk")

	s := newStack(t, model.TargetRepo{Slug: "app", URL: up.URL(), Token: fakeToken})
	run, err := s.runner.Run(t.Context(), "ingest", model.Scope{})
	if err != nil {
		t.Fatalf("Run(ingest) error = %v", err)
	}
	stored := s.latestRun("ingest")
	if stored.ID != run.ID || stored.Status != model.RunSucceeded {
		t.Errorf("latest ingest run = %+v, want run %d succeeded", stored, run.ID)
	}
	checkStats(t, stored, map[string]int64{"targets": 1, "fetched": 1, "cloned": 1, "failed": 0})

	repo := s.repo("app")
	if repo.DefaultBranch != "trunk" {
		t.Errorf("stored DefaultBranch = %q, want the remote HEAD branch %q", repo.DefaultBranch, "trunk")
	}
	wantClone := filepath.Join(s.reposDir, "app.git")
	if repo.ClonePath != wantClone {
		t.Errorf("stored ClonePath = %q, want %q", repo.ClonePath, wantClone)
	}
	if repo.LastFetchError != "" || !repo.LastFetchAt.Equal(fetchTime) {
		t.Errorf("stored fetch error %q at %v, want none at %v", repo.LastFetchError, repo.LastFetchAt, fetchTime)
	}

	if got := gitrepo.Git(t, wantClone, "rev-parse", "--is-bare-repository"); got != "true" {
		t.Errorf("rev-parse --is-bare-repository = %q, want true", got)
	}
	if got := gitrepo.Git(t, wantClone, "rev-parse", "refs/heads/trunk"); got != trunkHead {
		t.Errorf("clone refs/heads/trunk = %s, want %s", got, trunkHead)
	}
	if _, err := os.Stat(filepath.Join(wantClone, "go.mod")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("clone has a working-tree file go.mod (stat error %v)", err)
	}

	cfg := gitrepo.Git(t, wantClone, "config", "--list")
	lower := strings.ToLower(cfg)
	for _, bad := range []string{"url", "credential", "extraheader", "authorization", strings.ToLower(up.Path())} {
		if strings.Contains(lower, bad) {
			t.Errorf("clone git config --list contains %q:\n%s", bad, cfg)
		}
	}

	// No file anywhere under the repos directory carries the credential.
	err = filepath.WalkDir(s.reposDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, bad := range []string{fakeToken, "x-access-token", "Authorization"} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s contains %q", path, bad)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repos directory: %v", err)
	}

	// The timeline indexes the branch the remote reported, not another one.
	if _, err := s.runner.Run(t.Context(), "timeline", model.Scope{}); err != nil {
		t.Fatalf("Run(timeline) error = %v", err)
	}
	if got := s.repo("app").LastIndexedSHA; got != trunkHead {
		t.Errorf("LastIndexedSHA = %s, want the trunk head %s (main head is %s)", got, trunkHead, mainHead)
	}
}

func TestIngest_FailedFirstSyncLeavesNoClone(t *testing.T) {
	t.Parallel()
	missing := "file://" + filepath.ToSlash(filepath.Join(t.TempDir(), "no-such-repo"))
	s := newStack(t, model.TargetRepo{Slug: "app", URL: missing, Branch: "main"})

	if _, err := s.runner.Run(t.Context(), "ingest", model.Scope{}); err == nil {
		t.Fatal("Run(ingest) on a missing repository: error = nil, want the fetch failure")
	}
	run := s.latestRun("ingest")
	if run.Status != model.RunFailed || run.Error == "" {
		t.Errorf("latest ingest run = %+v, want failed with an error", run)
	}
	checkStats(t, run, map[string]int64{"targets": 1, "fetched": 0, "cloned": 0, "failed": 1})

	findings, err := s.db.ListOpenFindings(t.Context())
	if err != nil {
		t.Fatalf("ListOpenFindings() error = %v", err)
	}
	if len(findings) != 1 || findings[0].Kind != model.KindJobFailure ||
		findings[0].Fingerprint != pipeline.JobFailureFingerprint("ingest") {
		t.Errorf("open findings = %+v, want one job_failure for ingest", findings)
	}

	repo := s.repo("app")
	if repo.LastFetchError == "" || !repo.LastFetchAt.Equal(fetchTime) {
		t.Errorf("stored fetch error %q at %v, want an error at %v", repo.LastFetchError, repo.LastFetchAt, fetchTime)
	}
	if repo.ClonePath != "" || repo.DefaultBranch != "" {
		t.Errorf("stored clone path %q, branch %q after a failed first sync, want both empty", repo.ClonePath, repo.DefaultBranch)
	}
	if _, err := os.Stat(filepath.Join(s.reposDir, "app.git")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("clone directory exists after a failed first sync (stat error %v)", err)
	}

	// The timeline skips a target that was never fetched.
	tl, err := s.runner.Run(t.Context(), "timeline", model.Scope{})
	if err != nil {
		t.Fatalf("Run(timeline) error = %v", err)
	}
	checkStats(t, tl, map[string]int64{"targets": 1, "skipped": 1, "failed": 0})
}

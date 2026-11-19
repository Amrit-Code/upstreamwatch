package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/findings"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/notify/stdout"
	"github.com/Amrit-Code/upstreamwatch/internal/store"
)

// These tests run the digest from a real SQLite file, written the way the
// vulns job writes it (advisories, matches, then findings through
// UpsertFinding), through report.Service to a real notifier.

const (
	e2eSlug    = "example-server"
	e2eLibPURL = "pkg:golang/github.com/example-org/lib"
	e2eToolURL = "pkg:golang/toolchain"
	e2eLibAdv  = "GO-2026-0101"
	e2eToolAdv = "GO-2026-0102"
)

// e2eDay returns 08:00 UTC on day n of March 2026.
func e2eDay(n int) time.Time { return time.Date(2026, 3, n, 8, 0, 0, 0, time.UTC) }

// cest is the digest's location: dates and run times are shown in it.
var cest = time.FixedZone("CEST", 2*3600)

func openE2EStore(t *testing.T) *store.DB {
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

// e2eSeed is what seedVulnFindings stored.
type e2eSeed struct {
	repoID    int64
	intervals map[string]model.RepoInterval // by "<purl> <version>"
	libF      model.Finding
	toolF     model.Finding
}

// seedVulnFindings stores one repo whose go.mod pinned lib v1.1.0, then
// v1.2.0, and the toolchain at 1.26; two GO advisories (one in KEV with an
// EPSS score); their matches; and one advisory finding per (repo, package,
// advisory), scored with model.RankAdvisory as the vulns job does.
func seedVulnFindings(t *testing.T, db *store.DB, changedAt time.Time) e2eSeed {
	t.Helper()
	ctx := t.Context()
	r, err := db.UpsertRepo(ctx, e2eSlug, "https://github.com/example-org/example-server")
	if err != nil {
		t.Fatalf("UpsertRepo() error = %v", err)
	}
	commit := func(n int) model.Commit {
		return model.Commit{
			SHA: fmt.Sprintf("%040x", n), CommittedAt: e2eDay(n), AuthorName: "Example Author",
			AuthorEmail: "author@example.com", Subject: fmt.Sprintf("commit %d", n), Paths: []string{"go.mod"},
		}
	}
	lib := model.Package{PURL: e2eLibPURL, Ecosystem: "golang", Name: "github.com/example-org/lib"}
	tool := model.Package{PURL: e2eToolURL, Ecosystem: "golang", Name: "toolchain"}
	err = db.ApplyTimeline(ctx, model.TimelineUpdate{
		RepoID:    r.ID,
		Commits:   []model.Commit{commit(1), commit(2), commit(3)},
		Manifests: map[string]model.ManifestInfo{"go.mod": {Ecosystem: "golang", Kind: model.ManifestKindManifest}},
		Intervals: []model.DepInterval{
			{ManifestPath: "go.mod", Package: lib, Version: "v1.1.0", Requirement: "v1.1.0", Direct: model.DirectYes,
				FromSHA: commit(1).SHA, From: e2eDay(1), ToSHA: commit(3).SHA, To: e2eDay(3)},
			{ManifestPath: "go.mod", Package: lib, Version: "v1.2.0", Requirement: "v1.2.0", Direct: model.DirectYes,
				FromSHA: commit(3).SHA, From: e2eDay(3)},
			{ManifestPath: "go.mod", Package: tool, Version: "1.26", Requirement: "1.26", Direct: model.DirectUnknown,
				FromSHA: commit(1).SHA, From: e2eDay(1)},
		},
		IndexedSHA: commit(3).SHA,
	})
	if err != nil {
		t.Fatalf("ApplyTimeline() error = %v", err)
	}
	all, err := db.ListAllIntervals(ctx)
	if err != nil {
		t.Fatalf("ListAllIntervals() error = %v", err)
	}
	seed := e2eSeed{repoID: r.ID, intervals: map[string]model.RepoInterval{}}
	for _, ri := range all {
		seed.intervals[ri.Interval.Package.PURL+" "+ri.Interval.Version] = ri
	}
	oldLib, newLib, toolIv := seed.intervals[e2eLibPURL+" v1.1.0"], seed.intervals[e2eLibPURL+" v1.2.0"], seed.intervals[e2eToolURL+" 1.26"]

	semver := func(fixed string) []model.AffectedRange {
		return []model.AffectedRange{{Type: model.RangeSemver, Events: []model.RangeEvent{{Introduced: "0"}, {Fixed: fixed}}}}
	}
	libAdv := model.Advisory{
		ID: e2eLibAdv, Severity: model.SeverityHigh,
		// Upstream text with a control sequence and line breaks: the digest
		// shows it on one clean line.
		Summary:   "Parser\x1b[2J panics on\r\ncrafted   input",
		Published: e2eDay(2), Modified: e2eDay(4),
		Affected: []model.AffectedPackage{{Package: lib, Ranges: semver("1.2.5"), SourceDB: "GO"}},
	}
	toolAdv := model.Advisory{
		ID: e2eToolAdv, Aliases: []string{"CVE-2026-0102"}, Severity: model.SeverityCritical,
		Details:   "net/http: request smuggling via chunked encoding. Upgrade to 1.26.4.",
		Published: e2eDay(2), Modified: e2eDay(4),
		Affected: []model.AffectedPackage{{Package: tool, Ranges: semver("1.26.4"), SourceDB: "GO"}},
	}
	for _, a := range []model.Advisory{libAdv, toolAdv} {
		if err := db.SaveAdvisory(ctx, a); err != nil {
			t.Fatalf("SaveAdvisory(%s) error = %v", a.ID, err)
		}
	}
	signals := []model.Signal{
		{Kind: model.SignalKEV, CVE: "CVE-2026-0102", KEVAddedAt: e2eDay(5)},
		{Kind: model.SignalEPSS, CVE: "CVE-2026-0102", EPSS: 0.4321, EPSSPercentile: 0.874},
	}
	if _, err := db.ApplySignals(ctx, signals, e2eDay(6)); err != nil {
		t.Fatalf("ApplySignals() error = %v", err)
	}

	matches := []model.AdvisoryMatch{
		{RepoID: r.ID, DepVersionID: oldLib.Interval.ID, AdvisoryID: e2eLibAdv, ExposedFrom: e2eDay(2), ExposedTo: e2eDay(3),
			FixedVersion: "1.2.5", Confidence: model.ConfidenceHigh, ConfidenceReason: "native GO record, fixed in 1.2.5, exact pin"},
		{RepoID: r.ID, DepVersionID: newLib.Interval.ID, AdvisoryID: e2eLibAdv, ExposedFrom: e2eDay(3),
			FixedVersion: "1.2.5", Confidence: model.ConfidenceHigh, ConfidenceReason: "native GO record, fixed in 1.2.5, exact pin"},
		{RepoID: r.ID, DepVersionID: toolIv.Interval.ID, AdvisoryID: e2eToolAdv, ExposedFrom: e2eDay(2),
			FixedVersion: "1.26.4", Confidence: model.ConfidenceMedium, ConfidenceReason: "native GO record, fixed in 1.26.4, toolchain 1.26 treated as 1.26.0"},
	}
	if err := db.ReplaceMatches(ctx, r.ID, matches); err != nil {
		t.Fatalf("ReplaceMatches() error = %v", err)
	}

	libScore, libReason := model.RankAdvisory(model.AdvisoryRankInput{
		Severity: model.SeverityHigh, Direct: model.DirectYes, CurrentlyExposed: true, Confidence: model.ConfidenceHigh,
	})
	toolScore, toolReason := model.RankAdvisory(model.AdvisoryRankInput{
		Severity: model.SeverityCritical, KEV: true, HasEPSS: true, EPSSPercentile: 0.874,
		CurrentlyExposed: true, Confidence: model.ConfidenceMedium,
	})
	seed.libF = upsertE2E(t, db, model.Finding{
		Kind: model.KindAdvisory, RepoID: r.ID, PackageID: newLib.PackageID, DepVersionID: newLib.Interval.ID,
		AdvisoryID: e2eLibAdv, Fingerprint: "advisory|" + e2eSlug + "|" + e2eLibPURL + "|" + e2eLibAdv,
		Score: libScore, RankReason: libReason + "; fixed in 1.2.5", LastChangedAt: changedAt,
	})
	seed.toolF = upsertE2E(t, db, model.Finding{
		Kind: model.KindAdvisory, RepoID: r.ID, PackageID: toolIv.PackageID, DepVersionID: toolIv.Interval.ID,
		AdvisoryID: e2eToolAdv, Fingerprint: "advisory|" + e2eSlug + "|" + e2eToolURL + "|" + e2eToolAdv,
		Score: toolScore, RankReason: toolReason + "; fixed in 1.26.4", LastChangedAt: changedAt,
	})
	return seed
}

func upsertE2E(t *testing.T, db *store.DB, f model.Finding) model.Finding {
	t.Helper()
	got, err := db.UpsertFinding(t.Context(), f)
	if err != nil {
		t.Fatalf("UpsertFinding(%s) error = %v", f.Fingerprint, err)
	}
	return got
}

// recordRun stores one finished run of job.
func recordRun(t *testing.T, db *store.DB, job string, started, finished time.Time, status model.RunStatus) {
	t.Helper()
	r, err := db.StartRun(t.Context(), job, started)
	if err != nil {
		t.Fatalf("StartRun(%s) error = %v", job, err)
	}
	r.FinishedAt, r.Status = finished, status
	if err := db.FinishRun(t.Context(), r); err != nil {
		t.Fatalf("FinishRun(%s) error = %v", job, err)
	}
}

func TestDigest_EndToEndStdout(t *testing.T) {
	t.Parallel()
	db := openE2EStore(t)
	ctx := t.Context()
	seedVulnFindings(t, db, e2eDay(6))
	if _, err := db.UpsertFinding(ctx, model.Finding{
		Kind: model.KindJobFailure, Fingerprint: "job_failure|ingest", Score: model.JobFailureScore,
		RankReason: "job ingest failed: fetch example-server: exit status 128", LastChangedAt: e2eDay(6),
	}); err != nil {
		t.Fatalf("UpsertFinding(job failure) error = %v", err)
	}
	recordRun(t, db, "ingest", e2eDay(7).Add(-55*time.Minute), e2eDay(7).Add(-54*time.Minute), model.RunFailed)
	recordRun(t, db, "vulns", e2eDay(7).Add(-50*time.Minute), e2eDay(7).Add(-48*time.Minute), model.RunSucceeded)

	var out bytes.Buffer
	now := e2eDay(7).In(cest)
	svc := NewService(db, []Channel{{Name: "stdout", Notifier: stdout.New(&out)}}, Options{
		BaseURL: "https://upw.example.test",
		Now:     func() time.Time { return now },
	}, slog.New(slog.DiscardHandler))

	stats, err := svc.Run(ctx, model.Scope{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	checkStats(t, stats, map[string]int64{"new": 3, "sent": 1})
	assertGolden(t, t.Name(), out.Bytes())

	// The stored payload is exactly what was sent.
	last, err := db.LastSentDigest(ctx)
	if err != nil {
		t.Fatalf("LastSentDigest() error = %v", err)
	}
	var stored model.Digest
	if err := json.Unmarshal(last.Payload, &stored); err != nil {
		t.Fatalf("decode stored payload: %v", err)
	}
	if got := stored.PlainText() + "\n"; got != out.String() {
		t.Errorf("stored payload renders as\n%s\nwant what stdout got\n%s", got, out.String())
	}
	if last.Channel != "stdout" || !last.SentAt.Equal(now) {
		t.Errorf("digest row = %s at %v, want stdout at %v", last.Channel, last.SentAt, now)
	}

	// Nothing changed: the next day's run sends nothing.
	sent := out.Len()
	now = now.Add(24 * time.Hour)
	stats, err = svc.Run(ctx, model.Scope{})
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	checkStats(t, stats, map[string]int64{"skipped": 1})
	if out.Len() != sent {
		t.Errorf("second run wrote %q, want nothing", out.Bytes()[sent:])
	}
}

// An acked finding that changes after it was reported stays out of the
// digest: M2 leaves ack re-send to M5.
func TestDigest_AckedThenChangedLeftOut(t *testing.T) {
	t.Parallel()
	db := openE2EStore(t)
	ctx := t.Context()
	seed := seedVulnFindings(t, db, e2eDay(6))
	clock := e2eDay(7)
	now := func() time.Time { return clock }
	n := &fakeNotifier{}
	svc := NewService(db, []Channel{{Name: "email", Notifier: n}}, Options{Now: now}, slog.New(slog.DiscardHandler))
	actions := findings.NewService(db, now)

	if _, err := svc.Run(ctx, model.Scope{}); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if got := itemIDs(n.sent[0].New); !slices.Contains(got, seed.libF.ID) {
		t.Fatalf("first digest New = %v, want it to hold finding %d", got, seed.libF.ID)
	}

	clock = e2eDay(7).Add(time.Hour)
	if _, err := actions.Ack(ctx, seed.libF.ID, "upgrade planned", "operator"); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	// The advisory lands in KEV: the job raises the score.
	changed := seed.libF
	changed.Score += 30
	changed.RankReason = "high 70; KEV +30; direct +10; exposed now +20; fixed in 1.2.5"
	changed.LastChangedAt = e2eDay(7).Add(2 * time.Hour)
	upsertE2E(t, db, changed)
	// A new failure gives the next digest something to send.
	fresh := upsertE2E(t, db, model.Finding{
		Kind: model.KindJobFailure, Fingerprint: "job_failure|vulns", Score: model.JobFailureScore,
		RankReason: "job vulns failed: status 503", LastChangedAt: e2eDay(7).Add(3 * time.Hour),
	})
	got, err := db.GetFinding(ctx, seed.libF.ID)
	if err != nil {
		t.Fatalf("GetFinding() error = %v", err)
	}
	if !got.LastChangedAt.After(got.ReportedInDigestAt) || got.State.State != model.StateAcked {
		t.Fatalf("finding %d: state %s, last changed %v, reported %v; want acked and changed after it was reported",
			got.ID, got.State.State, got.LastChangedAt, got.ReportedInDigestAt)
	}

	clock = e2eDay(8)
	if _, err := svc.Run(ctx, model.Scope{}); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if len(n.sent) != 2 {
		t.Fatalf("notifier got %d digests, want 2", len(n.sent))
	}
	d := n.sent[1]
	if gotNew, gotChanged := itemIDs(d.New), itemIDs(d.Changed); !slices.Equal(gotNew, []int64{fresh.ID}) || len(gotChanged) != 0 {
		t.Errorf("second digest New = %v, Changed = %v; want New [%d] and no Changed (acked finding %d left out)",
			gotNew, gotChanged, fresh.ID, seed.libF.ID)
	}
	if d.Acked != 1 {
		t.Errorf("second digest Acked = %d, want 1", d.Acked)
	}

	// Reopened, the same finding is reported as changed: only the ack kept
	// it out.
	clock = e2eDay(8).Add(time.Hour)
	if _, err := actions.Reopen(ctx, seed.libF.ID, "operator"); err != nil {
		t.Fatalf("Reopen() error = %v", err)
	}
	clock = e2eDay(9)
	if _, err := svc.Run(ctx, model.Scope{}); err != nil {
		t.Fatalf("third Run() error = %v", err)
	}
	if len(n.sent) != 3 {
		t.Fatalf("notifier got %d digests, want 3", len(n.sent))
	}
	if gotChanged := itemIDs(n.sent[2].Changed); !slices.Equal(gotChanged, []int64{seed.libF.ID}) {
		t.Errorf("third digest Changed = %v, want [%d] once reopened", gotChanged, seed.libF.ID)
	}
}

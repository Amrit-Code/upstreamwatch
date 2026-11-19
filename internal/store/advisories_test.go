package store

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln"
)

var _ vuln.AdvisoryStore = (*DB)(nil)

// advPkgNew is a package no timeline has stored.
var advPkgNew = model.Package{PURL: "pkg:pypi/example-lib", Ecosystem: "pypi", Name: "example-lib"}

// advSeedRepo stores a repo under slug with commits 1 to 3 and intervals, and
// returns its ID and stored intervals (as ListIntervals orders them).
func advSeedRepo(t *testing.T, db *DB, slug string, intervals ...model.DepInterval) (int64, []model.DepInterval) {
	t.Helper()
	r, err := db.UpsertRepo(t.Context(), slug, "https://github.com/example-org/"+slug)
	if err != nil {
		t.Fatalf("UpsertRepo(%s) error = %v", slug, err)
	}
	apply(t, db, model.TimelineUpdate{
		RepoID:     r.ID,
		Commits:    []model.Commit{commitAt(1), commitAt(2), commitAt(3)},
		Manifests:  map[string]model.ManifestInfo{"go.mod": goMod},
		Intervals:  intervals,
		IndexedSHA: sha(3),
	})
	return r.ID, listIntervals(t, db, r.ID)
}

// advSample builds an advisory with two affected entries: lib-a (stored by
// the caller's timeline, or new) and a package no timeline stores.
func advSample(id string) model.Advisory {
	return model.Advisory{
		ID:         id,
		Aliases:    []string{"CVE-2026-1001", "GHSA-xxxx-yyyy-zzzz"},
		Summary:    "Example library mishandles input",
		Details:    "A crafted request crashes the parser.",
		Severity:   model.SeverityHigh,
		CVSSScore:  7.5,
		CVSSVector: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H",
		Published:  day(1),
		Modified:   day(2),
		Withdrawn:  day(3),
		Affected: []model.AffectedPackage{
			{
				Package: pkgLibA,
				Ranges: []model.AffectedRange{{
					Type:   model.RangeSemver,
					Events: []model.RangeEvent{{Introduced: "0"}, {Fixed: "1.2.3"}},
				}},
				Versions:          []string{"v1.0.0", "v1.1.0"},
				EcosystemSpecific: []byte(`{"imports":[{"path":"github.com/example-org/lib-a","symbols":["Parse"]}]}`),
				SourceDB:          model.SourceDB(id),
			},
			{
				Package: advPkgNew,
				Ranges: []model.AffectedRange{{
					Type:   model.RangeGit,
					Repo:   "https://github.com/example-org/example-lib",
					Events: []model.RangeEvent{{Introduced: "abc123"}, {LastAffected: "def456"}, {Limit: "fff000"}},
				}},
				SourceDB: model.SourceDB(id),
			},
		},
		Raw: []byte(`{"id":"` + id + `"}`),
	}
}

// advListAll calls ListAdvisories and fails the test on error.
func advListAll(t *testing.T, db *DB) []model.Advisory {
	t.Helper()
	got, err := db.ListAdvisories(t.Context())
	if err != nil {
		t.Fatalf("ListAdvisories() error = %v", err)
	}
	return got
}

// advSave calls SaveAdvisory and fails the test on error.
func advSave(t *testing.T, db *DB, a model.Advisory) {
	t.Helper()
	if err := db.SaveAdvisory(t.Context(), a); err != nil {
		t.Fatalf("SaveAdvisory(%s) error = %v", a.ID, err)
	}
}

func TestSaveAdvisory_RoundTrip(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	advSeedRepo(t, db, "example-server", openInterval(pkgLibA, "v1.0.0", 1))

	in := advSample("GO-2026-0001")
	advSave(t, db, in)

	want := in
	want.Raw = nil
	got := advListAll(t, db)
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Errorf("ListAdvisories() =\n%+v\nwant\n[%+v]", got, want)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM packages WHERE purl = ? AND ecosystem = 'pypi' AND name = 'example-lib'`, advPkgNew.PURL); n != 1 {
		t.Errorf("new package rows = %d, want 1", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM packages`); n != 2 {
		t.Errorf("package rows = %d, want 2 (the existing one reused)", n)
	}

	// Storage formats.
	var aliases, ranges, versions, raw string
	if err := db.pool.QueryRowContext(t.Context(), `SELECT aliases, raw FROM advisories`).Scan(&aliases, &raw); err != nil {
		t.Fatalf("read advisory row: %v", err)
	}
	if want := `["CVE-2026-1001","GHSA-xxxx-yyyy-zzzz"]`; aliases != want {
		t.Errorf("aliases = %s, want %s", aliases, want)
	}
	if raw != string(in.Raw) {
		t.Errorf("raw = %s, want %s", raw, in.Raw)
	}
	if err := db.pool.QueryRowContext(t.Context(),
		`SELECT ranges, versions FROM advisory_affected WHERE package_id = (SELECT id FROM packages WHERE purl = ?)`,
		advPkgNew.PURL).Scan(&ranges, &versions); err != nil {
		t.Fatalf("read affected row: %v", err)
	}
	wantRanges := `[{"type":"GIT","repo":"https://github.com/example-org/example-lib","events":[{"introduced":"abc123"},{"last_affected":"def456"},{"limit":"fff000"}]}]`
	if ranges != wantRanges {
		t.Errorf("ranges = %s, want %s", ranges, wantRanges)
	}
	if versions != "[]" {
		t.Errorf("versions = %s, want []", versions)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM advisory_affected WHERE ranges = ?`,
		`[{"type":"SEMVER","events":[{"introduced":"0"},{"fixed":"1.2.3"}]}]`); n != 1 {
		t.Errorf("SEMVER range rows without repo = %d, want 1", n)
	}
}

func TestSaveAdvisory_NullColumns(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	advSave(t, db, model.Advisory{ID: "PYSEC-2026-1", CVSSScore: 9.9})

	got := advListAll(t, db)
	want := []model.Advisory{{ID: "PYSEC-2026-1"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListAdvisories() = %+v, want %+v", got, want)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM advisories WHERE aliases = '[]' AND severity_label IS NULL
		AND cvss_score IS NULL AND published_ts IS NULL AND modified_ts IS NULL AND withdrawn_ts IS NULL
		AND kev = 0 AND kev_added_ts IS NULL AND epss IS NULL AND epss_percentile IS NULL AND epss_ts IS NULL
		AND raw IS NULL AND created_ts = ? AND updated_ts = ?`, ts0, ts0); n != 1 {
		t.Errorf("advisory rows with NULL defaults = %d, want 1", n)
	}
}

func TestSaveAdvisory_UpdateKeepsSignals(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	first := advSample("GHSA-aaaa-bbbb-cccc")
	advSave(t, db, first)
	at := day(5)
	n, err := db.ApplySignals(ctx, []model.Signal{
		{Kind: model.SignalKEV, CVE: "CVE-2026-1001", KEVAddedAt: day(4)},
		{Kind: model.SignalEPSS, CVE: "CVE-2026-1001", EPSS: 0.42, EPSSPercentile: 0.97},
	}, at)
	if err != nil || n != 1 {
		t.Fatalf("ApplySignals() = %d, %v, want 1, nil", n, err)
	}

	later := t0.Add(time.Hour)
	db.now = func() time.Time { return later }
	second := first
	second.Summary = "Changed summary"
	second.Affected = first.Affected[:1]
	// Signals on the input are ignored by an update.
	second.KEV, second.HasEPSS, second.EPSS = false, true, 0.01
	advSave(t, db, second)

	got := advListAll(t, db)
	want := second
	want.Raw = nil
	want.KEV, want.KEVAddedAt = true, day(4)
	want.HasEPSS, want.EPSS, want.EPSSPercentile, want.EPSSAt = true, 0.42, 0.97, at
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Errorf("ListAdvisories() =\n%+v\nwant\n[%+v]", got, want)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM advisory_affected`); n != 1 {
		t.Errorf("affected rows = %d, want 1", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM advisories WHERE created_ts = ? AND updated_ts = ?`,
		ts0, "2026-03-01T13:00:00Z"); n != 1 {
		t.Errorf("rows keeping created_ts and moving updated_ts = %d, want 1", n)
	}
}

func TestSaveAdvisory_InsertTakesSignals(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	in := model.Advisory{ID: "CVE-2026-7", KEV: true, KEVAddedAt: day(1), HasEPSS: true, EPSS: 0.5,
		EPSSPercentile: 0.9, EPSSAt: day(2)}
	advSave(t, db, in)
	if got := advListAll(t, db); len(got) != 1 || !reflect.DeepEqual(got[0], in) {
		t.Errorf("ListAdvisories() = %+v, want [%+v]", got, in)
	}
}

func TestSaveAdvisory_InsertWithoutEPSSStoresNullEPSSTime(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	// EPSSAt is set but HasEPSS is false: no EPSS column is stored.
	advSave(t, db, model.Advisory{ID: "CVE-2026-8", EPSS: 0.5, EPSSPercentile: 0.9, EPSSAt: day(2)})
	if n := count(t, db, `SELECT COUNT(*) FROM advisories
		WHERE id = 'CVE-2026-8' AND epss IS NULL AND epss_percentile IS NULL AND epss_ts IS NULL`); n != 1 {
		t.Errorf("advisory rows with NULL epss, epss_percentile and epss_ts = %d, want 1", n)
	}
	want := []model.Advisory{{ID: "CVE-2026-8"}}
	if got := advListAll(t, db); !reflect.DeepEqual(got, want) {
		t.Errorf("ListAdvisories() = %+v, want %+v", got, want)
	}
}

func TestSaveAdvisory_Errors(t *testing.T) {
	t.Parallel()
	badEvent := advSample("GO-2026-0002")
	badEvent.Affected[1].Ranges[0].Events = append(badEvent.Affected[1].Ranges[0].Events,
		model.RangeEvent{Introduced: "1", Fixed: "2"})
	emptyEvent := advSample("GO-2026-0003")
	emptyEvent.Affected[0].Ranges[0].Events = []model.RangeEvent{{}}
	noPURL := advSample("GO-2026-0004")
	noPURL.Affected[1].Package.PURL = ""
	tests := []struct {
		name string
		in   model.Advisory
	}{
		{"empty id", model.Advisory{Summary: "no id"}},
		{"event with two fields", badEvent},
		{"event with no field", emptyEvent},
		{"affected entry without package URL", noPURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			if err := db.SaveAdvisory(t.Context(), tt.in); err == nil {
				t.Fatalf("SaveAdvisory() error = nil, want an error")
			}
			for _, table := range []string{"advisories", "advisory_affected", "packages"} {
				if n := count(t, db, `SELECT COUNT(*) FROM `+table); n != 0 {
					t.Errorf("%s rows after failed save = %d, want 0", table, n)
				}
			}
		})
	}
}

func TestListAdvisories_EmptyAndOrder(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if got := advListAll(t, db); got != nil {
		t.Errorf("ListAdvisories() on empty store = %+v, want nil", got)
	}
	for _, id := range []string{"PYSEC-2026-2", "GHSA-zzzz-zzzz-zzzz", "GO-2026-0009"} {
		advSave(t, db, model.Advisory{ID: id})
	}
	var ids []string
	for _, a := range advListAll(t, db) {
		ids = append(ids, a.ID)
	}
	if want := []string{"GHSA-zzzz-zzzz-zzzz", "GO-2026-0009", "PYSEC-2026-2"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("ListAdvisories() ids = %v, want %v", ids, want)
	}
}

func TestAdvisoryModified(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	got, err := db.AdvisoryModified(ctx, nil)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("AdvisoryModified(nil) = %v, %v, want empty map, nil", got, err)
	}

	advSave(t, db, model.Advisory{ID: "GO-2026-0001", Modified: day(2).Add(500 * time.Millisecond)})
	advSave(t, db, model.Advisory{ID: "GO-2026-0002"})
	// Over 500 ids, with the stored ones at both ends, crosses a chunk boundary.
	ids := []string{"GO-2026-0001"}
	for i := range 700 {
		ids = append(ids, fmt.Sprintf("GO-2025-%04d", i))
	}
	ids = append(ids, "GO-2026-0002")
	got, err = db.AdvisoryModified(ctx, ids)
	if err != nil {
		t.Fatalf("AdvisoryModified() error = %v", err)
	}
	want := map[string]time.Time{"GO-2026-0001": day(2), "GO-2026-0002": {}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AdvisoryModified() = %v, want %v", got, want)
	}
}

func TestApplySignals_ByAlias(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	advSave(t, db, model.Advisory{ID: "GHSA-x", Aliases: []string{"CVE-2026-1"}})
	advSave(t, db, model.Advisory{ID: "CVE-2026-2"})
	advSave(t, db, model.Advisory{ID: "GO-2026-0003"})
	at := day(7)
	n, err := db.ApplySignals(t.Context(), []model.Signal{
		{Kind: model.SignalKEV, CVE: "CVE-2026-1", KEVAddedAt: day(6)},
		{Kind: model.SignalEPSS, CVE: "CVE-2026-2", EPSS: 0.12, EPSSPercentile: 0.8},
		{Kind: model.SignalEPSS, CVE: "CVE-2026-9", EPSS: 0.99, EPSSPercentile: 0.99},
	}, at)
	if err != nil || n != 2 {
		t.Fatalf("ApplySignals() = %d, %v, want 2, nil", n, err)
	}
	want := []model.Advisory{
		{ID: "CVE-2026-2", HasEPSS: true, EPSS: 0.12, EPSSPercentile: 0.8, EPSSAt: at},
		{ID: "GHSA-x", Aliases: []string{"CVE-2026-1"}, KEV: true, KEVAddedAt: day(6)},
		{ID: "GO-2026-0003"},
	}
	if got := advListAll(t, db); !reflect.DeepEqual(got, want) {
		t.Errorf("ListAdvisories() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestApplySignals_SharedAliasAndZeroKEVDate(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	advSave(t, db, model.Advisory{ID: "GHSA-a", Aliases: []string{"CVE-2026-5"}})
	advSave(t, db, model.Advisory{ID: "GO-2026-0005", Aliases: []string{"CVE-2026-5", "GHSA-a"}})
	n, err := db.ApplySignals(t.Context(), []model.Signal{
		{Kind: model.SignalKEV, CVE: "CVE-2026-5"},
		{Kind: model.SignalKEV, CVE: "CVE-2026-5"},
	}, day(1))
	if err != nil || n != 2 {
		t.Fatalf("ApplySignals() = %d, %v, want 2, nil", n, err)
	}
	if c := count(t, db, `SELECT COUNT(*) FROM advisories WHERE kev = 1 AND kev_added_ts IS NULL AND epss_ts IS NULL`); c != 2 {
		t.Errorf("advisories in KEV without a date = %d, want 2", c)
	}
}

func TestApplySignals_EmptyAndUnknownKind(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	advSave(t, db, model.Advisory{ID: "CVE-2026-3"})
	if n, err := db.ApplySignals(t.Context(), nil, day(1)); err != nil || n != 0 {
		t.Errorf("ApplySignals(nil) = %d, %v, want 0, nil", n, err)
	}
	_, err := db.ApplySignals(t.Context(), []model.Signal{
		{Kind: model.SignalKEV, CVE: "CVE-2026-3"},
		{Kind: "vulncheck", CVE: "CVE-2026-3"},
	}, day(1))
	if err == nil {
		t.Fatal("ApplySignals() with unknown kind error = nil, want an error")
	}
	if c := count(t, db, `SELECT COUNT(*) FROM advisories WHERE kev = 1`); c != 0 {
		t.Errorf("advisories in KEV after failed apply = %d, want 0", c)
	}
}

// advMatches returns the stored matches of repoID, ordered by ID.
func advMatches(t *testing.T, db *DB, repoID int64) []model.AdvisoryMatch {
	t.Helper()
	rows, err := db.pool.QueryContext(t.Context(),
		`SELECT `+advMatchColumns+` FROM advisory_matches am WHERE am.repo_id = ? ORDER BY am.id`, repoID)
	if err != nil {
		t.Fatalf("read matches: %v", err)
	}
	defer rows.Close()
	var out []model.AdvisoryMatch
	for rows.Next() {
		var mr advMatchRow
		if err := rows.Scan(mr.dests()...); err != nil {
			t.Fatalf("read matches: %v", err)
		}
		m, err := mr.match()
		if err != nil {
			t.Fatalf("read matches: %v", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read matches: %v", err)
	}
	return out
}

func TestReplaceMatches_KeepsIDsAndDeletes(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	repoA, ivA := advSeedRepo(t, db, "example-server",
		closeAt(openInterval(pkgLibA, "v1.0.0", 1), 2), openInterval(pkgLibA, "v1.1.0", 2), openInterval(pkgLibB, "v0.1.0", 1))
	repoB, ivB := advSeedRepo(t, db, "example-worker", openInterval(pkgLibA, "v1.0.0", 1))
	advSave(t, db, model.Advisory{ID: "GO-2026-0001"})
	advSave(t, db, model.Advisory{ID: "GO-2026-0002"})

	m1 := model.AdvisoryMatch{RepoID: repoA, DepVersionID: ivA[0].ID, AdvisoryID: "GO-2026-0001",
		ExposedFrom: day(1), ExposedTo: day(2), FixedVersion: "v1.2.0", Confidence: model.ConfidenceHigh,
		ConfidenceReason: "lockfile pin"}
	m2 := model.AdvisoryMatch{RepoID: repoA, DepVersionID: ivA[1].ID, AdvisoryID: "GO-2026-0001",
		ExposedFrom: day(2), Confidence: model.ConfidenceMedium}
	other := model.AdvisoryMatch{RepoID: repoB, DepVersionID: ivB[0].ID, AdvisoryID: "GO-2026-0001",
		ExposedFrom: day(1), Confidence: model.ConfidenceLow}
	if err := db.ReplaceMatches(ctx, repoA, []model.AdvisoryMatch{m1, m2}); err != nil {
		t.Fatalf("first ReplaceMatches() error = %v", err)
	}
	if err := db.ReplaceMatches(ctx, repoB, []model.AdvisoryMatch{other}); err != nil {
		t.Fatalf("ReplaceMatches(repo B) error = %v", err)
	}
	stored := advMatches(t, db, repoA)
	if len(stored) != 2 {
		t.Fatalf("stored matches = %+v, want 2", stored)
	}
	otherStored := advMatches(t, db, repoB)

	// m1 is updated, m2 dropped, m3 new.
	m1.FixedVersion, m1.ConfidenceReason, m1.ExposedTo = "", "", time.Time{}
	m1.Confidence = model.ConfidenceLow
	m3 := model.AdvisoryMatch{RepoID: repoA, DepVersionID: ivA[2].ID, AdvisoryID: "GO-2026-0002",
		ExposedFrom: day(1), FixedVersion: "v0.2.0", Confidence: model.ConfidenceHigh}
	if err := db.ReplaceMatches(ctx, repoA, []model.AdvisoryMatch{m3, m1}); err != nil {
		t.Fatalf("second ReplaceMatches() error = %v", err)
	}
	got := advMatches(t, db, repoA)
	m1.ID = stored[0].ID
	if len(got) != 2 || !reflect.DeepEqual(got[0], m1) {
		t.Errorf("matches after replace = %+v, want first %+v", got, m1)
	}
	if len(got) == 2 {
		m3.ID = got[1].ID
		if !reflect.DeepEqual(got[1], m3) || m3.ID == stored[1].ID {
			t.Errorf("new match = %+v, want %+v with a fresh ID", got[1], m3)
		}
	}
	if n := count(t, db, `SELECT COUNT(*) FROM advisory_matches WHERE id = ?`, stored[1].ID); n != 0 {
		t.Errorf("dropped match rows = %d, want 0", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM advisory_matches WHERE fixed_version IS NULL AND confidence_reason IS NULL
		AND exposed_to_ts IS NULL AND id = ?`, m1.ID); n != 1 {
		t.Errorf("updated match with NULL columns = %d, want 1", n)
	}
	if g := advMatches(t, db, repoB); !reflect.DeepEqual(g, otherStored) {
		t.Errorf("other repo's matches = %+v, want %+v", g, otherStored)
	}

	// A match for another repo is refused and nothing changes.
	before := advMatches(t, db, repoA)
	err := db.ReplaceMatches(ctx, repoA, []model.AdvisoryMatch{m1, other})
	if err == nil {
		t.Fatal("ReplaceMatches() with a wrong RepoID error = nil, want an error")
	}
	if g := advMatches(t, db, repoA); !reflect.DeepEqual(g, before) {
		t.Errorf("matches after refused replace = %+v, want %+v", g, before)
	}

	// An empty set deletes every match of the repo and no other.
	if err := db.ReplaceMatches(ctx, repoA, nil); err != nil {
		t.Fatalf("ReplaceMatches(nil) error = %v", err)
	}
	if g := advMatches(t, db, repoA); g != nil {
		t.Errorf("matches after empty replace = %+v, want nil", g)
	}
	if g := advMatches(t, db, repoB); !reflect.DeepEqual(g, otherStored) {
		t.Errorf("other repo's matches after empty replace = %+v, want %+v", g, otherStored)
	}
}

func TestReplaceMatches_Invalid(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repoID, ivs := advSeedRepo(t, db, "example-server", openInterval(pkgLibA, "v1.0.0", 1))
	advSave(t, db, model.Advisory{ID: "GO-2026-0001"})
	valid := model.AdvisoryMatch{RepoID: repoID, DepVersionID: ivs[0].ID, AdvisoryID: "GO-2026-0001",
		ExposedFrom: day(1), Confidence: model.ConfidenceHigh}
	if err := db.ReplaceMatches(t.Context(), repoID, []model.AdvisoryMatch{valid}); err != nil {
		t.Fatalf("ReplaceMatches() error = %v", err)
	}
	before := advMatches(t, db, repoID)

	// Another repo holds a match on its own interval for the same advisory.
	otherRepo, otherIvs := advSeedRepo(t, db, "example-worker", openInterval(pkgLibA, "v1.0.0", 1))
	if err := db.ReplaceMatches(t.Context(), otherRepo, []model.AdvisoryMatch{{RepoID: otherRepo,
		DepVersionID: otherIvs[0].ID, AdvisoryID: "GO-2026-0001", ExposedFrom: day(1),
		Confidence: model.ConfidenceLow}}); err != nil {
		t.Fatalf("ReplaceMatches(other repo) error = %v", err)
	}
	otherBefore := advMatches(t, db, otherRepo)

	noAdvisory, noInterval, unstored, foreign, unknownInterval := valid, valid, valid, valid, valid
	noAdvisory.AdvisoryID = ""
	noInterval.DepVersionID = 0
	unstored.AdvisoryID = "GO-2026-9999"
	// Right repo ID, but the other repo's interval: the upsert would move its row.
	foreign.DepVersionID = otherIvs[0].ID
	unknownInterval.DepVersionID = otherIvs[0].ID + 1000
	tests := []struct {
		name    string
		matches []model.AdvisoryMatch
	}{
		{"empty advisory", []model.AdvisoryMatch{noAdvisory}},
		{"zero interval", []model.AdvisoryMatch{noInterval}},
		{"duplicate pair", []model.AdvisoryMatch{valid, valid}},
		{"advisory not stored", []model.AdvisoryMatch{unstored}},
		{"interval of another repo", []model.AdvisoryMatch{valid, foreign}},
		{"interval not stored", []model.AdvisoryMatch{valid, unknownInterval}},
	}
	for _, tt := range tests {
		if err := db.ReplaceMatches(t.Context(), repoID, tt.matches); err == nil {
			t.Errorf("%s: ReplaceMatches() error = nil, want an error", tt.name)
		}
		if g := advMatches(t, db, repoID); !reflect.DeepEqual(g, before) {
			t.Errorf("%s: matches = %+v, want unchanged %+v", tt.name, g, before)
		}
		if g := advMatches(t, db, otherRepo); !reflect.DeepEqual(g, otherBefore) {
			t.Errorf("%s: other repo's matches = %+v, want unchanged %+v", tt.name, g, otherBefore)
		}
	}
}

func TestQueriedVersions_Upsert(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	if got, err := db.QueriedVersions(ctx); err != nil || got != nil {
		t.Fatalf("QueriedVersions() on empty store = %v, %v, want nil, nil", got, err)
	}
	if err := db.MarkQueried(ctx, nil, day(1)); err != nil {
		t.Fatalf("MarkQueried(nil) error = %v", err)
	}
	a1 := model.PackageVersion{PURL: pkgLibA.PURL, Version: "v1.0.0"}
	a2 := model.PackageVersion{PURL: pkgLibA.PURL, Version: "v0.9.0"}
	pad := model.PackageVersion{PURL: pkgPad.PURL, Version: "1.3.0"}
	if err := db.MarkQueried(ctx, []model.PackageVersion{pad, a1}, day(1)); err != nil {
		t.Fatalf("first MarkQueried() error = %v", err)
	}
	later := t0.Add(time.Hour)
	db.now = func() time.Time { return later }
	if err := db.MarkQueried(ctx, []model.PackageVersion{a1, a2}, day(2)); err != nil {
		t.Fatalf("second MarkQueried() error = %v", err)
	}
	got, err := db.QueriedVersions(ctx)
	if err != nil {
		t.Fatalf("QueriedVersions() error = %v", err)
	}
	if want := []model.PackageVersion{a2, a1, pad}; !reflect.DeepEqual(got, want) {
		t.Errorf("QueriedVersions() = %v, want %v", got, want)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM osv_queries WHERE purl = ? AND version = ?
		AND queried_ts = ? AND created_ts = ? AND updated_ts = ?`,
		a1.PURL, a1.Version, "2026-03-03T12:00:00Z", ts0, "2026-03-01T13:00:00Z"); n != 1 {
		t.Errorf("re-marked row with new queried and updated times = %d, want 1", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM osv_queries WHERE purl = ? AND queried_ts = ?`,
		pad.PURL, "2026-03-02T12:00:00Z"); n != 1 {
		t.Errorf("untouched row keeping its queried time = %d, want 1", n)
	}
}

func TestListAllIntervals(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if got, err := db.ListAllIntervals(t.Context()); err != nil || got != nil {
		t.Fatalf("ListAllIntervals() on empty store = %v, %v, want nil, nil", got, err)
	}
	// The repo stored first sorts last by slug.
	repoZ, ivZ := advSeedRepo(t, db, "zeta-server", openInterval(pkgLibB, "v0.1.0", 1))
	repoA, ivA := advSeedRepo(t, db, "alpha-server",
		openInterval(pkgLibB, "v0.2.0", 2), closeAt(openInterval(pkgLibA, "v1.0.0", 1), 3), openInterval(pkgLibA, "v1.1.0", 3))

	pkgID := func(purl string) int64 { return advPackageID(t, db, purl) }
	var want []model.RepoInterval
	for _, iv := range ivA {
		want = append(want, model.RepoInterval{RepoID: repoA, RepoSlug: "alpha-server", PackageID: pkgID(iv.Package.PURL), Interval: iv})
	}
	for _, iv := range ivZ {
		want = append(want, model.RepoInterval{RepoID: repoZ, RepoSlug: "zeta-server", PackageID: pkgID(iv.Package.PURL), Interval: iv})
	}
	got, err := db.ListAllIntervals(t.Context())
	if err != nil {
		t.Fatalf("ListAllIntervals() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListAllIntervals() =\n%+v\nwant\n%+v", got, want)
	}
	// ListIntervals orders lib-a's two intervals by opening time, then lib-b.
	if len(got) == 4 && (got[0].Interval.Version != "v1.0.0" || got[1].Interval.Version != "v1.1.0" ||
		got[2].Interval.Package != pkgLibB || got[0].Interval.ID == 0) {
		t.Errorf("ListAllIntervals() order = %+v", got)
	}
}

func TestAdvisoryStore_ClosedReturnsErrors(t *testing.T) {
	t.Parallel()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "upw.sqlite"), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	ctx := t.Context()
	pv := []model.PackageVersion{{PURL: pkgLibA.PURL, Version: "v1.0.0"}}
	for name, call := range map[string]func() error{
		"ListAllIntervals": func() error { _, err := db.ListAllIntervals(ctx); return err },
		"QueriedVersions":  func() error { _, err := db.QueriedVersions(ctx); return err },
		"MarkQueried":      func() error { return db.MarkQueried(ctx, pv, t0) },
		"AdvisoryModified": func() error { _, err := db.AdvisoryModified(ctx, []string{"GO-2026-0001"}); return err },
		"SaveAdvisory":     func() error { return db.SaveAdvisory(ctx, model.Advisory{ID: "GO-2026-0001"}) },
		"ListAdvisories":   func() error { _, err := db.ListAdvisories(ctx); return err },
		"ApplySignals": func() error {
			_, err := db.ApplySignals(ctx, []model.Signal{{Kind: model.SignalKEV, CVE: "CVE-2026-1"}}, t0)
			return err
		},
		"ReplaceMatches":   func() error { return db.ReplaceMatches(ctx, 1, nil) },
		"GetKV":            func() error { _, err := db.GetKV(ctx, "k"); return err },
		"SetKV":            func() error { return db.SetKV(ctx, "k", "v") },
		"GetFindingDetail": func() error { _, err := db.GetFindingDetail(ctx, 1); return err },
	} {
		if err := call(); err == nil || errors.Is(err, model.ErrNotFound) {
			t.Errorf("%s after Close() error = %v, want a non-not-found error", name, err)
		}
	}
}

package vuln

import (
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const (
	goLib     = "pkg:golang/github.com/example-org/lib"
	goOther   = "pkg:golang/github.com/example-org/other"
	pypiLib   = "pkg:pypi/example-lib"
	actionPkg = "pkg:githubactions/example-org/action"
	dockerImg = "pkg:docker/example-org/image"
	sha       = "0123456789abcdef0123456789abcdef01234567"
	otherSHA  = "89abcdef0123456789abcdef0123456789abcdef"
)

func intro(v string) model.RangeEvent { return model.RangeEvent{Introduced: v} }
func fix(v string) model.RangeEvent   { return model.RangeEvent{Fixed: v} }
func last(v string) model.RangeEvent  { return model.RangeEvent{LastAffected: v} }
func limit(v string) model.RangeEvent { return model.RangeEvent{Limit: v} }

func semverRange(events ...model.RangeEvent) model.AffectedRange {
	return model.AffectedRange{Type: model.RangeSemver, Events: events}
}

func ecosystemRange(events ...model.RangeEvent) model.AffectedRange {
	return model.AffectedRange{Type: model.RangeEcosystem, Events: events}
}

func gitRange() model.AffectedRange {
	return model.AffectedRange{
		Type:   model.RangeGit,
		Repo:   "https://github.com/example-org/example-server",
		Events: []model.RangeEvent{intro("0"), fix(sha)},
	}
}

func entry(purl string, versions []string, ranges ...model.AffectedRange) model.AffectedPackage {
	return model.AffectedPackage{Package: model.Package{PURL: purl}, Ranges: ranges, Versions: versions}
}

func pinned(purl, version string) model.DepInterval {
	return model.DepInterval{
		ManifestPath: "go.mod",
		Package:      model.Package{PURL: purl},
		Version:      version,
		FromSHA:      "a1",
		From:         time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
	}
}

func adv(id string) model.Advisory {
	return model.Advisory{ID: id, Published: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)}
}

func TestEvaluate(t *testing.T) {
	t.Parallel()
	withdrawn := adv("GO-2026-0009")
	withdrawn.Withdrawn = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	simple := entry(goLib, nil, semverRange(intro("0"), fix("1.2.3")))
	twoRanges := entry(goLib, nil,
		semverRange(fix("1.5.0"), intro("1.0.0")),
		semverRange(fix("2.4.0"), intro("2.0.0")),
	)
	shuffled := entry(goLib, nil, semverRange(fix("2.4.0"), intro("0"), intro("2.0.0"), fix("1.5.0")))
	lastAffected := entry(goLib, nil, semverRange(intro("1.0.0"), last("1.4.0")))
	openEnded := entry(goLib, nil, semverRange(intro("2.0.0")))
	limited := entry(goLib, nil, semverRange(intro("0"), limit("2.0.0")))
	pep440 := entry(pypiLib, nil, ecosystemRange(intro("1.0rc1"), fix("2.0.post1")))
	listOnly := entry(goLib, []string{"1.0.0", "1.1.0"})
	gitOnly := entry(goLib, []string{"1.0.0"}, gitRange())
	toolchain := entry(toolchainPURL, nil, semverRange(intro("1.26.0-0"), fix("1.26.4")))
	badRange := entry(pypiLib, nil, ecosystemRange(intro("not a version"), fix("3.0")))
	badAndGood := entry(pypiLib, nil,
		ecosystemRange(intro("not a version"), fix("3.0")),
		ecosystemRange(intro("1.0"), fix("1.5")),
	)
	rangeAndList := entry(goLib, []string{"1.2.2"}, semverRange(intro("0"), fix("1.2.3")))
	action := entry(actionPkg, nil, ecosystemRange(intro("0"), fix("4.1.0")))
	actionMajor := entry(actionPkg, nil, ecosystemRange(intro("4.0.0"), fix("4.1.0")))
	actionMinor := entry(actionPkg, nil, ecosystemRange(intro("4.0.0"), fix("4.2.0")))
	actionSHA := entry(actionPkg, []string{sha}, ecosystemRange(intro("0"), fix("4.2.0")))
	docker := entry(dockerImg, []string{"1.0.0"}, ecosystemRange(intro("0"), fix("2.0.0")))
	badPin := entry(pypiLib, []string{"not valid"}, ecosystemRange(intro("0"), fix("2.0")))

	tests := []struct {
		name string
		adv  model.Advisory
		aff  model.AffectedPackage
		iv   model.DepInterval
		want matchResult
	}{
		{
			name: "semver below fix is affected, high for a GO id",
			adv:  adv("GO-2026-0001"), aff: simple, iv: pinned(goLib, "1.2.2"),
			want: matchResult{affected: true, fixedVersion: "1.2.3", confidence: model.ConfidenceHigh, reason: "native GO record, fixed in 1.2.3, exact pin"},
		},
		{
			name: "semver at fix is not affected",
			adv:  adv("GO-2026-0001"), aff: simple, iv: pinned(goLib, "1.2.3"),
			want: matchResult{reason: "1.2.3 outside every evaluated range and not on the versions list"},
		},
		{
			name: "semver Go form with v is affected",
			adv:  adv("GO-2026-0001"), aff: simple, iv: pinned(goLib, "v1.2.2"),
			want: matchResult{affected: true, fixedVersion: "1.2.3", confidence: model.ConfidenceHigh, reason: "native GO record, fixed in 1.2.3, exact pin"},
		},
		{
			name: "two ranges out of order, pin in the second",
			adv:  adv("GO-2026-0002"), aff: twoRanges, iv: pinned(goLib, "v2.3.0"),
			want: matchResult{affected: true, fixedVersion: "2.4.0", confidence: model.ConfidenceHigh, reason: "native GO record, fixed in 2.4.0, exact pin"},
		},
		{
			name: "two ranges out of order, pin between them",
			adv:  adv("GO-2026-0002"), aff: twoRanges, iv: pinned(goLib, "v1.7.0"),
			want: matchResult{reason: "1.7.0 outside every evaluated range and not on the versions list"},
		},
		{
			name: "shuffled events in one range, pin in the second segment",
			adv:  adv("GHSA-aaaa-bbbb-cccc"), aff: shuffled, iv: pinned(goLib, "2.0.0"),
			want: matchResult{affected: true, fixedVersion: "2.4.0", confidence: model.ConfidenceHigh, reason: "native GHSA record, fixed in 2.4.0, exact pin"},
		},
		{
			name: "shuffled events in one range, pin in the first segment",
			adv:  adv("GHSA-aaaa-bbbb-cccc"), aff: shuffled, iv: pinned(goLib, "1.4.9"),
			want: matchResult{affected: true, fixedVersion: "1.5.0", confidence: model.ConfidenceHigh, reason: "native GHSA record, fixed in 1.5.0, exact pin"},
		},
		{
			name: "shuffled events in one range, pin in the gap",
			adv:  adv("GHSA-aaaa-bbbb-cccc"), aff: shuffled, iv: pinned(goLib, "1.5.0"),
			want: matchResult{reason: "1.5.0 outside every evaluated range and not on the versions list"},
		},
		{
			name: "last_affected at the bound is affected, medium",
			adv:  adv("GO-2026-0003"), aff: lastAffected, iv: pinned(goLib, "1.4.0"),
			want: matchResult{affected: true, confidence: model.ConfidenceMedium, reason: "native GO record, no fixed version, exact pin"},
		},
		{
			name: "last_affected above the bound is not affected",
			adv:  adv("GO-2026-0003"), aff: lastAffected, iv: pinned(goLib, "1.4.1"),
			want: matchResult{reason: "1.4.1 outside every evaluated range and not on the versions list"},
		},
		{
			name: "last_affected below the introduction is not affected",
			adv:  adv("GO-2026-0003"), aff: lastAffected, iv: pinned(goLib, "0.9.0"),
			want: matchResult{reason: "0.9.0 outside every evaluated range and not on the versions list"},
		},
		{
			name: "open-ended introduction is affected, medium",
			adv:  adv("GO-2026-0004"), aff: openEnded, iv: pinned(goLib, "3.0"),
			want: matchResult{affected: true, confidence: model.ConfidenceMedium, reason: "native GO record, no fixed version, exact pin"},
		},
		{
			name: "limit: below is affected without a fixed version",
			adv:  adv("GO-2026-0005"), aff: limited, iv: pinned(goLib, "1.9.9"),
			want: matchResult{affected: true, confidence: model.ConfidenceMedium, reason: "native GO record, no fixed version, exact pin"},
		},
		{
			name: "limit: at the limit is not affected",
			adv:  adv("GO-2026-0005"), aff: limited, iv: pinned(goLib, "2.0.0"),
			want: matchResult{reason: "2.0.0 outside every evaluated range and not on the versions list"},
		},
		{
			name: "pep440 release candidate at the introduction",
			adv:  adv("PYSEC-2026-1"), aff: pep440, iv: pinned(pypiLib, "1.0rc1"),
			want: matchResult{affected: true, fixedVersion: "2.0.post1", confidence: model.ConfidenceHigh, reason: "native PYSEC record, fixed in 2.0.post1, exact pin"},
		},
		{
			name: "pep440 beta below the release candidate",
			adv:  adv("PYSEC-2026-1"), aff: pep440, iv: pinned(pypiLib, "1.0b1"),
			want: matchResult{reason: "1.0b1 outside every evaluated range and not on the versions list"},
		},
		{
			name: "pep440 release below the post release",
			adv:  adv("PYSEC-2026-1"), aff: pep440, iv: pinned(pypiLib, "2.0"),
			want: matchResult{affected: true, fixedVersion: "2.0.post1", confidence: model.ConfidenceHigh, reason: "native PYSEC record, fixed in 2.0.post1, exact pin"},
		},
		{
			name: "pep440 post release is fixed",
			adv:  adv("PYSEC-2026-1"), aff: pep440, iv: pinned(pypiLib, "2.0.post1"),
			want: matchResult{reason: "2.0.post1 outside every evaluated range and not on the versions list"},
		},
		{
			name: "versions list only: listed is affected, low",
			adv:  adv("GHSA-dddd-eeee-ffff"), aff: listOnly, iv: pinned(goLib, "v1.1.0"),
			want: matchResult{affected: true, confidence: model.ConfidenceLow, reason: "matched on the versions list"},
		},
		{
			name: "versions list only: not listed",
			adv:  adv("GHSA-dddd-eeee-ffff"), aff: listOnly, iv: pinned(goLib, "1.2.0"),
			want: matchResult{reason: "1.2.0 outside every evaluated range and not on the versions list"},
		},
		{
			name: "GIT-only entry, pin listed",
			adv:  adv("GHSA-gggg-hhhh-iiii"), aff: gitOnly, iv: pinned(goLib, "1.0.0"),
			want: matchResult{affected: true, confidence: model.ConfidenceLow, reason: "GIT ranges not evaluated; matched on the versions list"},
		},
		{
			name: "GIT-only entry, pin not listed",
			adv:  adv("GHSA-gggg-hhhh-iiii"), aff: gitOnly, iv: pinned(goLib, "2.0.0"),
			want: matchResult{reason: "GIT ranges not evaluated; 2.0.0 not on the versions list"},
		},
		{
			name: "floating toolchain treated as .0, medium",
			adv:  adv("GO-2026-0006"), aff: toolchain, iv: pinned(toolchainPURL, "1.26"),
			want: matchResult{affected: true, fixedVersion: "1.26.4", confidence: model.ConfidenceMedium, reason: "native GO record, fixed in 1.26.4, toolchain 1.26 treated as 1.26.0"},
		},
		{
			name: "exact toolchain with go prefix, high",
			adv:  adv("GO-2026-0006"), aff: toolchain, iv: pinned(toolchainPURL, "go1.26.3"),
			want: matchResult{affected: true, fixedVersion: "1.26.4", confidence: model.ConfidenceHigh, reason: "native GO record, fixed in 1.26.4, exact pin"},
		},
		{
			name: "toolchain at the fix is not affected",
			adv:  adv("GO-2026-0006"), aff: toolchain, iv: pinned(toolchainPURL, "go1.26.4"),
			want: matchResult{reason: "1.26.4 outside every evaluated range and not on the versions list"},
		},
		{
			name: "CVE record with a fixed event is medium",
			adv:  adv("CVE-2026-0001"), aff: simple, iv: pinned(goLib, "1.2.2"),
			want: matchResult{affected: true, fixedVersion: "1.2.3", confidence: model.ConfidenceMedium, reason: "non-native CVE record, fixed in 1.2.3, exact pin"},
		},
		{
			name: "withdrawn advisory",
			adv:  withdrawn, aff: simple, iv: pinned(goLib, "1.2.2"),
			want: matchResult{reason: "advisory withdrawn"},
		},
		{
			name: "entry for another package",
			adv:  adv("GO-2026-0001"), aff: entry(goOther, nil, semverRange(intro("0"))), iv: pinned(goLib, "1.2.2"),
			want: matchResult{reason: "entry is for " + goOther + ", not " + goLib},
		},
		{
			name: "empty version",
			adv:  adv("GO-2026-0001"), aff: simple, iv: pinned(goLib, ""),
			want: matchResult{reason: "no resolved version"},
		},
		{
			name: "range with a rejected value is skipped and noted",
			adv:  adv("PYSEC-2026-2"), aff: badRange, iv: pinned(pypiLib, "2.0"),
			want: matchResult{reason: `2.0 outside every evaluated range and not on the versions list; range 1 skipped: "not a version" rejected by the pypi scheme`},
		},
		{
			name: "rejected range skipped, valid range still matches",
			adv:  adv("PYSEC-2026-2"), aff: badAndGood, iv: pinned(pypiLib, "1.2"),
			want: matchResult{affected: true, fixedVersion: "1.5", confidence: model.ConfidenceHigh, reason: `native PYSEC record, fixed in 1.5, exact pin; range 1 skipped: "not a version" rejected by the pypi scheme`},
		},
		{
			name: "range match wins over the versions list",
			adv:  adv("GO-2026-0007"), aff: rangeAndList, iv: pinned(goLib, "1.2.2"),
			want: matchResult{affected: true, fixedVersion: "1.2.3", confidence: model.ConfidenceHigh, reason: "native GO record, fixed in 1.2.3, exact pin"},
		},
		{
			name: "github action tag compared without v",
			adv:  adv("GHSA-jjjj-kkkk-llll"), aff: action, iv: pinned(actionPkg, "v4.0.0"),
			want: matchResult{affected: true, fixedVersion: "4.1.0", confidence: model.ConfidenceHigh, reason: "native GHSA record, fixed in 4.1.0, exact pin"},
		},
		{
			name: "action major tag v4 floats, medium",
			adv:  adv("GHSA-jjjj-kkkk-llll"), aff: actionMajor, iv: pinned(actionPkg, "v4"),
			want: matchResult{affected: true, fixedVersion: "4.1.0", confidence: model.ConfidenceMedium, reason: "native GHSA record, fixed in 4.1.0, action v4 treated as 4.0.0"},
		},
		{
			name: "action minor tag v4.1 at the fix is not affected",
			adv:  adv("GHSA-jjjj-kkkk-llll"), aff: actionMajor, iv: pinned(actionPkg, "v4.1"),
			want: matchResult{reason: "4.1.0 outside every evaluated range and not on the versions list"},
		},
		{
			name: "action minor tag v4.1 floats, medium",
			adv:  adv("GHSA-jjjj-kkkk-llll"), aff: actionMinor, iv: pinned(actionPkg, "v4.1"),
			want: matchResult{affected: true, fixedVersion: "4.2.0", confidence: model.ConfidenceMedium, reason: "native GHSA record, fixed in 4.2.0, action v4.1 treated as 4.1.0"},
		},
		{
			name: "action full tag v4.1.2 is exact, high",
			adv:  adv("GHSA-jjjj-kkkk-llll"), aff: actionMinor, iv: pinned(actionPkg, "v4.1.2"),
			want: matchResult{affected: true, fixedVersion: "4.2.0", confidence: model.ConfidenceHigh, reason: "native GHSA record, fixed in 4.2.0, exact pin"},
		},
		{
			name: "action SHA pin on the versions list, ranges skipped",
			adv:  adv("GHSA-jjjj-kkkk-llll"), aff: actionSHA, iv: pinned(actionPkg, sha),
			want: matchResult{affected: true, confidence: model.ConfidenceLow, reason: "matched on the versions list; commit SHA pin not comparable to version ranges; ranges not evaluated"},
		},
		{
			name: "action SHA pin not listed, ranges skipped",
			adv:  adv("GHSA-jjjj-kkkk-llll"), aff: actionSHA, iv: pinned(actionPkg, otherSHA),
			want: matchResult{reason: otherSHA + " outside every evaluated range and not on the versions list; commit SHA pin not comparable to version ranges; ranges not evaluated"},
		},
		{
			name: "cargo scheme",
			adv:  adv("RUSTSEC-2026-0001"), aff: entry("pkg:cargo/example-crate", nil, ecosystemRange(intro("0"), fix("1.2.3"))), iv: pinned("pkg:cargo/example-crate", "1.2.0"),
			want: matchResult{affected: true, fixedVersion: "1.2.3", confidence: model.ConfidenceHigh, reason: "native RUSTSEC record, fixed in 1.2.3, exact pin"},
		},
		{
			name: "gem scheme",
			adv:  adv("GHSA-gem0-0000-0000"), aff: entry("pkg:gem/example-gem", nil, ecosystemRange(intro("1.0.0"), fix("2.0.0"))), iv: pinned("pkg:gem/example-gem", "1.9.9"),
			want: matchResult{affected: true, fixedVersion: "2.0.0", confidence: model.ConfidenceHigh, reason: "native GHSA record, fixed in 2.0.0, exact pin"},
		},
		{
			name: "nuget scheme",
			adv:  adv("GHSA-nuge-0000-0000"), aff: entry("pkg:nuget/Example.Lib", nil, ecosystemRange(intro("0"), fix("4.0.1"))), iv: pinned("pkg:nuget/Example.Lib", "4.0.0"),
			want: matchResult{affected: true, fixedVersion: "4.0.1", confidence: model.ConfidenceHigh, reason: "native GHSA record, fixed in 4.0.1, exact pin"},
		},
		{
			name: "maven scheme",
			adv:  adv("GHSA-mave-0000-0000"), aff: entry("pkg:maven/org.example/example-lib", nil, ecosystemRange(intro("2.0"), fix("2.17.1"))), iv: pinned("pkg:maven/org.example/example-lib", "2.14.1"),
			want: matchResult{affected: true, fixedVersion: "2.17.1", confidence: model.ConfidenceHigh, reason: "native GHSA record, fixed in 2.17.1, exact pin"},
		},
		{
			name: "composer scheme",
			adv:  adv("GHSA-comp-0000-0000"), aff: entry("pkg:composer/example-org/lib", nil, ecosystemRange(intro("0"), fix("5.4.2"))), iv: pinned("pkg:composer/example-org/lib", "5.4.1"),
			want: matchResult{affected: true, fixedVersion: "5.4.2", confidence: model.ConfidenceHigh, reason: "native GHSA record, fixed in 5.4.2, exact pin"},
		},
		{
			name: "pub scheme",
			adv:  adv("GHSA-pub0-0000-0000"), aff: entry("pkg:pub/example_lib", nil, ecosystemRange(intro("0"), fix("1.0.1"))), iv: pinned("pkg:pub/example_lib", "1.0.0"),
			want: matchResult{affected: true, fixedVersion: "1.0.1", confidence: model.ConfidenceHigh, reason: "native GHSA record, fixed in 1.0.1, exact pin"},
		},
		{
			name: "hex scheme",
			adv:  adv("GHSA-hex0-0000-0000"), aff: entry("pkg:hex/example_lib", nil, ecosystemRange(intro("0"), fix("0.6.1"))), iv: pinned("pkg:hex/example_lib", "0.6.0"),
			want: matchResult{affected: true, fixedVersion: "0.6.1", confidence: model.ConfidenceHigh, reason: "native GHSA record, fixed in 0.6.1, exact pin"},
		},
		{
			name: "package type without a scheme falls back to the versions list",
			adv:  adv("GHSA-mmmm-nnnn-oooo"), aff: docker, iv: pinned(dockerImg, "1.0.0"),
			want: matchResult{affected: true, confidence: model.ConfidenceLow, reason: `matched on the versions list; no version scheme for package type "docker"; ranges not evaluated`},
		},
		{
			name: "pin the scheme rejects falls back to the versions list",
			adv:  adv("PYSEC-2026-3"), aff: badPin, iv: pinned(pypiLib, "not valid"),
			want: matchResult{affected: true, confidence: model.ConfidenceLow, reason: `matched on the versions list; "not valid" rejected by the pypi scheme; ranges not evaluated`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := evaluate(tt.adv, tt.aff, tt.iv)
			if got != tt.want {
				t.Errorf("evaluate(%s, %s@%s) =\n  %+v\nwant\n  %+v", tt.adv.ID, tt.iv.Package.PURL, tt.iv.Version, got, tt.want)
			}
		})
	}
}

func TestExposureWindow(t *testing.T) {
	t.Parallel()
	day := func(d int) time.Time { return time.Date(2026, 1, d, 12, 0, 0, 0, time.UTC) }
	closed := model.DepInterval{FromSHA: "a1", From: day(10), ToSHA: "b2", To: day(20)}
	open := model.DepInterval{FromSHA: "a1", From: day(10)}
	plus2 := time.FixedZone("plus2", 2*60*60)

	tests := []struct {
		name      string
		iv        model.DepInterval
		published time.Time
		basis     exposureBasis
		wantFrom  time.Time
		wantTo    time.Time
	}{
		{"pin basis ignores published", closed, day(15), basisPin, day(10), day(20)},
		{"published before the pin", closed, day(5), basisPublished, day(10), day(20)},
		{"published inside the interval", closed, day(15), basisPublished, day(15), day(20)},
		{"published after a closed interval", closed, day(25), basisPublished, day(10), day(20)},
		{"published at the end of a closed interval", closed, day(20), basisPublished, day(10), day(20)},
		{"zero published", closed, time.Time{}, basisPublished, day(10), day(20)},
		{"open interval, published inside", open, day(15), basisPublished, day(15), time.Time{}},
		{"open interval, published later", open, day(25), basisPublished, day(25), time.Time{}},
		{"open interval, pin basis", open, day(15), basisPin, day(10), time.Time{}},
		{"published in another zone comes back UTC", open, day(15).In(plus2), basisPublished, day(15), time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			from, to := exposureWindow(tt.iv, tt.published, tt.basis)
			if !from.Equal(tt.wantFrom) || !to.Equal(tt.wantTo) {
				t.Errorf("exposureWindow(%v..%v, %v, %s) = (%v, %v), want (%v, %v)", tt.iv.From, tt.iv.To, tt.published, tt.basis, from, to, tt.wantFrom, tt.wantTo)
			}
			if from.Location() != time.UTC || (!to.IsZero() && to.Location() != time.UTC) {
				t.Errorf("exposureWindow returned non-UTC times (%v, %v)", from, to)
			}
			if tt.wantTo.IsZero() && !to.IsZero() {
				t.Errorf("exposureWindow to = %v, want zero for an open interval", to)
			}
		})
	}
}

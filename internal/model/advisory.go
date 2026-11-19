package model

import (
	"strings"
	"time"
)

// PackageVersion is one package at one resolved version: the unit an
// advisory database is queried with.
type PackageVersion struct {
	// PURL is the version-less package URL, as in Package.PURL.
	PURL    string
	Version string
}

// AdvisoryRef names an advisory and the time its record last changed, as an
// advisory database lists it before the full record is fetched.
type AdvisoryRef struct {
	ID       string
	Modified time.Time
}

// SeverityLabel is an advisory's severity, as stored in
// advisories.severity_label. Empty means unknown.
type SeverityLabel string

// The stored values of SeverityLabel. A stored value is never renamed.
const (
	SeverityCritical SeverityLabel = "critical"
	SeverityHigh     SeverityLabel = "high"
	SeverityMedium   SeverityLabel = "medium"
	SeverityLow      SeverityLabel = "low"
	SeverityUnknown  SeverityLabel = ""
)

// Advisory is one vulnerability record (an OSV record) with the exploit
// signals attached to it. Times are UTC; a zero time means "not set".
type Advisory struct {
	// ID is the record's own id: GO-2026-4599, GHSA-..., PYSEC-..., CVE-...
	ID      string
	Aliases []string
	Summary string
	Details string
	// Severity is derived from the record (see the vuln adapter); empty when
	// the record gives none.
	Severity SeverityLabel
	// CVSSScore is zero and CVSSVector empty when the record has no CVSS v3
	// vector.
	CVSSScore  float64
	CVSSVector string
	Published  time.Time
	Modified   time.Time
	Withdrawn  time.Time
	Affected   []AffectedPackage
	// Exploit signals, keyed by a CVE id that is the advisory's id or one of
	// its aliases. HasEPSS is false when no EPSS score is known.
	KEV            bool
	KEVAddedAt     time.Time
	HasEPSS        bool
	EPSS           float64
	EPSSPercentile float64
	EPSSAt         time.Time
	// Raw is the full record as fetched. Store methods that list advisories
	// leave it nil.
	Raw []byte
}

// IsWithdrawn reports whether the advisory has been withdrawn.
func (a Advisory) IsWithdrawn() bool { return !a.Withdrawn.IsZero() }

// CVEs returns the advisory's id, if it is a CVE id, followed by its CVE
// aliases, in order and without repeats.
func (a Advisory) CVEs() []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range append([]string{a.ID}, a.Aliases...) {
		if strings.HasPrefix(id, "CVE-") && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// SourceDB returns the prefix of an advisory id, the database it comes from:
// "GO" for GO-2026-4599, "GHSA", "PYSEC", "CVE" and so on. It returns the
// whole id when there is no dash.
func SourceDB(id string) string {
	if i := strings.IndexByte(id, '-'); i > 0 {
		return id[:i]
	}
	return id
}

// AffectedPackage is one affected[] entry of an advisory: one package and the
// versions of it the advisory covers.
type AffectedPackage struct {
	// Package is upstreamwatch's version-less package, already mapped from
	// the database's ecosystem and name (Go "stdlib" and "toolchain" map to
	// pkg:golang/toolchain).
	Package  Package
	Ranges   []AffectedRange
	Versions []string
	// EcosystemSpecific is the entry's ecosystem_specific object as raw
	// JSON (Go advisories list affected symbols there); nil when absent.
	EcosystemSpecific []byte
	// DatabaseSpecific is the entry's database_specific object as raw JSON;
	// nil when absent. It is not stored separately (Raw holds it).
	DatabaseSpecific []byte
	// SourceDB is SourceDB(advisory id).
	SourceDB string
}

// RangeType is the type of an affected range, as OSV names it.
type RangeType string

// The OSV range types.
const (
	RangeSemver    RangeType = "SEMVER"
	RangeEcosystem RangeType = "ECOSYSTEM"
	RangeGit       RangeType = "GIT"
)

// AffectedRange is one OSV range: a type and its events, in the order the
// record lists them.
type AffectedRange struct {
	Type RangeType
	// Repo is the repository URL of a GIT range; empty otherwise.
	Repo   string
	Events []RangeEvent
}

// RangeEvent is one OSV range event. Exactly one field is set.
type RangeEvent struct {
	Introduced   string
	Fixed        string
	LastAffected string
	Limit        string
}

// Confidence is how far an advisory match can be trusted, as stored in
// advisory_matches.confidence.
type Confidence string

// The stored values of Confidence. A stored value is never renamed.
const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

// AdvisoryMatch is one advisory affecting one dependency interval: a row of
// advisory_matches.
type AdvisoryMatch struct {
	// ID is the row ID; zero before it is stored.
	ID           int64
	RepoID       int64
	DepVersionID int64
	AdvisoryID   string
	// ExposedFrom is the start of the exposure window. ExposedTo is its end,
	// zero while the interval is still open (still exposed).
	ExposedFrom time.Time
	ExposedTo   time.Time
	// FixedVersion is the first fixed version at or above the pinned one,
	// empty when none is known.
	FixedVersion     string
	Confidence       Confidence
	ConfidenceReason string
}

// IsOpen reports whether the match is still exposed at the indexed head.
func (m AdvisoryMatch) IsOpen() bool { return m.ExposedTo.IsZero() }

// RepoInterval is one stored dependency interval with the IDs of the repo and
// package it belongs to, as the advisory matcher reads them.
type RepoInterval struct {
	RepoID    int64
	RepoSlug  string
	PackageID int64
	// Interval.ID is the dep_versions row ID.
	Interval DepInterval
}

// SignalKind says which feed an exploit signal comes from.
type SignalKind string

// The signal feeds.
const (
	SignalKEV  SignalKind = "kev"
	SignalEPSS SignalKind = "epss"
)

// Signal is one exploit signal for one CVE id from one feed. A KEV signal
// sets KEV and KEVAddedAt; an EPSS signal sets EPSS and EPSSPercentile.
type Signal struct {
	Kind           SignalKind
	CVE            string
	KEVAddedAt     time.Time
	EPSS           float64
	EPSSPercentile float64
}

// osvEcosystems maps a package URL type to the OSV ecosystem name. Package
// types missing here (docker, upm, generic) have no OSV ecosystem.
var osvEcosystems = map[string]string{
	"golang":        "Go",
	"pypi":          "PyPI",
	"npm":           "npm",
	"cargo":         "crates.io",
	"gem":           "RubyGems",
	"nuget":         "NuGet",
	"maven":         "Maven",
	"composer":      "Packagist",
	"pub":           "Pub",
	"hex":           "Hex",
	"githubactions": "GitHub Actions",
}

// PURLType returns the type of a package URL: "golang" for
// "pkg:golang/github.com/x/y". It returns "" for a string that is not a
// package URL.
func PURLType(purl string) string {
	rest, ok := strings.CutPrefix(purl, "pkg:")
	if !ok {
		return ""
	}
	t, _, ok := strings.Cut(rest, "/")
	if !ok {
		return ""
	}
	return t
}

// OSVEcosystem returns the OSV ecosystem name for a package URL ("Go" for
// "pkg:golang/..."), and false when OSV has no ecosystem for its type.
func OSVEcosystem(purl string) (string, bool) {
	e, ok := osvEcosystems[PURLType(purl)]
	return e, ok
}

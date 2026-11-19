package vuln

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// exposureBasis is vulns.exposure_basis: "published" (default) or "pin".
type exposureBasis string

const (
	basisPublished exposureBasis = "published"
	basisPin       exposureBasis = "pin"
)

// matchResult is the outcome of evaluating one affected entry against one
// pinned version.
type matchResult struct {
	affected     bool
	fixedVersion string           // first fixed version above the pin, "" if none known
	confidence   model.Confidence // set when affected
	reason       string           // why, one line; set whether or not affected
}

// evaluate decides whether aff (an entry of adv whose package is the
// interval's package) covers the version iv pins.
func evaluate(adv model.Advisory, aff model.AffectedPackage, iv model.DepInterval) matchResult {
	if adv.IsWithdrawn() {
		return matchResult{reason: "advisory withdrawn"}
	}
	if aff.Package.PURL != iv.Package.PURL {
		return matchResult{reason: fmt.Sprintf("entry is for %s, not %s", aff.Package.PURL, iv.Package.PURL)}
	}
	if iv.Version == "" {
		return matchResult{reason: "no resolved version"}
	}

	purl := iv.Package.PURL
	v, exact := normalizeVersion(purl, iv.Version)
	scheme, hasScheme := schemeFor(purl)

	var notes []string
	rangeMatched := false
	fixed := ""
	gitOnly := len(aff.Ranges) > 0
	pinValid := hasScheme && scheme.valid(v)
	switch {
	case !hasScheme:
		notes = append(notes, fmt.Sprintf("no version scheme for package type %q; ranges not evaluated", model.PURLType(purl)))
	case !pinValid:
		notes = append(notes, fmt.Sprintf("%q rejected by the %s scheme; ranges not evaluated", v, scheme.name))
	}
	for i, r := range aff.Ranges {
		if r.Type == model.RangeGit {
			continue
		}
		gitOnly = false
		if !pinValid {
			continue
		}
		if r.Type != model.RangeSemver && r.Type != model.RangeEcosystem {
			notes = append(notes, fmt.Sprintf("range %d: type %q not evaluated", i+1, r.Type))
			continue
		}
		affected, rangeFixed, bad, ok := evalRange(scheme, v, r)
		if !ok {
			notes = append(notes, fmt.Sprintf("range %d skipped: %q rejected by the %s scheme", i+1, bad, scheme.name))
			continue
		}
		if !affected {
			continue
		}
		rangeMatched = true
		if rangeFixed != "" && (fixed == "" || scheme.compare(rangeFixed, fixed) < 0) {
			fixed = rangeFixed
		}
	}

	var res matchResult
	switch {
	case rangeMatched:
		res = rangeMatch(model.SourceDB(adv.ID), fixed, purl, iv.Version, v, exact)
	case listed(aff.Versions, purl, iv.Version):
		res = matchResult{affected: true, confidence: model.ConfidenceLow, reason: "matched on the versions list"}
		if gitOnly {
			res.reason = "GIT ranges not evaluated; matched on the versions list"
		}
	case gitOnly:
		res = matchResult{reason: fmt.Sprintf("GIT ranges not evaluated; %s not on the versions list", v)}
	default:
		res = matchResult{reason: fmt.Sprintf("%s outside every evaluated range and not on the versions list", v)}
	}
	if len(notes) > 0 {
		res.reason += "; " + strings.Join(notes, "; ")
	}
	return res
}

// rangeMatch builds the result of a match through a SEMVER or ECOSYSTEM
// range: src is the advisory's source database, fixed the fixed version
// found ("" if none), orig the interval's version and v its normalised form.
func rangeMatch(src, fixed, purl, orig, v string, exact bool) matchResult {
	native := nativeDB(src)
	parts := make([]string, 0, 3)
	if native {
		parts = append(parts, fmt.Sprintf("native %s record", src))
	} else {
		parts = append(parts, fmt.Sprintf("non-native %s record", src))
	}
	if fixed != "" {
		parts = append(parts, "fixed in "+fixed)
	} else {
		parts = append(parts, "no fixed version")
	}
	if exact {
		parts = append(parts, "exact pin")
	} else {
		parts = append(parts, fmt.Sprintf("%s %s treated as %s", purl[strings.LastIndexByte(purl, '/')+1:], orig, v))
	}
	conf := model.ConfidenceHigh
	if !native || fixed == "" || !exact {
		conf = model.ConfidenceMedium
	}
	return matchResult{affected: true, fixedVersion: fixed, confidence: conf, reason: strings.Join(parts, ", ")}
}

// nativeDB reports whether src is an ecosystem's own advisory database, whose
// ranges are trusted as published.
func nativeDB(src string) bool {
	switch src {
	case "GO", "PYSEC", "RUSTSEC", "GHSA":
		return true
	}
	return false
}

// listed reports whether the explicit versions list holds version, both
// sides normalised the same way for purl.
func listed(versions []string, purl, version string) bool {
	key := listKey(purl, version)
	return slices.ContainsFunc(versions, func(x string) bool { return listKey(purl, x) == key })
}

// eventKind is which field of a range event is set. The order breaks ties
// between events at the same version: an introduction is applied first.
type eventKind int

const (
	eventIntroduced eventKind = iota
	eventFixed
	eventLastAffected
	eventLimit
)

// rangeEvent is one range event reduced to its kind and version.
type rangeEvent struct {
	kind  eventKind
	value string
}

// isZero reports whether e is OSV's "introduced: 0", lower than every
// version.
func (e rangeEvent) isZero() bool { return e.kind == eventIntroduced && e.value == "0" }

// evalRange applies the OSV evaluation algorithm for one SEMVER or ECOSYSTEM
// range to the normalised version v. It returns whether v is affected and,
// when it is, the lowest fixed version above v ("" if the range has none).
// ok is false, with the offending value in bad, when an event's version is
// one the scheme rejects.
func evalRange(s versionScheme, v string, r model.AffectedRange) (affected bool, fixed, bad string, ok bool) {
	events := make([]rangeEvent, 0, len(r.Events))
	for _, e := range r.Events {
		var re rangeEvent
		switch {
		case e.Introduced != "":
			re = rangeEvent{kind: eventIntroduced, value: e.Introduced}
		case e.Fixed != "":
			re = rangeEvent{kind: eventFixed, value: e.Fixed}
		case e.LastAffected != "":
			re = rangeEvent{kind: eventLastAffected, value: e.LastAffected}
		case e.Limit != "":
			re = rangeEvent{kind: eventLimit, value: e.Limit}
		default:
			return false, "", "", false
		}
		if !re.isZero() && !s.valid(re.value) {
			return false, "", re.value, false
		}
		events = append(events, re)
	}
	slices.SortStableFunc(events, func(a, b rangeEvent) int {
		var c int
		switch {
		case a.isZero() && b.isZero():
		case a.isZero():
			c = -1
		case b.isZero():
			c = 1
		default:
			c = s.compare(a.value, b.value)
		}
		if c != 0 {
			return c
		}
		return int(a.kind) - int(b.kind)
	})

	for _, e := range events {
		switch e.kind {
		case eventIntroduced:
			if e.isZero() || s.compare(v, e.value) >= 0 {
				affected = true
			}
		case eventFixed, eventLimit:
			if s.compare(v, e.value) >= 0 {
				affected = false
			}
		case eventLastAffected:
			if s.compare(v, e.value) > 0 {
				affected = false
			}
		}
	}
	if !affected {
		return false, "", "", true
	}
	for _, e := range events {
		if e.kind == eventFixed && s.compare(e.value, v) > 0 {
			return true, e.value, "", true
		}
	}
	return true, "", "", true
}

// exposureWindow returns the exposure window of a match on iv.
func exposureWindow(iv model.DepInterval, published time.Time, basis exposureBasis) (from, to time.Time) {
	from, to = iv.From.UTC(), iv.To.UTC()
	if basis == basisPin || published.IsZero() {
		return from, to
	}
	p := published.UTC()
	if !iv.IsOpen() && !p.Before(to) {
		// The pin was gone before the advisory was public: the exposure
		// was real but unknown at the time, so the whole interval counts.
		return from, to
	}
	if p.After(from) {
		from = p
	}
	return from, to
}

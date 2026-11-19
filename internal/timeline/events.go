package timeline

import (
	"cmp"
	"slices"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// manifestState is one manifest's dependencies as last recorded by an event,
// keyed by package URL.
type manifestState map[string]model.Dependency

// repoState is every manifest's state, keyed by manifest path.
type repoState map[string]manifestState

// manifestChange is what one commit did to one manifest path: either it was
// deleted, or it was parsed into Deps (already filtered and deduplicated).
type manifestChange struct {
	Path    string
	Deleted bool
	Deps    []model.Dependency
}

// stateFromEvents folds an ordered event list into the state it leaves: per
// manifest path and package URL, the latest event unless it is a removal.
// Manifests left with no packages are absent from the result.
func stateFromEvents(events []model.DepEvent) repoState {
	st := repoState{}
	for _, ev := range events {
		ms := st[ev.ManifestPath]
		if ev.Change == model.ChangeRemoved {
			if ms != nil {
				delete(ms, ev.Package.PURL)
				if len(ms) == 0 {
					delete(st, ev.ManifestPath)
				}
			}
			continue
		}
		if ms == nil {
			ms = manifestState{}
			st[ev.ManifestPath] = ms
		}
		ms[ev.Package.PURL] = model.Dependency{
			Package:     ev.Package,
			Version:     ev.Version,
			Requirement: ev.Requirement,
			Direct:      ev.Direct,
			Scope:       ev.Scope,
		}
	}
	return st
}

// diffCommit diffs every manifest change of commit c against st, updates st
// in place to the state after the commit, and returns the commit's events
// ordered by manifest path, then package URL.
func diffCommit(c model.Commit, st repoState, changes []manifestChange) []model.DepEvent {
	var events []model.DepEvent
	for _, ch := range changes {
		var deps []model.Dependency
		if !ch.Deleted {
			deps = ch.Deps
		}
		evs, next := diffManifest(c, ch.Path, st[ch.Path], deps)
		events = append(events, evs...)
		if len(next) == 0 {
			delete(st, ch.Path)
		} else {
			st[ch.Path] = next
		}
	}
	slices.SortStableFunc(events, func(a, b model.DepEvent) int {
		return cmp.Or(
			cmp.Compare(a.ManifestPath, b.ManifestPath),
			cmp.Compare(a.Package.PURL, b.Package.PURL),
		)
	})
	return events
}

// diffManifest compares one manifest's previous state with its dependencies
// at commit c, which must have distinct package URLs. It returns the events,
// ordered by package URL, and the manifest's next state. A dependency whose
// version, requirement or direct flag differs produces a changed event; one
// whose scope alone differs produces no event and keeps its previous state.
func diffManifest(c model.Commit, path string, prev manifestState, deps []model.Dependency) ([]model.DepEvent, manifestState) {
	next := make(manifestState, len(deps))
	var events []model.DepEvent
	for _, d := range deps {
		old, ok := prev[d.Package.PURL]
		switch {
		case !ok:
			events = append(events, newEvent(c, path, d, model.ChangeAdded, ""))
			next[d.Package.PURL] = d
		case old.Version != d.Version || old.Requirement != d.Requirement || old.Direct != d.Direct:
			events = append(events, newEvent(c, path, d, model.ChangeChanged, old.Version))
			next[d.Package.PURL] = d
		default:
			next[d.Package.PURL] = old
		}
	}
	for purl, old := range prev {
		if _, ok := next[purl]; ok {
			continue
		}
		events = append(events, newEvent(c, path, old, model.ChangeRemoved, old.Version))
	}
	slices.SortFunc(events, func(a, b model.DepEvent) int {
		return cmp.Compare(a.Package.PURL, b.Package.PURL)
	})
	return events, next
}

func newEvent(c model.Commit, path string, d model.Dependency, change model.Change, prevVersion string) model.DepEvent {
	return model.DepEvent{
		CommitSHA:    c.SHA,
		CommittedAt:  c.CommittedAt,
		ManifestPath: path,
		Package:      d.Package,
		Change:       change,
		Version:      d.Version,
		Requirement:  d.Requirement,
		Direct:       d.Direct,
		Scope:        d.Scope,
		PrevVersion:  prevVersion,
	}
}

// fold builds the complete interval set from an event list, folding the
// events strictly in the order given (commit position, then manifest path,
// then package URL), never by date. Per manifest path and package URL, an
// added or changed event closes the open interval, if any, at its commit and
// opens a new one there; a removed event closes the open interval, if any.
// A closing commit dated before the interval's opening commit, as committer
// clock skew can produce, closes it at its opening time, so no interval ends
// before it starts. The result is ordered by package URL, manifest path,
// then opening time, with commit order kept for equal times.
func fold(events []model.DepEvent) []model.DepInterval {
	type key struct{ path, purl string }
	open := map[key]int{}
	var out []model.DepInterval
	for _, ev := range events {
		k := key{ev.ManifestPath, ev.Package.PURL}
		if i, ok := open[k]; ok {
			out[i].ToSHA = ev.CommitSHA
			out[i].To = ev.CommittedAt
			if out[i].To.Before(out[i].From) {
				out[i].To = out[i].From
			}
			delete(open, k)
		}
		if ev.Change == model.ChangeRemoved {
			continue
		}
		out = append(out, model.DepInterval{
			ManifestPath: ev.ManifestPath,
			Package:      ev.Package,
			Version:      ev.Version,
			Requirement:  ev.Requirement,
			Direct:       ev.Direct,
			FromSHA:      ev.CommitSHA,
			From:         ev.CommittedAt,
		})
		open[k] = len(out) - 1
	}
	slices.SortStableFunc(out, func(a, b model.DepInterval) int {
		return cmp.Or(
			cmp.Compare(a.Package.PURL, b.Package.PURL),
			cmp.Compare(a.ManifestPath, b.ManifestPath),
			a.From.Compare(b.From),
		)
	})
	return out
}

// filterDeps keeps the dependencies whose ecosystem is in ecosystems (all of
// them when ecosystems is empty) and drops repeated package URLs, keeping the
// first occurrence.
func filterDeps(deps []model.Dependency, ecosystems []string) []model.Dependency {
	seen := make(map[string]bool, len(deps))
	out := make([]model.Dependency, 0, len(deps))
	for _, d := range deps {
		if len(ecosystems) > 0 && !slices.Contains(ecosystems, d.Package.Ecosystem) {
			continue
		}
		if seen[d.Package.PURL] {
			continue
		}
		seen[d.Package.PURL] = true
		out = append(out, d)
	}
	return out
}

// unionPatterns returns the patterns of exts in order, without duplicates.
func unionPatterns(exts []Extractor) []string {
	var out []string
	for _, e := range exts {
		for _, p := range e.Patterns() {
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

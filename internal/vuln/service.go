package vuln

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// jobName is the vulns job's name in config schedules and upw run.
const jobName = "vulns"

// defaultSignalsEvery is the minimum gap between two refreshes of one exploit
// signal feed when Options.SignalsEvery is zero.
const defaultSignalsEvery = 20 * time.Hour

// kvTimeLayout is how cursor and refresh times are written to kv: RFC 3339
// in UTC, keeping any fraction of a second the source gave.
const kvTimeLayout = time.RFC3339Nano

// statKeys are the counters every run reports, present even when zero.
var statKeys = []string{
	"intervals", "unversioned", "pairs_new", "query_refs", "delta_refs", "fetched",
	"fetch_not_found", "delta_irrelevant", "advisories_saved", "kev_updated", "epss_updated",
	"kev_errors", "epss_errors", "matches", "matches_open", "findings_upserted",
	"findings_resolved",
}

// Options configures a Service.
type Options struct {
	ExposureBasis   string        // "published" (default when empty) or "pin"
	EcosystemsExtra []string      // OSV ecosystem names whose modified_id.csv is read even if no package uses them
	SignalsEvery    time.Duration // minimum gap between KEV/EPSS refreshes; default 20h
	Now             func() time.Time
}

// Service is the vulns job: it queries the advisory database for package
// versions not queried before, applies each ecosystem's modified-records
// delta, refreshes the exploit signals once a day, rebuilds the advisory
// matches of the repos in scope and raises one advisory finding per repo,
// package and advisory.
type Service struct {
	src      AdvisorySource
	kev      ExploitSignals
	epss     ExploitSignals
	store    AdvisoryStore
	kv       KVStore
	findings FindingStore
	basis    exposureBasis
	extra    []string
	every    time.Duration
	now      func() time.Time
	logger   *slog.Logger
}

// NewService returns a vulns Service. kev or epss may be nil, in which case
// that feed is never refreshed. An ExposureBasis of "pin" starts each
// exposure window when the vulnerable pin arrived; any other value starts it
// no earlier than the advisory's publication. It does no I/O.
func NewService(src AdvisorySource, kev, epss ExploitSignals, store AdvisoryStore, kv KVStore, findings FindingStore, opts Options, logger *slog.Logger) *Service {
	s := &Service{
		src:      src,
		kev:      kev,
		epss:     epss,
		store:    store,
		kv:       kv,
		findings: findings,
		basis:    basisPublished,
		extra:    slices.Clone(opts.EcosystemsExtra),
		every:    opts.SignalsEvery,
		now:      opts.Now,
		logger:   logger,
	}
	if opts.ExposureBasis == string(basisPin) {
		s.basis = basisPin
	}
	if s.every <= 0 {
		s.every = defaultSignalsEvery
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// Name returns "vulns".
func (s *Service) Name() string { return jobName }

// Run queries new package versions, applies the advisory deltas, refreshes
// the exploit signals when due, then rebuilds the matches and findings of the
// repos in scope. With scope.RepoSlug set, querying, matching and findings
// cover that repo only, while the deltas and signals, which are global, still
// run; a slug with no stored interval is an error wrapping model.ErrNotFound.
// The newly queried pairs are recorded as queried only when every advisory
// the query batch named was fetched or found missing. A store failure or a failed query batch stops the run. A failed record
// fetch, delta download or signal refresh is logged and the run continues
// through matching and findings, then returns those errors joined, redacted.
// When ctx is done Run stops before the next unit of work and returns ctx's
// error.
func (s *Service) Run(ctx context.Context, scope model.Scope) (model.Stats, error) {
	stats := model.Stats{}
	for _, k := range statKeys {
		stats[k] = 0
	}
	defer func() {
		s.logger.Info("vulns run finished",
			"intervals", stats["intervals"], "pairs_new", stats["pairs_new"],
			"delta_refs", stats["delta_refs"], "fetched", stats["fetched"],
			"advisories_saved", stats["advisories_saved"], "matches", stats["matches"],
			"matches_open", stats["matches_open"], "findings_upserted", stats["findings_upserted"],
			"findings_resolved", stats["findings_resolved"])
	}()
	now := s.now().UTC()

	all, err := s.store.ListAllIntervals(ctx)
	if err != nil {
		return stats, fmt.Errorf("list intervals: %w", err)
	}
	inScope := all
	if !scope.All() {
		inScope = slices.DeleteFunc(slices.Clone(all), func(ri model.RepoInterval) bool {
			return ri.RepoSlug != scope.RepoSlug
		})
		if len(inScope) == 0 {
			return stats, fmt.Errorf("repo %q: %w", scope.RepoSlug, model.ErrNotFound)
		}
	}
	stats["intervals"] = int64(len(inScope))

	var collected []error
	newPairs, queryRefs, err := s.queryNew(ctx, inScope, stats)
	if err != nil {
		return stats, err
	}
	deltas, errs, err := s.readDeltas(ctx, all, now, stats)
	if err != nil {
		return stats, err
	}
	collected = append(collected, errs...)
	failed, errs, err := s.fetchAll(ctx, queryRefs, deltas, usedPURLs(all), stats)
	if err != nil {
		return stats, err
	}
	collected = append(collected, errs...)
	// A failed fetch of a query-batch ref leaves this run's pairs unmarked,
	// so the next run queries them again and retries the fetch.
	if len(newPairs) > 0 && !slices.ContainsFunc(queryRefs, func(r model.AdvisoryRef) bool { return failed[r.ID] }) {
		if err := s.store.MarkQueried(ctx, newPairs, now); err != nil {
			return stats, fmt.Errorf("mark %d package versions queried: %w", len(newPairs), err)
		}
	}
	if err := s.advanceCursors(ctx, deltas, failed); err != nil {
		return stats, err
	}
	errs, err = s.refreshSignals(ctx, now, stats)
	if err != nil {
		return stats, err
	}
	collected = append(collected, errs...)

	if err := ctx.Err(); err != nil {
		return stats, err
	}
	advisories, err := s.store.ListAdvisories(ctx)
	if err != nil {
		return stats, fmt.Errorf("list advisories: %w", err)
	}
	matches := buildMatches(inScope, advisories, s.basis)
	if err := s.replaceMatches(ctx, inScope, matches, stats); err != nil {
		return stats, err
	}
	if err := s.writeFindings(ctx, scope, inScope, matches, advisories, now, stats); err != nil {
		return stats, err
	}

	if len(collected) > 0 {
		joined := errors.Join(collected...)
		return stats, &redactedError{msg: model.Redact(joined.Error()), err: joined}
	}
	return stats, nil
}

// queryNew sends the in-scope pairs never queried before to QueryBatch and
// returns those pairs and the refs it gave. Marking the pairs queried is left
// to the caller, after the refs are fetched.
func (s *Service) queryNew(ctx context.Context, inScope []model.RepoInterval, stats model.Stats) ([]model.PackageVersion, []model.AdvisoryRef, error) {
	seen := map[model.PackageVersion]bool{}
	var pairs []model.PackageVersion
	for _, ri := range inScope {
		iv := ri.Interval
		if iv.Version == "" {
			stats["unversioned"]++
			continue
		}
		if _, ok := model.OSVEcosystem(iv.Package.PURL); !ok {
			continue
		}
		pv := model.PackageVersion{PURL: iv.Package.PURL, Version: iv.Version}
		if !seen[pv] {
			seen[pv] = true
			pairs = append(pairs, pv)
		}
	}
	if len(pairs) == 0 {
		return nil, nil, nil
	}
	queried, err := s.store.QueriedVersions(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list queried versions: %w", err)
	}
	for _, pv := range queried {
		delete(seen, pv)
	}
	pairs = slices.DeleteFunc(pairs, func(pv model.PackageVersion) bool { return !seen[pv] })
	slices.SortFunc(pairs, comparePairs)
	stats["pairs_new"] = int64(len(pairs))
	if len(pairs) == 0 {
		return nil, nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	refs, err := s.src.QueryBatch(ctx, pairs)
	if err != nil {
		return nil, nil, fmt.Errorf("query advisories for %d package versions: %w", len(pairs), err)
	}
	stats["query_refs"] = int64(len(refs))
	return pairs, refs, nil
}

// comparePairs orders package versions by package URL, then version.
func comparePairs(a, b model.PackageVersion) int {
	return cmp.Or(cmp.Compare(a.PURL, b.PURL), cmp.Compare(a.Version, b.Version))
}

// ecoDelta is the outcome of reading one ecosystem's delta against a stored
// cursor: the refs modified since it, newest first.
type ecoDelta struct {
	ecosystem string
	refs      []model.AdvisoryRef
}

// cursorKey returns the kv key of an ecosystem's delta cursor.
func cursorKey(ecosystem string) string { return "osv.modified." + ecosystem }

// readDeltas reads the delta of every ecosystem in use. An ecosystem with no
// cursor gets one at the earlier of its newest record and runStart, the time
// taken before the query batch, and contributes no refs; the others return
// the refs modified since their cursor. A failed download is returned in the
// collected errors and leaves that ecosystem's cursor alone; a store failure
// stops the run.
func (s *Service) readDeltas(ctx context.Context, all []model.RepoInterval, runStart time.Time, stats model.Stats) ([]ecoDelta, []error, error) {
	var (
		deltas    []ecoDelta
		collected []error
	)
	for _, eco := range ecosystems(all, s.extra) {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		key := cursorKey(eco)
		var cursor time.Time
		raw, err := s.kv.GetKV(ctx, key)
		switch {
		case errors.Is(err, model.ErrNotFound):
		case err != nil:
			return nil, nil, fmt.Errorf("read %s delta cursor: %w", eco, err)
		default:
			if cursor, err = time.Parse(time.RFC3339, raw); err != nil {
				return nil, nil, fmt.Errorf("read %s delta cursor: %w", eco, err)
			}
		}
		refs, err := s.src.ModifiedSince(ctx, eco, cursor)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, nil, ctxErr
			}
			s.logger.Warn("vulns delta download failed", "ecosystem", eco, "error", model.Redact(err.Error()))
			collected = append(collected, fmt.Errorf("read %s delta: %w", eco, err))
			continue
		}
		if cursor.IsZero() {
			// The query batch covers every record that existed when the run
			// started, so the first cursor is the newest record and none is
			// fetched. A record modified after the run started may have
			// missed the query batch, so the cursor goes no later than the
			// start and the next delta reads it.
			if newest, ok := newestModified(refs); ok {
				first := newest.UTC()
				if runStart.Before(first) {
					first = runStart
				}
				if err := s.kv.SetKV(ctx, key, first.Format(kvTimeLayout)); err != nil {
					return nil, nil, fmt.Errorf("store %s delta cursor: %w", eco, err)
				}
			}
			continue
		}
		stats["delta_refs"] += int64(len(refs))
		deltas = append(deltas, ecoDelta{ecosystem: eco, refs: refs})
	}
	return deltas, collected, nil
}

// ecosystems returns the OSV ecosystems used by any of the intervals plus
// extra, sorted and without repeats.
func ecosystems(all []model.RepoInterval, extra []string) []string {
	var out []string
	for _, ri := range all {
		if eco, ok := model.OSVEcosystem(ri.Interval.Package.PURL); ok {
			out = append(out, eco)
		}
	}
	for _, eco := range extra {
		if eco != "" {
			out = append(out, eco)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// newestModified returns the greatest Modified among refs, and false when
// refs is empty.
func newestModified(refs []model.AdvisoryRef) (time.Time, bool) {
	if len(refs) == 0 {
		return time.Time{}, false
	}
	newest := refs[0].Modified
	for _, r := range refs[1:] {
		if r.Modified.After(newest) {
			newest = r.Modified
		}
	}
	return newest, true
}

// usedPURLs returns the package URLs of the intervals as a set.
func usedPURLs(all []model.RepoInterval) map[string]bool {
	used := make(map[string]bool, len(all))
	for _, ri := range all {
		used[ri.Interval.Package.PURL] = true
	}
	return used
}

// fetchItem is one advisory to consider fetching: its newest known modified
// time and whether the query batch named it.
type fetchItem struct {
	modified  time.Time
	fromQuery bool
}

// fetchAll fetches every advisory in the query refs and deltas that is not
// stored or is stored with an older modified time, and saves it. A delta
// record whose id is not stored is saved only when it names a package in
// used; a record whose id is stored is saved whatever it names. It returns
// the ids whose fetch failed and those failures; a not-found record is
// counted and skipped, and a store failure stops the run.
func (s *Service) fetchAll(ctx context.Context, queryRefs []model.AdvisoryRef, deltas []ecoDelta, used map[string]bool, stats model.Stats) (map[string]bool, []error, error) {
	items := map[string]fetchItem{}
	add := func(r model.AdvisoryRef, fromQuery bool) {
		it, ok := items[r.ID]
		if !ok || r.Modified.After(it.modified) {
			it.modified = r.Modified
		}
		it.fromQuery = it.fromQuery || fromQuery
		items[r.ID] = it
	}
	for _, r := range queryRefs {
		add(r, true)
	}
	for _, d := range deltas {
		for _, r := range d.refs {
			add(r, false)
		}
	}
	if len(items) == 0 {
		return nil, nil, nil
	}
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	stored, err := s.store.AdvisoryModified(ctx, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("read stored advisory times: %w", err)
	}

	failed := map[string]bool{}
	var collected []error
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		it := items[id]
		// The store keeps whole seconds, so a fraction in the source's time
		// must not make an unchanged record look newer.
		if st, ok := stored[id]; ok && !it.modified.Truncate(time.Second).After(st) {
			continue
		}
		adv, err := s.src.Fetch(ctx, id)
		switch {
		case errors.Is(err, model.ErrNotFound):
			stats["fetch_not_found"]++
			continue
		case err != nil:
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, nil, ctxErr
			}
			s.logger.Warn("vulns advisory fetch failed", "advisory", id, "error", model.Redact(err.Error()))
			failed[id] = true
			collected = append(collected, fmt.Errorf("fetch advisory %s: %w", id, err))
			continue
		}
		stats["fetched"]++
		// A stored advisory is always saved, so a correction that drops a
		// package in use removes its match; only a new delta record must name
		// a package in use to be kept.
		if _, isStored := stored[id]; !isStored && !it.fromQuery && !affectsAny(adv, used) {
			stats["delta_irrelevant"]++
			continue
		}
		if err := s.store.SaveAdvisory(ctx, adv); err != nil {
			return nil, nil, fmt.Errorf("save advisory %s: %w", id, err)
		}
		stats["advisories_saved"]++
	}
	return failed, collected, nil
}

// affectsAny reports whether any affected entry of adv names a package in
// used.
func affectsAny(adv model.Advisory, used map[string]bool) bool {
	return slices.ContainsFunc(adv.Affected, func(a model.AffectedPackage) bool { return used[a.Package.PURL] })
}

// advanceCursors moves each delta's cursor to its newest ref, unless the
// fetch of one of its refs failed, so the next run reads those refs again.
func (s *Service) advanceCursors(ctx context.Context, deltas []ecoDelta, failed map[string]bool) error {
	for _, d := range deltas {
		newest, ok := newestModified(d.refs)
		if !ok || slices.ContainsFunc(d.refs, func(r model.AdvisoryRef) bool { return failed[r.ID] }) {
			continue
		}
		if err := s.kv.SetKV(ctx, cursorKey(d.ecosystem), newest.UTC().Format(kvTimeLayout)); err != nil {
			return fmt.Errorf("store %s delta cursor: %w", d.ecosystem, err)
		}
	}
	return nil
}

// refreshSignals refreshes each configured feed whose last refresh is older
// than the refresh gap and stores its signals. A failed refresh is counted,
// logged and returned in the collected errors, keeping the stored values; a
// store failure stops the run.
func (s *Service) refreshSignals(ctx context.Context, now time.Time, stats model.Stats) ([]error, error) {
	feeds := []struct {
		kind model.SignalKind
		src  ExploitSignals
	}{{model.SignalKEV, s.kev}, {model.SignalEPSS, s.epss}}
	var collected []error
	for _, f := range feeds {
		if f.src == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key := "signals." + string(f.kind) + ".refreshed_at"
		due, err := s.signalsDue(ctx, key, now)
		if err != nil {
			return nil, err
		}
		if !due {
			continue
		}
		signals, err := f.src.Refresh(ctx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			stats[string(f.kind)+"_errors"]++
			s.logger.Warn("vulns signal refresh failed", "feed", string(f.kind), "error", model.Redact(err.Error()))
			collected = append(collected, fmt.Errorf("refresh %s signals: %w", f.kind, err))
			continue
		}
		n, err := s.store.ApplySignals(ctx, signals, now)
		if err != nil {
			return nil, fmt.Errorf("store %s signals: %w", f.kind, err)
		}
		stats[string(f.kind)+"_updated"] += int64(n)
		if err := s.kv.SetKV(ctx, key, now.Format(kvTimeLayout)); err != nil {
			return nil, fmt.Errorf("store %s refresh time: %w", f.kind, err)
		}
	}
	return collected, nil
}

// signalsDue reports whether the feed whose refresh time is stored under key
// was never refreshed or was refreshed more than the refresh gap before now.
func (s *Service) signalsDue(ctx context.Context, key string, now time.Time) (bool, error) {
	raw, err := s.kv.GetKV(ctx, key)
	if errors.Is(err, model.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", key, err)
	}
	last, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", key, err)
	}
	return now.Sub(last) > s.every, nil
}

// intervalMatch is one advisory match with the interval it is on.
type intervalMatch struct {
	ri    model.RepoInterval
	match model.AdvisoryMatch
}

// buildMatches evaluates every advisory that is not withdrawn against every
// interval with a resolved version. An advisory with several entries for the
// interval's package matches once, through its first affected entry in record
// order. Matches come in interval order, then advisory order.
func buildMatches(intervals []model.RepoInterval, advisories []model.Advisory, basis exposureBasis) []intervalMatch {
	byPURL := map[string][]int{}
	for i, adv := range advisories {
		if adv.IsWithdrawn() {
			continue
		}
		var purls []string
		for _, aff := range adv.Affected {
			if !slices.Contains(purls, aff.Package.PURL) {
				purls = append(purls, aff.Package.PURL)
				byPURL[aff.Package.PURL] = append(byPURL[aff.Package.PURL], i)
			}
		}
	}
	var out []intervalMatch
	for _, ri := range intervals {
		iv := ri.Interval
		if iv.Version == "" {
			continue
		}
		for _, i := range byPURL[iv.Package.PURL] {
			adv := advisories[i]
			for _, aff := range adv.Affected {
				if aff.Package.PURL != iv.Package.PURL {
					continue
				}
				res := evaluate(adv, aff, iv)
				if !res.affected {
					continue
				}
				from, to := exposureWindow(iv, adv.Published, basis)
				out = append(out, intervalMatch{ri: ri, match: model.AdvisoryMatch{
					RepoID:           ri.RepoID,
					DepVersionID:     iv.ID,
					AdvisoryID:       adv.ID,
					ExposedFrom:      from,
					ExposedTo:        to,
					FixedVersion:     res.fixedVersion,
					Confidence:       res.confidence,
					ConfidenceReason: res.reason,
				}})
				break
			}
		}
	}
	return out
}

// replaceMatches stores the matches of every repo in scope, including repos
// left with none, in repo slug order.
func (s *Service) replaceMatches(ctx context.Context, inScope []model.RepoInterval, matches []intervalMatch, stats model.Stats) error {
	byRepo := map[int64][]model.AdvisoryMatch{}
	for _, m := range matches {
		byRepo[m.match.RepoID] = append(byRepo[m.match.RepoID], m.match)
		stats["matches"]++
		if m.match.IsOpen() {
			stats["matches_open"]++
		}
	}
	for _, repoID := range repoIDs(inScope) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.store.ReplaceMatches(ctx, repoID, byRepo[repoID]); err != nil {
			return fmt.Errorf("replace advisory matches: %w", err)
		}
	}
	return nil
}

// repoIDs returns the repo IDs of the intervals in their order, without
// repeats.
func repoIDs(intervals []model.RepoInterval) []int64 {
	var ids []int64
	for _, ri := range intervals {
		if !slices.Contains(ids, ri.RepoID) {
			ids = append(ids, ri.RepoID)
		}
	}
	return ids
}

// writeFindings upserts one finding per repo, package and advisory with a
// match, then resolves the unresolved advisory findings in scope that were
// not upserted: every one on an unscoped run, including those of repos left
// with no interval, and on a scoped run those of the repo in scope.
func (s *Service) writeFindings(ctx context.Context, scope model.Scope, inScope []model.RepoInterval, matches []intervalMatch, advisories []model.Advisory, now time.Time, stats model.Stats) error {
	byID := make(map[string]model.Advisory, len(advisories))
	for _, adv := range advisories {
		byID[adv.ID] = adv
	}
	produced := map[string]bool{}
	for _, f := range buildFindings(matches, byID, now) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := s.findings.UpsertFinding(ctx, f); err != nil {
			return fmt.Errorf("write advisory finding: %w", err)
		}
		produced[f.Fingerprint] = true
		stats["findings_upserted"]++
	}

	unresolved, err := s.findings.ListUnresolvedFindings(ctx, model.KindAdvisory)
	if err != nil {
		return fmt.Errorf("list unresolved advisory findings: %w", err)
	}
	repos := repoIDs(inScope)
	for _, f := range unresolved {
		if produced[f.Fingerprint] || (!scope.All() && !slices.Contains(repos, f.RepoID)) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.findings.ResolveFinding(ctx, f.Fingerprint, now); err != nil {
			return fmt.Errorf("resolve advisory finding: %w", err)
		}
		stats["findings_resolved"]++
	}
	return nil
}

// findingKey groups the matches of one finding.
type findingKey struct {
	repoSlug   string
	purl       string
	advisoryID string
}

// fingerprint returns the finding fingerprint of k.
func (k findingKey) fingerprint() string {
	return "advisory|" + k.repoSlug + "|" + k.purl + "|" + k.advisoryID
}

// buildFindings returns one advisory finding per repo, package and advisory
// among matches, ordered by repo slug, package URL and advisory id. byID must
// hold every matched advisory.
func buildFindings(matches []intervalMatch, byID map[string]model.Advisory, now time.Time) []model.Finding {
	groups := map[findingKey][]intervalMatch{}
	var keys []findingKey
	for _, m := range matches {
		k := findingKey{m.ri.RepoSlug, m.ri.Interval.Package.PURL, m.match.AdvisoryID}
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], m)
	}
	slices.SortFunc(keys, func(a, b findingKey) int {
		return cmp.Or(cmp.Compare(a.repoSlug, b.repoSlug), cmp.Compare(a.purl, b.purl),
			cmp.Compare(a.advisoryID, b.advisoryID))
	})

	out := make([]model.Finding, 0, len(keys))
	for _, k := range keys {
		group := groups[k]
		link := linkedMatch(group)
		// The ranking describes the open matches when there are any, and
		// every match when the exposure is historical only.
		ranked := slices.DeleteFunc(slices.Clone(group), func(m intervalMatch) bool { return !m.match.IsOpen() })
		exposed := len(ranked) > 0
		if !exposed {
			ranked = group
		}
		in := model.AdvisoryRankInput{CurrentlyExposed: exposed}
		adv := byID[k.advisoryID]
		in.Severity = adv.Severity
		in.KEV = adv.KEV
		in.HasEPSS = adv.HasEPSS
		in.EPSSPercentile = adv.EPSSPercentile
		for _, m := range ranked {
			if m.ri.Interval.Direct == model.DirectYes {
				in.Direct = model.DirectYes
			}
			if confidenceRank(m.match.Confidence) > confidenceRank(in.Confidence) {
				in.Confidence = m.match.Confidence
			}
		}
		score, reason := model.RankAdvisory(in)
		if link.match.FixedVersion != "" {
			reason += "; fixed in " + link.match.FixedVersion
		}
		out = append(out, model.Finding{
			Kind:          model.KindAdvisory,
			RepoID:        link.ri.RepoID,
			PackageID:     link.ri.PackageID,
			DepVersionID:  link.ri.Interval.ID,
			AdvisoryID:    k.advisoryID,
			Fingerprint:   k.fingerprint(),
			Score:         score,
			RankReason:    reason,
			LastChangedAt: now,
		})
	}
	return out
}

// linkedMatch returns the match a finding links to: on the open interval
// that opened last, or when none is open, on the closed interval that closed
// last. Ties go to the earlier match. group must not be empty.
func linkedMatch(group []intervalMatch) intervalMatch {
	link := group[0]
	for _, m := range group[1:] {
		cur, cand := link.ri.Interval, m.ri.Interval
		switch {
		case cand.IsOpen() && !cur.IsOpen():
			link = m
		case cand.IsOpen() && cur.IsOpen() && cand.From.After(cur.From):
			link = m
		case !cand.IsOpen() && !cur.IsOpen() && cand.To.After(cur.To):
			link = m
		}
	}
	return link
}

// confidenceRank orders confidence labels: high above medium above low above
// anything else.
func confidenceRank(c model.Confidence) int {
	switch c {
	case model.ConfidenceHigh:
		return 3
	case model.ConfidenceMedium:
		return 2
	case model.ConfidenceLow:
		return 1
	}
	return 0
}

// redactedError carries the redacted text of the run's collected errors as
// its message and unwraps to them, so errors.Is still sees each cause.
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }

func (e *redactedError) Unwrap() error { return e.err }

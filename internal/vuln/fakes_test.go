package vuln_test

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln"
)

var (
	_ vuln.AdvisorySource = (*fakeSource)(nil)
	_ vuln.ExploitSignals = (*fakeFeed)(nil)
	_ vuln.AdvisoryStore  = (*fakeStore)(nil)
	_ vuln.KVStore        = (*fakeStore)(nil)
	_ vuln.FindingStore   = (*fakeStore)(nil)
)

// modCall records one ModifiedSince call.
type modCall struct {
	ecosystem string
	since     time.Time
}

// fakeSource is an advisory database held in memory.
type fakeSource struct {
	mu sync.Mutex
	// queryRefs maps a queried pair to the refs the query batch returns.
	queryRefs map[model.PackageVersion][]model.AdvisoryRef
	// records are what Fetch returns, by id; an id missing here is not
	// found.
	records map[string]model.Advisory
	// modified holds each ecosystem's modified list, newest first.
	modified map[string][]model.AdvisoryRef
	// queryErr, fetchErr and modErr make the calls fail; fetchErr only for
	// the ids it names.
	queryErr error
	fetchErr map[string]error
	modErr   map[string]error

	queryCalls [][]model.PackageVersion
	fetchCalls []string
	modCalls   []modCall
}

func newFakeSource() *fakeSource {
	return &fakeSource{
		queryRefs: map[model.PackageVersion][]model.AdvisoryRef{},
		records:   map[string]model.Advisory{},
		modified:  map[string][]model.AdvisoryRef{},
		fetchErr:  map[string]error{},
		modErr:    map[string]error{},
	}
}

func (f *fakeSource) QueryBatch(ctx context.Context, pvs []model.PackageVersion) ([]model.AdvisoryRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queryCalls = append(f.queryCalls, slices.Clone(pvs))
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	var out []model.AdvisoryRef
	for _, pv := range pvs {
		for _, r := range f.queryRefs[pv] {
			if !slices.ContainsFunc(out, func(o model.AdvisoryRef) bool { return o.ID == r.ID }) {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

func (f *fakeSource) Fetch(ctx context.Context, id string) (model.Advisory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetchCalls = append(f.fetchCalls, id)
	if err := f.fetchErr[id]; err != nil {
		return model.Advisory{}, err
	}
	a, ok := f.records[id]
	if !ok {
		return model.Advisory{}, fmt.Errorf("advisory %s: %w", id, model.ErrNotFound)
	}
	return a, nil
}

func (f *fakeSource) ModifiedSince(ctx context.Context, ecosystem string, since time.Time) ([]model.AdvisoryRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.modCalls = append(f.modCalls, modCall{ecosystem, since})
	if err := f.modErr[ecosystem]; err != nil {
		return nil, err
	}
	var out []model.AdvisoryRef
	for _, r := range f.modified[ecosystem] {
		if !r.Modified.After(since) {
			break
		}
		out = append(out, r)
	}
	return out, nil
}

// resetCalls forgets the recorded calls.
func (f *fakeSource) resetCalls() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queryCalls, f.fetchCalls, f.modCalls = nil, nil, nil
}

// fakeFeed is an exploit-signal feed returning canned signals.
type fakeFeed struct {
	signals []model.Signal
	err     error
	calls   int
}

func (f *fakeFeed) Refresh(ctx context.Context) ([]model.Signal, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.signals, nil
}

// replaceCall records one ReplaceMatches call.
type replaceCall struct {
	repoID  int64
	matches []model.AdvisoryMatch
}

// fakeStore keeps intervals, advisories, matches, kv values and findings in
// memory, with the store's documented behaviour.
type fakeStore struct {
	mu         sync.Mutex
	intervals  []model.RepoInterval
	queried    map[model.PackageVersion]time.Time
	advisories map[string]model.Advisory
	matches    map[int64][]model.AdvisoryMatch
	kv         map[string]string
	findings   []model.Finding
	nextID     int64

	listErr    error
	replaceErr error
	upsertErr  error

	replaceCalls []replaceCall
	upserts      []model.Finding
	resolved     []string
}

func newFakeStore(intervals ...model.RepoInterval) *fakeStore {
	return &fakeStore{
		intervals:  intervals,
		queried:    map[model.PackageVersion]time.Time{},
		advisories: map[string]model.Advisory{},
		matches:    map[int64][]model.AdvisoryMatch{},
		kv:         map[string]string{},
	}
}

func (s *fakeStore) ListAllIntervals(ctx context.Context) ([]model.RepoInterval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listErr != nil {
		return nil, s.listErr
	}
	out := slices.Clone(s.intervals)
	slices.SortStableFunc(out, func(a, b model.RepoInterval) int {
		return cmp.Or(cmp.Compare(a.RepoSlug, b.RepoSlug),
			cmp.Compare(a.Interval.Package.PURL, b.Interval.Package.PURL),
			cmp.Compare(a.Interval.ManifestPath, b.Interval.ManifestPath),
			a.Interval.From.Compare(b.Interval.From))
	})
	return out, nil
}

func (s *fakeStore) QueriedVersions(ctx context.Context) ([]model.PackageVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.PackageVersion
	for pv := range s.queried {
		out = append(out, pv)
	}
	return out, nil
}

func (s *fakeStore) MarkQueried(ctx context.Context, pvs []model.PackageVersion, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, pv := range pvs {
		s.queried[pv] = at
	}
	return nil
}

func (s *fakeStore) AdvisoryModified(ctx context.Context, ids []string) (map[string]time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]time.Time{}
	for _, id := range ids {
		if a, ok := s.advisories[id]; ok {
			out[id] = a.Modified.Truncate(time.Second)
		}
	}
	return out, nil
}

func (s *fakeStore) SaveAdvisory(ctx context.Context, a model.Advisory) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.advisories[a.ID]; ok {
		a.KEV, a.KEVAddedAt = old.KEV, old.KEVAddedAt
		a.HasEPSS, a.EPSS, a.EPSSPercentile, a.EPSSAt = old.HasEPSS, old.EPSS, old.EPSSPercentile, old.EPSSAt
	}
	s.advisories[a.ID] = a
	return nil
}

func (s *fakeStore) ListAdvisories(ctx context.Context) ([]model.Advisory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Advisory
	for _, a := range s.advisories {
		a.Raw = nil
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b model.Advisory) int { return cmp.Compare(a.ID, b.ID) })
	return out, nil
}

func (s *fakeStore) ApplySignals(ctx context.Context, signals []model.Signal, at time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated := map[string]bool{}
	for _, sig := range signals {
		for id, a := range s.advisories {
			if !slices.Contains(a.CVEs(), sig.CVE) {
				continue
			}
			switch sig.Kind {
			case model.SignalKEV:
				a.KEV, a.KEVAddedAt = true, sig.KEVAddedAt
			case model.SignalEPSS:
				a.HasEPSS, a.EPSS, a.EPSSPercentile, a.EPSSAt = true, sig.EPSS, sig.EPSSPercentile, at
			}
			s.advisories[id] = a
			updated[id] = true
		}
	}
	return len(updated), nil
}

func (s *fakeStore) ReplaceMatches(ctx context.Context, repoID int64, matches []model.AdvisoryMatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replaceCalls = append(s.replaceCalls, replaceCall{repoID, slices.Clone(matches)})
	if s.replaceErr != nil {
		return s.replaceErr
	}
	s.matches[repoID] = slices.Clone(matches)
	return nil
}

func (s *fakeStore) GetKV(ctx context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.kv[key]
	if !ok {
		return "", fmt.Errorf("kv %q: %w", key, model.ErrNotFound)
	}
	return v, nil
}

func (s *fakeStore) SetKV(ctx context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kv[key] = value
	return nil
}

func (s *fakeStore) UpsertFinding(ctx context.Context, f model.Finding) (model.Finding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upserts = append(s.upserts, f)
	if s.upsertErr != nil {
		return model.Finding{}, s.upsertErr
	}
	for i, old := range s.findings {
		if old.Fingerprint != f.Fingerprint {
			continue
		}
		if old.Score != f.Score || old.RankReason != f.RankReason || !old.ResolvedAt.IsZero() {
			old.LastChangedAt = f.LastChangedAt
		}
		old.DepVersionID, old.Score, old.RankReason, old.ResolvedAt = f.DepVersionID, f.Score, f.RankReason, time.Time{}
		s.findings[i] = old
		return old, nil
	}
	s.nextID++
	f.ID = s.nextID
	f.FirstSeenAt = f.LastChangedAt
	f.State = model.FindingState{State: model.StateOpen}
	s.findings = append(s.findings, f)
	return f, nil
}

func (s *fakeStore) ResolveFinding(ctx context.Context, fingerprint string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolved = append(s.resolved, fingerprint)
	for i, f := range s.findings {
		if f.Fingerprint == fingerprint && f.ResolvedAt.IsZero() {
			s.findings[i].ResolvedAt, s.findings[i].LastChangedAt = at, at
		}
	}
	return nil
}

func (s *fakeStore) ListUnresolvedFindings(ctx context.Context, kind model.FindingKind) ([]model.Finding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Finding
	for _, f := range s.findings {
		if f.Kind == kind && f.ResolvedAt.IsZero() {
			out = append(out, f)
		}
	}
	return out, nil
}

// finding returns the stored finding with this fingerprint, and false when
// there is none.
func (s *fakeStore) finding(fp string) (model.Finding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.findings {
		if f.Fingerprint == fp {
			return f, true
		}
	}
	return model.Finding{}, false
}

// resetCalls forgets the recorded calls.
func (s *fakeStore) resetCalls() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replaceCalls, s.upserts, s.resolved = nil, nil, nil
}

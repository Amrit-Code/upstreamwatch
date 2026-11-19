package timeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// jobName is the timeline job's name in config schedules and upw run.
const jobName = "timeline"

// blobChunkSize caps the refs passed to one ReadBlobs call.
const blobChunkSize = 500

// statKeys are the counters every run reports, present even when zero.
var statKeys = []string{
	"targets", "skipped", "failed", "resets", "commits_walked", "files_read",
	"events_written", "intervals", "parse_errors", "files_skipped", "files_excluded",
}

// TargetSettings are the timeline settings of one configured target.
type TargetSettings struct {
	Slug string
	// Unity adds the Unity extractor for this target.
	Unity bool
	// Ecosystems, when not empty, keeps only dependencies whose
	// Package.Ecosystem is listed.
	Ecosystems []string
	// ExcludePaths are directory prefixes (relative to the repository root,
	// slash-separated) whose manifests are not indexed for this target.
	ExcludePaths []string
}

// Options configures a Service.
type Options struct {
	Targets []TargetSettings // config order
}

// Service is the timeline job: per target it walks the indexed branch from
// the last indexed commit, turns manifest changes into dependency events,
// folds them into intervals and stores the result in one update.
type Service struct {
	git        GitSource
	repos      RepoStore
	store      TimelineStore
	extractors []Extractor
	unity      Extractor
	opts       Options
	logger     *slog.Logger
}

// NewService returns a timeline Service. extractors are offered every
// manifest in the order given; unity is offered first for targets with Unity
// set and must be non-nil when any target sets it. It does no I/O.
func NewService(git GitSource, repos RepoStore, store TimelineStore, extractors []Extractor, unity Extractor, opts Options, logger *slog.Logger) *Service {
	return &Service{
		git:        git,
		repos:      repos,
		store:      store,
		extractors: extractors,
		unity:      unity,
		opts:       opts,
		logger:     logger,
	}
}

// Name returns "timeline".
func (s *Service) Name() string { return jobName }

// Run indexes every target in scope, in config order. A target that is not
// stored or was never fetched is skipped. A target whose git or store call
// fails is logged and the others continue; the returned error then names
// every failed target. The stats count the work done, including the work of
// failed targets. When ctx is done Run stops before the next target and
// returns ctx's error.
func (s *Service) Run(ctx context.Context, scope model.Scope) (model.Stats, error) {
	stats := model.Stats{}
	for _, k := range statKeys {
		stats[k] = 0
	}
	targets, err := s.targetsInScope(scope)
	if err != nil {
		return stats, err
	}

	var failures []string
	for _, t := range targets {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		stats["targets"]++
		ts, err := s.indexTarget(ctx, t)
		for k, v := range ts {
			stats[k] += v
		}
		if err != nil {
			stats["failed"]++
			s.logger.Warn("timeline target failed", "repo", t.Slug, "error", err)
			failures = append(failures, t.Slug+": "+err.Error())
		}
		// A cancellation during the target, often seen as that target's
		// failure, ends the run with the context's error, not the summary.
		if err := ctx.Err(); err != nil {
			return stats, err
		}
	}
	if len(failures) > 0 {
		return stats, fmt.Errorf("timeline: %d of %d targets failed: %s",
			len(failures), stats["targets"], strings.Join(failures, "; "))
	}
	return stats, nil
}

// targetsInScope returns every configured target, or the one named by scope.
func (s *Service) targetsInScope(scope model.Scope) ([]TargetSettings, error) {
	if scope.All() {
		return s.opts.Targets, nil
	}
	for _, t := range s.opts.Targets {
		if t.Slug == scope.RepoSlug {
			return []TargetSettings{t}, nil
		}
	}
	return nil, fmt.Errorf("timeline: unknown target %q", scope.RepoSlug)
}

// indexTarget brings one target's timeline up to its branch head and returns
// the target's counters, which on an error hold the work done before it.
// Nothing is stored unless ApplyTimeline succeeds.
func (s *Service) indexTarget(ctx context.Context, t TargetSettings) (model.Stats, error) {
	st := model.Stats{}
	repo, err := s.repos.GetRepo(ctx, t.Slug)
	if errors.Is(err, model.ErrNotFound) {
		s.logger.Warn("timeline target skipped", "repo", t.Slug, "reason", "not stored")
		st["skipped"] = 1
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("read repo: %w", err)
	}
	if repo.ClonePath == "" || repo.DefaultBranch == "" {
		s.logger.Warn("timeline target skipped", "repo", t.Slug, "reason", "never fetched")
		st["skipped"] = 1
		return st, nil
	}

	head, err := s.git.Head(ctx, repo)
	if err != nil {
		return st, fmt.Errorf("read head: %w", err)
	}
	if head == repo.LastIndexedSHA {
		return st, nil
	}

	from := repo.LastIndexedSHA
	reset := false
	if from != "" {
		ok, err := s.git.IsAncestor(ctx, repo, from, head)
		if err != nil {
			return st, fmt.Errorf("check ancestry: %w", err)
		}
		if !ok {
			s.logger.Warn("timeline history rewritten, rebuilding from root",
				"repo", t.Slug, "last_indexed_sha", from, "head", head)
			reset = true
			from = ""
			st["resets"] = 1
		}
	}
	var stored []model.DepEvent
	if !reset {
		stored, err = s.store.ListEvents(ctx, repo.ID)
		if err != nil {
			return st, fmt.Errorf("list events: %w", err)
		}
	}
	state := stateFromEvents(stored)

	exts := s.targetExtractors(t)
	commits, err := s.git.ManifestCommits(ctx, repo, from, head, unionPatterns(exts))
	if err != nil {
		return st, fmt.Errorf("list manifest commits: %w", err)
	}
	st["commits_walked"] = int64(len(commits))
	// Excluded paths leave the commits before any blob is read, so they cost
	// no read and produce no events or manifest rows.
	commits, st["files_excluded"] = dropExcluded(commits, t.ExcludePaths)

	var refs []model.BlobRef
	for _, c := range commits {
		for _, p := range c.Paths {
			refs = append(refs, model.BlobRef{Commit: c.SHA, Path: p})
		}
	}
	blobs := &blobQueue{git: s.git, repo: repo, refs: refs}

	manifests := map[string]model.ManifestInfo{}
	var events []model.DepEvent
	for _, c := range commits {
		var changes []manifestChange
		for _, path := range c.Paths {
			b, err := blobs.next(ctx)
			if err != nil {
				return st, fmt.Errorf("read blobs: %w", err)
			}
			switch {
			case b.Missing:
				changes = append(changes, manifestChange{Path: path, Deleted: true})
			case b.TooLarge:
				s.logger.Warn("timeline manifest too large, skipped",
					"repo", t.Slug, "commit", c.SHA, "path", path)
				st["files_skipped"]++
			default:
				pm, handled, err := parseManifest(exts, path, b.Content)
				if !handled {
					continue
				}
				if err != nil {
					s.logger.Warn("timeline manifest parse failed",
						"repo", t.Slug, "commit", c.SHA, "path", path, "error", err)
					st["parse_errors"]++
					continue
				}
				st["files_read"]++
				manifests[path] = model.ManifestInfo{Ecosystem: pm.Ecosystem, Kind: pm.Kind}
				changes = append(changes, manifestChange{
					Path: path,
					Deps: filterDeps(pm.Dependencies, t.Ecosystems),
				})
			}
		}
		events = append(events, diffCommit(c, state, changes)...)
	}

	all := make([]model.DepEvent, 0, len(stored)+len(events))
	all = append(all, stored...)
	all = append(all, events...)
	intervals := fold(all)

	st["events_written"] = int64(len(events))
	st["intervals"] = int64(len(intervals))
	err = s.store.ApplyTimeline(ctx, model.TimelineUpdate{
		RepoID:     repo.ID,
		Reset:      reset,
		Commits:    commits,
		Manifests:  manifests,
		Events:     events,
		Intervals:  intervals,
		IndexedSHA: head,
	})
	if err != nil {
		return st, fmt.Errorf("apply timeline: %w", err)
	}
	return st, nil
}

// targetExtractors returns the extractors offered t's manifests: the Unity
// extractor first when t.Unity is set, then the configured ones in order.
func (s *Service) targetExtractors(t TargetSettings) []Extractor {
	exts := make([]Extractor, 0, len(s.extractors)+1)
	if t.Unity {
		exts = append(exts, s.unity)
	}
	return append(exts, s.extractors...)
}

// parseManifest offers content to each extractor in order. The first that
// does not report model.ErrUnsupportedFile handles it; handled is false when
// none does.
func parseManifest(exts []Extractor, path string, content []byte) (pm model.ParsedManifest, handled bool, err error) {
	for _, e := range exts {
		pm, err := e.Parse(path, content)
		if errors.Is(err, model.ErrUnsupportedFile) {
			continue
		}
		return pm, true, err
	}
	return model.ParsedManifest{}, false, nil
}

// blobQueue hands out the blobs of refs in order, reading them through
// GitSource in chunks of at most blobChunkSize refs, so only one chunk of
// file contents is held at a time.
type blobQueue struct {
	git  GitSource
	repo model.TargetRepo
	refs []model.BlobRef
	read int
	buf  []model.Blob
}

func (q *blobQueue) next(ctx context.Context) (model.Blob, error) {
	// Callers ask for exactly one blob per ref, so the buffer is empty here
	// only while refs remain unread.
	if len(q.buf) == 0 {
		end := min(q.read+blobChunkSize, len(q.refs))
		chunk := q.refs[q.read:end]
		blobs, err := q.git.ReadBlobs(ctx, q.repo, chunk)
		if err != nil {
			return model.Blob{}, err
		}
		if len(blobs) != len(chunk) {
			return model.Blob{}, fmt.Errorf("got %d blobs for %d refs", len(blobs), len(chunk))
		}
		q.buf = blobs
		q.read = end
	}
	b := q.buf[0]
	q.buf = q.buf[1:]
	return b, nil
}

package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// jobName is the job's name in the config schedules, in "upw run" and in the
// runs table.
const jobName = "ingest"

// The keys of the stats a run reports.
const (
	statTargets = "targets"
	statFetched = "fetched"
	statCloned  = "cloned"
	statFailed  = "failed"
)

// recordTimeout bounds RecordFetch, which runs on a context detached from the
// caller's cancellation.
const recordTimeout = 10 * time.Second

// Options configures a Service.
type Options struct {
	// Targets are the configured targets in config order, with Slug, URL,
	// Branch, Token and AllowPrivateHost set.
	Targets []model.TargetRepo
	// Now returns the current time; nil means time.Now.
	Now func() time.Time
}

// Service is the ingest job: for each configured target it stores the repo,
// syncs its bare clone through GitSource and records the fetch attempt. One
// failing target does not stop the others.
type Service struct {
	git     GitSource
	repos   RepoStore
	targets []model.TargetRepo
	now     func() time.Time
	logger  *slog.Logger
}

// NewService returns a Service that syncs opts.Targets through git and records
// them in repos. It does no I/O.
func NewService(git GitSource, repos RepoStore, opts Options, logger *slog.Logger) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		git:     git,
		repos:   repos,
		targets: slices.Clone(opts.Targets),
		now:     now,
		logger:  logger.With("job", jobName),
	}
}

// Name returns "ingest".
func (s *Service) Name() string { return jobName }

// Run syncs every target in scope, in config order. The stats always hold
// targets, fetched, cloned and failed. A scope naming no configured target is
// an error returned before any work. If ctx is done before a target, or by
// the time the last one ends, Run stops and returns an error wrapping
// ctx.Err(). Otherwise it returns nil when every target synced and was
// recorded, or one error listing each failed target as
// "ingest: <n> of <m> targets failed: <slug>: <error>; ...". Error text that
// Run logs, stores or returns passes through model.Redact.
func (s *Service) Run(ctx context.Context, scope model.Scope) (model.Stats, error) {
	stats := model.Stats{statTargets: 0, statFetched: 0, statCloned: 0, statFailed: 0}

	targets, err := s.inScope(scope)
	if err != nil {
		return stats, err
	}

	var failures []targetError
	for _, t := range targets {
		if err := ctx.Err(); err != nil {
			return stats, fmt.Errorf("ingest stopped before target %s: %w", t.Slug, err)
		}
		stats[statTargets]++

		res, synced, err := s.syncTarget(ctx, t)
		if synced {
			stats[statFetched]++
			if res.Cloned {
				stats[statCloned]++
			}
		}
		// Only the slug identifies the target in a log line: the TargetRepo
		// value carries the token.
		switch {
		case err == nil:
			s.logger.Info("target synced", "slug", t.Slug, "branch", res.Branch, "head", res.Head, "cloned", res.Cloned)
		case synced:
			stats[statFailed]++
			failures = append(failures, targetError{slug: t.Slug, err: err})
			s.logger.Warn("target fetch not recorded", "slug", t.Slug, "error", model.Redact(err.Error()))
		default:
			stats[statFailed]++
			failures = append(failures, targetError{slug: t.Slug, err: err})
			s.logger.Warn("target sync failed", "slug", t.Slug, "error", model.Redact(err.Error()))
		}
	}

	if err := ctx.Err(); err != nil {
		return stats, fmt.Errorf("ingest stopped: %w", err)
	}
	if len(failures) > 0 {
		return stats, &runError{total: len(targets), failures: failures}
	}
	return stats, nil
}

// inScope returns the targets scope covers, in config order.
func (s *Service) inScope(scope model.Scope) ([]model.TargetRepo, error) {
	if scope.All() {
		return s.targets, nil
	}
	i := slices.IndexFunc(s.targets, func(t model.TargetRepo) bool { return t.Slug == scope.RepoSlug })
	if i < 0 {
		return nil, fmt.Errorf("ingest: no target with slug %q", scope.RepoSlug)
	}
	return s.targets[i : i+1], nil
}

// syncTarget stores the repo, syncs it and records the attempt. synced
// reports whether Sync succeeded. A sync error is recorded and returned; an
// error from storing or recording is returned as well.
func (s *Service) syncTarget(ctx context.Context, t model.TargetRepo) (res model.SyncResult, synced bool, err error) {
	stored, err := s.repos.UpsertRepo(ctx, t.Slug, t.URL)
	if err != nil {
		return model.SyncResult{}, false, fmt.Errorf("upsert repo: %w", err)
	}

	repo := model.TargetRepo{
		ID:               stored.ID,
		Slug:             t.Slug,
		URL:              t.URL,
		Branch:           t.Branch,
		Token:            t.Token,
		AllowPrivateHost: t.AllowPrivateHost,
	}
	res, syncErr := s.git.Sync(ctx, repo)

	fetch := model.FetchResult{RepoID: stored.ID, At: s.now().UTC()}
	if syncErr != nil {
		// GitSource does not promise a credential-free error.
		fetch.Error = model.Redact(syncErr.Error())
	} else {
		fetch.Branch = res.Branch
		fetch.ClonePath = res.ClonePath
	}

	// The attempt is recorded even when ctx was cancelled during Sync, so the
	// repos row describes the latest attempt.
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()
	if err := s.repos.RecordFetch(recordCtx, fetch); err != nil {
		recordErr := fmt.Errorf("record fetch: %w", err)
		if syncErr != nil {
			return res, false, fmt.Errorf("%w (%w)", syncErr, recordErr)
		}
		return res, true, recordErr
	}
	return res, syncErr == nil, syncErr
}

// targetError is one target's failure in a run.
type targetError struct {
	slug string
	err  error
}

// runError is the error of a run in which at least one target failed. Its
// text passes each target's error through model.Redact.
type runError struct {
	total    int
	failures []targetError
}

func (e *runError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "ingest: %d of %d targets failed: ", len(e.failures), e.total)
	for i, f := range e.failures {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(f.slug)
		b.WriteString(": ")
		b.WriteString(model.Redact(f.err.Error()))
	}
	return b.String()
}

// Unwrap returns each target's error, so errors.Is sees through a run error.
func (e *runError) Unwrap() []error {
	errs := make([]error, len(e.failures))
	for i, f := range e.failures {
		errs[i] = f.err
	}
	return errs
}

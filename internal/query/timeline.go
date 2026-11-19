package query

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// TimelineFilter narrows a repo's intervals. Zero fields do not filter.
type TimelineFilter struct {
	Package  string    // exact version-less package URL
	Contains string    // case-insensitive substring of the package URL
	On       time.Time // only intervals in force at this instant (DepInterval.Contains)
}

// keep reports whether iv passes every non-zero field of f.
func (f TimelineFilter) keep(iv model.DepInterval) bool {
	if f.Package != "" && iv.Package.PURL != f.Package {
		return false
	}
	if f.Contains != "" && !strings.Contains(strings.ToLower(iv.Package.PURL), strings.ToLower(f.Contains)) {
		return false
	}
	if !f.On.IsZero() && !iv.Contains(f.On) {
		return false
	}
	return true
}

// Timeline returns the repo and its intervals that pass f, in store order.
// An unknown slug is an error wrapping model.ErrNotFound.
func (s *Service) Timeline(ctx context.Context, slug string, f TimelineFilter) (model.TargetRepo, []model.DepInterval, error) {
	repo, err := s.timeline.GetRepo(ctx, slug)
	if err != nil {
		return model.TargetRepo{}, nil, fmt.Errorf("read timeline of %q: %w", slug, err)
	}
	all, err := s.timeline.ListIntervals(ctx, repo.ID)
	if err != nil {
		return model.TargetRepo{}, nil, fmt.Errorf("read timeline of %q: %w", slug, err)
	}
	var kept []model.DepInterval
	for _, iv := range all {
		if f.keep(iv) {
			kept = append(kept, iv)
		}
	}
	return repo, kept, nil
}

// Repos returns every stored repo ordered by slug.
func (s *Service) Repos(ctx context.Context) ([]model.TargetRepo, error) {
	repos, err := s.timeline.ListRepos(ctx)
	if err != nil {
		return nil, fmt.Errorf("read repos: %w", err)
	}
	return repos, nil
}

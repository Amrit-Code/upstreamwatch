package query

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// Service answers the read-only questions of the web pages and the CLI. It
// returns what the store holds and adds no logic.
type Service struct {
	runs     RunStore
	findings FindingStore
	logger   *slog.Logger
}

// NewService returns a Service that reads runs and findings from the given
// stores.
func NewService(runs RunStore, findings FindingStore, logger *slog.Logger) *Service {
	return &Service{runs: runs, findings: findings, logger: logger}
}

// LastRuns returns the most recent run of each job, ordered by job name.
func (s *Service) LastRuns(ctx context.Context) ([]model.Run, error) {
	runs, err := s.runs.LatestRuns(ctx)
	if err != nil {
		return nil, fmt.Errorf("read latest runs: %w", err)
	}
	return runs, nil
}

// OpenFindings returns the findings whose state is open or acked, highest
// score first.
func (s *Service) OpenFindings(ctx context.Context) ([]model.Finding, error) {
	findings, err := s.findings.ListOpenFindings(ctx)
	if err != nil {
		return nil, fmt.Errorf("read open findings: %w", err)
	}
	return findings, nil
}

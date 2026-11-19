package query

import (
	"context"
	"fmt"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// Findings returns the detail of every unresolved finding that passes f, in
// the store's order: highest score first, then lowest ID. A zero f.Now is set
// to the current time. It returns nil when no finding passes.
func (s *Service) Findings(ctx context.Context, f model.FindingFilter) ([]model.FindingDetail, error) {
	if f.Now.IsZero() {
		f.Now = s.now().UTC()
	}
	findings, err := s.findings.ListFindings(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("read findings: %w", err)
	}
	var out []model.FindingDetail
	for _, fd := range findings {
		d, err := s.findings.GetFindingDetail(ctx, fd.ID)
		if err != nil {
			return nil, fmt.Errorf("read findings: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// FindingDetail returns the finding with this ID and everything shown with
// it. An unknown ID is an error wrapping model.ErrNotFound.
func (s *Service) FindingDetail(ctx context.Context, id int64) (model.FindingDetail, error) {
	d, err := s.findings.GetFindingDetail(ctx, id)
	if err != nil {
		return model.FindingDetail{}, fmt.Errorf("read finding %d: %w", id, err)
	}
	return d, nil
}

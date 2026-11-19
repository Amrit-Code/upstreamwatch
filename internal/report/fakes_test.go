package report

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// fakeStore is an in-memory Store. Findings are returned in the order given;
// MarkReported updates them, so a second run sees what the first marked.
type fakeStore struct {
	findings []model.Finding
	// details overrides the detail of a finding; without an entry the detail
	// holds only the finding.
	details map[int64]model.FindingDetail
	runs    []model.Run
	digests []model.DigestRecord

	filters  []model.FindingFilter
	marked   [][]int64
	markedAt []time.Time
	detailed []int64

	listErr, detailErr, markErr, runsErr    error
	saveErr, failedErr, retriedErr, lastErr error
}

func (s *fakeStore) ListFindings(_ context.Context, f model.FindingFilter) ([]model.Finding, error) {
	s.filters = append(s.filters, f)
	if s.listErr != nil {
		return nil, s.listErr
	}
	return slices.Clone(s.findings), nil
}

func (s *fakeStore) GetFindingDetail(_ context.Context, id int64) (model.FindingDetail, error) {
	s.detailed = append(s.detailed, id)
	if s.detailErr != nil {
		return model.FindingDetail{}, s.detailErr
	}
	for _, f := range s.findings {
		if f.ID != id {
			continue
		}
		d := s.details[id]
		d.Finding = f
		return d, nil
	}
	return model.FindingDetail{}, fmt.Errorf("finding %d: %w", id, model.ErrNotFound)
}

func (s *fakeStore) MarkReported(_ context.Context, ids []int64, at time.Time) error {
	if s.markErr != nil {
		return s.markErr
	}
	s.marked = append(s.marked, slices.Clone(ids))
	s.markedAt = append(s.markedAt, at)
	for i := range s.findings {
		if slices.Contains(ids, s.findings[i].ID) {
			s.findings[i].ReportedInDigestAt = at
		}
	}
	return nil
}

func (s *fakeStore) LatestRuns(context.Context) ([]model.Run, error) {
	if s.runsErr != nil {
		return nil, s.runsErr
	}
	return slices.Clone(s.runs), nil
}

// SaveDigest fails with ctx's error once ctx is done, as the real store's
// write queue does.
func (s *fakeStore) SaveDigest(ctx context.Context, rec model.DigestRecord) (model.DigestRecord, error) {
	if err := ctx.Err(); err != nil {
		return model.DigestRecord{}, err
	}
	if s.saveErr != nil {
		return model.DigestRecord{}, s.saveErr
	}
	rec.ID = int64(len(s.digests) + 1)
	rec.SentAt = rec.SentAt.UTC().Truncate(time.Second)
	s.digests = append(s.digests, rec)
	return rec, nil
}

func (s *fakeStore) FailedDigests(context.Context) ([]model.DigestRecord, error) {
	if s.failedErr != nil {
		return nil, s.failedErr
	}
	var out []model.DigestRecord
	for _, d := range s.digests {
		if d.Status == model.DigestFailed {
			out = append(out, d)
		}
	}
	return out, nil
}

// MarkDigestRetried fails with ctx's error once ctx is done.
func (s *fakeStore) MarkDigestRetried(ctx context.Context, id int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.retriedErr != nil {
		return s.retriedErr
	}
	for i := range s.digests {
		if s.digests[i].ID == id {
			s.digests[i].Status = model.DigestRetried
			return nil
		}
	}
	return fmt.Errorf("digest %d: %w", id, model.ErrNotFound)
}

func (s *fakeStore) LastSentDigest(context.Context) (model.DigestRecord, error) {
	if s.lastErr != nil {
		return model.DigestRecord{}, s.lastErr
	}
	var sent []model.DigestRecord
	for _, d := range s.digests {
		if d.Status == model.DigestSent {
			sent = append(sent, d)
		}
	}
	if len(sent) == 0 {
		return model.DigestRecord{}, fmt.Errorf("last sent digest: %w", model.ErrNotFound)
	}
	return slices.MaxFunc(sent, func(a, b model.DigestRecord) int {
		return cmp.Or(a.SentAt.Compare(b.SentAt), cmp.Compare(a.ID, b.ID))
	}), nil
}

// fakeNotifier records every digest it accepts and fails with err when set.
// attempts counts every call, failed or not.
type fakeNotifier struct {
	sent     []model.Digest
	attempts int
	err      error
}

func (n *fakeNotifier) Send(_ context.Context, d model.Digest) error {
	n.attempts++
	if n.err != nil {
		return n.err
	}
	n.sent = append(n.sent, d)
	return nil
}

// slowNotifier takes delay, on the clock of the synctest bubble it runs in,
// and then fails with err.
type slowNotifier struct {
	delay time.Duration
	err   error
}

func (n *slowNotifier) Send(context.Context, model.Digest) error {
	time.Sleep(n.delay)
	return n.err
}

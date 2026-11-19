package report

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// jobName is the digest job's name in config schedules and upw run.
const jobName = "digest"

// defaultMaxItems is the per-section item cap when Options.MaxItems is not
// positive.
const defaultMaxItems = 25

// defaultRecordTimeout bounds the store calls that record one send, which run
// on a context detached from the run's cancellation.
const defaultRecordTimeout = 10 * time.Second

// statKeys are the counters every run reports, present even when zero.
var statKeys = []string{
	"new", "changed", "more", "heartbeat", "sent", "failed", "retried", "skipped", "no_channels",
}

// Channel is one enabled notifier and the name its sends are recorded under.
type Channel struct {
	Name     string // unique; recorded in digests.channel
	Notifier Notifier
}

// Options configures a Service.
type Options struct {
	BaseURL  string // http.base_url without a trailing slash; empty means no links
	MaxItems int    // per section; default 25
	// HeartbeatLastFire returns the most recent heartbeat fire time at or
	// before now; ok is false when no heartbeat is configured.
	HeartbeatLastFire func(now time.Time) (t time.Time, ok bool)
	Now               func() time.Time // default time.Now; the digest uses its location
}

// Service is the digest job: it resends digests whose send failed, builds one
// digest from the open findings that are new or changed since they were last
// reported (or a weekly heartbeat when there are none), marks those findings
// reported, sends the digest through every channel and records each send.
type Service struct {
	store    Store
	channels []Channel
	opts     Options
	logger   *slog.Logger
	// recordTimeout bounds the store calls that record one send, counted
	// from when that send returned. NewService sets it to
	// defaultRecordTimeout; tests in this package shorten it.
	recordTimeout time.Duration
}

// NewService returns a digest Service sending through channels, in order. A
// non-positive opts.MaxItems becomes 25 and a nil opts.Now becomes time.Now.
// It does no I/O.
func NewService(store Store, channels []Channel, opts Options, logger *slog.Logger) *Service {
	if opts.MaxItems <= 0 {
		opts.MaxItems = defaultMaxItems
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Service{store: store, channels: channels, opts: opts, logger: logger, recordTimeout: defaultRecordTimeout}
}

// recordContext returns the context for recording one send: detached from
// ctx's cancellation, so a send cut short by shutdown still leaves its row,
// and bounded by recordTimeout from now. Call it after the send returns, so
// the time a send took does not count against the record.
func (s *Service) recordContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), s.recordTimeout)
}

// Name returns "digest".
func (s *Service) Name() string { return jobName }

// Run resends failed digests, then builds and sends today's digest. scope is
// ignored: the digest always covers every repo.
//
// With no channels it sends and marks nothing and returns nil. Otherwise it
// first resends each failed digest whose channel is still configured,
// unchanged. It then builds a digest of the open findings never reported
// (NEW) or changed since they were reported (CHANGED). When there is nothing
// to report it sends the heartbeat if one fell due since the last sent digest,
// and otherwise nothing. Before a non-heartbeat digest is sent its findings are
// marked reported. Each channel's send is recorded in the digests table;
// one channel failing does not stop the others, and Run then returns an error
// naming every channel whose send (or resend) failed.
func (s *Service) Run(ctx context.Context, _ model.Scope) (model.Stats, error) {
	stats := model.Stats{}
	for _, k := range statKeys {
		stats[k] = 0
	}
	if len(s.channels) == 0 {
		s.logger.Warn("digest has no channels configured")
		stats["no_channels"] = 1
		return stats, nil
	}
	now := s.opts.Now()

	var failures []string
	var errs []error
	rf, err := s.retryFailed(ctx, now, stats)
	failures = append(failures, rf...)
	if err != nil {
		errs = append(errs, err)
	}
	if err := ctx.Err(); err != nil {
		return stats, errors.Join(append(errs, err)...)
	}

	d, candidates, err := s.build(ctx, now, stats)
	if err != nil {
		return stats, errors.Join(append(errs, err)...)
	}

	if d.IsEmpty() {
		due, err := s.heartbeatDue(ctx, now)
		if err != nil {
			return stats, errors.Join(append(errs, err)...)
		}
		if !due {
			stats["skipped"] = 1
			return stats, s.result(failures, errs)
		}
		d.Heartbeat = true
		stats["heartbeat"] = 1
	} else {
		if err := ctx.Err(); err != nil {
			return stats, errors.Join(append(errs, err)...)
		}
		if err := s.store.MarkReported(ctx, candidates, now); err != nil {
			return stats, errors.Join(append(errs, fmt.Errorf("mark findings reported: %w", err))...)
		}
	}

	sf, err := s.sendAll(ctx, d, now, stats)
	failures = append(failures, sf...)
	if err != nil {
		errs = append(errs, err)
	}
	return stats, s.result(failures, errs)
}

// result joins the failed channel sends, as one error naming them, with errs.
func (s *Service) result(failures []string, errs []error) error {
	if len(failures) > 0 {
		errs = append([]error{fmt.Errorf("digest send failed: %s", strings.Join(failures, "; "))}, errs...)
	}
	return errors.Join(errs...)
}

// retryFailed resends every failed digest whose channel is still configured,
// oldest first, with its stored payload. A successful resend is recorded as a
// new sent row and the old row is marked retried; a failed one leaves the row
// as it is. It returns one "<channel> (retry of digest <id>): <error>" entry
// per failed resend and any store error.
func (s *Service) retryFailed(ctx context.Context, now time.Time, stats model.Stats) ([]string, error) {
	rows, err := s.store.FailedDigests(ctx)
	if err != nil {
		return nil, fmt.Errorf("list failed digests: %w", err)
	}
	byName := make(map[string]Channel, len(s.channels))
	for _, c := range s.channels {
		byName[c.Name] = c
	}
	var failures []string
	var errs []error
	unconfigured := map[string]int{}
	var unconfiguredOrder []string
	for _, row := range rows {
		ch, ok := byName[row.Channel]
		if !ok {
			if unconfigured[row.Channel] == 0 {
				unconfiguredOrder = append(unconfiguredOrder, row.Channel)
			}
			unconfigured[row.Channel]++
			continue
		}
		// Run reports the cancellation once the loop has stopped.
		if ctx.Err() != nil {
			break
		}
		label := row.Channel + " (retry of digest " + strconv.FormatInt(row.ID, 10) + ")"
		var d model.Digest
		if err := json.Unmarshal(row.Payload, &d); err != nil {
			stats["failed"]++
			failures = append(failures, label+": decode payload: "+err.Error())
			continue
		}
		if err := ch.Notifier.Send(ctx, d); err != nil {
			text := model.Redact(err.Error())
			stats["failed"]++
			failures = append(failures, label+": "+text)
			s.logger.Warn("digest resend failed", "channel", row.Channel, "digest_id", row.ID, "error", text)
			continue
		}
		stats["retried"]++
		if err := s.recordRetry(ctx, row, now); err != nil {
			errs = append(errs, err)
		}
	}
	for _, name := range unconfiguredOrder {
		s.logger.Warn("failed digest left for unconfigured channel", "channel", name, "rows", unconfigured[name])
	}
	return failures, errors.Join(errs...)
}

// recordRetry saves a sent row with row's payload and marks row retried, on a
// context that outlives the run's cancellation so a delivered resend is not
// sent again.
func (s *Service) recordRetry(ctx context.Context, row model.DigestRecord, now time.Time) error {
	rctx, cancel := s.recordContext(ctx)
	defer cancel()
	if _, err := s.store.SaveDigest(rctx, model.DigestRecord{
		Channel: row.Channel,
		Status:  model.DigestSent,
		SentAt:  now,
		Payload: row.Payload,
	}); err != nil {
		return fmt.Errorf("record resend of digest %d: %w", row.ID, err)
	}
	if err := s.store.MarkDigestRetried(rctx, row.ID); err != nil {
		return fmt.Errorf("mark digest %d retried: %w", row.ID, err)
	}
	return nil
}

// build lists the unresolved findings and returns the digest of the open ones
// that are new or changed, and the IDs of every such candidate, shown or
// counted in a "more" count. It counts the candidates in stats.
func (s *Service) build(ctx context.Context, now time.Time, stats model.Stats) (model.Digest, []int64, error) {
	findings, err := s.store.ListFindings(ctx, model.FindingFilter{Suppressed: true, Now: now})
	if err != nil {
		return model.Digest{}, nil, fmt.Errorf("list findings: %w", err)
	}
	runs, err := s.store.LatestRuns(ctx)
	if err != nil {
		return model.Digest{}, nil, fmt.Errorf("list latest runs: %w", err)
	}
	d := model.Digest{GeneratedAt: now, LastRuns: runs}
	if s.opts.BaseURL != "" {
		d.FindingsURL = s.opts.BaseURL + "/"
	}

	sel := selectCandidates(findings, now)
	d.Open, d.Acked, d.Snoozed = sel.open, sel.acked, sel.snoozed

	if d.New, d.MoreNew, err = s.renderSection(ctx, sel.newFindings, false, now); err != nil {
		return model.Digest{}, nil, err
	}
	if d.Changed, d.MoreChanged, err = s.renderSection(ctx, sel.changedFindings, true, now); err != nil {
		return model.Digest{}, nil, err
	}
	stats["new"] = int64(len(sel.newFindings))
	stats["changed"] = int64(len(sel.changedFindings))
	stats["more"] = int64(d.MoreNew + d.MoreChanged)

	ids := make([]int64, 0, len(sel.newFindings)+len(sel.changedFindings))
	for _, f := range sel.newFindings {
		ids = append(ids, f.ID)
	}
	for _, f := range sel.changedFindings {
		ids = append(ids, f.ID)
	}
	return d, ids, nil
}

// renderSection renders the first MaxItems of findings as items and returns
// them with the number left over.
func (s *Service) renderSection(ctx context.Context, findings []model.Finding, changed bool, now time.Time) ([]model.DigestItem, int, error) {
	shown := findings[:min(len(findings), s.opts.MaxItems)]
	items := make([]model.DigestItem, 0, len(shown))
	for _, f := range shown {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		detail, err := s.store.GetFindingDetail(ctx, f.ID)
		if err != nil {
			return nil, 0, fmt.Errorf("read finding %d: %w", f.ID, err)
		}
		item, err := renderItem(detail, changed, s.opts.BaseURL, now.Location())
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, len(findings) - len(shown), nil
}

// heartbeatDue reports whether the heartbeat should be sent at now: a fire
// time is configured, is not after now, and is after the last sent digest (or
// no digest was ever sent).
func (s *Service) heartbeatDue(ctx context.Context, now time.Time) (bool, error) {
	if s.opts.HeartbeatLastFire == nil {
		return false, nil
	}
	fire, ok := s.opts.HeartbeatLastFire(now)
	if !ok || fire.After(now) {
		return false, nil
	}
	last, err := s.store.LastSentDigest(ctx)
	if errors.Is(err, model.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read last sent digest: %w", err)
	}
	return fire.After(last.SentAt), nil
}

// selection is the unresolved findings sorted for one digest.
type selection struct {
	open, acked, snoozed int
	// newFindings and changedFindings keep the order they were listed in.
	newFindings, changedFindings []model.Finding
}

// selectCandidates counts findings by effective state at now and picks the
// open ones never reported (new) or changed after they were last reported
// (changed). Dismissed findings count nowhere.
func selectCandidates(findings []model.Finding, now time.Time) selection {
	var sel selection
	for _, f := range findings {
		switch f.State.Effective(now) {
		case model.StateOpen:
			sel.open++
			switch {
			case f.ReportedInDigestAt.IsZero():
				sel.newFindings = append(sel.newFindings, f)
			case f.LastChangedAt.After(f.ReportedInDigestAt):
				sel.changedFindings = append(sel.changedFindings, f)
			}
		case model.StateAcked:
			sel.acked++
		case model.StateSnoozed:
			sel.snoozed++
		}
	}
	return sel
}

// sendAll sends d to every channel in order and records each attempt. It
// returns one "<channel>: <error>" entry per failed send and any store error.
func (s *Service) sendAll(ctx context.Context, d model.Digest, now time.Time, stats model.Stats) ([]string, error) {
	payload, err := json.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("encode digest: %w", err)
	}
	var failures []string
	var errs []error
	for _, ch := range s.channels {
		rec := model.DigestRecord{Channel: ch.Name, Status: model.DigestSent, SentAt: now, Payload: payload}
		if err := ch.Notifier.Send(ctx, d); err != nil {
			text := model.Redact(err.Error())
			rec.Status = model.DigestFailed
			rec.Error = text
			stats["failed"]++
			failures = append(failures, ch.Name+": "+text)
			s.logger.Warn("digest send failed", "channel", ch.Name, "error", text)
		} else {
			stats["sent"]++
		}
		// The findings are already marked reported, so every send, failed
		// ones above all, must leave its row; a failed row is resent next run.
		if err := s.recordSend(ctx, rec); err != nil {
			errs = append(errs, fmt.Errorf("record digest for channel %s: %w", ch.Name, err))
		}
	}
	return failures, errors.Join(errs...)
}

// recordSend saves rec on its own record context.
func (s *Service) recordSend(ctx context.Context, rec model.DigestRecord) error {
	rctx, cancel := s.recordContext(ctx)
	defer cancel()
	_, err := s.store.SaveDigest(rctx, rec)
	return err
}

package report

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

var now0 = time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC)

// jobFinding returns an open job_failure finding first seen and last changed
// at changed.
func jobFinding(id int64, score float64, changed time.Time) model.Finding {
	job := "job" + strconv.FormatInt(id, 10)
	return model.Finding{
		ID:            id,
		Kind:          model.KindJobFailure,
		Fingerprint:   "job_failure|" + job,
		Score:         score,
		RankReason:    "job " + job + " failed: boom",
		FirstSeenAt:   changed,
		LastChangedAt: changed,
		State:         model.FindingState{State: model.StateOpen},
	}
}

func newTestService(st Store, channels []Channel, opts Options) *Service {
	if opts.Now == nil {
		opts.Now = func() time.Time { return now0 }
	}
	return NewService(st, channels, opts, slog.New(slog.DiscardHandler))
}

func itemIDs(items []model.DigestItem) []int64 {
	ids := []int64{}
	for _, it := range items {
		ids = append(ids, it.FindingID)
	}
	return ids
}

func checkStats(t *testing.T, got model.Stats, want map[string]int64) {
	t.Helper()
	for _, k := range statKeys {
		if got[k] != want[k] {
			t.Errorf("stats[%q] = %d, want %d (all stats %v)", k, got[k], want[k], got)
		}
	}
	if len(got) != len(statKeys) {
		t.Errorf("stats has %d keys, want %d: %v", len(got), len(statKeys), got)
	}
}

func TestNewService_Defaults(t *testing.T) {
	t.Parallel()
	s := NewService(&fakeStore{}, nil, Options{}, slog.New(slog.DiscardHandler))
	if got := s.Name(); got != "digest" {
		t.Errorf("Name() = %q, want %q", got, "digest")
	}
	if s.opts.MaxItems != 25 {
		t.Errorf("MaxItems = %d, want 25", s.opts.MaxItems)
	}
	if s.opts.Now == nil {
		t.Error("Now is nil, want time.Now")
	}
}

func TestRun_NewAndChanged(t *testing.T) {
	t.Parallel()
	yesterday := now0.Add(-24 * time.Hour)

	neverReported := jobFinding(1, 90, yesterday)
	changedSince := jobFinding(2, 80, now0.Add(-time.Hour))
	changedSince.ReportedInDigestAt = yesterday
	unchanged := jobFinding(3, 70, yesterday.Add(-time.Hour))
	unchanged.ReportedInDigestAt = yesterday
	acked := jobFinding(4, 65, yesterday)
	acked.State = model.FindingState{State: model.StateAcked}
	snoozed := jobFinding(5, 60, yesterday)
	snoozed.State = model.FindingState{State: model.StateSnoozed, SnoozeUntil: now0.Add(time.Hour)}
	expired := jobFinding(6, 50, yesterday)
	expired.State = model.FindingState{State: model.StateSnoozed, SnoozeUntil: now0.Add(-time.Hour)}
	dismissed := jobFinding(7, 40, yesterday)
	dismissed.State = model.FindingState{State: model.StateDismissed}

	runs := []model.Run{{ID: 9, Job: "vulns", Status: model.RunSucceeded, StartedAt: yesterday}}
	st := &fakeStore{
		findings: []model.Finding{neverReported, changedSince, unchanged, acked, snoozed, expired, dismissed},
		runs:     runs,
	}
	n := &fakeNotifier{}
	s := newTestService(st, []Channel{{Name: "email", Notifier: n}}, Options{BaseURL: "https://upw.example.test"})

	stats, err := s.Run(t.Context(), model.Scope{RepoSlug: "ignored"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	checkStats(t, stats, map[string]int64{"new": 2, "changed": 1, "sent": 1})

	if want := []model.FindingFilter{{Suppressed: true, Now: now0}}; !reflect.DeepEqual(st.filters, want) {
		t.Errorf("ListFindings filters = %+v, want %+v", st.filters, want)
	}
	if len(n.sent) != 1 {
		t.Fatalf("notifier got %d digests, want 1", len(n.sent))
	}
	d := n.sent[0]
	if got, want := itemIDs(d.New), []int64{1, 6}; !reflect.DeepEqual(got, want) {
		t.Errorf("New IDs = %v, want %v", got, want)
	}
	if got, want := itemIDs(d.Changed), []int64{2}; !reflect.DeepEqual(got, want) {
		t.Errorf("Changed IDs = %v, want %v", got, want)
	}
	if d.Open != 4 || d.Acked != 1 || d.Snoozed != 1 {
		t.Errorf("counts open/acked/snoozed = %d/%d/%d, want 4/1/1", d.Open, d.Acked, d.Snoozed)
	}
	if d.Heartbeat || d.MoreNew != 0 || d.MoreChanged != 0 || d.Waiting != 0 {
		t.Errorf("Heartbeat/MoreNew/MoreChanged/Waiting = %v/%d/%d/%d, want false/0/0/0", d.Heartbeat, d.MoreNew, d.MoreChanged, d.Waiting)
	}
	if !d.GeneratedAt.Equal(now0) {
		t.Errorf("GeneratedAt = %v, want %v", d.GeneratedAt, now0)
	}
	if !reflect.DeepEqual(d.LastRuns, runs) {
		t.Errorf("LastRuns = %+v, want %+v", d.LastRuns, runs)
	}
	if want := "https://upw.example.test/"; d.FindingsURL != want {
		t.Errorf("FindingsURL = %q, want %q", d.FindingsURL, want)
	}
	if want := "https://upw.example.test/f/1"; d.New[0].URL != want {
		t.Errorf("New[0].URL = %q, want %q", d.New[0].URL, want)
	}

	if want := [][]int64{{1, 6, 2}}; !reflect.DeepEqual(st.marked, want) {
		t.Errorf("MarkReported ids = %v, want %v", st.marked, want)
	}
	if len(st.markedAt) != 1 || !st.markedAt[0].Equal(now0) {
		t.Errorf("MarkReported at = %v, want [%v]", st.markedAt, now0)
	}

	if len(st.digests) != 1 {
		t.Fatalf("digests rows = %d, want 1", len(st.digests))
	}
	row := st.digests[0]
	if row.Channel != "email" || row.Status != model.DigestSent || !row.SentAt.Equal(now0) || row.Error != "" {
		t.Errorf("digest row = %+v, want email sent at %v with no error", row, now0)
	}
	want, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal digest: %v", err)
	}
	if string(row.Payload) != string(want) {
		t.Errorf("payload = %s, want %s", row.Payload, want)
	}
}

func TestRun_Heartbeat(t *testing.T) {
	t.Parallel()
	st := &fakeStore{
		findings: []model.Finding{func() model.Finding {
			f := jobFinding(1, 50, now0.Add(-72*time.Hour))
			f.ReportedInDigestAt = now0.Add(-48 * time.Hour)
			return f
		}()},
		digests: []model.DigestRecord{{ID: 1, Channel: "email", Status: model.DigestSent, SentAt: now0.Add(-48 * time.Hour), Payload: []byte(`{}`)}},
	}
	n := &fakeNotifier{}
	fire := now0.Add(-time.Hour)
	s := newTestService(st, []Channel{{Name: "email", Notifier: n}}, Options{
		HeartbeatLastFire: func(time.Time) (time.Time, bool) { return fire, true },
	})

	stats, err := s.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	checkStats(t, stats, map[string]int64{"heartbeat": 1, "sent": 1})
	if len(n.sent) != 1 {
		t.Fatalf("notifier got %d digests, want 1", len(n.sent))
	}
	d := n.sent[0]
	if !d.Heartbeat || len(d.New) != 0 || len(d.Changed) != 0 {
		t.Errorf("digest Heartbeat=%v New=%d Changed=%d, want true 0 0", d.Heartbeat, len(d.New), len(d.Changed))
	}
	if d.Open != 1 {
		t.Errorf("heartbeat Open = %d, want 1", d.Open)
	}
	if len(st.marked) != 0 {
		t.Errorf("MarkReported called %v, want no calls", st.marked)
	}
	if len(st.digests) != 2 || st.digests[1].Status != model.DigestSent {
		t.Fatalf("digests = %+v, want a second sent row", st.digests)
	}

	stats, err = s.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	checkStats(t, stats, map[string]int64{"skipped": 1})
	if len(n.sent) != 1 || len(st.digests) != 2 {
		t.Errorf("after second run: sends %d, rows %d, want 1 and 2", len(n.sent), len(st.digests))
	}
}

func TestRun_HeartbeatDecision(t *testing.T) {
	t.Parallel()
	lastSent := now0.Add(-24 * time.Hour)
	tests := []struct {
		name      string
		fire      func(time.Time) (time.Time, bool)
		neverSent bool
		want      bool
	}{
		{name: "no heartbeat configured", fire: nil},
		{name: "not configured by function", fire: func(time.Time) (time.Time, bool) { return now0.Add(-time.Hour), false }},
		{name: "fire after now", fire: func(time.Time) (time.Time, bool) { return now0.Add(time.Minute), true }},
		{name: "fire before last sent", fire: func(time.Time) (time.Time, bool) { return lastSent.Add(-time.Hour), true }},
		{name: "fire at last sent", fire: func(time.Time) (time.Time, bool) { return lastSent, true }},
		{name: "fire after last sent", fire: func(time.Time) (time.Time, bool) { return lastSent.Add(time.Hour), true }, want: true},
		{name: "fire at now", fire: func(time.Time) (time.Time, bool) { return now0, true }, want: true},
		{name: "never sent", fire: func(time.Time) (time.Time, bool) { return lastSent.Add(-time.Hour), true }, neverSent: true, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := &fakeStore{}
			if !tt.neverSent {
				st.digests = []model.DigestRecord{{ID: 1, Channel: "email", Status: model.DigestSent, SentAt: lastSent}}
			}
			n := &fakeNotifier{}
			s := newTestService(st, []Channel{{Name: "email", Notifier: n}}, Options{HeartbeatLastFire: tt.fire})
			stats, err := s.Run(t.Context(), model.Scope{})
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if got := len(n.sent) == 1 && n.sent[0].Heartbeat; got != tt.want {
				t.Errorf("heartbeat sent = %v, want %v (stats %v)", got, tt.want, stats)
			}
			wantSkipped := int64(1)
			if tt.want {
				wantSkipped = 0
			}
			if stats["skipped"] != wantSkipped {
				t.Errorf("stats[skipped] = %d, want %d", stats["skipped"], wantSkipped)
			}
		})
	}
}

func TestRun_FailedSendRetried(t *testing.T) {
	t.Parallel()
	st := &fakeStore{findings: []model.Finding{jobFinding(1, 50, now0.Add(-time.Hour))}}
	a := &fakeNotifier{err: errors.New("post https://user:secret@hooks.example.test/x: status 500")}
	b := &fakeNotifier{}
	clock := now0
	s := newTestService(st, []Channel{{Name: "A", Notifier: a}, {Name: "B", Notifier: b}}, Options{
		Now: func() time.Time { return clock },
	})

	stats, err := s.Run(t.Context(), model.Scope{})
	if err == nil {
		t.Fatal("first Run() error = nil, want an error naming A")
	}
	if !strings.Contains(err.Error(), "A: ") || strings.Contains(err.Error(), "B: ") {
		t.Errorf("first Run() error = %q, want it to name A and not B", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("first Run() error = %q, leaks the credential", err)
	}
	checkStats(t, stats, map[string]int64{"new": 1, "sent": 1, "failed": 1})
	if len(st.digests) != 2 {
		t.Fatalf("digests rows = %d, want 2", len(st.digests))
	}
	failed, sent := st.digests[0], st.digests[1]
	if failed.Channel != "A" || failed.Status != model.DigestFailed {
		t.Errorf("row 1 = %s %s, want A failed", failed.Channel, failed.Status)
	}
	if strings.Contains(failed.Error, "secret") || !strings.Contains(failed.Error, "[REDACTED]") {
		t.Errorf("row 1 error = %q, want the credential redacted", failed.Error)
	}
	if sent.Channel != "B" || sent.Status != model.DigestSent || sent.Error != "" {
		t.Errorf("row 2 = %+v, want B sent with no error", sent)
	}
	if string(failed.Payload) != string(sent.Payload) {
		t.Error("failed and sent rows hold different payloads")
	}

	// Next day, A is healthy again.
	clock = now0.Add(24 * time.Hour)
	a.err = nil
	stats, err = s.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	checkStats(t, stats, map[string]int64{"retried": 1, "skipped": 1})
	if len(a.sent) != 1 {
		t.Fatalf("A got %d digests, want 1", len(a.sent))
	}
	if len(b.sent) != 1 {
		t.Errorf("B got %d digests, want 1 (none on the retry)", len(b.sent))
	}
	resent, err := json.Marshal(a.sent[0])
	if err != nil {
		t.Fatalf("marshal resent digest: %v", err)
	}
	if string(resent) != string(failed.Payload) {
		t.Errorf("resent digest = %s, want the stored payload %s", resent, failed.Payload)
	}
	if len(st.digests) != 3 {
		t.Fatalf("digests rows = %d, want 3", len(st.digests))
	}
	if st.digests[0].Status != model.DigestRetried {
		t.Errorf("old row status = %s, want retried", st.digests[0].Status)
	}
	row := st.digests[2]
	if row.Channel != "A" || row.Status != model.DigestSent || !row.SentAt.Equal(clock) || string(row.Payload) != string(failed.Payload) {
		t.Errorf("new row = %+v, want A sent at %v with the old payload", row, clock)
	}
}

func TestRun_RetryLeavesRowsItCannotSend(t *testing.T) {
	t.Parallel()
	st := &fakeStore{digests: []model.DigestRecord{
		{ID: 1, Channel: "old-webhook", Status: model.DigestFailed, SentAt: now0.Add(-24 * time.Hour), Payload: []byte(`{"heartbeat":true}`)},
		{ID: 2, Channel: "email", Status: model.DigestFailed, SentAt: now0.Add(-24 * time.Hour), Payload: []byte(`not json`)},
	}}
	n := &fakeNotifier{}
	s := newTestService(st, []Channel{{Name: "email", Notifier: n}}, Options{})

	stats, err := s.Run(t.Context(), model.Scope{})
	if err == nil || !strings.Contains(err.Error(), "email (retry of digest 2)") {
		t.Fatalf("Run() error = %v, want one naming the email retry", err)
	}
	if strings.Contains(err.Error(), "old-webhook") {
		t.Errorf("Run() error = %q, names the unconfigured channel", err)
	}
	checkStats(t, stats, map[string]int64{"failed": 1, "skipped": 1})
	if n.attempts != 0 {
		t.Errorf("notifier called %d times, want 0", n.attempts)
	}
	for _, d := range st.digests {
		if d.Status != model.DigestFailed {
			t.Errorf("row %d status = %s, want failed", d.ID, d.Status)
		}
	}
	if len(st.digests) != 2 {
		t.Errorf("digests rows = %d, want 2", len(st.digests))
	}
}

func TestRun_MaxItems(t *testing.T) {
	t.Parallel()
	st := &fakeStore{}
	var wantIDs []int64
	for i := range 30 {
		id := int64(i + 1)
		st.findings = append(st.findings, jobFinding(id, float64(100-i), now0.Add(-time.Hour)))
		wantIDs = append(wantIDs, id)
	}
	n := &fakeNotifier{}
	s := newTestService(st, []Channel{{Name: "email", Notifier: n}}, Options{MaxItems: 25})

	stats, err := s.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	checkStats(t, stats, map[string]int64{"new": 30, "more": 5, "sent": 1})
	d := n.sent[0]
	if got, want := itemIDs(d.New), wantIDs[:25]; !reflect.DeepEqual(got, want) {
		t.Errorf("New IDs = %v, want %v", got, want)
	}
	if d.MoreNew != 5 {
		t.Errorf("MoreNew = %d, want 5", d.MoreNew)
	}
	if want := [][]int64{wantIDs}; !reflect.DeepEqual(st.marked, want) {
		t.Errorf("MarkReported ids = %v, want %v", st.marked, want)
	}
	if len(st.detailed) != 25 {
		t.Errorf("GetFindingDetail called %d times, want 25", len(st.detailed))
	}
}

func TestRun_NoChannels(t *testing.T) {
	t.Parallel()
	st := &fakeStore{findings: []model.Finding{jobFinding(1, 50, now0)}}
	s := newTestService(st, nil, Options{})

	stats, err := s.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	checkStats(t, stats, map[string]int64{"no_channels": 1})
	if len(st.marked) != 0 || len(st.digests) != 0 {
		t.Errorf("marked %v, rows %d, want nothing", st.marked, len(st.digests))
	}
}

func TestRun_StoreErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	tests := []struct {
		name      string
		setup     func(*fakeStore)
		wantSends int
		wantMark  bool
	}{
		{name: "list findings", setup: func(s *fakeStore) { s.listErr = boom }},
		{name: "latest runs", setup: func(s *fakeStore) { s.runsErr = boom }},
		{name: "finding detail", setup: func(s *fakeStore) { s.detailErr = boom }},
		{name: "mark reported", setup: func(s *fakeStore) { s.markErr = boom }},
		{name: "failed digests", setup: func(s *fakeStore) { s.failedErr = boom }, wantSends: 1, wantMark: true},
		{name: "save digest", setup: func(s *fakeStore) { s.saveErr = boom }, wantSends: 1, wantMark: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := &fakeStore{findings: []model.Finding{jobFinding(1, 50, now0)}}
			tt.setup(st)
			n := &fakeNotifier{}
			s := newTestService(st, []Channel{{Name: "email", Notifier: n}}, Options{})
			_, err := s.Run(t.Context(), model.Scope{})
			if !errors.Is(err, boom) {
				t.Errorf("Run() error = %v, want it to wrap %v", err, boom)
			}
			if n.attempts != tt.wantSends {
				t.Errorf("sends = %d, want %d", n.attempts, tt.wantSends)
			}
			if got := len(st.marked) > 0; got != tt.wantMark {
				t.Errorf("marked = %v, want %v", got, tt.wantMark)
			}
		})
	}
}

func TestRun_CancelledContext(t *testing.T) {
	t.Parallel()
	st := &fakeStore{findings: []model.Finding{jobFinding(1, 50, now0)}}
	n := &fakeNotifier{}
	s := newTestService(st, []Channel{{Name: "email", Notifier: n}}, Options{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := s.Run(ctx, model.Scope{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}
	if n.attempts != 0 || len(st.marked) != 0 || len(st.digests) != 0 {
		t.Errorf("sends %d, marked %v, rows %d, want nothing", n.attempts, st.marked, len(st.digests))
	}
}

func TestSelectCandidates(t *testing.T) {
	t.Parallel()
	reported := now0.Add(-24 * time.Hour)
	tests := []struct {
		name        string
		f           model.Finding
		wantNew     bool
		wantChanged bool
		wantCounts  [3]int // open, acked, snoozed
	}{
		{name: "never reported", f: jobFinding(1, 50, reported), wantNew: true, wantCounts: [3]int{1, 0, 0}},
		{name: "changed after report", f: func() model.Finding {
			f := jobFinding(1, 50, reported.Add(time.Second))
			f.ReportedInDigestAt = reported
			return f
		}(), wantChanged: true, wantCounts: [3]int{1, 0, 0}},
		{name: "changed at report time", f: func() model.Finding {
			f := jobFinding(1, 50, reported)
			f.ReportedInDigestAt = reported
			return f
		}(), wantCounts: [3]int{1, 0, 0}},
		{name: "acked", f: func() model.Finding {
			f := jobFinding(1, 50, reported)
			f.State.State = model.StateAcked
			return f
		}(), wantCounts: [3]int{0, 1, 0}},
		{name: "snooze ends at now", f: func() model.Finding {
			f := jobFinding(1, 50, reported)
			f.State = model.FindingState{State: model.StateSnoozed, SnoozeUntil: now0}
			return f
		}(), wantNew: true, wantCounts: [3]int{1, 0, 0}},
		{name: "dismissed", f: func() model.Finding {
			f := jobFinding(1, 50, reported)
			f.State.State = model.StateDismissed
			return f
		}()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := selectCandidates([]model.Finding{tt.f}, now0)
			if (len(got.newFindings) == 1) != tt.wantNew || (len(got.changedFindings) == 1) != tt.wantChanged {
				t.Errorf("new %d changed %d, want new %v changed %v", len(got.newFindings), len(got.changedFindings), tt.wantNew, tt.wantChanged)
			}
			if counts := [3]int{got.open, got.acked, got.snoozed}; counts != tt.wantCounts {
				t.Errorf("counts = %v, want %v", counts, tt.wantCounts)
			}
		})
	}
}

func TestRun_RecordErrorsOnRetryAndHeartbeat(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	failedRow := model.DigestRecord{ID: 1, Channel: "email", Status: model.DigestFailed, SentAt: now0.Add(-24 * time.Hour), Payload: []byte(`{"heartbeat":true}`)}
	tests := []struct {
		name  string
		setup func(*fakeStore)
	}{
		{name: "mark retried", setup: func(s *fakeStore) {
			s.digests = []model.DigestRecord{failedRow}
			s.retriedErr = boom
		}},
		{name: "last sent digest", setup: func(s *fakeStore) { s.lastErr = boom }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := &fakeStore{}
			tt.setup(st)
			n := &fakeNotifier{}
			s := newTestService(st, []Channel{{Name: "email", Notifier: n}}, Options{
				HeartbeatLastFire: func(time.Time) (time.Time, bool) { return now0.Add(-time.Hour), true },
			})
			if _, err := s.Run(t.Context(), model.Scope{}); !errors.Is(err, boom) {
				t.Errorf("Run() error = %v, want it to wrap %v", err, boom)
			}
		})
	}
}

// The record timeout counts from when each send returns, so a send slower
// than the timeout still leaves its row, and so does every send after it.
func TestRun_SlowSendStillRecorded(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		st := &fakeStore{findings: []model.Finding{jobFinding(1, 50, now0)}}
		slow := &slowNotifier{delay: 100 * time.Millisecond, err: errors.New("smtp: i/o timeout")}
		fast := &fakeNotifier{}
		s := newTestService(st, []Channel{{Name: "email", Notifier: slow}, {Name: "webhook", Notifier: fast}}, Options{})
		s.recordTimeout = 50 * time.Millisecond

		_, err := s.Run(t.Context(), model.Scope{})
		if err == nil || !strings.Contains(err.Error(), "email: smtp: i/o timeout") {
			t.Errorf("Run() error = %v, want one naming the email send", err)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Run() error = %v, a record ran out of time", err)
		}
		if len(st.digests) != 2 {
			t.Fatalf("digests rows = %+v, want 2", st.digests)
		}
		if got := st.digests[0]; got.Channel != "email" || got.Status != model.DigestFailed {
			t.Errorf("row 1 = %s %s, want email failed", got.Channel, got.Status)
		}
		if got := st.digests[1]; got.Channel != "webhook" || got.Status != model.DigestSent {
			t.Errorf("row 2 = %s %s, want webhook sent", got.Channel, got.Status)
		}
		if len(fast.sent) != 1 {
			t.Errorf("webhook got %d digests, want 1", len(fast.sent))
		}
	})
}

func TestRun_ChannelsInOrder(t *testing.T) {
	t.Parallel()
	st := &fakeStore{findings: []model.Finding{jobFinding(1, 50, now0)}}
	chans := []Channel{
		{Name: "email", Notifier: &fakeNotifier{}},
		{Name: "stdout", Notifier: &fakeNotifier{}},
		{Name: "stdout#2", Notifier: &fakeNotifier{}},
	}
	s := newTestService(st, chans, Options{})
	if _, err := s.Run(t.Context(), model.Scope{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	var got []string
	for _, d := range st.digests {
		got = append(got, d.Channel)
	}
	if want := []string{"email", "stdout", "stdout#2"}; !slices.Equal(got, want) {
		t.Errorf("rows by channel = %v, want %v", got, want)
	}
}

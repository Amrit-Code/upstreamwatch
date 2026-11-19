package web_test

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/query"
	"github.com/Amrit-Code/upstreamwatch/internal/web"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/")

type fakeHealth struct {
	runs []model.Run
	err  error
}

func (f *fakeHealth) LastRuns(ctx context.Context) ([]model.Run, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.runs, nil
}

// fakeFindings is a FindingsReader over canned details. Like the store, it
// leaves out findings whose state at t0 is snoozed or dismissed unless the
// filter asks for suppressed ones; it ignores the repo and kind fields, which
// it only records.
type fakeFindings struct {
	details   []model.FindingDetail
	err       error
	detailErr error

	mu         sync.Mutex
	gotFilters []model.FindingFilter
	gotIDs     []int64
}

func (f *fakeFindings) Findings(ctx context.Context, flt model.FindingFilter) ([]model.FindingDetail, error) {
	f.mu.Lock()
	f.gotFilters = append(f.gotFilters, flt)
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	var out []model.FindingDetail
	for _, d := range f.details {
		switch d.Finding.State.Effective(t0) {
		case model.StateSnoozed, model.StateDismissed:
			if !flt.Suppressed {
				continue
			}
		}
		out = append(out, d)
	}
	return out, nil
}

func (f *fakeFindings) FindingDetail(ctx context.Context, id int64) (model.FindingDetail, error) {
	f.mu.Lock()
	f.gotIDs = append(f.gotIDs, id)
	f.mu.Unlock()
	if f.detailErr != nil {
		return model.FindingDetail{}, f.detailErr
	}
	for _, d := range f.details {
		if d.Finding.ID == id {
			return d, nil
		}
	}
	return model.FindingDetail{}, fmt.Errorf("finding %d: %w", id, model.ErrNotFound)
}

// filters returns the filters Findings was called with.
func (f *fakeFindings) filters() []model.FindingFilter {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.gotFilters)
}

// actionCall is one call to a FindingActions method.
type actionCall struct {
	Method string
	ID     int64
	For    time.Duration // Snooze only
	Note   string
	By     string
}

// fakeActions is a FindingActions that records every call and returns err.
type fakeActions struct {
	err error

	mu    sync.Mutex
	calls []actionCall
}

func (f *fakeActions) record(c actionCall) (model.FindingState, error) {
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	if f.err != nil {
		return model.FindingState{}, f.err
	}
	return model.FindingState{State: model.StateAcked}, nil
}

func (f *fakeActions) Ack(ctx context.Context, id int64, note, by string) (model.FindingState, error) {
	return f.record(actionCall{Method: "Ack", ID: id, Note: note, By: by})
}

func (f *fakeActions) Snooze(ctx context.Context, id int64, d time.Duration, note, by string) (model.FindingState, error) {
	return f.record(actionCall{Method: "Snooze", ID: id, For: d, Note: note, By: by})
}

func (f *fakeActions) Dismiss(ctx context.Context, id int64, note, by string) (model.FindingState, error) {
	return f.record(actionCall{Method: "Dismiss", ID: id, Note: note, By: by})
}

func (f *fakeActions) Reopen(ctx context.Context, id int64, by string) (model.FindingState, error) {
	return f.record(actionCall{Method: "Reopen", ID: id, By: by})
}

// recorded returns the calls made so far.
func (f *fakeActions) recorded() []actionCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// fakeTimeline is a TimelineReader returning canned values and recording the
// slug and filter of the last Timeline call.
type fakeTimeline struct {
	repos     []model.TargetRepo
	repo      model.TargetRepo
	intervals []model.DepInterval
	err       error

	gotSlug   string
	gotFilter query.TimelineFilter
	calls     int
}

func (f *fakeTimeline) Repos(ctx context.Context) ([]model.TargetRepo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.repos, nil
}

func (f *fakeTimeline) Timeline(ctx context.Context, slug string, filter query.TimelineFilter) (model.TargetRepo, []model.DepInterval, error) {
	f.gotSlug, f.gotFilter = slug, filter
	f.calls++
	if f.err != nil {
		return model.TargetRepo{}, nil, f.err
	}
	return f.repo, f.intervals, nil
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// slogTo returns a logger writing text records to w.
func slogTo(w io.Writer) *slog.Logger { return slog.New(slog.NewTextHandler(w, nil)) }

// newTestServer builds a Server with the given fakes, no-op finding actions
// and an empty timeline reader, failing the test on a construction error.
func newTestServer(t *testing.T, h web.HealthReader, f web.FindingsReader) *web.Server {
	t.Helper()
	return newTimelineServer(t, h, f, &fakeTimeline{})
}

// newTimelineServer builds a Server with the given fakes and no-op finding
// actions, failing the test on a construction error.
func newTimelineServer(t *testing.T, h web.HealthReader, f web.FindingsReader, tl web.TimelineReader) *web.Server {
	t.Helper()
	return newServer(t, h, f, &fakeActions{}, tl, discardLogger())
}

// newFindingsServer builds a Server around the findings fakes and tl, with
// its clock fixed at t0, failing the test on a construction error.
func newFindingsServer(t *testing.T, f web.FindingsReader, a web.FindingActions, tl web.TimelineReader, logger *slog.Logger) *web.Server {
	t.Helper()
	return newServer(t, &fakeHealth{}, f, a, tl, logger)
}

// newServer builds a Server around the given fakes, with its clock fixed at
// t0, failing the test on a construction error.
func newServer(t *testing.T, h web.HealthReader, f web.FindingsReader, a web.FindingActions, tl web.TimelineReader, logger *slog.Logger) *web.Server {
	t.Helper()
	s, err := web.NewServer(h, f, a, tl, web.Options{Listen: "127.0.0.1:0"}, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	web.SetNow(s, func() time.Time { return t0 })
	return s
}

// newLoggingTimelineServer builds a Server around tl that logs to logs.
func newLoggingTimelineServer(t *testing.T, tl web.TimelineReader, logs io.Writer) *web.Server {
	t.Helper()
	s, err := web.NewServer(&fakeHealth{}, &fakeFindings{}, &fakeActions{}, tl, web.Options{Listen: "127.0.0.1:0"},
		slog.New(slog.NewTextHandler(logs, nil)))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	return s
}

// addrCapture is a slog.Handler that sends the addr attribute of the
// "http server listening" record on a channel.
type addrCapture struct {
	once  *sync.Once
	addrs chan string
}

func newAddrCapture() addrCapture {
	return addrCapture{once: new(sync.Once), addrs: make(chan string, 1)}
}

func (h addrCapture) Enabled(context.Context, slog.Level) bool { return true }

func (h addrCapture) Handle(_ context.Context, r slog.Record) error {
	if r.Message != "http server listening" {
		return nil
	}
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "addr" {
			h.once.Do(func() { h.addrs <- a.Value.String() })
			return false
		}
		return true
	})
	return nil
}

func (h addrCapture) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h addrCapture) WithGroup(string) slog.Handler      { return h }

// assertGolden compares got with testdata/<name>.golden, rewriting the file
// first when -update is set.
func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with -update to create it): %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("output differs from %s; rerun with -update and review the diff\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

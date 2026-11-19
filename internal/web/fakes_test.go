package web_test

import (
	"bytes"
	"context"
	"flag"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

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

type fakeFindings struct {
	findings []model.Finding
	err      error
}

func (f *fakeFindings) OpenFindings(ctx context.Context) ([]model.Finding, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.findings, nil
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

// newTestServer builds a Server with the given fakes and an empty timeline
// reader, failing the test on a construction error.
func newTestServer(t *testing.T, h web.HealthReader, f web.FindingsReader) *web.Server {
	t.Helper()
	return newTimelineServer(t, h, f, &fakeTimeline{})
}

// newTimelineServer builds a Server with the given fakes, failing the test on
// a construction error.
func newTimelineServer(t *testing.T, h web.HealthReader, f web.FindingsReader, tl web.TimelineReader) *web.Server {
	t.Helper()
	s, err := web.NewServer(h, f, tl, web.Options{Listen: "127.0.0.1:0"}, discardLogger())
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	return s
}

// newLoggingTimelineServer builds a Server around tl that logs to logs.
func newLoggingTimelineServer(t *testing.T, tl web.TimelineReader, logs io.Writer) *web.Server {
	t.Helper()
	s, err := web.NewServer(&fakeHealth{}, &fakeFindings{}, tl, web.Options{Listen: "127.0.0.1:0"},
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

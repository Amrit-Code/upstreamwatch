package stdout_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/notify/stdout"
	"github.com/Amrit-Code/upstreamwatch/internal/report"
)

var _ report.Notifier = (*stdout.Notifier)(nil)

// countingWriter records each Write call separately.
type countingWriter struct {
	writes [][]byte
	err    error
}

func (w *countingWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	w.writes = append(w.writes, bytes.Clone(p))
	return len(p), nil
}

func sampleDigest() model.Digest {
	return model.Digest{
		GeneratedAt: time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC),
		New: []model.DigestItem{{
			FindingID: 1,
			Kind:      model.KindAdvisory,
			Tag:       "[A]",
			Headline:  "high  GO-2026-0001  pkg:golang/example.com/lib 1.2.0  (repo: example-server)",
			Lines:     []string{"fixed in 1.2.1"},
		}},
		Open: 1,
	}
}

func TestSend(t *testing.T) {
	t.Parallel()
	w := &countingWriter{}
	n := stdout.New(w)
	d := sampleDigest()

	if err := n.Send(t.Context(), d); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if len(w.writes) != 1 {
		t.Fatalf("Send() made %d Write calls, want 1", len(w.writes))
	}
	if got, want := string(w.writes[0]), d.PlainText()+"\n"; got != want {
		t.Errorf("Send() wrote %q, want %q", got, want)
	}
}

func TestSend_Heartbeat(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	n := stdout.New(&buf)
	d := model.Digest{GeneratedAt: time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC), Heartbeat: true}

	if err := n.Send(t.Context(), d); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if got, want := buf.String(), d.PlainText()+"\n"; got != want {
		t.Errorf("Send() wrote %q, want %q", got, want)
	}
}

func TestSend_WriteError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("disk full")
	n := stdout.New(&countingWriter{err: wantErr})

	err := n.Send(t.Context(), sampleDigest())
	if !errors.Is(err, wantErr) {
		t.Errorf("Send() error = %v, want it to wrap %v", err, wantErr)
	}
}

func TestSend_Concurrent(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	n := stdout.New(&buf)
	d := sampleDigest()

	done := make(chan error)
	const senders = 8
	for range senders {
		go func() { done <- n.Send(t.Context(), d) }()
	}
	for range senders {
		if err := <-done; err != nil {
			t.Fatalf("Send() error = %v", err)
		}
	}
	want := bytes.Repeat([]byte(d.PlainText()+"\n"), senders)
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("concurrent Send() output interleaved:\n%s", buf.String())
	}
}

// Package stdout is the notifier that writes the digest as plain text to a
// writer, usually the process's standard output.
package stdout

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// Notifier writes each digest's plain text to a writer. It is safe for
// concurrent use: one digest is written with one Write call, so two digests
// never interleave.
type Notifier struct {
	mu sync.Mutex
	w  io.Writer
}

// New returns a Notifier that writes to w.
func New(w io.Writer) *Notifier {
	return &Notifier{w: w}
}

// Send writes d.PlainText() followed by one newline to the writer.
func (n *Notifier) Send(_ context.Context, d model.Digest) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, err := io.WriteString(n.w, d.PlainText()+"\n"); err != nil {
		return fmt.Errorf("stdout: write digest: %w", err)
	}
	return nil
}

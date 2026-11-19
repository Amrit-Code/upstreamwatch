package gitsrc_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/gitsrc"
)

// checker is the one-method interface upw doctor calls.
type checker interface {
	Check(ctx context.Context) error
}

var _ checker = (*gitsrc.CLI)(nil)

func TestCLI_Check_Installed(t *testing.T) {
	t.Parallel()
	g := gitsrc.New(gitsrc.Options{}, slog.New(slog.DiscardHandler))
	if err := g.Check(t.Context()); err != nil {
		t.Errorf("Check() = %v, want nil", err)
	}
}

func TestCLI_Check_NotInstalled(t *testing.T) {
	t.Setenv("PATH", "")
	g := gitsrc.New(gitsrc.Options{}, slog.New(slog.DiscardHandler))
	err := g.Check(t.Context())
	if err == nil {
		t.Fatal("Check() = nil, want the not-installed error")
	}
	if got, want := err.Error(), "git is not installed"; !strings.Contains(got, want) {
		t.Errorf("Check() = %q, want it to contain %q", got, want)
	}
}

func TestCLI_Check_CancelledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	g := gitsrc.New(gitsrc.Options{}, slog.New(slog.DiscardHandler))
	if err := g.Check(ctx); err == nil {
		t.Error("Check(cancelled) = nil, want an error")
	}
}

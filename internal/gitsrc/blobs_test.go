package gitsrc_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/gitsrc"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo"
)

func TestReadBlobs(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "main")
	up.Write("go.mod", "module example.com/app\n")
	up.Write("sub/requirements.txt", "requests==2.31.0\n")
	first := up.Commit("first", day(1))
	up.Write("go.mod", "module example.com/app\n\ngo 1.27\n")
	up.Write("big.lock", strings.Repeat("x", 1000))
	second := up.Commit("second", day(2))

	opts := testOptions(t)
	opts.MaxBlobBytes = 64
	g, repo := syncFixture(t, up, opts)

	refs := []model.BlobRef{
		{Commit: second, Path: "go.mod"},
		{Commit: first, Path: "big.lock"},          // not there yet
		{Commit: second, Path: "big.lock"},         // over the cap
		{Commit: first, Path: "go.mod"},            // order kept
		{Commit: first, Path: "no\nsuch/file.txt"}, // a newline in a missing path
		{Commit: first, Path: "sub"},               // a directory, not a file
		{Commit: first, Path: "sub/requirements.txt"},
	}
	got, err := g.ReadBlobs(t.Context(), repo, refs)
	if err != nil {
		t.Fatalf("ReadBlobs() error = %v", err)
	}
	want := []model.Blob{
		{Ref: refs[0], Content: []byte("module example.com/app\n\ngo 1.27\n")},
		{Ref: refs[1], Missing: true},
		{Ref: refs[2], TooLarge: true},
		{Ref: refs[3], Content: []byte("module example.com/app\n")},
		{Ref: refs[4], Missing: true},
		{Ref: refs[5], Missing: true},
		{Ref: refs[6], Content: []byte("requests==2.31.0\n")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReadBlobs() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestReadBlobs_DefaultCap(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "main")
	up.Write("huge.lock", strings.Repeat("h", 2<<20))
	up.Write("exact.lock", strings.Repeat("e", 1<<20))
	up.Write("medium.lock", strings.Repeat("m", 512<<10))
	head := up.Commit("lockfiles", day(1))

	opts := testOptions(t) // MaxBlobBytes zero: the 1 MiB default
	g, repo := syncFixture(t, up, opts)
	refs := []model.BlobRef{
		{Commit: head, Path: "huge.lock"},
		{Commit: head, Path: "exact.lock"},
		{Commit: head, Path: "medium.lock"},
	}
	got, err := g.ReadBlobs(t.Context(), repo, refs)
	if err != nil {
		t.Fatalf("ReadBlobs() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ReadBlobs() returned %d blobs, want 3", len(got))
	}
	if !got[0].TooLarge || got[0].Content != nil {
		t.Errorf("2 MiB blob = TooLarge %v, %d bytes, want TooLarge and no content", got[0].TooLarge, len(got[0].Content))
	}
	if got[1].TooLarge || len(got[1].Content) != 1<<20 {
		t.Errorf("1 MiB blob = TooLarge %v, %d bytes, want it read whole", got[1].TooLarge, len(got[1].Content))
	}
	if got[2].TooLarge || string(got[2].Content) != strings.Repeat("m", 512<<10) {
		t.Errorf("512 KiB blob = TooLarge %v, %d bytes, want it read whole", got[2].TooLarge, len(got[2].Content))
	}
}

func TestReadBlobs_NoRefsStartsNothing(t *testing.T) {
	t.Parallel()
	g := gitsrc.New(gitsrc.Options{}, discard())
	// The clone path does not exist: a call that started git or checked the
	// clone would fail.
	repo := model.TargetRepo{Slug: "none", ClonePath: "/nonexistent/none.git"}
	got, err := g.ReadBlobs(t.Context(), repo, nil)
	if err != nil || got != nil {
		t.Errorf("ReadBlobs(no refs) = %v, %v, want nil, nil", got, err)
	}
}

func TestReadBlobs_Errors(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "main")
	up.Write("go.mod", "module example.com/app\n")
	head := up.Commit("first", day(1))
	g, repo := syncFixture(t, up, testOptions(t))

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	tests := []struct {
		name string
		ctx  context.Context
		refs []model.BlobRef
		want string
	}{
		{"empty path", t.Context(), []model.BlobRef{{Commit: head}}, "not allowed"},
		{"option as commit", t.Context(), []model.BlobRef{{Commit: "--batch-all-objects", Path: "go.mod"}}, "revision"},
		{"cancelled", cancelled, []model.BlobRef{{Commit: head, Path: "go.mod"}}, "context canceled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := g.ReadBlobs(tt.ctx, repo, tt.refs)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("ReadBlobs() = %v, %v, want an error containing %q", got, err, tt.want)
			}
			if got != nil {
				t.Errorf("ReadBlobs() blobs = %v on error, want nil", got)
			}
		})
	}
}

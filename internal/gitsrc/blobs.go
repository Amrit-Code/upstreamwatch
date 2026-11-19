package gitsrc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// blobReaderSize is the minimum buffer for cat-file's output; it must hold
// the longest "<input> missing" reply.
const blobReaderSize = 64 << 10

// ReadBlobs returns one Blob per ref, in the order given, through one
// git cat-file --batch -z process. A path absent at its commit sets Missing;
// a blob over MaxBlobBytes is read past without being buffered and sets
// TooLarge.
func (g *CLI) ReadBlobs(ctx context.Context, repo model.TargetRepo, refs []model.BlobRef) (out []model.Blob, err error) {
	if len(refs) == 0 {
		return nil, nil
	}
	clone, err := g.prepareClone(repo)
	if err != nil {
		return nil, fmt.Errorf("read blobs of %s: %w", repo.Slug, err)
	}
	inputs := make([]string, len(refs))
	longest := 0
	for i, ref := range refs {
		if err := checkRevision(ref.Commit); err != nil {
			return nil, fmt.Errorf("read blobs of %s: %w", repo.Slug, err)
		}
		if ref.Path == "" || strings.ContainsRune(ref.Path, 0) || strings.ContainsRune(ref.Commit, 0) {
			return nil, fmt.Errorf("read blobs of %s: blob ref %d not allowed", repo.Slug, i)
		}
		inputs[i] = ref.Commit + ":" + ref.Path
		longest = max(longest, len(inputs[i]))
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stderr := &cappedBuffer{limit: outputLimit}
	cmd, err := g.command(ctx, invocation{
		gitDir: clone,
		args:   []string{"cat-file", "--batch", "-z"},
	}, stderr)
	if err != nil {
		return nil, fmt.Errorf("read blobs of %s: %w", repo.Slug, err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("read blobs of %s: %w", repo.Slug, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("read blobs of %s: %w", repo.Slug, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("read blobs of %s: start git cat-file: %w", repo.Slug, err)
	}

	var wg sync.WaitGroup
	// The writer feeds every ref and closes stdin while this side reads;
	// writing and reading in turn could deadlock on full pipes.
	wg.Go(func() {
		defer stdin.Close()
		for _, in := range inputs {
			if _, err := io.WriteString(stdin, in+"\x00"); err != nil {
				return // git exited or was killed; Wait reports why
			}
		}
	})
	defer func() {
		// On an early return git is killed; either way it is waited for
		// and the writer has finished before ReadBlobs returns.
		if err != nil {
			cancel()
		}
		werr := runError(ctx, "cat-file", cmd.Wait(), stderr, nil)
		wg.Wait()
		if err == nil && werr != nil {
			out, err = nil, fmt.Errorf("read blobs of %s: %w", repo.Slug, werr)
		}
	}()

	br := bufio.NewReaderSize(stdout, max(blobReaderSize, longest+len(" missing\n")))
	blobs := make([]model.Blob, len(refs))
	for i, ref := range refs {
		b, err := g.readBlob(br, inputs[i])
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				err = fmt.Errorf("%w (%w)", ctxErr, err)
			}
			return nil, fmt.Errorf("read blobs of %s: %s: %w", repo.Slug, ref.Path, err)
		}
		b.Ref = ref
		blobs[i] = b
	}
	return blobs, nil
}

// readBlob reads cat-file's reply to one input from br.
func (g *CLI) readBlob(br *bufio.Reader, input string) (model.Blob, error) {
	// The missing reply echoes the input, which may contain a newline, so
	// it is matched whole before the reply is read as a header line.
	missing := input + " missing\n"
	if peek, err := br.Peek(len(missing)); err == nil && string(peek) == missing {
		if _, err := br.Discard(len(missing)); err != nil {
			return model.Blob{}, err
		}
		return model.Blob{Missing: true}, nil
	}

	line, err := br.ReadString('\n')
	if err != nil {
		return model.Blob{}, fmt.Errorf("read cat-file header: %w", err)
	}
	fields := strings.Fields(line)
	if len(fields) != 3 || !isObjectName(fields[0]) {
		return model.Blob{}, fmt.Errorf("unexpected cat-file reply %q", strings.TrimSpace(line))
	}
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || size < 0 {
		return model.Blob{}, fmt.Errorf("unexpected cat-file size %q", fields[2])
	}

	var b model.Blob
	switch {
	case fields[1] != "blob":
		// A directory or submodule at that path is not a file.
		b.Missing = true
		err = discard(br, size)
	case size > g.opts.MaxBlobBytes:
		b.TooLarge = true
		err = discard(br, size)
	default:
		b.Content = make([]byte, size)
		_, err = io.ReadFull(br, b.Content)
	}
	if err != nil {
		return model.Blob{}, fmt.Errorf("read cat-file content: %w", err)
	}
	if c, err := br.ReadByte(); err != nil || c != '\n' {
		return model.Blob{}, errors.New("read cat-file content: missing terminator")
	}
	return b, nil
}

// discard reads past n bytes without keeping them.
func discard(r io.Reader, n int64) error {
	got, err := io.CopyN(io.Discard, r, n)
	if err != nil {
		return err
	}
	if got != n {
		return io.ErrUnexpectedEOF
	}
	return nil
}

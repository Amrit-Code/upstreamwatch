package epss

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
)

func gzipped(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestRefresh_DecompressedCap(t *testing.T) {
	t.Parallel()
	const feed = "cve,epss,percentile\nCVE-2026-0001,0.5,0.5\n"
	body := gzipped(t, feed)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	// Cleanup, not defer: the parallel subtests run after this function returns.
	t.Cleanup(srv.Close)

	tests := []struct {
		name    string
		limit   int64
		wantErr bool
	}{
		{name: "exactly the cap", limit: int64(len(feed))},
		{name: "one byte over the cap", limit: int64(len(feed)) - 1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger := slog.New(slog.DiscardHandler)
			hc := httpx.New(nil, httpx.Options{AllowPrivateHosts: []string{"127.0.0.1"}}, logger)
			f := New(hc, Options{URL: srv.URL}, logger)
			f.maxDecompressed = tt.limit

			got, err := f.Refresh(t.Context())
			if tt.wantErr {
				if !errors.Is(err, errDecompressedTooLarge) {
					t.Fatalf("Refresh() error = %v, want errDecompressedTooLarge", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Refresh() error = %v", err)
			}
			if len(got) != 1 {
				t.Errorf("Refresh() returned %d signals, want 1", len(got))
			}
		})
	}
}

func TestCappedReader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		limit   int64
		wantErr bool
	}{
		{name: "under", in: "abc", limit: 10},
		{name: "at", in: "abcdefghij", limit: 10},
		{name: "over", in: "abcdefghijk", limit: 10, wantErr: true},
		{name: "zero limit empty", in: "", limit: 0},
		{name: "zero limit one byte", in: "a", limit: 0, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := io.ReadAll(&cappedReader{r: bytes.NewReader([]byte(tt.in)), left: tt.limit})
			if tt.wantErr {
				if !errors.Is(err, errDecompressedTooLarge) {
					t.Fatalf("ReadAll() error = %v, want errDecompressedTooLarge", err)
				}
				return
			}
			if err != nil || string(got) != tt.in {
				t.Errorf("ReadAll() = %q, %v, want %q, nil", got, err, tt.in)
			}
		})
	}
}

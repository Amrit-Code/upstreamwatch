package osv_test

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln/osv"
)

const modifiedCSV = "2026-03-05T10:00:00Z,GO-2026-0005\n" +
	"2026-03-04T10:00:00.123456Z,GHSA-aaaa-bbbb-cccc\n" +
	"2026-03-03T10:00:00Z,GO-2026-0003\n" +
	"2026-03-02T10:00:00Z,GO-2026-0002\n" +
	"2026-03-01T10:00:00Z,GO-2026-0001\n"

// csvServer serves body at any path and records the escaped path asked for.
func csvServer(t *testing.T, body string, gotPath *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		*gotPath = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "text/csv")
		_, _ = w.Write([]byte(body))
	}))
}

func TestModifiedSince(t *testing.T) {
	t.Parallel()
	var path string
	srv := csvServer(t, modifiedCSV, &path)
	defer srv.Close()

	since := time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC)
	got, err := newClientOpts(osv.Options{BucketURL: srv.URL + "/"}).ModifiedSince(t.Context(), "Go", since)
	if err != nil {
		t.Fatalf("ModifiedSince() error = %v", err)
	}
	if path != "/Go/modified_id.csv" {
		t.Errorf("request path = %q, want /Go/modified_id.csv", path)
	}
	want := []model.AdvisoryRef{
		{ID: "GO-2026-0005", Modified: time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)},
		{ID: "GHSA-aaaa-bbbb-cccc", Modified: time.Date(2026, 3, 4, 10, 0, 0, 123456000, time.UTC)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ModifiedSince() = %v, want %v", got, want)
	}
}

func TestModifiedSince_Cases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		body    string
		since   time.Time
		wantIDs []string
		wantErr string
	}{
		{name: "zero since returns every line", body: modifiedCSV, wantIDs: []string{"GO-2026-0005", "GHSA-aaaa-bbbb-cccc", "GO-2026-0003", "GO-2026-0002", "GO-2026-0001"}},
		{name: "since before every line", body: modifiedCSV, since: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), wantIDs: []string{"GO-2026-0005", "GHSA-aaaa-bbbb-cccc", "GO-2026-0003", "GO-2026-0002", "GO-2026-0001"}},
		{name: "since at the newest line", body: modifiedCSV, since: time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC), wantIDs: nil},
		{name: "CRLF and no trailing newline", body: "2026-03-05T10:00:00Z,GO-1\r\n2026-03-04T10:00:00Z,GO-2", wantIDs: []string{"GO-1", "GO-2"}},
		{name: "empty file", body: "", wantIDs: nil},
		{name: "bad time", body: "2026-03-05T10:00:00Z,GO-1\nyesterday,GO-2\n", wantErr: "line 2: time is not RFC 3339"},
		{name: "no comma", body: "2026-03-05T10:00:00Z,GO-1\n2026-03-04T10:00:00Z,GO-2\n" + strings.Repeat("z", 400) + "\n", wantErr: "line 3: "},
		{name: "empty id", body: "2026-03-05T10:00:00Z,\n", wantErr: "line 1: "},
		{name: "blank line inside", body: "2026-03-05T10:00:00Z,GO-1\n\n2026-03-04T10:00:00Z,GO-2\n", wantErr: "line 2: "},
		{name: "malformed line after since is not read", body: "2026-03-05T10:00:00Z,GO-1\n2026-03-01T10:00:00Z,GO-0\ngarbage\n", since: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), wantIDs: []string{"GO-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var path string
			srv := csvServer(t, tt.body, &path)
			defer srv.Close()

			got, err := newClientOpts(osv.Options{BucketURL: srv.URL}).ModifiedSince(t.Context(), "PyPI", tt.since)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ModifiedSince() = %v, want an error containing %q", got, tt.wantErr)
				}
				msg := err.Error()
				if !strings.HasPrefix(msg, "osv: modified ids PyPI: ") || !strings.Contains(msg, "127.0.0.1") || !strings.Contains(msg, tt.wantErr) {
					t.Errorf("ModifiedSince() error = %q, want the operation, host and %q", msg, tt.wantErr)
				}
				if len(msg) > 300 {
					t.Errorf("ModifiedSince() error is %d bytes long, want no response body in it", len(msg))
				}
				return
			}
			if err != nil {
				t.Fatalf("ModifiedSince() error = %v", err)
			}
			var ids []string
			for _, r := range got {
				ids = append(ids, r.ID)
			}
			if !reflect.DeepEqual(ids, tt.wantIDs) {
				t.Errorf("ModifiedSince() ids = %v, want %v", ids, tt.wantIDs)
			}
		})
	}
}

func TestModifiedSince_EscapesEcosystem(t *testing.T) {
	t.Parallel()
	var path string
	srv := csvServer(t, modifiedCSV, &path)
	defer srv.Close()

	if _, err := newClientOpts(osv.Options{BucketURL: srv.URL}).ModifiedSince(t.Context(), "GitHub Actions", time.Time{}); err != nil {
		t.Fatalf("ModifiedSince() error = %v", err)
	}
	if path != "/GitHub%20Actions/modified_id.csv" {
		t.Errorf("request path = %q, want /GitHub%%20Actions/modified_id.csv", path)
	}
}

func TestModifiedSince_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := newClientOpts(osv.Options{BucketURL: srv.URL}).ModifiedSince(t.Context(), "Go", time.Time{})
	if err == nil || !strings.Contains(err.Error(), "osv: modified ids Go: 127.0.0.1") || !strings.Contains(err.Error(), "status 403") {
		t.Errorf("ModifiedSince() error = %v, want the operation, host and status 403", err)
	}
}

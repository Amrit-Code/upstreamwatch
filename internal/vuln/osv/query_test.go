package osv_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln/osv"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/")

// wireQuery is one querybatch query as the fake server decodes it.
type wireQuery struct {
	Package struct {
		Name      string `json:"name"`
		Ecosystem string `json:"ecosystem"`
	} `json:"package"`
	Version   string `json:"version"`
	PageToken string `json:"page_token"`
}

type wireVuln struct {
	ID       string `json:"id"`
	Modified string `json:"modified"`
}

type wireResult struct {
	Vulns         []wireVuln `json:"vulns,omitempty"`
	NextPageToken string     `json:"next_page_token,omitempty"`
}

// fakeBatch is a querybatch server: answer maps each query to its result,
// and every request is recorded.
type fakeBatch struct {
	t      *testing.T
	answer func(q wireQuery) wireResult

	mu           sync.Mutex
	bodies       [][]byte
	queries      [][]wireQuery
	contentTypes []string
}

func (f *fakeBatch) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/querybatch" {
		f.t.Errorf("request = %s %s, want POST /v1/querybatch", r.Method, r.URL.Path)
		http.NotFound(w, r)
		return
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r.Body); err != nil {
		f.t.Errorf("read request body: %v", err)
	}
	var req struct {
		Queries []wireQuery `json:"queries"`
	}
	if err := json.Unmarshal(buf.Bytes(), &req); err != nil {
		f.t.Errorf("decode request body: %v", err)
	}
	f.mu.Lock()
	f.bodies = append(f.bodies, buf.Bytes())
	f.queries = append(f.queries, req.Queries)
	f.contentTypes = append(f.contentTypes, r.Header.Get("Content-Type"))
	f.mu.Unlock()

	resp := struct {
		Results []wireResult `json:"results"`
	}{Results: make([]wireResult, len(req.Queries))}
	for i, q := range req.Queries {
		resp.Results[i] = f.answer(q)
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		f.t.Errorf("encode response: %v", err)
	}
}

var baseTime = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func TestQueryBatch_Batches(t *testing.T) {
	t.Parallel()
	const n = 1001
	fake := &fakeBatch{t: t, answer: func(q wireQuery) wireResult {
		var i int
		if _, err := fmt.Sscanf(q.Version, "1.0.%d", &i); err != nil {
			t.Errorf("unexpected version %q", q.Version)
		}
		return wireResult{Vulns: []wireVuln{
			{ID: "GO-TEST-" + q.Package.Name, Modified: baseTime.Format(time.RFC3339)},
			// Every query shares this id with a different modified time.
			{ID: "GO-TEST-SHARED", Modified: baseTime.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)},
		}}
	}}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	pvs := []model.PackageVersion{
		{PURL: "pkg:docker/library/golang", Version: "1.26"},
		{PURL: "pkg:golang/example.com/unpinned", Version: ""},
	}
	for i := range n {
		pvs = append(pvs, model.PackageVersion{PURL: fmt.Sprintf("pkg:golang/example.com/m%d", i), Version: fmt.Sprintf("v1.0.%d", i)})
	}

	got, err := newClientOpts(osv.Options{BaseURL: srv.URL, BatchSize: 500}).QueryBatch(t.Context(), pvs)
	if err != nil {
		t.Fatalf("QueryBatch() error = %v", err)
	}

	if len(fake.queries) != 3 {
		t.Fatalf("requests = %d, want 3", len(fake.queries))
	}
	for i, want := range []int{500, 500, 1} {
		if len(fake.queries[i]) != want {
			t.Errorf("request %d carried %d queries, want %d", i, len(fake.queries[i]), want)
		}
		if fake.contentTypes[i] != "application/json" {
			t.Errorf("request %d Content-Type = %q, want application/json", i, fake.contentTypes[i])
		}
		for _, q := range fake.queries[i] {
			if q.Package.Ecosystem != "Go" || q.Version == "" || strings.Contains(q.Package.Name, "unpinned") {
				t.Errorf("request %d sent query %+v, want only pinned Go pairs", i, q)
			}
		}
	}
	if len(got) != n+1 {
		t.Fatalf("QueryBatch() returned %d refs, want %d", len(got), n+1)
	}
	seen := map[string]bool{}
	for _, r := range got {
		if seen[r.ID] {
			t.Errorf("id %s returned twice", r.ID)
		}
		seen[r.ID] = true
		if r.ID == "GO-TEST-SHARED" {
			if want := baseTime.Add((n - 1) * time.Minute); !r.Modified.Equal(want) {
				t.Errorf("GO-TEST-SHARED modified = %v, want the greatest, %v", r.Modified, want)
			}
		}
	}
	for i := range n {
		if id := fmt.Sprintf("GO-TEST-example.com/m%d", i); !seen[id] {
			t.Errorf("id %s missing", id)
		}
	}
}

func TestQueryBatch_PageToken(t *testing.T) {
	t.Parallel()
	fake := &fakeBatch{t: t, answer: func(q wireQuery) wireResult {
		switch {
		case q.Package.Name == "example.com/a" && q.PageToken == "":
			return wireResult{Vulns: []wireVuln{{ID: "GO-1", Modified: "2026-01-01T00:00:00Z"}}, NextPageToken: "tok-a"}
		case q.Package.Name == "example.com/a" && q.PageToken == "tok-a":
			return wireResult{Vulns: []wireVuln{{ID: "GO-2", Modified: "2026-01-02T00:00:00.5Z"}}}
		case q.Package.Name == "example.com/b":
			return wireResult{Vulns: []wireVuln{{ID: "GO-3", Modified: "2026-01-03T00:00:00Z"}}}
		}
		t.Errorf("unexpected query %+v", q)
		return wireResult{}
	}}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	got, err := newClient(srv.URL).QueryBatch(t.Context(), []model.PackageVersion{
		{PURL: "pkg:golang/example.com/a", Version: "v1.0.0"},
		{PURL: "pkg:golang/example.com/b", Version: "v2.0.0"},
	})
	if err != nil {
		t.Fatalf("QueryBatch() error = %v", err)
	}
	if len(fake.queries) != 2 {
		t.Fatalf("requests = %d, want 2", len(fake.queries))
	}
	follow := fake.queries[1]
	if len(follow) != 1 || follow[0].Package.Name != "example.com/a" || follow[0].Version != "1.0.0" || follow[0].PageToken != "tok-a" {
		t.Errorf("follow-up queries = %+v, want only example.com/a 1.0.0 with page_token tok-a", follow)
	}
	for _, q := range fake.queries[0] {
		if q.PageToken != "" {
			t.Errorf("first request query %+v carries a page token", q)
		}
	}
	want := []model.AdvisoryRef{
		{ID: "GO-1", Modified: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "GO-3", Modified: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)},
		{ID: "GO-2", Modified: time.Date(2026, 1, 2, 0, 0, 0, 5e8, time.UTC)},
	}
	if len(got) != len(want) {
		t.Fatalf("QueryBatch() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i].ID != want[i].ID || !got[i].Modified.Equal(want[i].Modified) {
			t.Errorf("ref %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestQueryBatch_PageRoundsCapped(t *testing.T) {
	t.Parallel()
	fake := &fakeBatch{t: t, answer: func(q wireQuery) wireResult {
		return wireResult{NextPageToken: "again"}
	}}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	_, err := newClient(srv.URL).QueryBatch(t.Context(), []model.PackageVersion{{PURL: "pkg:pypi/requests", Version: "2.31.0"}})
	if err == nil {
		t.Fatal("QueryBatch() error = nil, want an error after 50 follow-up rounds")
	}
	if !strings.Contains(err.Error(), "osv: querybatch") || !strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("QueryBatch() error = %q, want it to name the operation and host", err)
	}
	if got := len(fake.queries); got != 51 {
		t.Errorf("requests = %d, want 51 (one plus 50 follow-ups)", got)
	}
}

func TestQueryBatch_Mapping(t *testing.T) {
	t.Parallel()
	fake := &fakeBatch{t: t, answer: func(wireQuery) wireResult { return wireResult{} }}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	got, err := newClient(srv.URL).QueryBatch(t.Context(), []model.PackageVersion{
		{PURL: "pkg:golang/toolchain", Version: "1.26"},
		{PURL: "pkg:golang/github.com/gin-gonic/gin", Version: "v1.9.1"},
		{PURL: "pkg:pypi/pyyaml", Version: "6.0.1"},
		{PURL: "pkg:githubactions/actions/checkout", Version: "v4.1.1"},
	})
	if err != nil {
		t.Fatalf("QueryBatch() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("QueryBatch() = %v, want no refs", got)
	}
	if len(fake.bodies) != 1 {
		t.Fatalf("requests = %d, want 1", len(fake.bodies))
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, fake.bodies[0], "", "  "); err != nil {
		t.Fatalf("indent request body: %v", err)
	}
	pretty.WriteByte('\n')
	golden := filepath.Join("testdata", t.Name()+".golden")
	if *update {
		if err := os.WriteFile(golden, pretty.Bytes(), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(pretty.Bytes(), want) {
		t.Errorf("request body =\n%s\nwant\n%s", pretty.Bytes(), want)
	}
}

func TestQueryBatch_NothingToQuery(t *testing.T) {
	t.Parallel()
	fake := &fakeBatch{t: t, answer: func(wireQuery) wireResult { return wireResult{} }}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	got, err := newClient(srv.URL).QueryBatch(t.Context(), []model.PackageVersion{{PURL: "pkg:docker/library/alpine", Version: "3.20"}})
	if err != nil || len(got) != 0 {
		t.Errorf("QueryBatch() = %v, %v, want no refs and no error", got, err)
	}
	if len(fake.queries) != 0 {
		t.Errorf("requests = %d, want none", len(fake.queries))
	}
}

func TestQueryBatch_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{
			name:    "server error",
			handler: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
			want:    "status 500",
		},
		{
			name: "result count mismatch",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"results":[]}`))
			},
			want: "got 0 results for 1 queries",
		},
		{
			name: "bad modified time",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"results":[{"vulns":[{"id":"GO-1","modified":"` + strings.Repeat("x", 500) + `"}]}]}`))
			},
			want: "not RFC 3339",
		},
		{
			name: "not JSON",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(strings.Repeat("<html>", 100)))
			},
			want: "decode response",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()
			_, err := newClient(srv.URL).QueryBatch(t.Context(), []model.PackageVersion{{PURL: "pkg:npm/%40scope/name", Version: "1.0.0"}})
			if err == nil {
				t.Fatal("QueryBatch() error = nil, want an error")
			}
			msg := err.Error()
			if !strings.HasPrefix(msg, "osv: querybatch: ") || !strings.Contains(msg, "127.0.0.1") || !strings.Contains(msg, tt.want) {
				t.Errorf("QueryBatch() error = %q, want osv: querybatch prefix, the host and %q", msg, tt.want)
			}
			if len(msg) > 300 {
				t.Errorf("QueryBatch() error is %d bytes long, want no response body in it", len(msg))
			}
		})
	}
}

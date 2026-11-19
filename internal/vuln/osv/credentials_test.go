package osv_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln/osv"
)

// OSV is public: no request the client makes carries a credential, and the
// querybatch and delta requests are not conditional.
func TestClient_RequestsCarryNoCredentials(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path)
		mu.Unlock()
		for _, h := range []string{"Authorization", "Cookie", "Proxy-Authorization", "If-None-Match", "If-Modified-Since"} {
			if v := r.Header.Get(h); v != "" {
				t.Errorf("%s %s sent %s: %q, want none", r.Method, r.URL.Path, h, v)
			}
		}
		switch {
		case r.URL.Path == "/v1/querybatch":
			if ct := r.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("querybatch Content-Type = %q, want application/json", ct)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":[{"vulns":[{"id":"GO-2026-0001","modified":"2026-03-01T12:00:00Z"}]}]}`))
		case strings.HasSuffix(r.URL.Path, "/modified_id.csv"):
			_, _ = w.Write([]byte("2026-03-05T10:00:00Z,GO-2026-0005\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := newClientOpts(osv.Options{BaseURL: srv.URL, BucketURL: srv.URL})

	refs, err := c.QueryBatch(t.Context(), []model.PackageVersion{{PURL: "pkg:golang/github.com/example-org/lib", Version: "v1.2.0"}})
	if err != nil || len(refs) != 1 {
		t.Fatalf("QueryBatch() = %v, %v; want one ref", refs, err)
	}
	refs, err = c.ModifiedSince(t.Context(), "Go", time.Time{})
	if err != nil || len(refs) != 1 {
		t.Fatalf("ModifiedSince() = %v, %v; want one ref", refs, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := []string{"POST /v1/querybatch", "GET /Go/modified_id.csv"}; strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Errorf("requests = %v, want %v", seen, want)
	}
}

package vuln_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/extract"
	"github.com/Amrit-Code/upstreamwatch/internal/httpx"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln/osv"
)

// pypiDottedRecord is an OSV record that names the project the way the PyPI
// advisory database writes it, with the dot.
const pypiDottedRecord = `{
  "id": "PYSEC-2026-0001",
  "modified": "2026-03-20T00:00:00Z",
  "published": "2026-03-01T00:00:00Z",
  "summary": "example summary",
  "affected": [{
    "package": {"name": "ruamel.yaml", "ecosystem": "PyPI"},
    "ranges": [{"type": "ECOSYSTEM", "events": [{"introduced": "0"}, {"fixed": "0.17.21"}]}]
  }]
}`

// TestPyPIDottedNameMatches parses a requirements file pinning a dotted PyPI
// name, runs the vulns job against the OSV adapter served by a fake OSV whose
// record spells the name with the dot, and expects a match and a finding.
func TestPyPIDottedNameMatches(t *testing.T) {
	t.Parallel()
	const advID = "PYSEC-2026-0001"

	var queried []string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/querybatch", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Queries []struct {
				Package struct {
					Name      string `json:"name"`
					Ecosystem string `json:"ecosystem"`
				} `json:"package"`
				Version string `json:"version"`
			} `json:"queries"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		type vulnRef struct {
			ID       string `json:"id"`
			Modified string `json:"modified"`
		}
		type result struct {
			Vulns []vulnRef `json:"vulns"`
		}
		var resp struct {
			Results []result `json:"results"`
		}
		for _, q := range req.Queries {
			queried = append(queried, q.Package.Ecosystem+"/"+q.Package.Name+"@"+q.Version)
			var res result
			if q.Package.Ecosystem == "PyPI" {
				res.Vulns = []vulnRef{{ID: advID, Modified: "2026-03-20T00:00:00Z"}}
			}
			resp.Results = append(resp.Results, res)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp) // a failed write fails the test through the client
	})
	mux.HandleFunc("GET /v1/vulns/"+advID, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pypiDottedRecord))
	})
	mux.HandleFunc("GET /bucket/PyPI/modified_id.csv", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("2026-03-20T00:00:00Z," + advID + "\n"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	logger := slog.New(slog.DiscardHandler)
	pm, err := extract.New(extract.Options{}, logger).Parse("requirements.txt", []byte("ruamel.yaml==0.17.0\n"))
	if err != nil {
		t.Fatalf("Parse(requirements.txt) error = %v", err)
	}
	if len(pm.Dependencies) != 1 {
		t.Fatalf("Parse(requirements.txt) = %+v, want one dependency", pm.Dependencies)
	}
	dep := pm.Dependencies[0]
	st := newFakeStore(interval(serverID, serverSlug, 31, 301, "requirements.txt", dep.Package.PURL, dep.Version, dep.Direct, day(1), time.Time{}))

	hc := httpx.New(nil, httpx.Options{AllowPrivateHosts: []string{"127.0.0.1"}, MaxWait: time.Millisecond}, logger)
	src := osv.New(hc, osv.Options{BaseURL: srv.URL, BucketURL: srv.URL + "/bucket"}, logger)
	svc := vuln.NewService(src, nil, nil, st, st, st, vuln.Options{Now: func() time.Time { return runAt }}, logger)
	stats, err := svc.Run(t.Context(), model.Scope{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if want := "PyPI/ruamel-yaml@0.17.0"; len(queried) != 1 || queried[0] != want {
		t.Errorf("queries = %v, want %v", queried, want)
	}
	const purl = "pkg:pypi/ruamel-yaml"
	ms := st.matches[serverID]
	if len(ms) != 1 || ms[0].AdvisoryID != advID || ms[0].DepVersionID != 31 || ms[0].FixedVersion != "0.17.21" {
		t.Errorf("matches of %s = %+v, want one %s match on interval 31 fixed in 0.17.21", serverSlug, ms, advID)
	}
	if _, ok := st.finding(fingerprint(serverSlug, purl, advID)); !ok {
		t.Errorf("no finding %s", fingerprint(serverSlug, purl, advID))
	}
	checkStats(t, stats, map[string]int64{"advisories_saved": 1, "matches": 1, "matches_open": 1, "findings_upserted": 1})
}

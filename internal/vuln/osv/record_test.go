package osv_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// recordServer serves the testdata record named by the request path, and 404
// for any other id. It records the escaped request path.
func recordServer(t *testing.T, gotPath *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotPath = r.URL.EscapedPath()
		id, ok := strings.CutPrefix(r.URL.Path, "/v1/vulns/")
		if r.Method != http.MethodGet || !ok || r.Header.Get("Authorization") != "" {
			t.Errorf("request = %s %s (auth %q), want an unauthenticated GET /v1/vulns/{id}", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		body, err := os.ReadFile(filepath.Join("testdata", filepath.Base(id)+".json"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// jsonEqual reports whether two JSON documents hold the same value.
func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatalf("decode %s: %v", a, err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	return reflect.DeepEqual(x, y)
}

func TestFetch_GoRecord(t *testing.T) {
	t.Parallel()
	var path string
	srv := recordServer(t, &path)
	defer srv.Close()

	got, err := newClient(srv.URL).Fetch(t.Context(), "GO-2024-2887")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if path != "/v1/vulns/GO-2024-2887" {
		t.Errorf("request path = %q, want /v1/vulns/GO-2024-2887", path)
	}
	if !bytes.Equal(got.Raw, readTestdata(t, "GO-2024-2887.json")) {
		t.Error("Raw differs from the served record")
	}
	if got.ID != "GO-2024-2887" || got.Summary == "" || got.Details == "" {
		t.Errorf("ID, Summary, Details = %q, %q, %q", got.ID, got.Summary, got.Details)
	}
	if want := []string{"BIT-golang-2024-24790", "CVE-2024-24790"}; !reflect.DeepEqual(got.Aliases, want) {
		t.Errorf("Aliases = %v, want %v", got.Aliases, want)
	}
	if want := time.Date(2026, 3, 23, 5, 7, 22, 471505044, time.UTC); !got.Modified.Equal(want) || got.Modified.Location() != time.UTC {
		t.Errorf("Modified = %v, want %v in UTC", got.Modified, want)
	}
	if want := time.Date(2024, 6, 4, 22, 48, 55, 0, time.UTC); !got.Published.Equal(want) {
		t.Errorf("Published = %v, want %v", got.Published, want)
	}
	if !got.Withdrawn.IsZero() {
		t.Errorf("Withdrawn = %v, want zero", got.Withdrawn)
	}
	if got.Severity != model.SeverityUnknown || got.CVSSScore != 0 || got.CVSSVector != "" {
		t.Errorf("Severity, CVSSScore, CVSSVector = %q, %v, %q, want unknown and no score", got.Severity, got.CVSSScore, got.CVSSVector)
	}
	if len(got.Affected) != 1 {
		t.Fatalf("Affected has %d entries, want 1", len(got.Affected))
	}
	a := got.Affected[0]
	if want := (model.Package{PURL: "pkg:golang/toolchain", Ecosystem: "golang", Name: "toolchain"}); a.Package != want {
		t.Errorf("Package = %+v, want %+v", a.Package, want)
	}
	if a.SourceDB != "GO" {
		t.Errorf("SourceDB = %q, want GO", a.SourceDB)
	}
	wantRanges := []model.AffectedRange{{
		Type: model.RangeSemver,
		Events: []model.RangeEvent{
			{Introduced: "0"}, {Fixed: "1.21.11"}, {Introduced: "1.22.0-0"}, {Fixed: "1.22.4"},
		},
	}}
	if !reflect.DeepEqual(a.Ranges, wantRanges) {
		t.Errorf("Ranges = %+v, want %+v", a.Ranges, wantRanges)
	}
	if a.Versions != nil {
		t.Errorf("Versions = %v, want none", a.Versions)
	}
	wantEco := []byte(`{"imports":[{"path":"net/netip","symbols":["Addr.IsGlobalUnicast","Addr.IsInterfaceLocalMulticast","Addr.IsLinkLocalMulticast","Addr.IsLoopback","Addr.IsMulticast","Addr.IsPrivate"]}]}`)
	if !jsonEqual(t, a.EcosystemSpecific, wantEco) {
		t.Errorf("EcosystemSpecific = %s, want %s", a.EcosystemSpecific, wantEco)
	}
	if !jsonEqual(t, a.DatabaseSpecific, []byte(`{"source":"https://vuln.go.dev/ID/GO-2024-2887.json"}`)) {
		t.Errorf("DatabaseSpecific = %s", a.DatabaseSpecific)
	}
}

func TestFetch_GHSARecord(t *testing.T) {
	t.Parallel()
	var path string
	srv := recordServer(t, &path)
	defer srv.Close()

	got, err := newClient(srv.URL).Fetch(t.Context(), "GHSA-6v2p-p543-phr9")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got.Severity != model.SeverityHigh {
		t.Errorf("Severity = %q, want high", got.Severity)
	}
	if got.CVSSScore != 7.5 || got.CVSSVector != "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H" {
		t.Errorf("CVSS = %v %q, want 7.5 and the record's vector", got.CVSSScore, got.CVSSVector)
	}
	if len(got.Affected) != 1 {
		t.Fatalf("Affected has %d entries, want 1", len(got.Affected))
	}
	a := got.Affected[0]
	if want := (model.Package{PURL: "pkg:golang/golang.org/x/oauth2", Ecosystem: "golang", Name: "golang.org/x/oauth2"}); a.Package != want {
		t.Errorf("Package = %+v, want %+v", a.Package, want)
	}
	if a.SourceDB != "GHSA" || a.EcosystemSpecific != nil {
		t.Errorf("SourceDB, EcosystemSpecific = %q, %s, want GHSA and nil", a.SourceDB, a.EcosystemSpecific)
	}
}

func TestFetch_PyPIRecord(t *testing.T) {
	t.Parallel()
	var path string
	srv := recordServer(t, &path)
	defer srv.Close()

	got, err := newClient(srv.URL).Fetch(t.Context(), "GHSA-9wx4-h78v-vm56")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got.Severity != model.SeverityMedium || got.CVSSScore != 5.6 {
		t.Errorf("Severity, CVSSScore = %q, %v, want medium (MODERATE) and 5.6", got.Severity, got.CVSSScore)
	}
	if len(got.Affected) != 1 {
		t.Fatalf("Affected has %d entries, want 1", len(got.Affected))
	}
	a := got.Affected[0]
	if want := (model.Package{PURL: "pkg:pypi/requests", Ecosystem: "pypi", Name: "requests"}); a.Package != want {
		t.Errorf("Package = %+v, want %+v", a.Package, want)
	}
	if len(a.Ranges) != 1 || a.Ranges[0].Type != model.RangeEcosystem || len(a.Versions) == 0 {
		t.Errorf("Ranges, len(Versions) = %+v, %d, want one ECOSYSTEM range and a versions list", a.Ranges, len(a.Versions))
	}
}

func TestFetch_NotFound(t *testing.T) {
	t.Parallel()
	var path string
	srv := recordServer(t, &path)
	defer srv.Close()

	_, err := newClient(srv.URL).Fetch(t.Context(), "GO-0000-0000")
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("Fetch() error = %v, want model.ErrNotFound", err)
	}
	if msg := err.Error(); !strings.HasPrefix(msg, "osv: fetch GO-0000-0000: ") || !strings.Contains(msg, "127.0.0.1") {
		t.Errorf("Fetch() error = %q, want the operation, id and host", msg)
	}
}

func TestFetch_EscapesID(t *testing.T) {
	t.Parallel()
	var path string
	srv := recordServer(t, &path)
	defer srv.Close()

	_, err := newClient(srv.URL).Fetch(t.Context(), "../x y")
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("Fetch() error = %v, want model.ErrNotFound", err)
	}
	if path != "/v1/vulns/..%2Fx%20y" {
		t.Errorf("request path = %q, want /v1/vulns/..%%2Fx%%20y", path)
	}
}

func TestFetch_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{
			name:    "server error",
			handler: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadRequest) },
			want:    "status 400",
		},
		{
			name: "not JSON",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(strings.Repeat("<html>", 100)))
			},
			want: "decode record",
		},
		{
			name: "bad published time",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"id":"GO-1","modified":"2026-01-01T00:00:00Z","published":"` + strings.Repeat("y", 400) + `"}`))
			},
			want: "published: time is not RFC 3339",
		},
		{
			name: "no id",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"modified":"2026-01-01T00:00:00Z"}`))
			},
			want: "record has no id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()
			_, err := newClient(srv.URL).Fetch(t.Context(), "GO-1")
			if err == nil {
				t.Fatal("Fetch() error = nil, want an error")
			}
			if errors.Is(err, model.ErrNotFound) {
				t.Errorf("Fetch() error = %v, want it not to be ErrNotFound", err)
			}
			msg := err.Error()
			if !strings.HasPrefix(msg, "osv: fetch GO-1: ") || !strings.Contains(msg, "127.0.0.1") || !strings.Contains(msg, tt.want) {
				t.Errorf("Fetch() error = %q, want the operation, host and %q", msg, tt.want)
			}
			if len(msg) > 300 {
				t.Errorf("Fetch() error is %d bytes long, want no response body in it", len(msg))
			}
		})
	}
}

func TestFetch_RecordIDMismatch(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"GO-2","modified":"2026-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()

	got, err := newClient(srv.URL).Fetch(t.Context(), "GO-1")
	if err == nil {
		t.Fatalf("Fetch() = %+v, nil, want an error for a record with another id", got)
	}
	if want := "osv: fetch GO-1: " + srv.Listener.Addr().String() + ": record id differs from the id requested"; err.Error() != want {
		t.Errorf("Fetch() error = %q, want %q", err, want)
	}
	if got.ID != "" || got.Raw != nil {
		t.Errorf("Fetch() advisory = %+v, want the zero value", got)
	}
}

func TestFetch_BodyOverCapIsError(t *testing.T) {
	t.Parallel()
	const capBytes = 2 << 20
	tests := []struct {
		name    string
		size    int
		wantErr bool
	}{
		{name: "at the cap", size: capBytes},
		{name: "over the cap", size: capBytes + 1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prefix := `{"id":"GO-1","modified":"2026-01-01T00:00:00Z","summary":"`
			suffix := `"}`
			body := prefix + strings.Repeat("s", tt.size-len(prefix)-len(suffix)) + suffix
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()

			_, err := newClient(srv.URL).Fetch(t.Context(), "GO-1")
			if (err != nil) != tt.wantErr {
				t.Fatalf("Fetch() of a %d-byte record error = %v, wantErr %v", tt.size, err, tt.wantErr)
			}
		})
	}
}

func TestFetch_LongDetailsCut(t *testing.T) {
	t.Parallel()
	details := strings.Repeat("é", 40<<10) // 80 KiB of two-byte runes
	body := []byte(`{"id":"GO-1","modified":"2026-01-01T00:00:00Z","details":"` + details + `"}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	got, err := newClient(srv.URL).Fetch(t.Context(), "GO-1")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if want := strings.Repeat("é", 32<<10) + "…"; got.Details != want {
		t.Errorf("Details is %d bytes, want %d bytes: 64 KiB of whole runes and …", len(got.Details), len(want))
	}
	if !bytes.Equal(got.Raw, body) {
		t.Error("Raw differs from the served record")
	}
}

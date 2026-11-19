package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/config"
	"github.com/Amrit-Code/upstreamwatch/internal/sched"
	"github.com/Amrit-Code/upstreamwatch/internal/store"
)

var listeningLine = regexp.MustCompile(`msg="http server listening" addr=(\S+)`)

// serveUntilDone starts "upw serve" with the config at cfgPath, waits for the
// HTTP server to listen, calls during with its address, then cancels the
// context. It returns run's exit code and the captured stderr.
func serveUntilDone(t *testing.T, cfgPath string, during func(addr string)) (int, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stdout := newSyncBuffer("")
	stderr := newSyncBuffer(`msg="http server listening"`)
	done := make(chan int, 1)
	go func() { done <- run(ctx, []string{"serve", "-config", cfgPath}, stdout, stderr, noEnv) }()

	select {
	case <-stderr.found:
	case code := <-done:
		t.Fatalf("serve returned %d before listening; stderr:\n%s", code, stderr.String())
	case <-time.After(10 * time.Second):
		cancel()
		<-done
		t.Fatalf("serve did not listen within 10s; stderr:\n%s", stderr.String())
	}

	m := listeningLine.FindStringSubmatch(stderr.String())
	if m == nil {
		cancel()
		<-done
		t.Fatalf("no listening address in stderr:\n%s", stderr.String())
	}
	if during != nil {
		during(m[1])
	}

	cancel()
	var code int
	select {
	case code = <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("serve did not return within 5s of cancellation; stderr:\n%s", stderr.String())
	}
	if out := stdout.String(); out != "" {
		t.Errorf("serve stdout = %q, want empty", out)
	}
	return code, stderr.String()
}

// get fetches url and returns the status code and body.
func get(t *testing.T, url string) (int, string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request %s: %v", url, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read GET %s body: %v", url, err)
	}
	return resp.StatusCode, string(body)
}

// assertMigrated opens the database at path as cmd/upw does and checks that
// migrating it applies nothing: every embedded migration is recorded and no
// newer one is.
func assertMigrated(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database file: %v", err)
	}
	var logs bytes.Buffer
	db, err := store.Open(t.Context(), path, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatalf("store.Open(%s): %v", path, err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate after serve: %v", err)
	}
	if n := strings.Count(logs.String(), `msg="migration applied"`); n != 0 {
		t.Errorf("Migrate after serve applied %d migrations, want 0; logs:\n%s", n, logs.String())
	}
}

func TestServe_HealthAndShutdown(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	code, stderr := serveUntilDone(t, cfg, func(addr string) {
		status, body := get(t, "http://"+addr+"/health")
		if status != http.StatusOK || !strings.Contains(body, `"ok":true`) {
			t.Errorf("GET /health = %d %q, want 200 containing \"ok\":true", status, body)
		}
		status, body = get(t, "http://"+addr+"/")
		if status != http.StatusOK || !strings.Contains(body, "No open findings.") {
			t.Errorf("GET / = %d %q, want 200 containing \"No open findings.\"", status, body)
		}
	})
	if code != 0 {
		t.Fatalf("serve exit code = %d, want 0; stderr:\n%s", code, stderr)
	}

	// Each embedded migration is applied once.
	files, err := os.ReadDir(filepath.Join("..", "..", "internal", "store", "migrations"))
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	if n := strings.Count(stderr, `msg="migration applied"`); n != len(files) {
		t.Errorf("serve applied %d migrations, want %d; stderr:\n%s", n, len(files), stderr)
	}
	assertMigrated(t, dbPath(cfg))

	if n := strings.Count(stderr, `msg="schedule registered"`); n != 6 {
		t.Errorf("serve logged %d schedule registered lines, want 6; stderr:\n%s", n, stderr)
	}
	for _, s := range []struct{ name, cron string }{
		{"ingest", "5 */6 * * *"},
		{"vulns", "10 */6 * * *"},
		{"issues", "15 * * * *"},
		{"llm", "20 * * * *"},
		{"digest", "0 8 * * *"},
		{"backup", "30 3 * * *"},
	} {
		want := regexp.MustCompile(`msg="schedule registered" schedule=` + s.name + ` cron="` + regexp.QuoteMeta(s.cron) + `"`)
		if !want.MatchString(stderr) {
			t.Errorf("stderr has no schedule registered line for %s with cron %q; stderr:\n%s", s.name, s.cron, stderr)
		}
	}
}

func TestServe_SecondStartIsNoop(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	code, first := serveUntilDone(t, cfg, nil)
	if code != 0 {
		t.Fatalf("first serve exit code = %d, want 0; stderr:\n%s", code, first)
	}
	code, second := serveUntilDone(t, cfg, nil)
	if code != 0 {
		t.Fatalf("second serve exit code = %d, want 0; stderr:\n%s", code, second)
	}
	if n := strings.Count(second, `msg="migration applied"`); n != 0 {
		t.Errorf("second serve applied %d migrations, want 0; stderr:\n%s", n, second)
	}
	assertMigrated(t, dbPath(cfg))
}

func TestServe_BadTargetScheme(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "targets:\n  - slug: example-server\n    url: file:///tmp/x\n")
	var stdout, stderr bytes.Buffer
	got := run(t.Context(), []string{"serve", "-config", cfg}, &stdout, &stderr, noEnv)
	if want := 1; got != want {
		t.Errorf("serve exit code = %d, want %d", got, want)
	}
	errOut := stderr.String()
	if !strings.HasPrefix(errOut, "upw: ") || !strings.Contains(errOut, "targets[0].url") || !strings.Contains(errOut, `scheme "file"`) {
		t.Errorf("serve stderr = %q, want upw: naming targets[0].url and scheme \"file\"", errOut)
	}
	if strings.Contains(errOut, "http server listening") {
		t.Errorf("serve stderr = %q, want nothing listening", errOut)
	}
	if _, err := os.Stat(filepath.Dir(dbPath(cfg))); err == nil {
		t.Errorf("serve created the data directory, want no database opened")
	}
	if got := stdout.String(); got != "" {
		t.Errorf("serve stdout = %q, want empty", got)
	}
}

func TestServe_ConfigErrorsOnePerLine(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "targets:\n  - slug: example-server\n    url: file:///tmp/x\nprefilter:\n  threshold: 0\n")
	var stdout, stderr bytes.Buffer
	if got, want := run(t.Context(), []string{"serve", "-config", cfg}, &stdout, &stderr, noEnv), 1; got != want {
		t.Errorf("serve exit code = %d, want %d", got, want)
	}
	lines := strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n")
	var sawURL, sawThreshold bool
	for _, l := range lines {
		sawURL = sawURL || strings.Contains(l, "targets[0].url")
		sawThreshold = sawThreshold || strings.Contains(l, "prefilter.threshold")
		if strings.Contains(l, "targets[0].url") && strings.Contains(l, "prefilter.threshold") {
			t.Errorf("two problems on one line: %q", l)
		}
	}
	if !sawURL || !sawThreshold {
		t.Errorf("serve stderr = %q, want lines for targets[0].url and prefilter.threshold", stderr.String())
	}
}

func TestServe_MissingConfig(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "absent.yaml")
	var stdout, stderr bytes.Buffer
	if got, want := run(t.Context(), []string{"serve", "-config", missing}, &stdout, &stderr, noEnv), 1; got != want {
		t.Errorf("serve exit code = %d, want %d", got, want)
	}
	if errOut := stderr.String(); !strings.HasPrefix(errOut, "upw: read config:") {
		t.Errorf("serve stderr = %q, want it to start with \"upw: read config:\"", errOut)
	}
}

func TestServe_SchemaTooNew(t *testing.T) {
	t.Parallel()
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 binary not found; it is needed to insert a future schema_migrations row")
	}
	cfg := writeConfig(t, "")
	path := dbPath(cfg)

	db, err := store.Open(t.Context(), path, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), sqlite3, "-batch", "-init", os.DevNull, path,
		"INSERT INTO schema_migrations (version, name, checksum, applied_ts) VALUES (9999, '9999_future.sql', 'x', '2026-03-01T12:00:00Z');")
	cmd.Env = []string{"HOME=" + t.TempDir()}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sqlite3 insert: %v\n%s", err, out)
	}

	var stdout, stderr bytes.Buffer
	if got, want := run(t.Context(), []string{"serve", "-config", cfg}, &stdout, &stderr, noEnv), 1; got != want {
		t.Errorf("serve exit code = %d, want %d", got, want)
	}
	errOut := stderr.String()
	want := "upw: " + store.ErrSchemaTooNew.Error()
	if !strings.Contains(errOut, want) {
		t.Errorf("serve stderr = %q, want it to contain %q", errOut, want)
	}
	if strings.Contains(errOut, "http server listening") {
		t.Errorf("serve stderr = %q, want nothing listening", errOut)
	}
}

func TestScheduleEntries(t *testing.T) {
	t.Parallel()
	s := config.Schedules{Ingest: "1 * * * *", Vulns: "2 * * * *", Issues: "3 * * * *", LLM: "4 * * * *", Digest: "5 * * * *", Backup: "6 * * * *"}
	tests := []struct {
		name       string
		registered []string
		want       []sched.Entry
	}{
		{
			name:       "none registered",
			registered: nil,
			want: []sched.Entry{
				{Name: "ingest", Cron: "1 * * * *"},
				{Name: "vulns", Cron: "2 * * * *"},
				{Name: "issues", Cron: "3 * * * *"},
				{Name: "llm", Cron: "4 * * * *"},
				{Name: "digest", Cron: "5 * * * *"},
				{Name: "backup", Cron: "6 * * * *"},
			},
		},
		{
			name:       "all registered",
			registered: []string{"ingest", "timeline", "usage", "upstream", "vulns", "issues", "prefilter", "triage", "digest", "backup"},
			want: []sched.Entry{
				{Name: "ingest", Cron: "1 * * * *", Jobs: []string{"ingest", "timeline", "usage", "upstream"}},
				{Name: "vulns", Cron: "2 * * * *", Jobs: []string{"vulns"}},
				{Name: "issues", Cron: "3 * * * *", Jobs: []string{"issues", "prefilter"}},
				{Name: "llm", Cron: "4 * * * *", Jobs: []string{"triage"}},
				{Name: "digest", Cron: "5 * * * *", Jobs: []string{"digest"}},
				{Name: "backup", Cron: "6 * * * *", Jobs: []string{"backup"}},
			},
		},
		{
			name:       "some registered keep chain order",
			registered: []string{"upstream", "ingest", "prefilter"},
			want: []sched.Entry{
				{Name: "ingest", Cron: "1 * * * *", Jobs: []string{"ingest", "upstream"}},
				{Name: "vulns", Cron: "2 * * * *"},
				{Name: "issues", Cron: "3 * * * *", Jobs: []string{"prefilter"}},
				{Name: "llm", Cron: "4 * * * *"},
				{Name: "digest", Cron: "5 * * * *"},
				{Name: "backup", Cron: "6 * * * *"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := scheduleEntries(s, tt.registered); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("scheduleEntries(%v) =\n%+v\nwant\n%+v", tt.registered, got, tt.want)
			}
		})
	}
}

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/pipeline"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln/epss"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln/kev"
	"github.com/Amrit-Code/upstreamwatch/internal/vuln/osv"
)

// TestMain points the vulns job's sources at a local server that answers
// with empty results, for every test in the package, so no test that runs
// the vulns job reaches the internet. vulnSources is set before any test
// starts and never changed while they run.
func TestMain(m *testing.M) {
	srv := httptest.NewServer(http.HandlerFunc(serveEmptyVulnSources))
	u, err := url.Parse(srv.URL)
	if err != nil {
		panic(err)
	}
	vulnSources = vulnEndpoints{
		osv:               osv.Options{BaseURL: srv.URL, BucketURL: srv.URL + "/bucket"},
		kev:               kev.Options{URL: srv.URL + "/kev.json"},
		epss:              epss.Options{URL: srv.URL + "/epss.csv"},
		allowPrivateHosts: []string{u.Hostname()},
	}
	code := m.Run()
	srv.Close()
	os.Exit(code)
}

// fakeSMTP is a plain-text SMTP server on loopback that accepts any login
// and keeps the DATA of every message, dot-unstuffed, with CRLF endings.
type fakeSMTP struct {
	ln   net.Listener
	wg   sync.WaitGroup
	mu   sync.Mutex
	msgs []string
}

// startFakeSMTP starts a fakeSMTP that stops when the test ends.
func startFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeSMTP{ln: ln}
	s.wg.Go(func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // the listener was closed
			}
			s.wg.Go(func() { s.serve(conn) })
		}
	})
	t.Cleanup(func() {
		_ = ln.Close() // best effort: the accept loop ends either way
		s.wg.Wait()
	})
	return s
}

// port is the port the server listens on.
func (s *fakeSMTP) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

// messages returns the DATA of every message received so far.
func (s *fakeSMTP) messages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.msgs...)
}

// serve speaks just enough SMTP for net/smtp: EHLO with AUTH, AUTH, MAIL,
// RCPT, DATA, RSET, NOOP and QUIT.
func (s *fakeSMTP) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second)) // best effort: a stuck client then fails its test
	r := bufio.NewReader(conn)
	reply := func(text string) { _, _ = io.WriteString(conn, text+"\r\n") }
	reply("220 fake.example.invalid ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		verb, _, _ := strings.Cut(strings.TrimRight(line, "\r\n"), " ")
		switch strings.ToUpper(verb) {
		case "EHLO":
			reply("250-fake.example.invalid")
			reply("250 AUTH PLAIN")
		case "AUTH":
			reply("235 2.7.0 accepted")
		case "MAIL", "RCPT", "RSET", "NOOP":
			reply("250 ok")
		case "DATA":
			reply("354 go ahead")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(strings.TrimPrefix(l, "."))
			}
			s.mu.Lock()
			s.msgs = append(s.msgs, b.String())
			s.mu.Unlock()
			reply("250 queued")
		case "QUIT":
			reply("221 bye")
			return
		default:
			reply("502 not implemented")
		}
	}
}

// hitCounter counts requests per URL path.
type hitCounter struct {
	mu   sync.Mutex
	hits map[string]int
}

func (c *hitCounter) add(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hits == nil {
		c.hits = map[string]int{}
	}
	c.hits[path]++
}

func (c *hitCounter) get(path string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits[path]
}

// vulnSourceHits counts the requests the local vuln sources server has
// answered, across every test in the package.
var vulnSourceHits hitCounter

// serveEmptyVulnSources answers like OSV, KEV and EPSS with nothing in them:
// one empty querybatch result per query, empty modified_id.csv files, an empty KEV catalog
// and an EPSS CSV with only its header. Anything else is 404.
func serveEmptyVulnSources(w http.ResponseWriter, r *http.Request) {
	vulnSourceHits.add(r.URL.Path)
	switch p := r.URL.Path; {
	case p == "/v1/querybatch":
		// OSV answers with one result per query, in order; the client
		// rejects any other count.
		var req struct {
			Queries []json.RawMessage `json:"queries"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad querybatch body", http.StatusBadRequest)
			return
		}
		results := make([]struct{}, len(req.Queries))
		w.Header().Set("Content-Type", "application/json")
		// Best effort: a failed write fails the test that caused it.
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
	case strings.HasPrefix(p, "/bucket/") && strings.HasSuffix(p, "/modified_id.csv"):
		w.Header().Set("Content-Type", "text/csv")
	case p == "/kev.json":
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"vulnerabilities":[]}`)) // best effort, as above
	case p == "/epss.csv":
		w.Header().Set("Content-Type", "text/csv")
		_, _ = w.Write([]byte("cve,epss,percentile\n")) // best effort, as above
	default:
		http.NotFound(w, r)
	}
}

// fakeJob is a pipeline job that records the scope of every run and returns
// what run returns, or nothing when run is nil.
type fakeJob struct {
	name   string
	run    func(ctx context.Context, scope model.Scope) (model.Stats, error)
	mu     sync.Mutex
	scopes []model.Scope
}

func (j *fakeJob) Name() string { return j.name }

func (j *fakeJob) Run(ctx context.Context, scope model.Scope) (model.Stats, error) {
	j.mu.Lock()
	j.scopes = append(j.scopes, scope)
	j.mu.Unlock()
	if j.run == nil {
		return nil, nil
	}
	return j.run(ctx, scope)
}

// runScopes returns the scopes the job has been run with.
func (j *fakeJob) runScopes() []model.Scope {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]model.Scope(nil), j.scopes...)
}

// fakeRunStore hands out run IDs and stores nothing.
type fakeRunStore struct {
	mu     sync.Mutex
	nextID int64
}

func (s *fakeRunStore) StartRun(_ context.Context, job string, startedAt time.Time) (model.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	return model.Run{ID: s.nextID, Job: job, StartedAt: startedAt, Status: model.RunRunning}, nil
}

func (s *fakeRunStore) FinishRun(context.Context, model.Run) error { return nil }

// fakeFindingStore accepts every finding and stores nothing.
type fakeFindingStore struct{}

func (fakeFindingStore) UpsertFinding(_ context.Context, f model.Finding) (model.Finding, error) {
	return f, nil
}

func (fakeFindingStore) ResolveFinding(context.Context, string, time.Time) error { return nil }

// newFakeRunner returns a Runner over jobs with stores that keep nothing.
func newFakeRunner(t *testing.T, jobs ...pipeline.Job) *pipeline.Runner {
	t.Helper()
	r, err := pipeline.NewRunner(jobs, &fakeRunStore{}, fakeFindingStore{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	return r
}

# Template: fake HTTP servers for GitHub, OSV and deps.dev

Use these when testing an adapter that calls an outside HTTP API. Each fake is an `http.Handler` served by `httptest.NewServer(h)`. The server is plain http on loopback, which httpx accepts only because the test names `127.0.0.1` in `AllowPrivateHosts`.

- Point the adapter's base URL at `srv.URL`.
- Build its `httpx` client with `httpx.New(nil, httpx.Options{AllowPrivateHosts: []string{"127.0.0.1"}}, logger)`. There is no test-only policy and no certificate to trust.
- Call `t.Cleanup(srv.Close)`.

The `newTestHTTPX` helper and the adapter constructors below show the shape, not a fixed API.

Recorded bodies under `testdata/` are trimmed copies of real public responses: a few fields, a few items. They contain no tokens and nothing private.

## GitHub issues: ETag, 304, pagination and rate-limit headers

```go
type fakeGitHub struct {
	mu        sync.Mutex
	etag      string        // current ETag; GitHub sends weak ETags like W/"abc"
	pages     [][]byte      // page bodies, 1-based in the Link header
	remaining int           // value for X-RateLimit-Remaining
	reset     time.Time     // value for X-RateLimit-Reset
	status    int           // if non-zero, answer every request with this status
	retry     string        // Retry-After value to send with status
	queries   []string      // RawQuery of every request, in order
	auth      []string      // Authorization header of every request
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, r.URL.RawQuery)
	f.auth = append(f.auth, r.Header.Get("Authorization"))

	h := w.Header()
	h.Set("X-RateLimit-Limit", "5000")
	h.Set("X-RateLimit-Remaining", strconv.Itoa(f.remaining))
	h.Set("X-RateLimit-Reset", strconv.FormatInt(f.reset.Unix(), 10))

	if f.status != 0 {
		if f.retry != "" {
			h.Set("Retry-After", f.retry)
		}
		w.WriteHeader(f.status)
		return
	}
	if r.URL.Path != "/repos/example-org/example-lib/issues" {
		http.NotFound(w, r)
		return
	}
	if inm := r.Header.Get("If-None-Match"); inm != "" && inm == f.etag {
		w.WriteHeader(http.StatusNotModified) // 304 carries no body
		return
	}

	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		page, _ = strconv.Atoi(p)
	}
	if page < len(f.pages) {
		next := *r.URL
		q := next.Query()
		q.Set("page", strconv.Itoa(page+1))
		next.RawQuery = q.Encode()
		h.Set("Link", fmt.Sprintf(`<https://%s%s?%s>; rel="next"`, r.Host, next.Path, next.RawQuery)) // adapters follow it verbatim
	}
	h.Set("ETag", f.etag)
	h.Set("Content-Type", "application/json")
	w.Write(f.pages[page-1])
}
```

The cases every GitHub `IssueSource` test covers:

```go
func TestGitHub_ListUpdated(t *testing.T) {
	t.Parallel()
	since := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	repo := model.UpstreamRepo{Host: "github.com", Owner: "example-org", Name: "example-lib"}

	tests := []struct {
		name        string
		fake        *fakeGitHub
		etag        string // ETag the caller already holds
		wantItems   int
		wantNotMod  bool
		wantErr     error // sentinel or typed error, checked with errors.Is / errors.AsType
	}{
		{name: "first poll returns items and ETag", fake: newFake(t, `W/"v1"`, "issues-page1.json"), wantItems: 2},
		{name: "matching ETag is not modified", fake: newFake(t, `W/"v1"`, "issues-page1.json"), etag: `W/"v1"`, wantNotMod: true},
		{name: "follows Link to the second page", fake: newFake(t, `W/"v1"`, "issues-page1.json", "issues-page2.json"), wantItems: 3},
		{name: "secondary limit with Retry-After", fake: &fakeGitHub{status: http.StatusForbidden, retry: "60"}, wantErr: github.ErrRateLimited},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(tt.fake)
			t.Cleanup(srv.Close)
			src := github.New(github.Options{BaseURL: srv.URL, Token: "test-token"}, newTestHTTPX(t, srv), slog.New(slog.DiscardHandler))

			got, err := src.ListUpdated(t.Context(), repo, since, tt.etag)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ListUpdated() error = %v, want %v", err, tt.wantErr)
			}
			if len(got.Items) != tt.wantItems || got.NotModified != tt.wantNotMod {
				t.Errorf("ListUpdated() = %d items, notModified %v; want %d, %v", len(got.Items), got.NotModified, tt.wantItems, tt.wantNotMod)
			}
		})
	}
}
```

Also assert:

- **Identical parameters:** two polls with the same cursor produce byte-identical `RawQuery` values (`fake.queries[0] == fake.queries[1]`). GitHub matches ETags only when the request is identical.
- **Low budget:** with `remaining` below the stop threshold, the adapter returns its rate-limit error carrying the reset time, and makes no further requests.
- **The token is sent and never logged:** `fake.auth` holds the bearer token. A logger writing to a `bytes.Buffer` (`slog.New(slog.NewTextHandler(&buf, nil))`) never contains `"test-token"`.

## OSV: querybatch and record fetch

```go
type fakeOSV struct {
	t       *testing.T
	results map[string][]string // purl@version -> advisory IDs
	maxSize int                 // largest batch the adapter may send
}

func (f *fakeOSV) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/querybatch":
		var req struct {
			Queries []struct {
				Package struct {
					PURL string `json:"purl"`
				} `json:"package"`
			} `json:"queries"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(req.Queries) > f.maxSize {
			f.t.Errorf("querybatch sent %d queries, max %d", len(req.Queries), f.maxSize)
		}
		type vuln struct {
			ID       string `json:"id"`
			Modified string `json:"modified"`
		}
		resp := struct {
			Results []struct {
				Vulns []vuln `json:"vulns"`
			} `json:"results"`
		}{}
		for _, q := range req.Queries { // results are positional: one per query
			var e struct {
				Vulns []vuln `json:"vulns"`
			}
			for _, id := range f.results[q.Package.PURL] {
				e.Vulns = append(e.Vulns, vuln{ID: id, Modified: "2026-03-01T00:00:00Z"})
			}
			resp.Results = append(resp.Results, e)
		}
		json.NewEncoder(w).Encode(resp)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/vulns/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/vulns/")
		http.ServeFile(w, r, filepath.Join("testdata", "osv", id+".json"))
	default:
		http.NotFound(w, r)
	}
}
```

Cover: more distinct versions than one batch holds are split into several batches; an ID already held with the same `modified` is not fetched again; a 5xx returns an error and keeps nothing half-written.

## deps.dev: GetVersion with an encoded package name

deps.dev wants the package name as one path segment, percent-encoded: a Go module path `github.com/example/lib` arrives as `github.com%2Fexample%2Flib`. Check `r.URL.EscapedPath()`, not `r.URL.Path`, because `Path` is already decoded.

```go
type fakeDepsDev struct{ t *testing.T }

func (f *fakeDepsDev) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const want = "/v3/systems/pypi/packages/pyyaml/versions/6.0.1"
	if r.URL.EscapedPath() != want {
		f.t.Errorf("path = %q, want %q", r.URL.EscapedPath(), want)
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join("testdata", "depsdev", "pyyaml-6.0.1.json"))
}
```

The recorded body keeps only `versionKey` and `relatedProjects` (with `projectKey.id` and `relationType`). Cover: `SOURCE_REPO` and `ISSUE_TRACKER` both present; only `SOURCE_REPO`; none (the resolver reports not found and the chain moves on); a 404 for an unknown package.

package httpx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// step is one scripted response.
type step struct {
	status int
	header map[string]string
	body   string
}

// scripted answers request i with steps[i], repeating the last step, and
// records each request's headers.
type scripted struct {
	mu      sync.Mutex
	steps   []step
	headers []http.Header
}

func (s *scripted) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	i := len(s.headers)
	s.headers = append(s.headers, r.Header.Clone())
	st := s.steps[min(i, len(s.steps)-1)]
	s.mu.Unlock()
	for k, v := range st.header {
		w.Header().Set(k, v)
	}
	w.WriteHeader(st.status)
	fmt.Fprint(w, st.body)
}

func (s *scripted) hits() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.headers)
}

func (s *scripted) header(i int) http.Header {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.headers[i]
}

func serve(t *testing.T, steps ...step) (*httptest.Server, *scripted) {
	t.Helper()
	s := &scripted{steps: steps}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return srv, s
}

// stall blocks until the client gives up. With body set it sends the status
// line and a first chunk before stalling.
func stall(body bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if body {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "partial")
			w.(http.Flusher).Flush()
		}
		<-r.Context().Done()
	}
}

func TestNew_Defaults(t *testing.T) {
	t.Parallel()
	c := New(nil, Options{}, nil)
	want := Options{
		Timeout:      2 * time.Minute,
		MaxBodyBytes: 64 << 20,
		MaxAttempts:  3,
		MaxWait:      30 * time.Second,
		UserAgent:    "upstreamwatch",
		MinGap:       map[string]time.Duration{"api.github.com": 100 * time.Millisecond},
	}
	if !reflect.DeepEqual(c.opts, want) {
		t.Errorf("New(Options{}).opts = %+v, want %+v", c.opts, want)
	}
	if g := c.gates["api.github.com"]; g == nil || g.gap != 100*time.Millisecond {
		t.Errorf("api.github.com gate = %+v, want gap 100ms", g)
	}
}

func TestDo_Success(t *testing.T) {
	t.Parallel()
	srv, s := serve(t, step{status: 200, body: "hello", header: map[string]string{"ETag": `"e1"`}})
	c := newTestClient(t, Options{})

	got, err := c.Do(t.Context(), Request{URL: srv.URL + "/a?b=c", Header: http.Header{"Authorization": {"Bearer tok-1"}}})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if got.Status != 200 || string(got.Body) != "hello" || got.ETag != `"e1"` || got.URL != srv.URL+"/a?b=c" || got.NotModified {
		t.Errorf("Do() = %+v, want 200 hello with ETag and URL", got)
	}
	h := s.header(0)
	if ua := h.Get("User-Agent"); ua != "upstreamwatch" {
		t.Errorf("User-Agent = %q, want upstreamwatch", ua)
	}
	if a := h.Get("Authorization"); a != "Bearer tok-1" {
		t.Errorf("Authorization = %q, want the adapter's header", a)
	}

	if _, err := c.Do(t.Context(), Request{URL: srv.URL, Header: http.Header{"User-Agent": {"custom/1"}}}); err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if ua := s.header(1).Get("User-Agent"); ua != "custom/1" {
		t.Errorf("User-Agent = %q, want the request's own", ua)
	}
}

func TestDo_BlocksLoopbackWithoutAllow(t *testing.T) {
	t.Parallel()
	var hits atomic.Int64
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) })
	plain := httptest.NewServer(h)
	t.Cleanup(plain.Close)
	tlsSrv := httptest.NewTLSServer(h)
	t.Cleanup(tlsSrv.Close)
	c := newTestClient(t, Options{AllowPrivateHosts: []string{}})

	// The scheme is checked before any connection, so plain http to loopback
	// is refused for its scheme.
	_, err := c.Do(t.Context(), Request{URL: plain.URL + "/x"})
	if !errors.Is(err, ErrSchemeNotAllowed) {
		t.Errorf("Do(http loopback) error = %v, want ErrSchemeNotAllowed", err)
	}

	_, err = c.Do(t.Context(), Request{URL: tlsSrv.URL + "/private-path"})
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("Do(https loopback) error = %v, want ErrBlockedAddress", err)
	}
	host := strings.TrimPrefix(tlsSrv.URL, "https://")
	if msg := err.Error(); !strings.Contains(msg, host) || strings.Contains(msg, "private-path") {
		t.Errorf("error %q, want it to name %s and not the path", msg, host)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("server hits = %d, want 0", n)
	}
	if s := c.clock.Sleeps(); len(s) != 0 {
		t.Errorf("retry waits = %v, want none for a policy refusal", s)
	}
}

func TestDo_RedirectToBlockedHost(t *testing.T) {
	t.Parallel()
	var targetHits atomic.Int64
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetHits.Add(1) }))
	t.Cleanup(target.Close)
	targetPort := target.URL[strings.LastIndex(target.URL, ":")+1:]

	mux := http.NewServeMux()
	origin := httptest.NewServer(mux)
	t.Cleanup(origin.Close)
	originPort := origin.URL[strings.LastIndex(origin.URL, ":")+1:]
	mux.HandleFunc("/to-http", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://localhost:"+originPort+"/final", http.StatusFound)
	})
	mux.HandleFunc("/to-https", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://localhost:"+targetPort+"/final", http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("redirect target reached through %s", r.Host)
	})
	c := newTestClient(t, Options{})

	_, err := c.Do(t.Context(), Request{URL: origin.URL + "/to-http"})
	if !errors.Is(err, ErrSchemeNotAllowed) || !strings.Contains(err.Error(), "localhost") {
		t.Errorf("redirect to http://localhost error = %v, want ErrSchemeNotAllowed naming localhost", err)
	}
	_, err = c.Do(t.Context(), Request{URL: origin.URL + "/to-https"})
	if !errors.Is(err, ErrBlockedAddress) {
		t.Errorf("redirect to https://localhost error = %v, want ErrBlockedAddress", err)
	}
	if n := targetHits.Load(); n != 0 {
		t.Errorf("blocked target hits = %d, want 0", n)
	}
}

func TestDo_RejectsSchemeAndUserinfo(t *testing.T) {
	t.Parallel()
	c := newTestClient(t, Options{})
	tests := []struct {
		url    string
		want   error
		absent string
	}{
		{url: "http://example.com", want: ErrSchemeNotAllowed},
		{url: "ftp://example.com/file", want: ErrSchemeNotAllowed},
		{url: "file:///etc/passwd", want: ErrSchemeNotAllowed},
		{url: "https://user:pass@example.com", want: errUserinfo, absent: "pass"},
		{url: "https:///no-host", want: errNoHost},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			t.Parallel()
			_, err := c.Do(t.Context(), Request{URL: tt.url})
			if !errors.Is(err, tt.want) {
				t.Fatalf("Do(%q) error = %v, want %v", tt.url, err, tt.want)
			}
			if tt.absent != "" && strings.Contains(err.Error(), tt.absent) {
				t.Errorf("Do(%q) error = %q, must not contain %q", tt.url, err, tt.absent)
			}
		})
	}
}

func TestDo_InvalidURLHidesIt(t *testing.T) {
	t.Parallel()
	c := newTestClient(t, Options{})
	_, err := c.Do(t.Context(), Request{URL: "https://exa mple.com/s3cr3t-path?token=qv4lue"})
	if err == nil {
		t.Fatal("Do(invalid URL) error = nil, want an error")
	}
	if msg := err.Error(); strings.Contains(msg, "qv4lue") || strings.Contains(msg, "s3cr3t-path") {
		t.Errorf("Do(invalid URL) error = %q, must not quote the URL", msg)
	}
}

func TestDo_RedirectLimit(t *testing.T) {
	t.Parallel()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/r/"))
		if n > 0 {
			http.Redirect(w, r, "/r/"+strconv.Itoa(n-1), http.StatusFound)
			return
		}
		fmt.Fprint(w, "done")
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, Options{})

	got, err := c.Do(t.Context(), Request{URL: srv.URL + "/r/5"})
	if err != nil {
		t.Fatalf("Do(5 redirects) error = %v", err)
	}
	if got.URL != srv.URL+"/r/0" || string(got.Body) != "done" {
		t.Errorf("Do(5 redirects) = URL %q body %q, want the final hop", got.URL, got.Body)
	}

	hits.Store(0)
	_, err = c.Do(t.Context(), Request{URL: srv.URL + "/r/6"})
	if !errors.Is(err, errRedirectLimit) {
		t.Fatalf("Do(6 redirects) error = %v, want the redirect limit", err)
	}
	if n := hits.Load(); n != 6 {
		t.Errorf("hits = %d, want 6 (no retry of a policy refusal)", n)
	}
}

func TestDo_BodyCap(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("n"))
		fmt.Fprint(w, strings.Repeat("x", n))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, Options{MaxBodyBytes: 10})

	got, err := c.Do(t.Context(), Request{URL: srv.URL + "/?n=10"})
	if err != nil || len(got.Body) != 10 {
		t.Errorf("Do(body at cap) = %d bytes, %v; want 10, nil", len(got.Body), err)
	}
	got, err = c.Do(t.Context(), Request{URL: srv.URL + "/?n=11"})
	if !errors.Is(err, ErrBodyTooLarge) || got.Body != nil {
		t.Errorf("Do(body over cap) = %d bytes, %v; want no body, ErrBodyTooLarge", len(got.Body), err)
	}
	if s := c.clock.Sleeps(); len(s) != 0 {
		t.Errorf("retry waits = %v, want none for a body over the cap", s)
	}
	got, err = c.Do(t.Context(), Request{URL: srv.URL + "/?n=11", MaxBodyBytes: 20})
	if err != nil || len(got.Body) != 11 {
		t.Errorf("Do(per-request cap 20) = %d bytes, %v; want 11, nil", len(got.Body), err)
	}
}

func TestDo_Timeout(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		body bool
	}{{"stalled headers", false}, {"stalled body", true}} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(stall(tt.body))
			t.Cleanup(srv.Close)
			c := newTestClient(t, Options{Timeout: 100 * time.Millisecond, MaxAttempts: 1})
			start := time.Now()
			_, err := c.Do(t.Context(), Request{URL: srv.URL})
			if err == nil {
				t.Fatal("Do(stalled server) error = nil, want a timeout")
			}
			if d := time.Since(start); d > 2*time.Second {
				t.Errorf("Do(stalled server) took %v, want about the 100ms timeout", d)
			}
		})
	}

	t.Run("cancel during retry wait", func(t *testing.T) {
		t.Parallel()
		srv, s := serve(t, step{status: 503, header: map[string]string{"Retry-After": "20"}})
		c := newTestClient(t, Options{})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var waited time.Duration
		c.sleep = func(ctx context.Context, d time.Duration) error {
			waited = d
			cancel()
			return sleepCtx(ctx, d)
		}
		start := time.Now()
		_, err := c.Do(ctx, Request{URL: srv.URL})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Do() error = %v, want context.Canceled", err)
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("Do() took %v after cancel, want prompt return", d)
		}
		if waited != 20*time.Second || s.hits() != 1 {
			t.Errorf("waited %v after %d attempts, want 20s after 1", waited, s.hits())
		}
	})
}

func TestDo_Retries(t *testing.T) {
	t.Parallel()
	reset10s := unixAt(t0.Add(10 * time.Second))
	resetHour := unixAt(t0.Add(time.Hour))
	tests := []struct {
		name       string
		method     string
		steps      []step
		wantHits   int
		wantSleeps []time.Duration
		wantStatus int       // StatusError status, 0 for none
		wantReset  time.Time // RateLimitError ResetAt, zero for none
	}{
		{name: "503 then 200", steps: []step{{status: 503}, {status: 200}}, wantHits: 2, wantSleeps: []time.Duration{0}},
		{name: "503 three times", steps: []step{{status: 503}}, wantHits: 3, wantSleeps: []time.Duration{0, 0}, wantStatus: 503},
		{name: "HEAD 502 then 200", method: http.MethodHead, steps: []step{{status: 502}, {status: 200}}, wantHits: 2, wantSleeps: []time.Duration{0}},
		{name: "504 then 200", steps: []step{{status: 504}, {status: 200}}, wantHits: 2, wantSleeps: []time.Duration{0}},
		{
			name:     "429 retry-after seconds",
			steps:    []step{{status: 429, header: map[string]string{"Retry-After": "1"}}, {status: 200}},
			wantHits: 2, wantSleeps: []time.Duration{time.Second},
		},
		{
			name:     "429 retry-after date",
			steps:    []step{{status: 429, header: map[string]string{"Retry-After": t0.Add(5 * time.Second).Format(http.TimeFormat)}}, {status: 200}},
			wantHits: 2, wantSleeps: []time.Duration{5 * time.Second},
		},
		{
			name:     "429 retry-after an hour",
			steps:    []step{{status: 429, header: map[string]string{"Retry-After": "3600"}}},
			wantHits: 1, wantReset: t0.Add(time.Hour),
		},
		{
			name:     "403 limit spent, short reset",
			steps:    []step{{status: 403, header: map[string]string{"X-Ratelimit-Remaining": "0", "X-Ratelimit-Reset": reset10s}}, {status: 200}},
			wantHits: 2, wantSleeps: []time.Duration{10 * time.Second},
		},
		{
			name:     "403 limit spent, reset in an hour",
			steps:    []step{{status: 403, header: map[string]string{"X-Ratelimit-Remaining": "0", "X-Ratelimit-Reset": resetHour}}},
			wantHits: 1, wantReset: t0.Add(time.Hour),
		},
		{
			name:     "429 without hints until attempts run out",
			steps:    []step{{status: 429}},
			wantHits: 3, wantSleeps: []time.Duration{0, 0}, wantReset: t0.Add(2 * time.Second),
		},
		{name: "plain 403", steps: []step{{status: 403}}, wantHits: 1, wantStatus: 403},
		{name: "404", steps: []step{{status: 404}}, wantHits: 1, wantStatus: 404},
		{name: "POST 503", method: http.MethodPost, steps: []step{{status: 503}}, wantHits: 1, wantStatus: 503},
		{name: "POST 429", method: http.MethodPost, steps: []step{{status: 429, header: map[string]string{"Retry-After": "1"}}}, wantHits: 1, wantStatus: 429},
		{name: "304 without validators", steps: []step{{status: 304}}, wantHits: 1, wantStatus: 304},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv, s := serve(t, tt.steps...)
			c := newTestClient(t, Options{})
			got, err := c.Do(t.Context(), Request{Method: tt.method, URL: srv.URL + "/p"})

			if n := s.hits(); n != tt.wantHits {
				t.Errorf("hits = %d, want %d", n, tt.wantHits)
			}
			if sl := c.clock.Sleeps(); !reflect.DeepEqual(sl, tt.wantSleeps) {
				t.Errorf("waits = %v, want %v", sl, tt.wantSleeps)
			}
			switch {
			case tt.wantStatus != 0:
				se, ok := errors.AsType[*StatusError](err)
				if !ok || se.Status != tt.wantStatus || se.Path != "/p" {
					t.Errorf("error = %v, want *StatusError %d for /p", err, tt.wantStatus)
				}
				if !reflect.DeepEqual(got, Response{}) {
					t.Errorf("Response = %+v, want zero with an error", got)
				}
			case !tt.wantReset.IsZero():
				rl, ok := errors.AsType[*RateLimitError](err)
				if !ok || !rl.ResetAt.Equal(tt.wantReset) {
					t.Errorf("error = %v, want *RateLimitError reset at %v", err, tt.wantReset)
				}
			default:
				if err != nil || got.Status != 200 {
					t.Errorf("Do() = %d, %v; want 200, nil", got.Status, err)
				}
			}
		})
	}
}

func TestDo_TransportErrorRetried(t *testing.T) {
	t.Parallel()
	addr := closedPort(t)
	tests := []struct {
		method     string
		wantSleeps int
	}{
		{http.MethodGet, 2},
		{http.MethodPost, 0},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			t.Parallel()
			c := newTestClient(t, Options{})
			_, err := c.Do(t.Context(), Request{Method: tt.method, URL: "http://" + addr + "/x"})
			if err == nil || isPolicyRefusal(err) {
				t.Fatalf("Do(closed port) error = %v, want a connection error", err)
			}
			if n := len(c.clock.Sleeps()); n != tt.wantSleeps {
				t.Errorf("waits = %d, want %d", n, tt.wantSleeps)
			}
			if n := strings.Count(c.log.String(), "msg=\"http request\""); n != tt.wantSleeps+1 {
				t.Errorf("attempt log lines = %d, want %d", n, tt.wantSleeps+1)
			}
		})
	}
}

func TestDo_Pacing(t *testing.T) {
	t.Parallel()
	const gap = 100 * time.Millisecond

	t.Run("paced host", func(t *testing.T) {
		t.Parallel()
		var (
			mu       sync.Mutex
			starts   []time.Time
			inFlight atomic.Int64
			maxSeen  atomic.Int64
		)
		var c *testClient
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := inFlight.Add(1)
			defer inFlight.Add(-1)
			if n > maxSeen.Load() {
				maxSeen.Store(n)
			}
			mu.Lock()
			starts = append(starts, c.clock.Now())
			mu.Unlock()
		}))
		t.Cleanup(srv.Close)
		c = newTestClient(t, Options{MinGap: map[string]time.Duration{"127.0.0.1": gap}})

		var wg sync.WaitGroup
		for range 3 {
			wg.Go(func() {
				if _, err := c.Do(t.Context(), Request{URL: srv.URL}); err != nil {
					t.Errorf("Do() error = %v", err)
				}
			})
		}
		wg.Wait()

		mu.Lock()
		defer mu.Unlock()
		if len(starts) != 3 {
			t.Fatalf("requests = %d, want 3", len(starts))
		}
		for i := 1; i < len(starts); i++ {
			if d := starts[i].Sub(starts[i-1]); d < gap {
				t.Errorf("request %d started %v after the previous, want at least %v", i, d, gap)
			}
		}
		if m := maxSeen.Load(); m != 1 {
			t.Errorf("concurrent requests = %d, want 1", m)
		}
	})

	t.Run("unpaced host", func(t *testing.T) {
		t.Parallel()
		srv, _ := serve(t, step{status: 200})
		c := newTestClient(t, Options{MinGap: map[string]time.Duration{"example.com": time.Hour}})
		for range 2 {
			if _, err := c.Do(t.Context(), Request{URL: srv.URL}); err != nil {
				t.Fatalf("Do() error = %v", err)
			}
		}
		if s := c.clock.Sleeps(); len(s) != 0 {
			t.Errorf("waits = %v, want none", s)
		}
	})

	t.Run("cancelled while waiting at the gate", func(t *testing.T) {
		t.Parallel()
		srv, s := serve(t, step{status: 200})
		c := newTestClient(t, Options{MinGap: map[string]time.Duration{"127.0.0.1": gap}})
		g := c.gates["127.0.0.1"]
		g.slot <- struct{}{} // another request holds the gate
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := c.Do(ctx, Request{URL: srv.URL})
		if !errors.Is(err, context.Canceled) || s.hits() != 0 {
			t.Errorf("Do() = %v after %d hits, want context.Canceled and none", err, s.hits())
		}
	})
}

func TestDo_ValidatorsCached(t *testing.T) {
	t.Parallel()
	const lastMod = "Sun, 01 Mar 2026 10:00:00 GMT"
	var (
		mu   sync.Mutex
		etag = `"v1"`
		seen []http.Header
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, r.Header.Clone())
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Last-Modified", lastMod)
		fmt.Fprint(w, "feed "+etag)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, Options{})
	u := srv.URL + "/feed.json?b=2&a=1"
	req := Request{URL: u, Validators: ValidatorsCached}

	got, err := c.Do(t.Context(), req)
	if err != nil || got.NotModified || string(got.Body) != `feed "v1"` {
		t.Fatalf("first Do() = %+v, %v; want the full body", got, err)
	}
	want := model.HTTPValidator{URL: u, ETag: `"v1"`, LastModified: lastMod, FetchedAt: t0, Status: 200}
	if v, _ := c.cache.entry(u); v != want {
		t.Errorf("cached = %+v, want %+v", v, want)
	}
	if seen[0].Get("If-None-Match") != "" || seen[0].Get("If-Modified-Since") != "" {
		t.Errorf("first request sent validators %v", seen[0])
	}

	got, err = c.Do(t.Context(), req)
	if err != nil || !got.NotModified || got.Status != 304 || len(got.Body) != 0 {
		t.Fatalf("second Do() = %+v, %v; want NotModified", got, err)
	}
	if inm, ims := seen[1].Get("If-None-Match"), seen[1].Get("If-Modified-Since"); inm != `"v1"` || ims != lastMod {
		t.Errorf("second request sent If-None-Match %q If-Modified-Since %q", inm, ims)
	}
	if _, saves := c.cache.calls(); saves != 1 {
		t.Errorf("saves = %d after a 304, want 1", saves)
	}

	mu.Lock()
	etag = `"v2"`
	mu.Unlock()
	got, err = c.Do(t.Context(), req)
	if err != nil || got.NotModified || got.ETag != `"v2"` {
		t.Fatalf("third Do() = %+v, %v; want the changed body", got, err)
	}
	if v, _ := c.cache.entry(u); v.ETag != `"v2"` {
		t.Errorf("cached ETag = %q, want the replacement", v.ETag)
	}

	t.Run("store read error", func(t *testing.T) {
		c := newTestClient(t, Options{})
		c.cache.getErr = errors.New("disk I/O error")
		if _, err := c.Do(t.Context(), req); err == nil || !strings.Contains(err.Error(), "disk I/O error") {
			t.Errorf("Do() error = %v, want the store error", err)
		}
	})
	t.Run("store write error", func(t *testing.T) {
		c := newTestClient(t, Options{})
		c.cache.saveErr = errors.New("database is locked")
		if _, err := c.Do(t.Context(), req); err == nil || !strings.Contains(err.Error(), "database is locked") {
			t.Errorf("Do() error = %v, want the store error", err)
		}
	})
	t.Run("POST is refused", func(t *testing.T) {
		c := newTestClient(t, Options{})
		mu.Lock()
		before := len(seen)
		mu.Unlock()
		if _, err := c.Do(t.Context(), Request{Method: http.MethodPost, URL: u, Validators: ValidatorsCached}); err == nil {
			t.Error("Do(POST, ValidatorsCached) error = nil, want an error")
		}
		mu.Lock()
		after := len(seen)
		mu.Unlock()
		if gets, _ := c.cache.calls(); gets != 0 || after != before {
			t.Errorf("POST in cached mode reached the store (%d) or server (%d)", gets, after-before)
		}
	})
}

func TestDo_ValidatorsCaller(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `W/"abc"`)
		if r.Header.Get("If-None-Match") == `W/"abc"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		fmt.Fprint(w, "[]")
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, Options{})

	got, err := c.Do(t.Context(), Request{URL: srv.URL, Validators: ValidatorsCaller})
	if err != nil || got.NotModified || got.ETag != `W/"abc"` {
		t.Errorf("Do(no ETag) = %+v, %v; want full response with ETag", got, err)
	}
	got, err = c.Do(t.Context(), Request{URL: srv.URL, Validators: ValidatorsCaller, ETag: `W/"abc"`})
	if err != nil || !got.NotModified || got.ETag != `W/"abc"` {
		t.Errorf("Do(matching ETag) = %+v, %v; want NotModified with ETag", got, err)
	}
	if gets, saves := c.cache.calls(); gets != 0 || saves != 0 {
		t.Errorf("CacheStore calls = %d gets, %d saves; want none", gets, saves)
	}
}

func TestDo_RateLimitHeadersParsed(t *testing.T) {
	t.Parallel()
	srv, _ := serve(t,
		step{status: 200, header: map[string]string{"X-Ratelimit-Remaining": "4999", "X-Ratelimit-Reset": "1772366400"}},
		step{status: 200},
	)
	c := newTestClient(t, Options{})

	got, err := c.Do(t.Context(), Request{URL: srv.URL})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	want := RateLimit{Known: true, Remaining: 4999, ResetAt: time.Unix(1772366400, 0).UTC()}
	if got.RateLimit != want {
		t.Errorf("RateLimit = %+v, want %+v", got.RateLimit, want)
	}
	got, err = c.Do(t.Context(), Request{URL: srv.URL})
	if err != nil || got.RateLimit != (RateLimit{}) {
		t.Errorf("RateLimit without headers = %+v, %v; want zero", got.RateLimit, err)
	}
}

func TestDo_SecretPathNeverLeaks(t *testing.T) {
	t.Parallel()
	ok, _ := serve(t, step{status: 200})
	fail, _ := serve(t, step{status: 500})
	busy, _ := serve(t, step{status: 503})
	stalled := httptest.NewServer(stall(false))
	t.Cleanup(stalled.Close)
	refused := "http://" + closedPort(t)

	cases := []struct {
		name   string
		base   string
		method string
	}{
		{"success", ok.URL, http.MethodPost},
		{"500", fail.URL, http.MethodPost},
		{"503 retried", busy.URL, http.MethodGet},
		{"connection refused", refused, http.MethodGet},
		{"timeout", stalled.URL, http.MethodGet},
	}

	t.Run("secret path", func(t *testing.T) {
		t.Parallel()
		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				c := newTestClient(t, Options{Timeout: 100 * time.Millisecond})
				_, err := c.Do(t.Context(), Request{Method: tt.method, URL: tt.base + "/api/webhooks/123/s3cr3t-path", SecretPath: true})
				if err != nil && strings.Contains(err.Error(), "s3cr3t-path") {
					t.Errorf("error leaks the path: %v", err)
				}
				if se, ok := errors.AsType[*StatusError](err); ok && se.Path != "" {
					t.Errorf("StatusError.Path = %q, want empty", se.Path)
				}
				if log := c.log.String(); strings.Contains(log, "s3cr3t-path") || !strings.Contains(log, "http request") {
					t.Errorf("log leaks the path or is empty:\n%s", log)
				}
			})
		}
	})

	t.Run("query string", func(t *testing.T) {
		t.Parallel()
		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				c := newTestClient(t, Options{Timeout: 100 * time.Millisecond})
				_, err := c.Do(t.Context(), Request{Method: tt.method, URL: tt.base + "/items?token=qv4lue"})
				if err != nil && strings.Contains(err.Error(), "qv4lue") {
					t.Errorf("error leaks the query: %v", err)
				}
				if log := c.log.String(); strings.Contains(log, "qv4lue") {
					t.Errorf("log leaks the query:\n%s", log)
				}
			})
		}
	})
}

func TestDo_AuthorizationNotLogged(t *testing.T) {
	t.Parallel()
	srv, s := serve(t, step{status: 503}, step{status: 500})
	c := newTestClient(t, Options{})
	_, err := c.Do(t.Context(), Request{URL: srv.URL, Header: http.Header{"Authorization": {"Bearer tok-abc123"}}})
	if _, ok := errors.AsType[*StatusError](err); !ok {
		t.Fatalf("Do() error = %v, want *StatusError", err)
	}
	if strings.Contains(err.Error(), "tok-abc123") {
		t.Errorf("error leaks the token: %v", err)
	}
	if log := c.log.String(); strings.Contains(log, "tok-abc123") || !strings.Contains(log, "http retry") {
		t.Errorf("log leaks the token or lacks the retry line:\n%s", log)
	}
	if got := s.header(1).Get("Authorization"); got != "Bearer tok-abc123" {
		t.Errorf("retried request Authorization = %q, want the token", got)
	}
}

func TestHTTPClient_AppliesPolicy(t *testing.T) {
	t.Parallel()
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(tlsSrv.Close)
	srv, s := serve(t, step{status: 503})

	blocked := New(nil, Options{Timeout: 5 * time.Second}, nil).HTTPClient()
	if blocked.Timeout != 5*time.Second {
		t.Errorf("HTTPClient().Timeout = %v, want 5s", blocked.Timeout)
	}
	if _, err := blocked.Get(tlsSrv.URL); !errors.Is(err, ErrBlockedAddress) {
		t.Errorf("Get(https loopback) error = %v, want ErrBlockedAddress", err)
	}
	if _, err := blocked.Get(srv.URL); !errors.Is(err, ErrSchemeNotAllowed) {
		t.Errorf("Get(http loopback) error = %v, want ErrSchemeNotAllowed", err)
	}

	allowed := New(nil, Options{AllowPrivateHosts: []string{"127.0.0.1"}}, nil).HTTPClient()
	resp, err := allowed.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get(allowed host) error = %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	if resp.StatusCode != 503 || s.hits() != 1 {
		t.Errorf("Get(503) = status %d after %d hits, want 503 after 1", resp.StatusCode, s.hits())
	}

	u, _ := url.Parse(srv.URL)
	if _, err := allowed.Get("http://user:pw@" + u.Host + "/"); !errors.Is(err, errUserinfo) {
		t.Errorf("Get(userinfo) error = %v, want the userinfo refusal", err)
	}
}

// pathRecorder records the headers of the latest request to each path,
// redirects the paths in its table, answers the paths in statuses with that
// status and no body, and answers anything else 200 "done".
type pathRecorder struct {
	mu        sync.Mutex
	headers   map[string]http.Header
	redirects map[string]string // path -> absolute Location
	statuses  map[string]int    // path -> status
}

func (p *pathRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	if p.headers == nil {
		p.headers = map[string]http.Header{}
	}
	p.headers[r.URL.Path] = r.Header.Clone()
	to, redirect := p.redirects[r.URL.Path]
	status, scripted := p.statuses[r.URL.Path]
	p.mu.Unlock()
	switch {
	case redirect:
		http.Redirect(w, r, to, http.StatusFound)
	case scripted:
		w.WriteHeader(status)
	default:
		fmt.Fprint(w, "done")
	}
}

func (p *pathRecorder) header(path string) (http.Header, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	h, ok := p.headers[path]
	return h, ok
}

// redirectHeaders are the headers every redirect test sends.
func redirectHeaders() http.Header {
	return http.Header{
		"Authorization": {"Bearer tok-redirect"},
		"X-Api-Key":     {"key-redirect"},
		"Cookie":        {"session=cookie-redirect"},
		"If-None-Match": {`"etag-redirect"`},
		"Accept":        {"application/json"},
	}
}

// fetch sends a GET carrying redirectHeaders through Client.Do or through
// HTTPClient(), as via names.
func fetch(t *testing.T, via, rawURL string) {
	t.Helper()
	switch via {
	case "Do":
		c := newTestClient(t, Options{})
		if _, err := c.Do(t.Context(), Request{URL: rawURL, Header: redirectHeaders()}); err != nil {
			t.Fatalf("Do(%s) error = %v", rawURL, err)
		}
	case "HTTPClient":
		hc := New(nil, Options{AllowPrivateHosts: []string{"127.0.0.1"}}, nil).HTTPClient()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, rawURL, nil)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header = redirectHeaders()
		resp, err := hc.Do(req)
		if err != nil {
			t.Fatalf("HTTPClient().Do(%s) error = %v", rawURL, err)
		}
		if err := resp.Body.Close(); err != nil {
			t.Fatalf("close body: %v", err)
		}
	default:
		t.Fatalf("unknown client %q", via)
	}
}

func TestRedirect_CrossHostStripsHeaders(t *testing.T) {
	t.Parallel()
	for _, via := range []string{"Do", "HTTPClient"} {
		t.Run(via, func(t *testing.T) {
			t.Parallel()
			// Both servers listen on 127.0.0.1; their ports differ, so their
			// hosts (host:port) differ.
			second := &pathRecorder{}
			secondSrv := httptest.NewServer(second)
			t.Cleanup(secondSrv.Close)
			first := &pathRecorder{redirects: map[string]string{"/start": secondSrv.URL + "/final"}}
			firstSrv := httptest.NewServer(first)
			t.Cleanup(firstSrv.Close)

			fetch(t, via, firstSrv.URL+"/start")

			if orig, _ := first.header("/start"); orig.Get("Authorization") != "Bearer tok-redirect" {
				t.Errorf("first request Authorization = %q, want the adapter's header", orig.Get("Authorization"))
			}
			got, ok := second.header("/final")
			if !ok {
				t.Fatal("second server not reached")
			}
			for _, k := range []string{"Authorization", "X-Api-Key", "Cookie", "If-None-Match", "Referer"} {
				if v := got.Get(k); v != "" {
					t.Errorf("redirected request %s = %q, want it removed", k, v)
				}
			}
			if v := got.Get("Accept"); v != "application/json" {
				t.Errorf("redirected request Accept = %q, want application/json", v)
			}
			ua := got.Get("User-Agent")
			if ua == "" || (via == "Do" && ua != "upstreamwatch") {
				t.Errorf("redirected request User-Agent = %q, want it kept", ua)
			}
		})
	}
}

func TestRedirect_SameHostKeepsHeaders(t *testing.T) {
	t.Parallel()
	for _, via := range []string{"Do", "HTTPClient"} {
		t.Run(via, func(t *testing.T) {
			t.Parallel()
			rec := &pathRecorder{redirects: map[string]string{}}
			srv := httptest.NewServer(rec)
			t.Cleanup(srv.Close)
			rec.mu.Lock()
			rec.redirects["/start"] = srv.URL + "/final"
			rec.mu.Unlock()

			fetch(t, via, srv.URL+"/start")

			got, ok := rec.header("/final")
			if !ok {
				t.Fatal("redirect target not reached")
			}
			want := redirectHeaders()
			for k := range want {
				if g, w := got.Get(k), want.Get(k); g != w {
					t.Errorf("same-host redirect %s = %q, want %q", k, g, w)
				}
			}
		})
	}
}

func TestRedirect_StrippedStaysStripped(t *testing.T) {
	t.Parallel()
	for _, via := range []string{"Do", "HTTPClient"} {
		t.Run(via, func(t *testing.T) {
			t.Parallel()
			a := &pathRecorder{redirects: map[string]string{}}
			aSrv := httptest.NewServer(a)
			t.Cleanup(aSrv.Close)
			b := &pathRecorder{redirects: map[string]string{"/bounce": aSrv.URL + "/final"}}
			bSrv := httptest.NewServer(b)
			t.Cleanup(bSrv.Close)
			a.mu.Lock()
			a.redirects["/start"] = bSrv.URL + "/bounce"
			a.mu.Unlock()

			fetch(t, via, aSrv.URL+"/start")

			got, ok := a.header("/final")
			if !ok {
				t.Fatal("final hop back to the first host not reached")
			}
			for _, k := range []string{"X-Api-Key", "Authorization", "Cookie", "If-None-Match"} {
				if v := got.Get(k); v != "" {
					t.Errorf("A -> B -> A final request %s = %q, want it still removed", k, v)
				}
			}
			if v := got.Get("Accept"); v != "application/json" {
				t.Errorf("A -> B -> A final request Accept = %q, want application/json", v)
			}
		})
	}
}

func TestRedirect_JarCookiesStripped(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		chain      string // "AB" ends on B, "ABA" goes back to A, "AA" stays on A
		wantCookie bool   // the final request carries the jar's cookie
	}{
		{name: "cross port", chain: "AB", wantCookie: false},
		{name: "A -> B -> A", chain: "ABA", wantCookie: false},
		{name: "same host", chain: "AA", wantCookie: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// A and B share the hostname 127.0.0.1, so a cookie jar, which
			// ignores ports, offers A's cookie to B as well.
			a := &pathRecorder{redirects: map[string]string{}}
			aSrv := httptest.NewServer(a)
			t.Cleanup(aSrv.Close)
			b := &pathRecorder{redirects: map[string]string{"/bounce": aSrv.URL + "/final"}}
			bSrv := httptest.NewServer(b)
			t.Cleanup(bSrv.Close)
			final, finalPath := b, "/final"
			a.mu.Lock()
			switch tt.chain {
			case "AB":
				a.redirects["/start"] = bSrv.URL + "/final"
			case "ABA":
				a.redirects["/start"] = bSrv.URL + "/bounce"
				final = a
			case "AA":
				a.redirects["/start"] = aSrv.URL + "/final"
				final = a
			}
			a.mu.Unlock()

			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatalf("cookiejar.New() error = %v", err)
			}
			aURL, err := url.Parse(aSrv.URL)
			if err != nil {
				t.Fatalf("parse server URL: %v", err)
			}
			jar.SetCookies(aURL, []*http.Cookie{{Name: "session", Value: "jar-secret"}})
			hc := New(nil, Options{AllowPrivateHosts: []string{"127.0.0.1"}}, nil).HTTPClient()
			hc.Jar = jar

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, aSrv.URL+"/start", nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			resp, err := hc.Do(req)
			if err != nil {
				t.Fatalf("HTTPClient().Do() error = %v", err)
			}
			if err := resp.Body.Close(); err != nil {
				t.Fatalf("close body: %v", err)
			}

			if first, _ := a.header("/start"); first.Get("Cookie") != "session=jar-secret" {
				t.Fatalf("first request Cookie = %q, want the jar's cookie", first.Get("Cookie"))
			}
			got, ok := final.header(finalPath)
			if !ok {
				t.Fatal("final hop not reached")
			}
			if c := got.Get("Cookie"); (c != "") != tt.wantCookie {
				t.Errorf("chain %s final request Cookie = %q, want cookie sent %v", tt.chain, c, tt.wantCookie)
			}
		})
	}
}

func TestDo_RedirectEndingIn304(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		crossHost bool
	}{
		{name: "cross host 304 is a status error", crossHost: true},
		{name: "same host 304 is not modified", crossHost: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := &pathRecorder{redirects: map[string]string{}, statuses: map[string]int{"/final": http.StatusNotModified}}
			aSrv := httptest.NewServer(a)
			t.Cleanup(aSrv.Close)
			b := &pathRecorder{statuses: map[string]int{"/final": http.StatusNotModified}}
			bSrv := httptest.NewServer(b)
			t.Cleanup(bSrv.Close)
			final := a
			a.mu.Lock()
			if tt.crossHost {
				a.redirects["/start"] = bSrv.URL + "/final"
				final = b
			} else {
				a.redirects["/start"] = aSrv.URL + "/final"
			}
			a.mu.Unlock()
			c := newTestClient(t, Options{})

			got, err := c.Do(t.Context(), Request{URL: aSrv.URL + "/start", Validators: ValidatorsCaller, ETag: `"e1"`})

			sent, ok := final.header("/final")
			if !ok {
				t.Fatal("final hop not reached")
			}
			if tt.crossHost {
				if v := sent.Get("If-None-Match"); v != "" {
					t.Errorf("cross-host hop If-None-Match = %q, want it removed", v)
				}
				se, ok := errors.AsType[*StatusError](err)
				if !ok || se.Status != http.StatusNotModified {
					t.Errorf("Do() = %+v, error %v, want *StatusError 304", got, err)
				}
				return
			}
			if v := sent.Get("If-None-Match"); v != `"e1"` {
				t.Errorf("same-host hop If-None-Match = %q, want \"e1\"", v)
			}
			if err != nil || !got.NotModified {
				t.Errorf("Do() = %+v, error %v, want NotModified and no error", got, err)
			}
		})
	}
}

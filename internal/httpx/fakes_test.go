package httpx

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

var t0 = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// fakeClock is a clock that moves only when the client sleeps on it.
type fakeClock struct {
	mu     sync.Mutex
	t      time.Time
	sleeps []time.Duration
}

func newFakeClock() *fakeClock { return &fakeClock{t: t0} }

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = f.t.Add(d)
	f.sleeps = append(f.sleeps, d)
	return nil
}

func (f *fakeClock) Sleeps() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.sleeps...)
}

// fakeCache is a CacheStore held in memory that records calls and can fail.
type fakeCache struct {
	mu      sync.Mutex
	entries map[string]model.HTTPValidator
	getErr  error
	saveErr error
	gets    int
	saves   int
}

func newFakeCache() *fakeCache {
	return &fakeCache{entries: map[string]model.HTTPValidator{}}
}

func (f *fakeCache) GetHTTPValidator(_ context.Context, url string) (model.HTTPValidator, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	if f.getErr != nil {
		return model.HTTPValidator{}, f.getErr
	}
	v, ok := f.entries[url]
	if !ok {
		return model.HTTPValidator{}, model.ErrNotFound
	}
	return v, nil
}

func (f *fakeCache) SaveHTTPValidator(_ context.Context, v model.HTTPValidator) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves++
	if f.saveErr != nil {
		return f.saveErr
	}
	f.entries[v.URL] = v
	return nil
}

func (f *fakeCache) calls() (gets, saves int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gets, f.saves
}

func (f *fakeCache) entry(url string) (model.HTTPValidator, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.entries[url]
	return v, ok
}

// syncBuffer is a bytes.Buffer safe for the logger and the test to share.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// testClient is a Client with a fake clock, zero jitter and a captured debug
// log, reaching httptest servers through AllowPrivateHosts.
type testClient struct {
	*Client
	clock *fakeClock
	cache *fakeCache
	log   *syncBuffer
}

func newTestClient(t *testing.T, opts Options) *testClient {
	t.Helper()
	if opts.AllowPrivateHosts == nil {
		opts.AllowPrivateHosts = []string{"127.0.0.1"}
	}
	clock := newFakeClock()
	cache := newFakeCache()
	log := &syncBuffer{}
	c := New(cache, opts, slog.New(slog.NewTextHandler(log, &slog.HandlerOptions{Level: slog.LevelDebug})))
	c.now = clock.Now
	c.sleep = clock.Sleep
	c.jitter = func(time.Duration) time.Duration { return 0 }
	return &testClient{Client: c, clock: clock, cache: cache, log: log}
}

// closedPort returns a loopback address nothing listens on.
func closedPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return addr
}

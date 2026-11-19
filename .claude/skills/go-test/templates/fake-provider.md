# Template: fake LLM provider

Use this when testing `triage.Service`, or anything else that takes a `Provider`. The interface is in `docs/interfaces.md`; the request and response types are in `internal/model`. What the triage stage must guarantee (cache key parts, caps, evidence check, injection handling) is in `llm-triage`. This template only gives the fake and the shape of the tests.

## The fake

```go
// fakeProvider returns canned verdicts keyed by issue ID and records every request.
type fakeProvider struct {
	mu       sync.Mutex
	verdicts map[int64]model.JudgeResponse
	usage    model.Usage // returned with every successful call
	err      error       // if set, every call fails with it
	failOn   map[int64]error
	calls    []model.JudgeRequest
}

func (f *fakeProvider) Judge(ctx context.Context, req model.JudgeRequest) (model.JudgeResponse, model.Usage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	if err := ctx.Err(); err != nil {
		return model.JudgeResponse{}, model.Usage{}, err
	}
	if f.err != nil {
		return model.JudgeResponse{}, model.Usage{}, f.err
	}
	if err := f.failOn[req.IssueID]; err != nil {
		return model.JudgeResponse{}, model.Usage{}, err
	}
	v, ok := f.verdicts[req.IssueID]
	if !ok {
		return model.JudgeResponse{}, model.Usage{}, fmt.Errorf("fake provider: no verdict for issue %d", req.IssueID)
	}
	return v, f.usage, nil
}

func (f *fakeProvider) Name() string  { return "fake" }
func (f *fakeProvider) Model() string { return "fake-model-1" }

func (f *fakeProvider) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}
```

`Judge` keeps the `(JudgeResponse, Usage, error)` signature. `JudgeResponse` carries two extra fields, `RequestPayload` and `RawResponse` (the exact bytes sent and received), which triage stores as `request_payload` and `raw_response`. Fill both in the canned responses (for example `RequestPayload: []byte(`{"model":"fake-model-1"}`)`) so a test can assert they are persisted unchanged. The key is not the fake's concern: real adapters take a `KeySource`, but the fake needs none.

`Name()` and `Model()` are part of the cache key. A test that changes `Model()` between runs shows that a model change re-judges only when asked to.

## Tests to write with it

Pair the fake provider with fake `CandidateStore` and `VerdictStore`, or with a real temp-file store when the test is about what was persisted.

```go
func TestTriage_Run(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		cached        []model.Verdict // already in the verdict store
		candidates    []model.Candidate
		maxCalls      int
		wantCalls     int
		wantQueued    int // candidates left waiting for budget
	}{
		{name: "cached verdict makes no call", cached: verdictsFor(1), candidates: candidates(1), maxCalls: 50, wantCalls: 0},
		{name: "new candidate is judged once", candidates: candidates(1), maxCalls: 50, wantCalls: 1},
		{name: "per-run cap leaves the rest queued", candidates: candidates(5), maxCalls: 2, wantCalls: 2, wantQueued: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &fakeProvider{verdicts: affectsFor(tt.candidates), usage: model.Usage{InputTokens: 1000, OutputTokens: 200}}
			svc := newTestTriage(t, p, tt.cached, tt.candidates, tt.maxCalls)

			stats, err := svc.Run(t.Context(), model.Scope{})
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if got := p.callCount(); got != tt.wantCalls {
				t.Errorf("provider calls = %d, want %d", got, tt.wantCalls)
			}
			if got := stats.Queued; got != tt.wantQueued {
				t.Errorf("queued = %d, want %d", got, tt.wantQueued)
			}
		})
	}
}
```

Also cover:

- **Running twice bills nothing the second time:** the call count after two runs equals the count after one.
- **A provider error leaves the candidate queued** and the run reports it. It is not written as a verdict.
- **The daily dollar cap,** with `usage` set so a known number of calls crosses it.
- **An `evidence` quote that is not a substring of the issue text** is flagged and its confidence reduced.
- **A cancelled context** stops the run between candidates.
- **The request does not carry more than `snippet_mode` allows.** Assert on `p.calls[0]`; a golden file of the request payload works well here.

---
name: go-test
description: How tests are written and run in upstreamwatch. Covers table-driven tests with t.Run, hand-written fakes with no mocking framework, the race detector, fake HTTP servers for GitHub (ETag, 304, rate-limit headers), OSV and deps.dev, golden files under testdata with an -update flag, fixture git repositories built in a temp dir, a fake LLM Provider, store tests against a real temp-file SQLite database, and the two tagged referee tests (timeline against git pkgs history, advisories against osv-scanner). Also covers what a test writer may edit and how to report results and bugs. Triggers on writing or fixing tests, _test.go files, fakes, fixtures, golden files, testdata, referee or e2e tests, go test -race, flaky tests and coverage. Not for production code (use go-code), store schema and SQL (sqlite-store), or deciding what a provider adapter must do (llm-triage).
---

# Tests in upstreamwatch

## Rules

- **Tests sit beside the code:** `poller.go` and `poller_test.go`. Name a test after the behaviour it checks: `TestPoller_Run_KeepsCursorWhenNotModified`.
- **Table-driven, one `t.Run` per row.** Name the expected value `want` and the actual value `got`. Report failures as `t.Errorf("Score(%v) = %v, want %v", in, got, want)`.
- **Standard library only.** That means `testing`, `httptest`, `testing/fstest`, `testing/synctest`, `reflect.DeepEqual` and `errors.Is`. No testify, no gomock, no go-cmp. Adding a test dependency needs the owner's yes, like any other.
- **Fakes are written by hand,** in the test file or the package's `fakes_test.go`. A fake is a struct that returns canned values, records calls, and can be told to fail. Fake only what crosses an interface. Call real pure functions, real `model` methods and the real standard library.
- **Run with `-race` always.** A data race is a finding, not a flake.
- **Use `t.Parallel()` wherever tests share no state.** Not in tests that call `t.Setenv` or `t.Chdir`, which panic under it. Pass environment to child processes through `exec.Cmd.Env` instead.
- **Use `t.Context()` for contexts and `t.TempDir()` for files.** Use fixed times such as `time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)`. Never assert against `time.Now()`.
- **Never use `time.Sleep` to wait.** For code that waits (rate-limit backoff, the 100 ms request gap), inject the clock or run the test inside `synctest.Test`.
- **Unit tests never touch the network.** Every outside call goes to a fake server or a fake interface. A test that needs the real internet is a referee test and carries the build tag.

## What a test writer may edit

Test writers edit `*_test.go` files, `testdata/` directories and, when the work order lists it, `internal/testutil/`. They do not edit production code, even for a one-line fix.

When a test fails because production code is wrong:

1. Keep the test.
2. Mark it with a comment naming the bug.
3. Report it under Concerns as **BUG**: file and line, the test name, input, got versus want, and a one-sentence diagnosis.

When you cannot tell whether the test or the code is wrong (the doc and the code disagree), report **AMBIGUOUS** under Concerns with both readings and leave that case out of the suite. When a test fails only sometimes, rerun it with `-race -count=3`. If it is still inconsistent, report **FLAKY** with the suspected cause. Never mark it passing.

**Never report a skipped test as passing.** List every skip by name with its reason.

## Patterns

Each pattern has a short example here and a full one in `templates/`. Read the template when you write that kind of test.

### Service with fakes

A service takes its interfaces in its constructor, so the test passes fakes:

```go
type fakeIssueSource struct {
	pages map[string]model.IssuePage // keyed by repo URL
	err   error
	calls []string
}

func (f *fakeIssueSource) ListUpdated(ctx context.Context, r model.UpstreamRepo, since time.Time, etag string) (model.IssuePage, error) {
	f.calls = append(f.calls, r.URL)
	if f.err != nil {
		return model.IssuePage{}, f.err
	}
	return f.pages[r.URL], nil
}
```

Cover success, each sentinel error, a failing dependency, and a cancelled context. A fake for `InTx` just calls the function with the fake store; `sqlite-store` describes the shape.

### Fake HTTP server per outside API

Adapters are tested against a fake served by `httptest.NewTLSServer(handler)`. It speaks https on loopback, because the URL policy allows https only. Point the adapter's base URL at `srv.URL`. The production policy rejects loopback addresses, so build the `httpx` client with the test policy described in `outbound-client`, trusting `srv.Certificate()`.

- **GitHub:** serve an `ETag` and answer a matching `If-None-Match` with 304 and no body. Send `X-RateLimit-Remaining` and `X-RateLimit-Reset` on every response, and `Link` for the next page. Assert that the query string is byte-identical between polls.
- **OSV:** decode the `querybatch` body, check the batch size, and serve records from `testdata/osv/`.
- **deps.dev:** check that the package name arrives percent-encoded in the path, and serve from `testdata/depsdev/`.

Response bodies are trimmed copies of real public responses, committed under `testdata/`. Full examples: `templates/fake-http-servers.md`.

### Golden files

For rendered output (digest text, HTML pages, prompt payloads, CLI tables), compare against `testdata/<TestName>.golden`. Regenerate with `go test ./internal/<pkg> -run <Test> -update`. Pass `-update` only to the package that defines the flag; `go test ./... -update` fails in every package that does not. Read the diff of the golden file before reporting, because a regenerated golden file asserts nothing until a person has checked it. Helper: `templates/golden-files.md`.

### Fixture git repositories

Timeline and `gitsrc` tests run against a repository built in `t.TempDir()` by `internal/testutil/gitrepo` (built in M1; its doc comments are the API reference, and `templates/fixture-git-repo.md` shows the original design, whose names differ: `gitrepo.New(t, branch)` and `Repo.Commit(msg, at)` are the real ones, and `gitsrc.Options` takes `ReposDir`, `HomeDir` and `AllowFileURLs: true` for fixtures). The helper:

- fixes author, committer and dates per commit;
- runs `git` with an explicit environment, so no user or system git config, hooks or signing leak in;
- can create merge commits, to exercise first-parent walks;
- can make a bare clone, because `gitsrc` reads bare clones only.

With those fixed, commit SHAs come out the same on every machine and can appear in golden files.

```go
r := gitrepo.New(t, "main")
r.Write("go.mod", "module example.com/app\n\ngo 1.27\n\nrequire github.com/example/lib v1.2.0\n")
base := r.Commit("add lib", day(1))
```

Full helper: `templates/fixture-git-repo.md`.

### Fake LLM provider

The triage service is tested with a fake `Provider` that returns canned `JudgeResponse` values keyed by issue, records every request, can fail, and reports token usage. Use it to show that:

- a cached verdict makes no call;
- a second run bills nothing;
- the per-run and per-day caps stop the job and leave candidates queued;
- an evidence quote that is not in the issue is flagged.

Full fake: `templates/fake-provider.md`. What the stage must guarantee is in `llm-triage`.

### Store tests on a real database

`internal/store` is tested against a real SQLite file in `t.TempDir()`, opened and migrated exactly as `cmd/upw` does it, one database per test. Never mock the store's own SQL, and never use `:memory:`: each pooled connection would get its own empty database, and WAL and locking would go untested. Services never touch this; they use fakes. Full helper and the checks every store method needs: `templates/store-tests.md`.

### Referee tests

Two tests compare our output with an independent tool:

- **Timeline:** our first-parent event stream against `git pkgs history --format json` on public fixture repos.
- **Advisories:** advisory IDs at HEAD against `osv-scanner` on the same lockfiles.

They need the network and the referee binaries. Their files start with `//go:build referee`, so `make check` never compiles them and stays offline. They run through the separate target (`make e2e` in `mvp-plan/PLAN.md` section 8): `go test -race -tags referee -run Referee -count=1 ./...`. A missing referee binary fails the test with a message naming it. A difference is a bug in one of the two tools and is investigated, never filtered out. Full example: `templates/referee-tests.md`.

## Running and reporting

- During work: `go test -race -count=1 ./internal/<pkg>/...`
- Before reporting: `make check` (gofmt, vet, build, `go test -race ./...`, layering lint).
- Coverage, to find gaps: `go test -race -coverprofile=cover.out ./internal/<pkg>` and then `go tool cover -func=cover.out`. Aim for 80% on the packages you changed. Report the number; do not write assertion-free tests to raise it.

Write the report in the report file named in the work order, in the layout `.claude/agents/test-writer.md` defines. Fill its fields like this:

- **Files changed:** the test files, each marked created, modified or deleted.
- **Commands run:** the verbatim `go test` output, plus coverage per changed package.
- **Decisions:** one line per function under test with its case count, plus any choice the work order left open.
- **Outside scope:** bugs, contradictions, missing interface methods or doc errors noticed outside the owned files and not acted on.
- **Concerns:** BUG, AMBIGUOUS and FLAKY findings, every skipped test by name, and remaining gaps worth a follow-up.
- **Status:** a kept test that fails because it exposed a production bug makes the status DONE_WITH_CONCERNS.

Then reply with the short form that file gives: status, report path, `make check` result.

## Not covered here

- Production code style: `go-code`
- Where code belongs and which interfaces exist: `upw-architecture`
- Schema, migrations and store method conventions: `sqlite-store`
- `httpx` and git invocation rules the adapters must follow: `outbound-client`
- What the triage stage must guarantee: `llm-triage`
- Template and handler behaviour for web golden tests: `web-ui`
- Milestone acceptance runs: `milestone-accept`

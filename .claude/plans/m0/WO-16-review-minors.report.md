Status: NEEDS_CONTEXT
Work order: .claude/plans/m0/WO-16-review-minors.md
Files changed:
- internal/httpx/policy.go (modified)
- internal/httpx/policy_test.go (modified)
- internal/httpx/client_test.go (modified)
- internal/sched/integration_test.go (modified, comment only)
- internal/query/interfaces.go (modified, doc comment only)
- internal/config/load.go: not changed (requirement 3 is on hold, see below)
- internal/config/load_multidoc_test.go: not created (requirement 3 is on hold)

Requirements 1, 2, 4 and 5 are done. Requirement 3 is not started because it contradicts itself (see Question).

Commands run:
- `make check` -> exit 0
  ```
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.628s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.224s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	1.206s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.479s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.236s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.185s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.509s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.430s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.399s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	2.799s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.231s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.211s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.331s
  ==> arch-lint
  go run github.com/fe3dback/go-arch-lint@v1.19.2 check --output-color=false
  module: github.com/Amrit-Code/upstreamwatch
  linters:
     On | Base: component imports # always on
     On | Advanced: vendor imports # switch 'allow.depOnAnyVendor = false' (or delete) to on
    Off | Advanced: method calls and dependency injections # switch 'allow.deepScan = true' (or delete) to on

  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
  ```
- `go test -race -count=3 ./internal/httpx/ ./internal/config/` -> exit 0
  ```
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	2.174s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.211s
  ```
- `git status --porcelain` (before this report was written) -> only owned files
  ```
   M internal/httpx/client_test.go
   M internal/httpx/policy.go
   M internal/httpx/policy_test.go
   M internal/query/interfaces.go
   M internal/sched/integration_test.go
  ```
- I removed `Content-Type` from `keptOnForeignRedirect` for one run, then ran `go test -race -count=1 -run 'TestRedirect_CrossHost307' ./internal/httpx/` -> exit 1. This confirms the new test catches the missing header. I put the fix back afterwards.
  ```
  --- FAIL: TestRedirect_CrossHost307KeepsContentTypeAndBody (0.00s)
      --- FAIL: TestRedirect_CrossHost307KeepsContentTypeAndBody/HTTPClient (0.01s)
          client_test.go:988: redirected request Content-Type = "", want application/json
      --- FAIL: TestRedirect_CrossHost307KeepsContentTypeAndBody/Do (0.01s)
          client_test.go:988: redirected request Content-Type = "", want application/json
  FAIL
  ```
- I ran a throwaway probe test of go.yaml.in/yaml/v3, then deleted it. It decodes the first document and then decodes again into a `yaml.Node`:
  ```
  "a: 1\n---\n":       first=<nil> second=<nil> kind=1 content=1 [!!null| ] third=EOF
  "a: 1\n---\n...\n":  first=<nil> second=<nil> kind=1 content=1 [!!null| ] third=EOF
  "a: 1\n---\n# c\n":  first=<nil> second=<nil> kind=1 content=1 [!!null| ] third=EOF
  "a: 1\n---\n~\n":    first=<nil> second=<nil> kind=1 content=1 [!!null|~ ] third=EOF
  "a: 1\n---\nnull\n": first=<nil> second=<nil> kind=1 content=1 [!!null|null ] third=EOF
  "a: 1\n---\n---\n":  first=<nil> second=<nil> kind=1 content=1 [!!null| ] third=<nil>
  "a: 1\n...\n":       first=<nil> second=EOF
  "a: 1\n---\nb: 2\n": first=<nil> second=<nil>
  ```

Decisions:
- Requirement 1: `Content-Type` is now in the `keptOnForeignRedirect` list, and the doc comment says why. The new test `TestRedirect_CrossHost307KeepsContentTypeAndBody` runs through both `Client.Do` and `HTTPClient()`. It sends a JSON POST that gets a 307 to a second host, and checks that the second host receives the POST with the same body and `Content-Type`, and without `Authorization` or `X-Api-Key`.
- Requirement 2: `fec0::/10` is now in `refusedPrefixes`. A new prefix `v4Compat` (`::/96`) shares the NAT64 branch, so the embedded IPv4 address in the last 4 bytes is judged. `::` and `::1` are still refused earlier as unspecified and loopback. New table cases: `fec0::1` and `feff:ffff:...:ffff` (top of the range) are refused, `::10.0.0.1` and `::127.0.0.1` are refused, and `::8.8.8.8` is allowed.
- Requirement 5: I checked the new wording against `internal/store/findings.go:147`, which has `ORDER BY f.score DESC, f.id ASC`. They match.

Outside scope:
- `docs/external-sources.md` needs a docs-sync:
  - Line 32 lists the headers kept on a foreign redirect and does not include `Content-Type`.
  - Line 33 lists the refused ranges and lacks `fec0::/10` and the IPv4-compatible `::/96` rule.

Concerns: none

Question or blocker:
Requirement 3 contradicts itself when run against the approved YAML library (go.yaml.in/yaml/v3).
- It says: "After decoding the first document, a second decode must return `io.EOF`; anything else is an error".
- It also says: "a file with a trailing `---` and nothing after it ... still load[s]".

yaml.v3 does not return `io.EOF` for a trailing `---`. It returns a second document holding one empty `!!null` scalar, with err == nil (probe output above). A trailing `# comment` after the `---` behaves the same way. Only a file ending in `...` gives `io.EOF` on the second decode. Implemented literally, the strict rule rejects the trailing-`---` file that the required test expects to load.

Which rule should hold?
- (a) Strict, as the mechanism says: any second decode that does not return `io.EOF` is `parse <path>: more than one YAML document`. A trailing `---` is then rejected, and that test case changes to "rejected".
- (b) Tolerate empty documents: keep decoding until `io.EOF`. Any later document whose root is not an empty null node is the error. If (b), say which of these also count as empty and load: an explicit `~` or `null` document (`a: 1\n---\n~\n`), and several empty documents in a row (`a: 1\n---\n---\n`).

Nothing in `internal/config` has been changed yet. Once you answer, I will finish requirement 3 and its tests. All other changes are in place and `make check` passes.

## Fix round 1

Status: DONE
Work order: .claude/plans/m0/WO-16-review-minors.md
Files changed:
- internal/config/load.go (modified)
- internal/config/load_multidoc_test.go (created)
- internal/httpx/policy.go, internal/httpx/policy_test.go, internal/httpx/client_test.go, internal/sched/integration_test.go, internal/query/interfaces.go (modified, unchanged since the first round)

Requirement 3 is now implemented as the ruling on requirement 3 directs: rule (b), tolerate empty documents.

Commands run:
- `make check` -> exit 0
  ```
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.502s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.155s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.320s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	(cached)
  ==> arch-lint
  go run github.com/fe3dback/go-arch-lint@v1.19.2 check --output-color=false
  module: github.com/Amrit-Code/upstreamwatch
  linters:
     On | Base: component imports # always on
     On | Advanced: vendor imports # switch 'allow.depOnAnyVendor = false' (or delete) to on
    Off | Advanced: method calls and dependency injections # switch 'allow.deepScan = true' (or delete) to on

  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
  ```
- `go test -race -count=3 ./internal/httpx/ ./internal/config/` -> exit 0
  ```
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	2.183s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.221s
  ```
- `git status --porcelain` -> only owned files and this report
  ```
   M internal/config/load.go
   M internal/httpx/client_test.go
   M internal/httpx/policy.go
   M internal/httpx/policy_test.go
   M internal/query/interfaces.go
   M internal/sched/integration_test.go
  ?? .claude/plans/m0/WO-16-review-minors.report.md
  ?? internal/config/load_multidoc_test.go
  ```
- I switched off the `onlyEmptyDocuments` check for one run, then ran `go test -count=1 -run TestLoad_SecondDocument ./internal/config/` -> exit 1. All 4 subtests failed with "want an error", which confirms the test catches a missing check. I put the check back before the `make check` run above.

Decisions:
- How it works: after the first document decodes, `onlyEmptyDocuments` keeps decoding into a throwaway `yaml.Node` until `io.EOF`, so the later documents never touch the config. A later document is treated as empty when it has no content, or only a `!!null` scalar. That covers a bare `---`, a `---` with only a comment, `~` and `null`. Any other later document returns `parse <path>: more than one YAML document`. If the first decode returns `io.EOF`, the file is empty and the check is skipped, as before.
- A YAML syntax error in a later document also returns `parse <path>: more than one YAML document`. I did not wrap the yaml error, because the original requirement said "anything else is an error `parse <path>: more than one YAML document`".
- Tests in `internal/config/load_multidoc_test.go`:
  - These load and give the same result as `data_dir: x` alone (6 cases): trailing `---`; trailing `---` plus a comment; `...`; explicit `~`; explicit `null`; two empty documents.
  - These are rejected with the exact error message and the zero Config (4 cases): a second mapping; content after an empty document; a scalar second document; an empty first document followed by one with content.

Outside scope: as in the first round, `docs/external-sources.md` lines 32 and 33 need a docs-sync. If `docs/` describes how the config file is loaded, it may also need to say that a file holding more than one YAML document with content is now rejected.

Concerns: none

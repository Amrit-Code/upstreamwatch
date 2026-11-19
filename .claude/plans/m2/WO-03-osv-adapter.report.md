Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m2/WO-03-osv-adapter.md
Files changed:
- internal/vuln/osv/client.go (modified: Options gains BucketURL and BatchSize with defaults; Client holds bucket and batch size; New and Check unchanged in behaviour)
- internal/vuln/osv/client_test.go (modified: `var _ vuln.AdvisorySource = (*osv.Client)(nil)`, `newClientOpts` helper)
- internal/vuln/osv/query.go (created: QueryBatch, toQuery, completeMinor, refSet, parseTime)
- internal/vuln/osv/record.go (created: Fetch, parseRecord, databaseSeverity, scoreLabel, toPackage, normalizePyPI)
- internal/vuln/osv/cvss.go (created: cvssV3BaseScore, roundUp)
- internal/vuln/osv/modified.go (created: ModifiedSince, parseModified)
- internal/vuln/osv/query_test.go (created)
- internal/vuln/osv/query_internal_test.go (created)
- internal/vuln/osv/record_test.go (created)
- internal/vuln/osv/record_internal_test.go (created)
- internal/vuln/osv/cvss_internal_test.go (created)
- internal/vuln/osv/modified_test.go (created)
- internal/vuln/osv/testdata/GO-2024-2887.json (created: real record, fetched once from api.osv.dev, byte for byte)
- internal/vuln/osv/testdata/GHSA-6v2p-p543-phr9.json (created: real record, database_specific.severity HIGH, CVSS v3 vector)
- internal/vuln/osv/testdata/GHSA-9wx4-h78v-vm56.json (created: real PyPI record, MODERATE, CVSS v3 vector)
- internal/vuln/osv/testdata/TestQueryBatch_Mapping.golden (created with -update; diff read: toolchain 1.26 -> stdlib 1.26.0, Go v1.9.1 -> 1.9.1, PyPI pyyaml 6.0.1, GitHub Actions actions/checkout v4.1.1 unchanged)

Commands run:
- `go test ./internal/vuln/osv -run TestQueryBatch_Mapping -update -count=1` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	0.247s
- `go test -race -count=1 -coverprofile=cover.out ./internal/vuln/osv/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.344s	coverage: 97.2% of statements
- `make check` -> exit 1 (first run)
  ==> gofmt
  gofmt: these files need formatting (run make fmt):
  internal/vuln/osv/client_test.go
  make: *** [check] Error 1
  (fixed with gofmt -w on the owned file)
- `make check` -> exit 0 (final run, after the last edit)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/findings	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/ops	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/report	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/vuln	[no test files]
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
  make check exit 0
  (the run before it, uncached, printed `ok  github.com/Amrit-Code/upstreamwatch/internal/vuln/osv 1.285s` and every other package ok; the only change between the two runs was a doc comment in record.go)
- `go test -race -count=1 ./internal/vuln/osv/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.212s
- `git status --porcelain` -> exit 0 (before this report was written)
   M internal/vuln/osv/client.go
   M internal/vuln/osv/client_test.go
  ?? internal/vuln/osv/cvss.go
  ?? internal/vuln/osv/cvss_internal_test.go
  ?? internal/vuln/osv/modified.go
  ?? internal/vuln/osv/modified_test.go
  ?? internal/vuln/osv/query.go
  ?? internal/vuln/osv/query_internal_test.go
  ?? internal/vuln/osv/query_test.go
  ?? internal/vuln/osv/record.go
  ?? internal/vuln/osv/record_internal_test.go
  ?? internal/vuln/osv/record_test.go
  ?? internal/vuln/osv/testdata/

Tests (all requested ones present): TestQueryBatch_Batches, TestQueryBatch_PageToken, TestQueryBatch_Mapping (golden), TestFetch_GoRecord, TestFetch_GHSARecord, TestFetch_NotFound, TestCVSSv3BaseScore (13 published vector/score pairs incl. 9.8, scope-changed 10.0/9.9/6.1/4.8, plus 10 malformed cases), TestModifiedSince. Sanity extras: TestQueryBatch_PageRoundsCapped (51 requests then error), TestQueryBatch_NothingToQuery, TestQueryBatch_Errors, TestFetch_PyPIRecord, TestFetch_EscapesID, TestFetch_Errors, TestModifiedSince_Cases, TestModifiedSince_EscapesEcosystem (GitHub%20Actions), TestModifiedSince_ServerError, TestToQuery, TestCompleteMinor, TestToPackage, TestToPackage_EcosystemSuffixIgnored, TestPurlTypes_AllKnownToModel, TestParseRecord_Severity, TestParseRecord_AffectedAndTimes, TestParseRecord_MissingModified, TestNormalizePyPI, TestScoreLabel, TestRoundUp. No skipped tests.

Decisions:
- A package URL whose name does not percent-decode is skipped like a pair with no ecosystem (not an error).
- toolchain version: strip a leading `go`, then the Go rule's one leading `v`, then complete `X.Y` to `X.Y.0` only when both parts are all digits (`1.26rc1` is left alone); a version that becomes empty (`go`) is skipped.
- Page-token follow-ups are collected across all initial batches and sent together, BatchSize per request; round 0 is the initial pass, error when tokens remain after follow-up round 50.
- QueryBatch returns refs in first-seen order, one per id with the greatest modified.
- A querybatch result count different from the query count, a vuln with no id, and an unparsable modified are errors.
- Mapped model.Package.Name is the name part of the produced package URL, decoded: `toolchain`, the Go module path, the PEP 503-normalised PyPI name, the Actions `owner/repo`, the npm name with its `@`, and the OSV name for the other ecosystems.
- An affected[] entry with an empty package name is dropped, like one in an unknown ecosystem.
- A record with no id, or a missing or malformed `modified`, is an error; empty `published`/`withdrawn` mean zero time; JSON `null` ecosystem_specific/database_specific become nil.
- CVSS: the first CVSS_V3 entry is used whether or not database_specific.severity is present (TestFetch_GHSARecord requires CVSS stored alongside HIGH). `CVSS:3.0/` and `CVSS:3.1/` prefixes are accepted, both scored with the 3.1 formula; metrics other than the eight base metrics are ignored; a valid vector scoring 0.0 is stored with score 0 and label unknown. A non-string or unrecognised database_specific.severity falls back to the score.
- Error format is `osv: <querybatch | fetch <id> | modified ids <ecosystem>>: <host>: <cause>`; when the cause is an httpx error the host appears twice (httpx names it too), matching the existing Check. Response-derived values (times, ids, lines) are never quoted in errors, so no body text reaches an error.
- modified_id.csv: CRLF line ends and a missing trailing newline are accepted; an empty line before the end is malformed; lines after the stop line are not parsed.
- One Debug log line, `osv querybatch finished`, with pairs, queries, requests and advisories counts.
- The reverse lookup OSV ecosystem -> package URL type is a local `purlTypes` list checked against model.OSVEcosystem (see Outside scope).

Outside scope:
- internal/model has no reverse of OSVEcosystem (OSV ecosystem -> package URL type). The adapter keeps an unexported list of the 11 types and asks model.OSVEcosystem for each, so model stays the authority on names, but a type added to model's table later will not map back until it is added to the list. A model function such as `func PURLTypeForOSVEcosystem(eco string) (string, bool)` would remove the duplication; lead's call.
- Maven (latent): Requirement 5 maps OSV `group:artifact` to `pkg:maven/group:artifact`, and Requirement 1 sends the purl path `group/artifact` as the OSV name; standard purls are `pkg:maven/group/artifact` and OSV names use a colon, so Maven would neither query nor match. No Maven manifest is parsed today, so nothing is affected yet. Implemented as written.
- mvp-plan/PLAN.md 5.4 says querybatch by `purl`; the work order specifies `name`+`ecosystem`. Followed the work order (PLAN is not docs/, and docs/external-sources.md does not say).
- The outbound-client skill lists OSV `modified_id.csv` as a ValidatorsCached feed; the work order says ValidatorsNone. Followed the work order.
- Docs rows for docs-sync: docs/interfaces.md implementations table (osv.Client now implements vuln.AdvisorySource: QueryBatch, Fetch, ModifiedSince); docs/external-sources.md OSV bucket row could name the host storage.googleapis.com/osv-vulnerabilities (new outbound host for the egress list) and the 256 MiB cap.

Concerns:
- The Maven mapping above would be wrong if a Maven parser is added; the reviewer may want a ruling now.
- The `purlTypes` list duplicates the set of types in model's table (not the OSV names); TestPurlTypes_AllKnownToModel catches a type removed from model but not one added.

## Fix round 1

Status: DONE
Work order: .claude/plans/m2/WO-03-osv-adapter.md (plus the lead's Maven ruling, which overrides requirements 1 and 5 for Maven)
Files changed:
- internal/vuln/osv/query.go (modified: toQuery sends a Maven package URL pkg:maven/<group>/<artifact> as OSV name "<group>:<artifact>", splitting the decoded path at its last "/"; no group or no artifact is skipped)
- internal/vuln/osv/record.go (modified: toPackage maps OSV Maven "<group>:<artifact>" to pkg:maven/<group>/<artifact>; a name without ":", with an empty group or artifact, or with a second ":" is dropped)
- internal/vuln/osv/query_internal_test.go (modified: 4 Maven rows in TestToQuery: two mapped, including a percent-encoded artifact, two skipped)
- internal/vuln/osv/record_internal_test.go (modified: 5 Maven rows in TestToPackage: one mapped, four dropped)
Commands run:
- `go test -race -count=1 ./internal/vuln/osv/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.268s
- `make check` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/vuln	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.164s
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
  make check exit 0
  (every other package printed ok or [no test files])
- `git status --porcelain` -> exit 0 (before this section was appended)
   M internal/vuln/osv/query.go
   M internal/vuln/osv/query_internal_test.go
   M internal/vuln/osv/record.go
   M internal/vuln/osv/record_internal_test.go
Decisions:
- Maven Name follows the convention used for every other ecosystem, the decoded name part of the produced package URL: Name = "<group>/<artifact>" (for example "org.apache.tomcat/tomcat-coyote"), not "<artifact>" alone.
- An OSV Maven name with a second ":" (such as "g:a:jar") is dropped, since it is not "<group>:<artifact>".
- On the query side the decoded path is split at its last "/", so the group keeps its dots and the artifact is the final segment.
Outside scope: none new. The Maven point from round 1 is resolved; the rest stand as you ruled.

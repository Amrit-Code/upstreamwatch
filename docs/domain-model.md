# Domain model

This document lists the objects the code passes around and how they relate. They live in `internal/model` and carry the rules that are true regardless of where the data came from. Without that shared package, feature packages would import each other for types and Go would reject the cycle.

The methods shown on the objects are the pure decisions from the layers in [architecture.md](architecture.md). The tables that store these objects are in [database.md](database.md).

```mermaid
classDiagram
  direction LR
  class TargetRepo { slug url defaultBranch lastIndexedSHA }
  class Commit { sha position committedAt }
  class Manifest { path ecosystem kind }
  class Package { purl ecosystem name }
  class DepEvent { change version prevVersion direct scope }
  class DepInterval { version from to direct Contains(date) IsOpen() }
  class SymbolUse { symbol file line count }
  class Advisory { id aliases severity kev epss Affects(version) FixedVersion(pinned) }
  class AdvisoryMatch { exposedFrom exposedTo fixedVersion confidence }
  class UpstreamRepo { host owner name issueSource watchIssues cursor }
  class UpstreamLink { relation provenance confidence }
  class Issue { number title body labels state bodyHash }
  class Comment { author body createdAt }
  class Candidate { score features passed }
  class Verdict { verdict confidence justification evidence fixedIn cost CacheKey() }
  class Finding { kind fingerprint score Rank() Transition(to) ShouldResolve() }
  class FindingState { state snoozeUntil note }
  class EvalLabel { label note }
  class Digest { new changed waiting lastRuns }
  class Run { job startedAt status stats }

  TargetRepo "1" --> "many" Commit
  TargetRepo "1" --> "many" Manifest
  Commit "1" --> "many" DepEvent
  DepEvent --> Package
  DepEvent --> Manifest
  TargetRepo "1" --> "many" DepInterval : built from events
  DepInterval --> Package
  TargetRepo "1" --> "many" SymbolUse
  SymbolUse --> Package
  Advisory "1" --> "many" AdvisoryMatch
  AdvisoryMatch --> DepInterval
  Package "many" --> "many" UpstreamRepo : UpstreamLink
  UpstreamRepo "1" --> "many" Issue
  Issue "1" --> "many" Comment
  Candidate --> Issue
  Candidate --> DepInterval
  Verdict --> Candidate : judges
  Finding --> AdvisoryMatch : kind advisory
  Finding --> Verdict : kind issue
  Finding --> Run : kind job_failure
  Finding "1" --> "1" FindingState
  EvalLabel --> Issue
  Digest "1" --> "many" Finding
```

The spine is `TargetRepo` to `DepInterval`. Both halves of the product hang off an interval: an advisory matches it, and an upstream issue becomes a candidate against it.

## Where the rules live

- `DepInterval.Contains(date)` and `IsOpen()` answer "what was pinned on date X" and "is it still pinned".
- `Advisory.Affects(version)` and `FixedVersion(pinned)` evaluate version ranges.
- `Verdict.CacheKey()` builds the key that makes sure one issue is judged once per dependency version.
- `Finding.Rank()`, `Transition(to)` and `ShouldResolve()` hold ranking, the state machine and auto-resolve. The web handlers, the CLI and the digest all call these, so they agree.

## Scope and Stats

Two small types in `internal/model` are used by the `Job` interface in [interfaces.md](interfaces.md), not by the class diagram.

| Type | Meaning |
|---|---|
| `Scope` | What a job run covers: all repos, or one repo slug. |
| `Stats` | What a job run reports back; the `runs` table stores it as JSON in `stats`. |

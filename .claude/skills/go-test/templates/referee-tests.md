# Template: referee tests

Use this when writing or changing the two tests that compare upstreamwatch with an independent tool. They are the M1 and M2 acceptance checks in `mvp-plan/PLAN.md` sections 5.4, 9 and 10.

| Referee test | Our side | Referee | Must match |
|---|---|---|---|
| Timeline | dep_events for the first-parent chain of a public fixture repo | `git pkgs history --format json` (the `git-pkgs` binary on `PATH`) | every event on a first-parent commit |
| Advisories | advisory IDs on intervals still open at HEAD | `osv-scanner --format json -L <lockfile>` on the same lockfiles | the exact set of IDs |

## How they stay out of make check

- **Each file starts with a build tag:**
  ```go
  //go:build referee
  ```
  Without `-tags referee` the file does not compile. `make check` and plain `go test ./...` never see it, so they stay offline.
- **They run through the separate target** (`make e2e` in `mvp-plan/PLAN.md` section 8):
  ```sh
  go test -race -tags referee -run Referee -count=1 ./...
  ```
  `-count=1` stops a cached pass from hiding a changed upstream.
- **Test names start with `TestReferee`.**

## Rules

- **A missing referee binary fails the test with a message naming it.** It does not skip. Someone who runs the referee target wants an answer, and a skip would read as a pass.
- **Fixture repositories are public and pinned to a commit,** so a new upstream commit cannot change the result. The list lives in `eval/fixtures/`.
- **Compare normalised tuples, not raw output.** Timeline: (commit SHA, manifest path, package, change, version). Advisories: the set of advisory IDs. Normalise both sides with the same function, and keep that function in the test file so a reader sees exactly what is compared.
- **Restrict the timeline referee to first-parent commits only.** `git rev-list --first-parent <sha>` gives the set. git-pkgs walks the full graph, while we index "what was on main", so events on side-branch commits are expected differences. Nothing else is.
- **A remaining difference is a bug in one of the two tools.** Print both sides' tuples, investigate, and report. Never add a filter to make the sets agree.
- **Check the output fields against the installed tool versions** (`git pkgs --version`, `osv-scanner --version`) before parsing. PLAN.md records git-pkgs v0.22.0 and osv-scanner v2.7.1.

## Shape

```go
//go:build referee

package timeline_test

func TestRefereeTimelineMatchesGitPkgs(t *testing.T) {
	if _, err := exec.LookPath("git-pkgs"); err != nil {
		t.Fatalf("referee binary git-pkgs not found on PATH: %v", err)
	}
	for _, fx := range loadFixtures(t, "../../eval/fixtures") { // public repos pinned to a commit
		t.Run(fx.Slug, func(t *testing.T) {
			clone := cloneAt(t, fx.URL, fx.Commit) // network: allowed in referee tests only

			ours := normalise(runOurTimeline(t, clone, fx.Commit))
			theirs := normalise(firstParentOnly(t, clone, fx.Commit, runGitPkgsHistory(t, clone)))

			if missing, extra := diffTuples(theirs, ours); len(missing)+len(extra) > 0 {
				t.Errorf("timeline differs from git pkgs history for %s@%s\nmissing from ours: %v\nextra in ours: %v",
					fx.Slug, fx.Commit, missing, extra)
			}
		})
	}
}
```

`runOurTimeline` builds the real `gitsrc`, `extract` and `timeline` objects against a temp-file store, the same wiring `cmd/upw` uses, and reads the events back through the store. The advisory referee has the same shape: our matches at HEAD from the `vuln` job against a temp-file store, versus the set of IDs osv-scanner reports for each lockfile.

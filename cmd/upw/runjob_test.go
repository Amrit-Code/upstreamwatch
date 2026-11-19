package main

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

func TestRun_UnknownJob(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	var stdout, stderr bytes.Buffer
	args := []string{"run", "bogus", "-config", cfg}
	if got, want := run(t.Context(), args, &stdout, &stderr, noEnv), 1; got != want {
		t.Errorf("run(%q) = %d, want %d", args, got, want)
	}
	want := "upw: unknown job \"bogus\"; known jobs: ingest, timeline, vulns, digest, backup\n"
	if got := stderr.String(); !strings.HasSuffix(got, want) {
		t.Errorf("run(%q) stderr = %q, want it to end with %q", args, got, want)
	}
	if got := stdout.String(); got != "" {
		t.Errorf("run(%q) stdout = %q, want empty", args, got)
	}
}

func TestRun_MissingJob(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
	}{
		{name: "no arguments", args: []string{"run"}},
		{name: "flag first", args: []string{"run", "-config", "upw.yaml"}},
		{name: "empty name", args: []string{"run", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if got, want := run(t.Context(), tt.args, &stdout, &stderr, noEnv), 2; got != want {
				t.Errorf("run(%q) = %d, want %d", tt.args, got, want)
			}
			if errOut := stderr.String(); !strings.Contains(errOut, "missing job name") || !strings.Contains(errOut, "usage: upw run <job>") {
				t.Errorf("run(%q) stderr = %q, want the problem and the run usage", tt.args, errOut)
			}
			if got := stdout.String(); got != "" {
				t.Errorf("run(%q) stdout = %q, want empty", tt.args, got)
			}
		})
	}
}

func TestRun_AllWithoutOnce(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	var stdout, stderr bytes.Buffer
	args := []string{"run", "all", "-config", cfg}
	if got, want := run(t.Context(), args, &stdout, &stderr, noEnv), 2; got != want {
		t.Errorf("run(%q) = %d, want %d", args, got, want)
	}
	if errOut := stderr.String(); !strings.Contains(errOut, "-once") || !strings.Contains(errOut, `"upw serve"`) {
		t.Errorf("run(%q) stderr = %q, want it to name -once and \"upw serve\"", args, errOut)
	}
	if got := stdout.String(); got != "" {
		t.Errorf("run(%q) stdout = %q, want empty", args, got)
	}
}

func TestRun_OnceAndRepoMisuse(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "once with one job", args: []string{"run", "somejob", "-once", "-config", cfg}, want: `-once is only used with "run all"`},
		{name: "repo with all", args: []string{"run", "all", "-once", "-repo", "x", "-config", cfg}, want: `-repo cannot be used with "run all"`},
		{name: "extra argument", args: []string{"run", "somejob", "extra"}, want: `unexpected argument "extra"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if got, want := run(t.Context(), tt.args, &stdout, &stderr, noEnv), 2; got != want {
				t.Errorf("run(%q) = %d, want %d", tt.args, got, want)
			}
			if errOut := stderr.String(); !strings.Contains(errOut, tt.want) {
				t.Errorf("run(%q) stderr = %q, want it to contain %q", tt.args, errOut, tt.want)
			}
		})
	}
}

func TestRun_AllOnceNoTargets(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	var stdout, stderr bytes.Buffer
	args := []string{"run", "all", "-once", "-config", cfg}
	if got, want := run(t.Context(), args, &stdout, &stderr, noEnv), 0; got != want {
		t.Errorf("run(%q) = %d, want %d; stderr %q", args, got, want, stderr.String())
	}
	want := "ingest: succeeded cloned=0 failed=0 fetched=0 targets=0\n" +
		"timeline: succeeded commits_walked=0 deps_dropped=0 events_written=0 failed=0 files_excluded=0 files_read=0 files_skipped=0 intervals=0 parse_errors=0 resets=0 skipped=0 targets=0\n" +
		"vulns: succeeded advisories_saved=0 delta_irrelevant=0 delta_refs=0 epss_errors=0 epss_updated=0 fetch_not_found=0 fetched=0 findings_resolved=0 findings_upserted=0 intervals=0 kev_errors=0 kev_updated=0 matches=0 matches_open=0 pairs_new=0 query_refs=0 unversioned=0\n" +
		"digest: succeeded changed=0 failed=0 heartbeat=0 more=0 new=0 no_channels=1 retried=0 sent=0 skipped=0\n" +
		"backup: succeeded deleted=0 exists=0 written=1\n"
	if got := stdout.String(); got != want {
		t.Errorf("run(%q) stdout = %q, want %q", args, got, want)
	}
}

func TestRun_VulnsUsesLocalSources(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	var stdout, stderr bytes.Buffer
	args := []string{"run", "vulns", "-config", cfg}
	if got, want := run(t.Context(), args, &stdout, &stderr, noEnv), 0; got != want {
		t.Fatalf("run(%q) = %d, want %d; stderr %q", args, got, want, stderr.String())
	}
	// A new database is due a signals refresh, so both feeds were fetched,
	// from the local server TestMain set up.
	for _, path := range []string{"/kev.json", "/epss.csv"} {
		if vulnSourceHits.get(path) == 0 {
			t.Errorf("local server got no request for %s; the vulns job fetched it elsewhere", path)
		}
	}
	if !strings.Contains(stdout.String(), "kev_errors=0") || !strings.Contains(stdout.String(), "epss_errors=0") {
		t.Errorf("stdout = %q, want both feeds refreshed without error", stdout.String())
	}
}

func TestRun_VulnsQueriesStoredInterval(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	// One open interval of a Go package, never queried before.
	seedFindings(t, cfg)
	before := vulnSourceHits.get("/v1/querybatch")
	var stdout, stderr bytes.Buffer
	args := []string{"run", "vulns", "-config", cfg}
	if got, want := run(t.Context(), args, &stdout, &stderr, noEnv), 0; got != want {
		t.Fatalf("run(%q) = %d, want %d; stdout %q; stderr %q", args, got, want, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{" intervals=1 ", " pairs_new=1 ", " query_refs=0 "} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout = %q, want it to contain %q", out, want)
		}
	}
	if vulnSourceHits.get("/v1/querybatch") <= before {
		t.Error("local server got no querybatch request; the stored interval was not queried there")
	}
}

func TestRun_FlagsAfterJobName(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	var stdout, stderr bytes.Buffer
	// With either flag unparsed this would be a usage error (2) or a missing
	// default config file; reaching the unknown-job error shows both parsed.
	args := []string{"run", "somejob", "-repo", "x", "-config", cfg}
	if got, want := run(t.Context(), args, &stdout, &stderr, noEnv), 1; got != want {
		t.Errorf("run(%q) = %d, want %d; stderr %q", args, got, want, stderr.String())
	}
	if errOut := stderr.String(); !strings.Contains(errOut, `upw: unknown job "somejob"`) {
		t.Errorf("run(%q) stderr = %q, want the unknown-job message", args, errOut)
	}
}

func TestRun_RebuildFlag(t *testing.T) {
	t.Parallel()

	t.Run("builds the timeline scope", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name string
			args []string
			want model.Scope
		}{
			{name: "with repo", args: []string{"timeline", "-rebuild", "-repo", "x"}, want: model.Scope{RepoSlug: "x", Rebuild: true}},
			{name: "repo first", args: []string{"timeline", "-repo", "x", "-rebuild"}, want: model.Scope{RepoSlug: "x", Rebuild: true}},
			{name: "every repo", args: []string{"timeline", "-rebuild"}, want: model.Scope{Rebuild: true}},
			{name: "without rebuild", args: []string{"timeline", "-repo", "x"}, want: model.Scope{RepoSlug: "x"}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				var stdout, stderr bytes.Buffer
				c := newCommand("run", runSynopsis, &stdout, &stderr)
				req, err := parseRun(c, tt.args)
				if err != nil {
					t.Fatalf("parseRun(%q) error = %v; stderr %q", tt.args, err, stderr.String())
				}
				job := &fakeJob{name: "timeline"}
				if err := runRequested(t.Context(), newFakeRunner(t, job), req, &stdout, &stderr); err != nil {
					t.Fatalf("runRequested(%+v) error = %v", req, err)
				}
				if got, want := job.runScopes(), []model.Scope{tt.want}; !reflect.DeepEqual(got, want) {
					t.Errorf("timeline run with scopes %+v, want %+v", got, want)
				}
				if got, want := stdout.String(), "timeline: succeeded\n"; got != want {
					t.Errorf("stdout = %q, want %q", got, want)
				}
			})
		}
	})

	t.Run("usage errors", func(t *testing.T) {
		t.Parallel()
		cfg := writeConfig(t, "")
		tests := []struct {
			name string
			args []string
		}{
			{name: "other job", args: []string{"run", "ingest", "-rebuild", "-config", cfg}},
			{name: "other job with repo", args: []string{"run", "ingest", "-repo", "x", "-rebuild", "-config", cfg}},
			{name: "all", args: []string{"run", "all", "-once", "-rebuild", "-config", cfg}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				var stdout, stderr bytes.Buffer
				if got, want := run(t.Context(), tt.args, &stdout, &stderr, noEnv), 2; got != want {
					t.Errorf("run(%q) = %d, want %d; stderr %q", tt.args, got, want, stderr.String())
				}
				if errOut := stderr.String(); !strings.Contains(errOut, `-rebuild is only used with "run timeline"`) {
					t.Errorf("run(%q) stderr = %q, want the -rebuild problem", tt.args, errOut)
				}
				if got := stdout.String(); got != "" {
					t.Errorf("run(%q) stdout = %q, want empty", tt.args, got)
				}
			})
		}
	})

	t.Run("accepted end to end", func(t *testing.T) {
		t.Parallel()
		cfg := writeConfig(t, "")
		var stdout, stderr bytes.Buffer
		args := []string{"run", "timeline", "-rebuild", "-config", cfg}
		if got, want := run(t.Context(), args, &stdout, &stderr, noEnv), 0; got != want {
			t.Errorf("run(%q) = %d, want %d; stderr %q", args, got, want, stderr.String())
		}
		if got := stdout.String(); !strings.HasPrefix(got, "timeline: succeeded") {
			t.Errorf("run(%q) stdout = %q, want the timeline stats line", args, got)
		}
	})

	t.Run("documented", func(t *testing.T) {
		t.Parallel()
		if !strings.Contains(usage, "run timeline -rebuild") {
			t.Errorf("upw help does not mention \"run timeline -rebuild\":\n%s", usage)
		}
		if !strings.Contains(runSynopsis, "-rebuild") {
			t.Errorf("run synopsis does not mention -rebuild:\n%s", runSynopsis)
		}
	})
}

func TestRun_ErrorIsTerminalSafe(t *testing.T) {
	t.Parallel()
	const raw = "fetch: remote said \x1b[31mred\x1b[0m"
	failing := func() *fakeJob {
		return &fakeJob{name: "ingest", run: func(context.Context, model.Scope) (model.Stats, error) {
			return nil, errors.New(raw)
		}}
	}

	t.Run("one job", func(t *testing.T) {
		t.Parallel()
		var stdout, stderr bytes.Buffer
		err := runRequested(t.Context(), newFakeRunner(t, failing()), runRequest{job: "ingest"}, &stdout, &stderr)
		if err == nil {
			t.Fatal("runRequested() error = nil, want the job's error")
		}
		if got, want := exitCode(err, &stderr), 1; got != want {
			t.Errorf("exitCode() = %d, want %d", got, want)
		}
		want := "upw: " + strconv.Quote("run job ingest: "+raw) + "\n"
		if got := stderr.String(); got != want {
			t.Errorf("stderr = %q, want %q", got, want)
		}
		if strings.Contains(stderr.String(), "\x1b") {
			t.Errorf("stderr holds a raw escape: %q", stderr.String())
		}
		if got := stdout.String(); got != "" {
			t.Errorf("stdout = %q, want empty", got)
		}
	})

	t.Run("run all", func(t *testing.T) {
		t.Parallel()
		var stdout, stderr bytes.Buffer
		runner := newFakeRunner(t, failing(), &fakeJob{name: "timeline"})
		if err := runAll(t.Context(), runner, &stdout, &stderr); !errors.Is(err, errReported) {
			t.Fatalf("runAll() error = %v, want errReported", err)
		}
		want := "ingest: failed: " + strconv.Quote("run job ingest: "+raw) + "\n" +
			"timeline: succeeded\n"
		if got := stdout.String(); got != want {
			t.Errorf("stdout = %q, want %q", got, want)
		}
		if strings.Contains(stdout.String(), "\x1b") {
			t.Errorf("stdout holds a raw escape: %q", stdout.String())
		}
	})

	t.Run("joined errors stay one per line", func(t *testing.T) {
		t.Parallel()
		var stderr bytes.Buffer
		err := errors.Join(errors.New("data_dir: must be set"), errors.New("http.listen: must be set"))
		if got, want := exitCode(err, &stderr), 1; got != want {
			t.Errorf("exitCode() = %d, want %d", got, want)
		}
		if got, want := stderr.String(), "upw: data_dir: must be set\nhttp.listen: must be set\n"; got != want {
			t.Errorf("stderr = %q, want %q", got, want)
		}
	})
}

func TestTerminalSafeText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain", in: "run job ingest: clone failed", want: "run job ingest: clone failed"},
		{name: "newline and tab kept", in: "fatal: no\n\thint: retry", want: "fatal: no\n\thint: retry"},
		{name: "escape", in: "a\x1b[2Jb", want: `"a\x1b[2Jb"`},
		{name: "carriage return", in: "ok\rfake", want: `"ok\rfake"`},
		{name: "C1 control", in: "a\u009bb", want: `"a\u009bb"`},
		{name: "invalid UTF-8", in: "a\xffb", want: `"a\xffb"`},
		{name: "non-ASCII text", in: "größe überschritten", want: "größe überschritten"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := terminalSafeText(tt.in); got != tt.want {
				t.Errorf("terminalSafeText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
	// The table rule is stricter: a tab or newline would break a column.
	if got, want := terminalSafe("a\tb"), `"a\tb"`; got != want {
		t.Errorf("terminalSafe(%q) = %q, want %q", "a\tb", got, want)
	}
}

func TestSucceededLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		stats model.Stats
		want  string
	}{
		{name: "no stats", stats: nil, want: "ingest: succeeded"},
		{name: "one stat", stats: model.Stats{"fetched": 3}, want: "ingest: succeeded fetched=3"},
		{name: "name order", stats: model.Stats{"zeta": 1, "alpha": 0, "mid": -2}, want: "ingest: succeeded alpha=0 mid=-2 zeta=1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := succeededLine("ingest", tt.stats); got != tt.want {
				t.Errorf("succeededLine(ingest, %v) = %q, want %q", tt.stats, got, tt.want)
			}
		})
	}
}

func TestKnownJobs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		jobs []string
		want string
	}{
		{jobs: nil, want: "none registered yet"},
		{jobs: []string{"ingest"}, want: "ingest"},
		{jobs: []string{"ingest", "timeline"}, want: "ingest, timeline"},
	}
	for _, tt := range tests {
		if got := knownJobs(tt.jobs); got != tt.want {
			t.Errorf("knownJobs(%q) = %q, want %q", tt.jobs, got, tt.want)
		}
	}
}

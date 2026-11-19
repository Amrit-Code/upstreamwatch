package main

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/store"
)

const (
	sha1 = "1111111111111111111111111111111111111111"
	sha2 = "2222222222222222222222222222222222222222"
	sha3 = "3333333333333333333333333333333333333333"
)

var (
	at1 = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	at2 = time.Date(2026, 4, 15, 8, 30, 0, 0, time.UTC)
	at3 = time.Date(2026, 5, 10, 23, 15, 0, 0, time.UTC)

	pkgToml     = model.Package{PURL: "pkg:golang/github.com/BurntSushi/toml", Ecosystem: "golang", Name: "github.com/BurntSushi/toml"}
	pkgGin      = model.Package{PURL: "pkg:golang/github.com/gin-gonic/gin", Ecosystem: "golang", Name: "github.com/gin-gonic/gin"}
	pkgIdna     = model.Package{PURL: "pkg:pypi/idna", Ecosystem: "pypi", Name: "idna"}
	pkgRequests = model.Package{PURL: "pkg:pypi/requests", Ecosystem: "pypi", Name: "requests"}
)

// seedTimeline stores the repo "server" with three commits and five
// intervals: closed and open, a requirement-only one, and every directness
// value. It returns the config path.
func seedTimeline(t *testing.T) string {
	t.Helper()
	cfg := writeConfig(t, "")
	db := openSeedStore(t, cfg)
	repo, err := db.UpsertRepo(t.Context(), "server", "https://github.com/example-org/example-server")
	if err != nil {
		t.Fatalf("UpsertRepo() error = %v", err)
	}
	commit := func(sha string, at time.Time) model.Commit {
		return model.Commit{SHA: sha, CommittedAt: at, AuthorName: "Example Author", AuthorEmail: "author@example.com",
			Subject: "update dependencies", Paths: []string{"go.mod"}}
	}
	u := model.TimelineUpdate{
		RepoID:  repo.ID,
		Commits: []model.Commit{commit(sha1, at1), commit(sha2, at2), commit(sha3, at3)},
		Manifests: map[string]model.ManifestInfo{
			"go.mod":         {Ecosystem: "golang", Kind: model.ManifestKindManifest},
			"pyproject.toml": {Ecosystem: "pypi", Kind: model.ManifestKindManifest},
			"uv.lock":        {Ecosystem: "pypi", Kind: model.ManifestKindLockfile},
		},
		Intervals: []model.DepInterval{
			{ManifestPath: "go.mod", Package: pkgGin, Version: "v1.9.0", Requirement: "v1.9.0", Direct: model.DirectYes,
				FromSHA: sha1, From: at1, ToSHA: sha2, To: at2},
			{ManifestPath: "go.mod", Package: pkgGin, Version: "v1.10.0", Requirement: "v1.10.0", Direct: model.DirectYes,
				FromSHA: sha2, From: at2},
			{ManifestPath: "go.mod", Package: pkgToml, Version: "v1.4.0", Requirement: "v1.4.0", Direct: model.DirectNo,
				FromSHA: sha1, From: at1},
			{ManifestPath: "pyproject.toml", Package: pkgRequests, Requirement: ">=2.31", Direct: model.DirectYes,
				FromSHA: sha1, From: at1},
			{ManifestPath: "uv.lock", Package: pkgIdna, Version: "3.7", Direct: model.DirectUnknown,
				FromSHA: sha3, From: at3},
		},
		IndexedSHA: sha3,
	}
	if err := db.ApplyTimeline(t.Context(), u); err != nil {
		t.Fatalf("ApplyTimeline() error = %v", err)
	}
	closeSeedStore(t, db)
	return cfg
}

// openSeedStore opens and migrates the database a command run with cfg uses.
func openSeedStore(t *testing.T, cfg string) *store.DB {
	t.Helper()
	db, err := store.Open(t.Context(), dbPath(cfg), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return db
}

func closeSeedStore(t *testing.T, db *store.DB) {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

// runTimeline runs "upw timeline" with args and returns the exit code and
// both outputs.
func runTimeline(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(t.Context(), append([]string{"timeline"}, args...), &out, &errOut, noEnv)
	return code, out.String(), errOut.String()
}

func TestTimeline_Text(t *testing.T) {
	t.Parallel()
	cfg := seedTimeline(t)
	code, stdout, stderr := runTimeline(t, "server", "-config", cfg)
	if code != 0 {
		t.Fatalf("timeline server exit = %d, want 0; stderr %q", code, stderr)
	}
	want := `PACKAGE                                VERSION   FROM        TO          DIRECT  MANIFEST
pkg:golang/github.com/BurntSushi/toml  v1.4.0    2026-03-01  now         no      go.mod
pkg:golang/github.com/gin-gonic/gin    v1.9.0    2026-03-01  2026-04-15  yes     go.mod
pkg:golang/github.com/gin-gonic/gin    v1.10.0   2026-04-15  now         yes     go.mod
pkg:pypi/idna                          3.7       2026-05-10  now         ?       uv.lock
pkg:pypi/requests                      (>=2.31)  2026-03-01  now         yes     pyproject.toml
`
	if stdout != want {
		t.Errorf("timeline server stdout =\n%s\nwant\n%s", stdout, want)
	}
}

func TestTimeline_JSON(t *testing.T) {
	t.Parallel()
	cfg := seedTimeline(t)
	code, stdout, stderr := runTimeline(t, "server", "-json", "-config", cfg)
	if code != 0 {
		t.Fatalf("timeline server -json exit = %d, want 0; stderr %q", code, stderr)
	}
	want := `[
  {
    "package": "pkg:golang/github.com/BurntSushi/toml",
    "ecosystem": "golang",
    "name": "github.com/BurntSushi/toml",
    "version": "v1.4.0",
    "requirement": "v1.4.0",
    "direct": false,
    "manifest": "go.mod",
    "from": "2026-03-01T12:00:00Z",
    "to": null,
    "from_sha": "1111111111111111111111111111111111111111",
    "to_sha": null
  },
  {
    "package": "pkg:golang/github.com/gin-gonic/gin",
    "ecosystem": "golang",
    "name": "github.com/gin-gonic/gin",
    "version": "v1.9.0",
    "requirement": "v1.9.0",
    "direct": true,
    "manifest": "go.mod",
    "from": "2026-03-01T12:00:00Z",
    "to": "2026-04-15T08:30:00Z",
    "from_sha": "1111111111111111111111111111111111111111",
    "to_sha": "2222222222222222222222222222222222222222"
  },
  {
    "package": "pkg:golang/github.com/gin-gonic/gin",
    "ecosystem": "golang",
    "name": "github.com/gin-gonic/gin",
    "version": "v1.10.0",
    "requirement": "v1.10.0",
    "direct": true,
    "manifest": "go.mod",
    "from": "2026-04-15T08:30:00Z",
    "to": null,
    "from_sha": "2222222222222222222222222222222222222222",
    "to_sha": null
  },
  {
    "package": "pkg:pypi/idna",
    "ecosystem": "pypi",
    "name": "idna",
    "version": "3.7",
    "requirement": "",
    "direct": null,
    "manifest": "uv.lock",
    "from": "2026-05-10T23:15:00Z",
    "to": null,
    "from_sha": "3333333333333333333333333333333333333333",
    "to_sha": null
  },
  {
    "package": "pkg:pypi/requests",
    "ecosystem": "pypi",
    "name": "requests",
    "version": "",
    "requirement": ">=2.31",
    "direct": true,
    "manifest": "pyproject.toml",
    "from": "2026-03-01T12:00:00Z",
    "to": null,
    "from_sha": "1111111111111111111111111111111111111111",
    "to_sha": null
  }
]
`
	if stdout != want {
		t.Errorf("timeline server -json stdout =\n%s\nwant\n%s", stdout, want)
	}
}

func TestTimeline_Filters(t *testing.T) {
	t.Parallel()
	cfg := seedTimeline(t)
	const header = "PACKAGE                              VERSION  FROM        TO          DIRECT  MANIFEST\n"
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "package",
			args: []string{"server", "pkg:golang/github.com/gin-gonic/gin", "-config", cfg},
			want: header +
				"pkg:golang/github.com/gin-gonic/gin  v1.9.0   2026-03-01  2026-04-15  yes     go.mod\n" +
				"pkg:golang/github.com/gin-gonic/gin  v1.10.0  2026-04-15  now         yes     go.mod\n",
		},
		{
			name: "package and the day before the upgrade",
			args: []string{"server", "pkg:golang/github.com/gin-gonic/gin", "-on", "2026-04-14", "-config", cfg},
			want: header +
				"pkg:golang/github.com/gin-gonic/gin  v1.9.0   2026-03-01  2026-04-15  yes     go.mod\n",
		},
		{
			// -on means the end of the UTC day, after the 08:30 upgrade.
			name: "package and the day of the upgrade",
			args: []string{"server", "pkg:golang/github.com/gin-gonic/gin", "-on", "2026-04-15", "-config", cfg},
			want: "PACKAGE                              VERSION  FROM        TO   DIRECT  MANIFEST\n" +
				"pkg:golang/github.com/gin-gonic/gin  v1.10.0  2026-04-15  now  yes     go.mod\n",
		},
		{
			name: "on before any interval",
			args: []string{"server", "-on", "2026-02-28", "-config", cfg},
			want: "no intervals\n",
		},
		{
			name: "unknown package",
			args: []string{"server", "pkg:golang/example.com/absent", "-config", cfg},
			want: "no intervals\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			code, stdout, stderr := runTimeline(t, tt.args...)
			if code != 0 {
				t.Fatalf("timeline %q exit = %d, want 0; stderr %q", tt.args, code, stderr)
			}
			if stdout != tt.want {
				t.Errorf("timeline %q stdout =\n%s\nwant\n%s", tt.args, stdout, tt.want)
			}
		})
	}
}

func TestTimeline_NoIntervals(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	db := openSeedStore(t, cfg)
	if _, err := db.UpsertRepo(t.Context(), "fresh", "https://github.com/example-org/example-fresh"); err != nil {
		t.Fatalf("UpsertRepo() error = %v", err)
	}
	closeSeedStore(t, db)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "text", args: []string{"fresh", "-config", cfg}, want: "no intervals\n"},
		{name: "json", args: []string{"fresh", "-json", "-config", cfg}, want: "[]\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runTimeline(t, tt.args...)
			if code != 0 {
				t.Fatalf("timeline %q exit = %d, want 0; stderr %q", tt.args, code, stderr)
			}
			if stdout != tt.want {
				t.Errorf("timeline %q stdout = %q, want %q", tt.args, stdout, tt.want)
			}
		})
	}
}

func TestTimeline_UnknownRepo(t *testing.T) {
	t.Parallel()
	cfg := seedTimeline(t)
	for _, args := range [][]string{
		{"nope", "-config", cfg},
		{"nope", "-json", "-config", cfg},
	} {
		code, stdout, stderr := runTimeline(t, args...)
		if code != 1 {
			t.Errorf("timeline %q exit = %d, want 1", args, code)
		}
		if want := "upw: unknown repo \"nope\"\n"; !strings.HasSuffix(stderr, want) {
			t.Errorf("timeline %q stderr = %q, want it to end with %q", args, stderr, want)
		}
		if stdout != "" {
			t.Errorf("timeline %q stdout = %q, want empty", args, stdout)
		}
	}
}

func TestTimeline_BadDate(t *testing.T) {
	t.Parallel()
	cfg := writeConfig(t, "")
	for _, date := range []string{"yesterday", "2026-02-30", "2026-4-1", "2026-04-01T00:00:00Z", ""} {
		args := []string{"server", "-on", date, "-config", cfg}
		code, stdout, stderr := runTimeline(t, args...)
		if code != 2 {
			t.Errorf("timeline %q exit = %d, want 2", args, code)
		}
		if !strings.Contains(stderr, "-on") || !strings.Contains(stderr, "want a date as YYYY-MM-DD") || !strings.Contains(stderr, "usage: upw timeline") {
			t.Errorf("timeline %q stderr = %q, want the -on problem and the usage", args, stderr)
		}
		if stdout != "" {
			t.Errorf("timeline %q stdout = %q, want empty", args, stdout)
		}
	}
	if _, err := os.Stat(dbPath(cfg)); err == nil {
		t.Error("timeline with a bad date created the database, want nothing opened on a usage error")
	}
}

func TestTimeline_UsageErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "no slug", args: nil, want: "missing repo slug"},
		{name: "empty slug", args: []string{""}, want: "missing repo slug"},
		{name: "flag first", args: []string{"-json", "server"}, want: "missing repo slug"},
		{name: "third positional", args: []string{"server", "pkg:pypi/idna", "extra"}, want: `unexpected argument "extra"`},
		{name: "unknown flag", args: []string{"server", "-bogus"}, want: "flag provided but not defined: -bogus"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			code, stdout, stderr := runTimeline(t, tt.args...)
			if code != 2 {
				t.Errorf("timeline %q exit = %d, want 2", tt.args, code)
			}
			if !strings.Contains(stderr, tt.want) || !strings.Contains(stderr, "usage: upw timeline") {
				t.Errorf("timeline %q stderr = %q, want %q and the usage", tt.args, stderr, tt.want)
			}
			if stdout != "" {
				t.Errorf("timeline %q stdout = %q, want empty", tt.args, stdout)
			}
		})
	}
}

func TestTimeline_Help(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"-h"}, {"--help"}, {"server", "-h"}} {
		code, stdout, stderr := runTimeline(t, args...)
		if code != 0 {
			t.Errorf("timeline %q exit = %d, want 0", args, code)
		}
		for _, want := range []string{"usage: upw timeline <slug> [package-url]", "-on date", "-json", "-config path", "-log-level level"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("timeline %q stdout = %q, want it to contain %q", args, stdout, want)
			}
		}
		if stderr != "" {
			t.Errorf("timeline %q stderr = %q, want empty", args, stderr)
		}
	}
}

func TestTimeline_DoesNotRunJobs(t *testing.T) {
	t.Parallel()
	cfg := seedTimeline(t)
	if code, _, stderr := runTimeline(t, "server", "-config", cfg); code != 0 {
		t.Fatalf("timeline server exit = %d, want 0; stderr %q", code, stderr)
	}
	db := openSeedStore(t, cfg)
	defer closeSeedStore(t, db)
	runs, err := db.LatestRuns(t.Context())
	if err != nil {
		t.Fatalf("LatestRuns() error = %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("runs after timeline = %+v, want none", runs)
	}
}

func TestTimeline_TextEscapesControlChars(t *testing.T) {
	t.Parallel()
	evil := model.Package{PURL: "pkg:pypi/a\x1b[2Jb", Ecosystem: "pypi", Name: "a\x1b[2Jb"}
	intervals := []model.DepInterval{
		{ManifestPath: "go.mod", Package: pkgGin, Version: "v1.10.0", Requirement: "v1.10.0", Direct: model.DirectYes,
			FromSHA: sha1, From: at1},
		{ManifestPath: "dir\n/requirements.txt", Package: evil, Requirement: ">=1\t\x7f", Direct: model.DirectYes,
			FromSHA: sha1, From: at1},
		{ManifestPath: "uv.lock", Package: pkgIdna, Version: "3.7\r", Direct: model.DirectUnknown,
			FromSHA: sha3, From: at3},
		{ManifestPath: "go.mod", Package: model.Package{PURL: "pkg:golang/c1\u009b2J", Ecosystem: "golang"}, Version: "v1\xff",
			Direct: model.DirectYes, FromSHA: sha3, From: at3},
	}

	var text bytes.Buffer
	if err := writeTimelineText(&text, intervals); err != nil {
		t.Fatalf("writeTimelineText() error = %v", err)
	}
	want := `PACKAGE                              VERSION        FROM        TO   DIRECT  MANIFEST
pkg:golang/github.com/gin-gonic/gin  v1.10.0        2026-03-01  now  yes     go.mod
"pkg:pypi/a\x1b[2Jb"                 "(>=1\t\x7f)"  2026-03-01  now  yes     "dir\n/requirements.txt"
pkg:pypi/idna                        "3.7\r"        2026-05-10  now  ?       uv.lock
"pkg:golang/c1\u009b2J"              "v1\xff"       2026-05-10  now  yes     go.mod
`
	if got := text.String(); got != want {
		t.Errorf("writeTimelineText() =\n%s\nwant\n%s", got, want)
	}
	if !utf8.ValidString(text.String()) {
		t.Errorf("writeTimelineText() = %q, want valid UTF-8", text.String())
	}
	if strings.ContainsFunc(text.String(), func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) {
		t.Errorf("writeTimelineText() = %q, want no control character but newlines", text.String())
	}

	// JSON output keeps the raw values; the encoder escapes them itself.
	var js bytes.Buffer
	if err := writeTimelineJSON(&js, intervals[1:2]); err != nil {
		t.Fatalf("writeTimelineJSON() error = %v", err)
	}
	for _, wantPart := range []string{
		`"package": "pkg:pypi/a\u001b[2Jb"`,
		`"requirement": ">=1\t` + "\x7f" + `"`,
		`"manifest": "dir\n/requirements.txt"`,
	} {
		if !strings.Contains(js.String(), wantPart) {
			t.Errorf("writeTimelineJSON() = %s, want it to contain %s", js.String(), wantPart)
		}
	}
}

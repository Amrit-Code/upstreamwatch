package gitsrc_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io/fs"
	"log/slog"
	"math/rand/v2"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/gitsrc"
	"github.com/Amrit-Code/upstreamwatch/internal/ingest"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo"
	"github.com/Amrit-Code/upstreamwatch/internal/timeline"
)

var (
	_ ingest.GitSource   = (*gitsrc.CLI)(nil)
	_ timeline.GitSource = (*gitsrc.CLI)(nil)
)

// day returns a fixed time on day n of March 2026.
func day(n int) time.Time { return time.Date(2026, 3, n, 12, 0, 0, 0, time.UTC) }

// testOptions returns Options with fresh repos and home directories that
// allow file:// URLs, plus overrides applied by the caller.
func testOptions(t *testing.T) gitsrc.Options {
	t.Helper()
	root := t.TempDir()
	return gitsrc.Options{
		ReposDir:      filepath.Join(root, "repos"),
		HomeDir:       filepath.Join(root, "home"),
		AllowFileURLs: true,
		LookupHost: func(context.Context, string) ([]netip.Addr, error) {
			t.Error("LookupHost called for a file:// URL")
			return nil, errors.New("unexpected lookup")
		},
	}
}

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestSync_FirstThenIncremental(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "main")
	up.Write("go.mod", "module example.com/app\n")
	first := up.Commit("first", day(1))

	opts := testOptions(t)
	g := gitsrc.New(opts, discard())
	repo := model.TargetRepo{Slug: "app", URL: up.URL(), Branch: "main"}

	got, err := g.Sync(t.Context(), repo)
	if err != nil {
		t.Fatalf("first Sync() error = %v", err)
	}
	wantPath := filepath.Join(opts.ReposDir, "app.git")
	want := model.SyncResult{Branch: "main", Head: first, ClonePath: wantPath, Cloned: true}
	if got != want {
		t.Errorf("first Sync() = %+v, want %+v", got, want)
	}

	if bare := gitrepo.Git(t, wantPath, "rev-parse", "--is-bare-repository"); bare != "true" {
		t.Errorf("rev-parse --is-bare-repository = %q, want true", bare)
	}
	if _, err := os.Stat(filepath.Join(wantPath, "go.mod")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("clone has a working tree file go.mod (stat error %v)", err)
	}
	info, err := os.Stat(wantPath)
	if err != nil {
		t.Fatalf("stat clone: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("clone directory mode = %o, want 700", perm)
	}
	cfg := strings.ToLower(gitrepo.Git(t, wantPath, "config", "--list"))
	for _, bad := range []string{"url", "extraheader", "credential"} {
		if strings.Contains(cfg, bad) {
			t.Errorf("git config --list contains %q:\n%s", bad, cfg)
		}
	}

	up.Write("go.mod", "module example.com/app\n\ngo 1.27\n")
	second := up.Commit("second", day(2))
	got, err = g.Sync(t.Context(), repo)
	if err != nil {
		t.Fatalf("second Sync() error = %v", err)
	}
	want = model.SyncResult{Branch: "main", Head: second, ClonePath: wantPath, Cloned: false}
	if got != want {
		t.Errorf("second Sync() = %+v, want %+v", got, want)
	}
}

func TestSync_DefaultBranch(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "trunk")
	head := up.Commit("first", day(1))

	g := gitsrc.New(testOptions(t), discard())
	got, err := g.Sync(t.Context(), model.TargetRepo{Slug: "trunk-app", URL: up.URL()})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if got.Branch != "trunk" || got.Head != head {
		t.Errorf("Sync() = %+v, want Branch trunk and Head %s", got, head)
	}
}

func TestSync_RejectsURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"http", "http://example.com/org/repo.git", `scheme "http" not allowed, only https`},
		{"ssh", "ssh://git@example.com/org/repo.git", `scheme "ssh" not allowed, only https`},
		{"git", "git://example.com/org/repo.git", `scheme "git" not allowed, only https`},
		{"file not allowed", "file:///tmp/repo.git", `scheme "file" not allowed, only https`},
		{"ext helper", "ext::sh -c x", "remote helper syntax"},
		{"leading dash", "-oProxyCommand=x", `starts with "-"`},
		{"scp style", "host:path", "scp-style address not allowed"},
		{"userinfo", "https://user:fake-token-abcdefgh1234@example.com/org/repo.git", "user information not allowed"},
		{"no host", "https:///org/repo.git", "no host"},
		{"local path", "/srv/repo.git", "local path not allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := testOptions(t)
			opts.AllowFileURLs = false
			opts.LookupHost = func(context.Context, string) ([]netip.Addr, error) {
				t.Errorf("LookupHost called for %q; the URL check must run first", tt.url)
				return nil, errors.New("unexpected lookup")
			}
			g := gitsrc.New(opts, discard())
			_, err := g.Sync(t.Context(), model.TargetRepo{Slug: "bad", URL: tt.url, Branch: "main"})
			if err == nil {
				t.Fatalf("Sync(%q) = nil error, want %q", tt.url, tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Sync(%q) error = %q, want it to contain %q", tt.url, err, tt.want)
			}
			if strings.Contains(err.Error(), "fake-token") {
				t.Errorf("Sync(%q) error echoes the credential: %q", tt.url, err)
			}
			if _, statErr := os.Stat(opts.ReposDir); !errors.Is(statErr, fs.ErrNotExist) {
				t.Errorf("repos directory exists after a refused URL (stat error %v)", statErr)
			}
		})
	}
}

func TestSync_PrivateAddress(t *testing.T) {
	t.Parallel()
	const policy = "resolves to a private or reserved address"
	tests := []struct {
		name  string
		addrs []string
	}{
		{"loopback", []string{"127.0.0.1"}},
		{"rfc1918", []string{"10.0.0.5"}},
		{"one of several", []string{"8.8.8.8", "100.64.1.2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := testOptions(t)
			opts.LookupHost = func(_ context.Context, host string) ([]netip.Addr, error) {
				if host != "internal.example" {
					t.Errorf("LookupHost(%q), want internal.example", host)
				}
				var out []netip.Addr
				for _, a := range tt.addrs {
					out = append(out, netip.MustParseAddr(a))
				}
				return out, nil
			}
			g := gitsrc.New(opts, discard())
			_, err := g.Sync(t.Context(), model.TargetRepo{Slug: "internal", URL: "https://internal.example/x", Branch: "main"})
			if err == nil || !strings.Contains(err.Error(), policy) || !strings.Contains(err.Error(), "internal.example") {
				t.Errorf("Sync() error = %v, want it to name internal.example and contain %q", err, policy)
			}
			if _, statErr := os.Stat(opts.ReposDir); !errors.Is(statErr, fs.ErrNotExist) {
				t.Errorf("repos directory exists after a refused host (stat error %v)", statErr)
			}
		})
	}

	t.Run("no address", func(t *testing.T) {
		t.Parallel()
		opts := testOptions(t)
		opts.LookupHost = func(context.Context, string) ([]netip.Addr, error) { return nil, nil }
		_, err := gitsrc.New(opts, discard()).Sync(t.Context(),
			model.TargetRepo{Slug: "internal", URL: "https://internal.example/x", Branch: "main"})
		if err == nil || !strings.Contains(err.Error(), `host "internal.example" resolves to no address`) {
			t.Errorf("Sync() error = %v, want the no-address refusal", err)
		}
	})

	t.Run("allow private host", func(t *testing.T) {
		t.Parallel()
		opts := testOptions(t)
		opts.FetchTimeout = 20 * time.Second
		opts.LookupHost = func(context.Context, string) ([]netip.Addr, error) {
			t.Error("LookupHost called for a target with AllowPrivateHost")
			return nil, errors.New("unexpected lookup")
		}
		// A loopback literal on a closed port fails to connect at once
		// without leaving the machine.
		repo := model.TargetRepo{Slug: "internal", URL: "https://127.0.0.1:1/x", Branch: "main", AllowPrivateHost: true}
		_, err := gitsrc.New(opts, discard()).Sync(t.Context(), repo)
		if err == nil {
			t.Fatal("Sync() = nil error, want a connection failure")
		}
		if strings.Contains(err.Error(), policy) {
			t.Errorf("Sync() error = %q, want a connection error, not the address policy", err)
		}
		if !strings.Contains(err.Error(), "run git fetch") {
			t.Errorf("Sync() error = %q, want it to come from git fetch", err)
		}
		if _, statErr := os.Stat(filepath.Join(opts.ReposDir, "internal.git")); !errors.Is(statErr, fs.ErrNotExist) {
			t.Errorf("failed first sync left the clone directory (stat error %v)", statErr)
		}
	})

	t.Run("still https only", func(t *testing.T) {
		t.Parallel()
		opts := testOptions(t)
		opts.AllowFileURLs = false
		repo := model.TargetRepo{Slug: "internal", URL: "http://127.0.0.1/x", Branch: "main", AllowPrivateHost: true}
		_, err := gitsrc.New(opts, discard()).Sync(t.Context(), repo)
		if err == nil || !strings.Contains(err.Error(), `scheme "http" not allowed`) {
			t.Errorf("Sync() error = %v, want the scheme refusal", err)
		}
	})
}

func TestSync_TokenNotOnDisk(t *testing.T) {
	t.Parallel()
	const token = "fake-token-abcdefgh1234"
	up := gitrepo.New(t, "main")
	up.Write("go.mod", "module example.com/app\n")
	up.Commit("first", day(1))

	var logs bytes.Buffer
	opts := testOptions(t)
	g := gitsrc.New(opts, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	repo := model.TargetRepo{Slug: "private-app", URL: up.URL(), Token: model.Secret(token)}

	res, err := g.Sync(t.Context(), repo)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	up.Write("go.mod", "module example.com/app\n\ngo 1.27\n")
	up.Commit("second", day(2))
	if _, err := g.Sync(t.Context(), repo); err != nil {
		t.Fatalf("second Sync() error = %v", err)
	}

	// A failing sync with the token set: its error must not carry it.
	bad := repo
	bad.Branch = "no-such-branch"
	_, syncErr := g.Sync(t.Context(), bad)
	if syncErr == nil {
		t.Fatal("Sync(missing branch) = nil error, want an error")
	}

	needles := []string{
		token,
		"x-access-token",
		base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token)),
	}
	for _, root := range []string{res.ClonePath, opts.HomeDir} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, n := range needles {
				if bytes.Contains(data, []byte(n)) {
					t.Errorf("%s contains %q", path, n)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	for _, n := range needles {
		if strings.Contains(syncErr.Error(), n) {
			t.Errorf("Sync() error contains %q: %v", n, syncErr)
		}
		if strings.Contains(logs.String(), n) {
			t.Errorf("log output contains %q:\n%s", n, logs.String())
		}
	}
	if !strings.Contains(logs.String(), "git sync finished") {
		t.Errorf("log output lacks the sync summary; the log assertion checked nothing:\n%s", logs.String())
	}
}

func TestSync_SizeCap(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "main")
	big := make([]byte, 2<<20)
	rand.NewChaCha8([32]byte{1}).Read(big) // incompressible, so the pack stays large
	up.WriteBytes("vendor/blob.bin", big)
	up.Commit("large file", day(1))

	opts := testOptions(t)
	opts.MaxCloneBytes = 256 << 10
	g := gitsrc.New(opts, discard())
	_, err := g.Sync(t.Context(), model.TargetRepo{Slug: "big", URL: up.URL(), Branch: "main"})
	if err == nil || !strings.Contains(err.Error(), "clone exceeded the 0.25 MiB size cap") {
		t.Fatalf("Sync() error = %v, want the size cap error", err)
	}
	if _, statErr := os.Stat(filepath.Join(opts.ReposDir, "big.git")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("clone directory exists after the cap tripped (stat error %v)", statErr)
	}
}

func TestSync_LaterFetchFailureKeepsClone(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "main")
	head := up.Commit("first", day(1))
	g := gitsrc.New(testOptions(t), discard())
	repo := model.TargetRepo{Slug: "keep", URL: up.URL(), Branch: "main"}
	res, err := g.Sync(t.Context(), repo)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	repo.Branch = "gone"
	if _, err := g.Sync(t.Context(), repo); err == nil {
		t.Fatal("Sync(missing branch) = nil error, want an error")
	}
	if got := gitrepo.Git(t, res.ClonePath, "rev-parse", "refs/heads/main"); got != head {
		t.Errorf("clone's main after a failed fetch = %s, want %s", got, head)
	}
}

func TestSync_RejectsSlugAndBranch(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "main")
	up.Commit("first", day(1))
	tests := []struct {
		name   string
		slug   string
		branch string
		want   string
	}{
		{"slug traversal", "../etc", "main", "slug"},
		{"slug upper case", "App", "main", "slug"},
		{"slug empty", "", "main", "slug"},
		{"branch leading dash", "app", "-delete", `branch "-delete" not allowed`},
		{"branch invalid", "app", "a..b", `branch "a..b" not allowed`},
		{"branch reflog syntax", "app", "@{-1}", `branch "@{-1}" not allowed`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := testOptions(t)
			_, err := gitsrc.New(opts, discard()).Sync(t.Context(),
				model.TargetRepo{Slug: tt.slug, URL: up.URL(), Branch: tt.branch})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Sync(slug %q, branch %q) error = %v, want it to contain %q", tt.slug, tt.branch, err, tt.want)
			}
			if _, statErr := os.Stat(opts.ReposDir); !errors.Is(statErr, fs.ErrNotExist) {
				t.Errorf("repos directory exists after a refusal (stat error %v)", statErr)
			}
		})
	}
}

// TestSync_RelativeDirectories uses relative ReposDir, HomeDir and
// ClonePath. git runs in HomeDir, so each path must reach git in absolute
// form or git would act on a directory under HomeDir instead.
func TestSync_RelativeDirectories(t *testing.T) {
	up := gitrepo.New(t, "main")
	up.Write("go.mod", "module example.com/app\n")
	head := up.Commit("first", day(1))
	big := gitrepo.New(t, "main")
	blob := make([]byte, 2<<20)
	rand.NewChaCha8([32]byte{2}).Read(blob)
	big.WriteBytes("blob.bin", blob)
	big.Commit("large file", day(1))

	t.Chdir(t.TempDir())
	opts := gitsrc.Options{ReposDir: "data/repos", HomeDir: "data/git-home", AllowFileURLs: true}
	g := gitsrc.New(opts, discard())
	res, err := g.Sync(t.Context(), model.TargetRepo{Slug: "app", URL: up.URL(), Branch: "main"})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	wantPath, err := filepath.Abs("data/repos/app.git")
	if err != nil {
		t.Fatal(err)
	}
	if res.ClonePath != wantPath || res.Head != head {
		t.Errorf("Sync() = %+v, want ClonePath %s and Head %s", res, wantPath, head)
	}
	if got := gitrepo.Git(t, wantPath, "rev-parse", "refs/heads/main"); got != head {
		t.Errorf("clone's main = %s, want %s", got, head)
	}
	if _, err := os.Stat("data/git-home/data"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("git wrote under HomeDir (stat data/git-home/data error %v)", err)
	}

	// The read methods with a relative ClonePath.
	repo := model.TargetRepo{Slug: "app", ClonePath: "data/repos/app.git", DefaultBranch: "main"}
	if got, err := g.Head(t.Context(), repo); err != nil || got != head {
		t.Errorf("Head(relative clone) = %q, %v, want %q", got, err, head)
	}
	if ok, err := g.IsAncestor(t.Context(), repo, head, head); err != nil || !ok {
		t.Errorf("IsAncestor(relative clone) = %v, %v, want true", ok, err)
	}
	commits, err := g.ManifestCommits(t.Context(), repo, "", head, []string{"go.mod"})
	if err != nil || len(commits) != 1 || commits[0].SHA != head {
		t.Errorf("ManifestCommits(relative clone) = %+v, %v, want the one commit %s", commits, err, head)
	}
	blobs, err := g.ReadBlobs(t.Context(), repo, []model.BlobRef{{Commit: head, Path: "go.mod"}})
	if err != nil || len(blobs) != 1 || string(blobs[0].Content) != "module example.com/app\n" {
		t.Errorf("ReadBlobs(relative clone) = %+v, %v, want go.mod's content", blobs, err)
	}

	// The size watch and the cleanup act on the clone git wrote.
	capped := gitsrc.New(gitsrc.Options{ReposDir: "data/repos", HomeDir: "data/git-home", AllowFileURLs: true, MaxCloneBytes: 256 << 10}, discard())
	_, err = capped.Sync(t.Context(), model.TargetRepo{Slug: "big", URL: big.URL(), Branch: "main"})
	if err == nil || !strings.Contains(err.Error(), "size cap") {
		t.Errorf("Sync(over cap) error = %v, want the size cap error", err)
	}
	if _, err := os.Stat("data/repos/big.git"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("failed first sync left data/repos/big.git (stat error %v)", err)
	}
	if _, err := os.Stat("data/git-home/data"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("git wrote under HomeDir (stat data/git-home/data error %v)", err)
	}
}

func TestSync_IncrementalCapKeepsHead(t *testing.T) {
	t.Parallel()
	up := gitrepo.New(t, "main")
	up.Write("go.mod", "module example.com/app\n")
	first := up.Commit("first", day(1))

	opts := testOptions(t)
	res, err := gitsrc.New(opts, discard()).Sync(t.Context(), model.TargetRepo{Slug: "app", URL: up.URL(), Branch: "main"})
	if err != nil {
		t.Fatalf("first Sync() error = %v", err)
	}

	blob := make([]byte, 2<<20)
	rand.NewChaCha8([32]byte{3}).Read(blob)
	up.WriteBytes("blob.bin", blob)
	up.Commit("large file", day(2))

	// A cap with room for the clone as it is, but not for the new commit.
	opts.MaxCloneBytes = treeSize(t, res.ClonePath) + 256<<10
	_, err = gitsrc.New(opts, discard()).Sync(t.Context(), model.TargetRepo{Slug: "app", URL: up.URL(), Branch: "main"})
	if err == nil || !strings.Contains(err.Error(), "size cap") {
		t.Fatalf("second Sync() error = %v, want the size cap error", err)
	}
	if got := gitrepo.Git(t, res.ClonePath, "rev-parse", "refs/heads/main"); got != first {
		t.Errorf("clone's main after an over-cap fetch = %s, want the previous head %s", got, first)
	}
	if refs := gitrepo.Git(t, res.ClonePath, "for-each-ref", "refs/upw/"); refs != "" {
		t.Errorf("staging refs left after a failed fetch:\n%s", refs)
	}
}

// treeSize returns the total size of the regular files under dir.
func treeSize(t *testing.T, dir string) int64 {
	t.Helper()
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		t.Fatalf("measure %s: %v", dir, err)
	}
	return total
}

// TestSync_RedactsCredentialEchoedByGit runs Sync against a stand-in git
// whose fetch fails with the credential it was given echoed on stderr, as a
// hostile remote's error message could.
func TestSync_RedactsCredentialEchoedByGit(t *testing.T) {
	const token = "fake-token-abcdefgh1234"
	bin := t.TempDir()
	script := `#!/bin/sh
for arg do
	case "$arg" in
	check-ref-format) echo main; exit 0 ;;
	init|update-ref) exit 0 ;;
	fetch)
		echo "fatal: remote error: rejected $GIT_CONFIG_VALUE_0 for x-access-token:TOKEN (user TOKEN)" >&2
		exit 128 ;;
	esac
done
exit 0
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(strings.ReplaceAll(script, "TOKEN", token)), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	g := gitsrc.New(testOptions(t), discard())
	_, err := g.Sync(t.Context(), model.TargetRepo{Slug: "app", URL: "file:///srv/app.git", Branch: "main", Token: token})
	if err == nil {
		t.Fatal("Sync() = nil error, want the stand-in fetch failure")
	}
	msg := err.Error()
	if !strings.Contains(msg, "rejected") || !strings.Contains(msg, "[REDACTED]") {
		t.Errorf("Sync() error = %q, want git's stderr with the credential redacted", msg)
	}
	for _, n := range []string{token, base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token)), "x-access-token:fake"} {
		if strings.Contains(msg, n) {
			t.Errorf("Sync() error contains %q: %s", n, msg)
		}
	}
}

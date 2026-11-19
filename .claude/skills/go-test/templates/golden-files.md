# Template: golden files

Use this when the output is long and its exact text matters: digest bodies per notifier, rendered HTML pages, the JSON payload sent to the LLM, CLI tables.

## Flag and helper

Declare the flag once per package, in a test file. The comparison helper lives in `internal/testutil/golden` so packages share it.

```go
// In the package under test, e.g. internal/report/golden_test.go
var update = flag.Bool("update", false, "rewrite golden files in testdata/")
```

```go
// Package golden compares test output with files under testdata/.
package golden

// Assert compares got with testdata/<name>.golden. With update set it rewrites
// the file first, so the comparison always runs against what is on disk.
func Assert(t testing.TB, name string, got []byte, update bool) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with -update to create it): %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("output differs from %s; rerun with -update and review the diff\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
```

## Use

```go
func TestDiscordFormat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		digest model.Digest
	}{
		{name: "one advisory", digest: digestWithAdvisory()},
		{name: "budget waiting", digest: digestWithWaiting(2)},
		{name: "all quiet heartbeat", digest: model.Digest{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := discord.Format(tt.digest)
			if err != nil {
				t.Fatalf("Format() error = %v", err)
			}
			golden.Assert(t, t.Name(), got, *update)
		})
	}
}
```

`t.Name()` includes the subtest, so each row gets its own file (`testdata/TestDiscordFormat/one_advisory.golden`).

## Rules

- **Run `-update` against one package:** `go test ./internal/report -run TestDiscordFormat -update`. Passing `-update` to `go test ./...` fails with "flag provided but not defined" in every package that lacks it.
- **Inputs must be fixed:** fixed times, fixed IDs, sorted maps. Golden output that changes between runs is a bug in the test.
- **Review regenerated files before reporting.** `git diff -- '*/testdata/*'` shows what changed. A golden file that was regenerated and not read asserts nothing.
- **Golden files are committed** and contain only fixture data: `example-org` names, no real tokens or private repositories.

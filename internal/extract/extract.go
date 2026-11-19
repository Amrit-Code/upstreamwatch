// Package extract turns one manifest or lockfile at one commit into the
// normalised dependencies the timeline stores. It does no I/O: every parser
// is a function of the path and the file content.
package extract

import (
	"fmt"
	"log/slog"
	"path"
	"strings"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// Ecosystem names as the manifests library spells them.
const (
	ecosystemGolang  = "golang"
	ecosystemDocker  = "docker"
	ecosystemPyPI    = "pypi"
	ecosystemActions = "github-actions"
)

// Scope values the parsers in this package assign.
const (
	scopeRuntime = "runtime"
	scopeBuild   = "build"
)

// toolchainPURL is the pseudo-package the Go toolchain version is recorded
// under, so standard-library advisories can be matched against it.
const toolchainPURL = "pkg:golang/toolchain"

// patterns are the git glob pathspecs Parse handles. Read-only.
var patterns = []string{
	"**/go.mod",
	"**/requirements*.txt",
	"**/pyproject.toml",
	"**/uv.lock",
	"**/poetry.lock",
	"**/Dockerfile",
	"**/Dockerfile.*",
	"**/*.Dockerfile",
	"**/docker-compose*.yml",
	"**/docker-compose*.yaml",
	"**/compose*.yml",
	"**/compose*.yaml",
	".github/workflows/*.yml",
	".github/workflows/*.yaml",
}

// Options configures an Extractor.
type Options struct {
	// DeriveToolchainFromImages adds pkg:golang/toolchain for golang base
	// images (config vulns.derive_toolchain_from_images, default true).
	DeriveToolchainFromImages bool
}

// Extractor parses Go modules, Python requirements and lockfiles,
// Dockerfiles, compose files and GitHub Actions workflows.
type Extractor struct {
	opts   Options
	logger *slog.Logger
}

// New returns an Extractor. It does no I/O.
func New(opts Options, logger *slog.Logger) *Extractor {
	return &Extractor{opts: opts, logger: logger}
}

// Patterns returns the paths Parse handles, in git's glob pathspec syntax,
// where a leading "**/" also matches the repository root.
func (e *Extractor) Patterns() []string {
	return append([]string(nil), patterns...)
}

// fileFormat is the parser a path is dispatched to.
type fileFormat int

const (
	formatUnsupported fileFormat = iota
	formatGoMod
	formatDockerfile
	formatCompose
	formatPythonManifest
	formatPythonLockfile
	formatActions
)

// Parse parses one file. A path Parse does not handle returns an error
// wrapping model.ErrUnsupportedFile; content that does not parse returns a
// different error naming the path.
func (e *Extractor) Parse(p string, content []byte) (model.ParsedManifest, error) {
	var (
		pm  model.ParsedManifest
		err error
	)
	switch formatOf(p) {
	case formatGoMod:
		pm, err = parseGoMod(p, content)
	case formatDockerfile:
		pm, err = e.parseDockerfile(p, content)
	case formatCompose:
		pm, err = e.parseCompose(p, content)
	case formatPythonManifest:
		pm, err = e.parseWithLibrary(p, content, model.ManifestKindManifest)
	case formatPythonLockfile:
		pm, err = e.parseWithLibrary(p, content, model.ManifestKindLockfile)
	case formatActions:
		pm, err = e.parseWithLibrary(p, content, model.ManifestKindManifest)
	default:
		return model.ParsedManifest{}, fmt.Errorf("parse %s: %w", p, model.ErrUnsupportedFile)
	}
	if err != nil {
		return model.ParsedManifest{}, err
	}
	pm.Dependencies = dedupe(pm.Dependencies)
	return pm, nil
}

// formatOf picks the parser for a path from its base name, and for
// workflows from its directory, matching the globs in patterns.
func formatOf(p string) fileFormat {
	if matchAny(p, ".github/workflows/*.yml", ".github/workflows/*.yaml") {
		return formatActions
	}
	base := path.Base(p)
	switch {
	case base == "go.mod":
		return formatGoMod
	case base == "pyproject.toml" || matchAny(base, "requirements*.txt"):
		return formatPythonManifest
	case base == "uv.lock" || base == "poetry.lock":
		return formatPythonLockfile
	case matchAny(base, "Dockerfile", "Dockerfile.*", "*.Dockerfile"):
		return formatDockerfile
	case matchAny(base, "docker-compose*.yml", "docker-compose*.yaml", "compose*.yml", "compose*.yaml"):
		return formatCompose
	}
	return formatUnsupported
}

// matchAny reports whether name matches one of the path.Match globs.
func matchAny(name string, globs ...string) bool {
	for _, g := range globs {
		// The globs are constants without syntax errors, so the error is
		// always nil.
		if ok, _ := path.Match(g, name); ok {
			return true
		}
	}
	return false
}

// versionless strips the subpath, qualifiers and version from a package URL.
// Characters that are not separators are percent-encoded in a PURL, so the
// first '#', '?' and '@' are the separators.
func versionless(purl string) string {
	purl, _, _ = strings.Cut(purl, "#")
	purl, _, _ = strings.Cut(purl, "?")
	purl, _, _ = strings.Cut(purl, "@")
	return purl
}

// dedupe keeps the first dependency for each package URL, in order.
func dedupe(deps []model.Dependency) []model.Dependency {
	if len(deps) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(deps))
	out := deps[:0]
	for _, d := range deps {
		if seen[d.Package.PURL] {
			continue
		}
		seen[d.Package.PURL] = true
		out = append(out, d)
	}
	return out
}

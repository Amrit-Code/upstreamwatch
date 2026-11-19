//go:build e2e

package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/extract"
	"github.com/Amrit-Code/upstreamwatch/internal/extract/unity"
	"github.com/Amrit-Code/upstreamwatch/internal/gitsrc"
	"github.com/Amrit-Code/upstreamwatch/internal/ingest"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/store"
	"github.com/Amrit-Code/upstreamwatch/internal/timeline"
)

// parityFixtures are the public repositories whose timelines are compared
// with git pkgs history, run in this order. Each is pinned: the clone's
// default branch is reset to commit before either side indexes it, so a new
// upstream commit cannot change the result. Bumping a pin is a deliberate
// edit that may need the exceptions revisited.
var parityFixtures = []struct {
	slug   string
	url    string
	commit string
}{
	// Default-branch HEAD on 2026-11-19.
	{slug: "glow", url: "https://github.com/charmbracelet/glow", commit: "8b0600fa445535b9c53194c0d4f7fe3cf700ef3e"},
	// Default-branch HEAD on 2026-11-19.
	{slug: "cleo", url: "https://github.com/python-poetry/cleo", commit: "30df50748e7cf52195cf2f22262b432e6320fe21"},
}

// The decisions in docs/decisions.md ("Lead decisions for M1") that explain
// the excepted differences.
const (
	reasonReplace   = "go.mod replace directives are applied: go.mod is parsed with modfile because the manifests library ignores replace"
	reasonFirstWins = "a poetry.lock naming one package several times: the first occurrence in the file wins"
	reasonScopeOnly = "only poetry.lock groups and markers changed: a change of scope alone emits no event"
	reasonDirect    = "only the go.mod // indirect comment changed: a change of the direct flag emits changed"
)

// parityExceptions are the differences a recorded decision in
// docs/decisions.md ("Lead decisions for M1") explains. Each names one
// event by every compared field and must match a difference of its
// fixture, so a stale entry fails the test.
var parityExceptions = []parityException{
	// glow: replace directives to local paths and to a fork.
	{Fixture: "glow", Side: sideUpw, SHA: "fd62cebfd1725a58f8ebdfe1c5abd4bba353e0c3", ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/charmbracelet/boba", Change: model.ChangeChanged, Requirement: "=> ../boba", Reason: reasonReplace},
	{Fixture: "glow", Side: sideUpw, SHA: "b9c02d756b44dedd580ab79e85aea1a9a7a7875b", ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/charmbracelet/bubbles", Change: model.ChangeChanged, Requirement: "=> ../bubbles", Reason: reasonReplace},
	{Fixture: "glow", Side: sideUpw, SHA: "93e8370b2a555a590dd4884de3ffe367bbcf8884", ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/charmbracelet/charm", Change: model.ChangeChanged, Requirement: "=> ../charm", Reason: reasonReplace},
	{Fixture: "glow", Side: sideUpw, SHA: "a106bc0163b5ac13c091462bf864eab22bd8531c", ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/charmbracelet/charm", Change: model.ChangeAdded, Requirement: "=> ../charm", Reason: reasonReplace},
	{Fixture: "glow", Side: sideUpw, SHA: "bbd6b1acc74a5780a9d64e28d8f66b3660cf8bbe", ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/charmbracelet/charm", Change: model.ChangeChanged, Requirement: "v0.0.0-00010101000000-000000000000", Reason: reasonReplace},
	{Fixture: "glow", Side: sideUpw, SHA: "c66351127c585cfe8cf330f6b89e9b7eaa692e22", ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/charmbracelet/charm", Change: model.ChangeChanged, Requirement: "v0.4.2-0.20200522192006-ce78aabf4b25", Reason: reasonReplace},
	{Fixture: "glow", Side: sideUpw, SHA: "fd62cebfd1725a58f8ebdfe1c5abd4bba353e0c3", ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/charmbracelet/charm", Change: model.ChangeChanged, Requirement: "=> ../charm", Reason: reasonReplace},
	{Fixture: "glow", Side: sideUpw, SHA: "21eccfd2c3e2f598ae8b1f8ee9bb3fc66eaa982d", ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/meowgorithm/gitcha", Change: model.ChangeRemoved, Reason: reasonReplace},
	{Fixture: "glow", Side: sideUpw, SHA: "a3a05fde9c515be8ba000f505c26f77d1fce6822", ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/meowgorithm/gitcha", Change: model.ChangeAdded, Requirement: "github.com/muesli/gitcha => github.com/meowgorithm/gitcha v0.1.2-0.20200907220241-87b69df49486", Reason: reasonReplace},
	{Fixture: "glow", Side: sideUpw, SHA: "21eccfd2c3e2f598ae8b1f8ee9bb3fc66eaa982d", ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/muesli/gitcha", Change: model.ChangeAdded, Requirement: "v0.1.2-0.20200908172931-5aa4fdccf2f6", Reason: reasonReplace},
	{Fixture: "glow", Side: sideUpw, SHA: "a3a05fde9c515be8ba000f505c26f77d1fce6822", ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/muesli/gitcha", Change: model.ChangeRemoved, Reason: reasonReplace},
	{Fixture: "glow", Side: sideGitPkgs, SHA: "a106bc0163b5ac13c091462bf864eab22bd8531c", ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/charmbracelet/charm", Change: model.ChangeAdded, Requirement: "v0.0.0-00010101000000-000000000000", Reason: reasonReplace},
	{Fixture: "glow", Side: sideGitPkgs, SHA: "21eccfd2c3e2f598ae8b1f8ee9bb3fc66eaa982d", ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/muesli/gitcha", Change: model.ChangeChanged, Requirement: "v0.1.2-0.20200908172931-5aa4fdccf2f6", Reason: reasonReplace},

	// glow: a require line gaining or losing "// indirect" at the same
	// version; git-pkgs sees no change.
	{Fixture: "glow", Side: sideUpw, SHA: "1a13810c340b8833627eecf08c100c3b39c71f31", ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/charmbracelet/lipgloss", Change: model.ChangeChanged, Requirement: "v1.1.1-0.20250404203927-76690c660834", Reason: reasonDirect},
	{Fixture: "glow", Side: sideUpw, SHA: "19c6af747f4edd867cb70e0794d53255bfeb2745", ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/fsnotify/fsnotify", Change: model.ChangeChanged, Requirement: "v1.6.0", Reason: reasonDirect},
	{Fixture: "glow", Side: sideUpw, SHA: "6b365eea95f7541d4af441d971010b09f6082a0e", ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/muesli/cancelreader", Change: model.ChangeChanged, Requirement: "v0.2.2", Reason: reasonDirect},
	{Fixture: "glow", Side: sideUpw, SHA: "e0bd2dd466dfaef5230000a46de0c9c877f76f7c", ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/muesli/gitcha", Change: model.ChangeChanged, Requirement: "v0.0.0-20200714001838-c071c3c6c6e1", Reason: reasonDirect},
	{Fixture: "glow", Side: sideUpw, SHA: "6b365eea95f7541d4af441d971010b09f6082a0e", ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/rivo/uniseg", Change: model.ChangeChanged, Requirement: "v0.4.7", Reason: reasonDirect},
	{Fixture: "glow", Side: sideUpw, SHA: "1f2b3671c715f4a84d8103f617ee35d821fb347b", ManifestPath: "go.mod", Ecosystem: "golang", Name: "golang.org/x/crypto", Change: model.ChangeChanged, Requirement: "v0.0.0-20201221181555-eec23a3978ad", Reason: reasonDirect},
	{Fixture: "glow", Side: sideUpw, SHA: "1f2b3671c715f4a84d8103f617ee35d821fb347b", ManifestPath: "go.mod", Ecosystem: "golang", Name: "golang.org/x/term", Change: model.ChangeChanged, Requirement: "v0.0.0-20201210144234-2321bbc49cbf", Reason: reasonDirect},

	// cleo: poetry.lock entries listed two or three times with different
	// versions for different Python markers. git-pkgs reports every
	// entry; upstreamwatch the first, and a change when the first is
	// dropped.
	{Fixture: "cleo", Side: sideUpw, SHA: "10363c1488c65ea3458baefc45afe37ba34d8175", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "cfgv", Change: model.ChangeChanged, Requirement: "3.4.0", Reason: reasonFirstWins},
	{Fixture: "cleo", Side: sideUpw, SHA: "10363c1488c65ea3458baefc45afe37ba34d8175", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "mypy", Change: model.ChangeChanged, Requirement: "1.6.1", Reason: reasonFirstWins},
	{Fixture: "cleo", Side: sideUpw, SHA: "10363c1488c65ea3458baefc45afe37ba34d8175", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "pre-commit", Change: model.ChangeChanged, Requirement: "3.5.0", Reason: reasonFirstWins},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "016e0d2568bb9f16a7d22505428982e80aa3214b", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "cfgv", Change: model.ChangeAdded, Requirement: "3.4.0", Reason: reasonFirstWins},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "0919e78295521af49ce683faea884f451d22f67a", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "colorama", Change: model.ChangeAdded, Requirement: "0.4.3", Reason: reasonFirstWins},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "0919e78295521af49ce683faea884f451d22f67a", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "coverage", Change: model.ChangeAdded, Requirement: "5.0.4", Reason: reasonFirstWins},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "1e8c6398540769b0ca0f2604313c57319ddfd765", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "filelock", Change: model.ChangeAdded, Requirement: "3.12.4", Reason: reasonFirstWins},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "016e0d2568bb9f16a7d22505428982e80aa3214b", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "identify", Change: model.ChangeAdded, Requirement: "2.5.27", Reason: reasonFirstWins},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "1e8c6398540769b0ca0f2604313c57319ddfd765", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "identify", Change: model.ChangeChanged, Requirement: "2.5.30", Reason: reasonFirstWins},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "0919e78295521af49ce683faea884f451d22f67a", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "importlib-metadata", Change: model.ChangeAdded, Requirement: "1.5.2", Reason: reasonFirstWins},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "0919e78295521af49ce683faea884f451d22f67a", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "importlib-resources", Change: model.ChangeAdded, Requirement: "1.4.0", Reason: reasonFirstWins},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "0919e78295521af49ce683faea884f451d22f67a", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "more-itertools", Change: model.ChangeAdded, Requirement: "7.2.0", Reason: reasonFirstWins},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "0919e78295521af49ce683faea884f451d22f67a", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "more-itertools", Change: model.ChangeAdded, Requirement: "8.2.0", Reason: reasonFirstWins},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "016e0d2568bb9f16a7d22505428982e80aa3214b", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "mypy", Change: model.ChangeAdded, Requirement: "1.5.1", Reason: reasonFirstWins},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "1e8c6398540769b0ca0f2604313c57319ddfd765", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "mypy", Change: model.ChangeChanged, Requirement: "1.6.1", Reason: reasonFirstWins},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "016e0d2568bb9f16a7d22505428982e80aa3214b", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "pre-commit", Change: model.ChangeAdded, Requirement: "3.3.3", Reason: reasonFirstWins},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "0919e78295521af49ce683faea884f451d22f67a", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "pre-commit", Change: model.ChangeAdded, Requirement: "1.21.0", Reason: reasonFirstWins},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "1e8c6398540769b0ca0f2604313c57319ddfd765", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "pre-commit", Change: model.ChangeChanged, Requirement: "3.5.0", Reason: reasonFirstWins},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "0919e78295521af49ce683faea884f451d22f67a", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "pyyaml", Change: model.ChangeAdded, Requirement: "5.3.1", Reason: reasonFirstWins},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "016e0d2568bb9f16a7d22505428982e80aa3214b", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "setuptools", Change: model.ChangeChanged, Requirement: "68.1.2", Reason: reasonFirstWins},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "0919e78295521af49ce683faea884f451d22f67a", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "tox", Change: model.ChangeAdded, Requirement: "3.14.6", Reason: reasonFirstWins},

	// cleo: two commits whose poetry.lock diff changes only groups and
	// markers lines (PEP 621 move, dependency-groups migration); git-pkgs
	// reports each package whose group changed as modified at the same
	// version.
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "fd264efa3702c6de0231b8235741881d70bb4e3e", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "alabaster", Change: model.ChangeChanged, Requirement: "0.7.16", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "fd264efa3702c6de0231b8235741881d70bb4e3e", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "babel", Change: model.ChangeChanged, Requirement: "2.17.0", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "fd264efa3702c6de0231b8235741881d70bb4e3e", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "beautifulsoup4", Change: model.ChangeChanged, Requirement: "4.14.2", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "fd264efa3702c6de0231b8235741881d70bb4e3e", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "certifi", Change: model.ChangeChanged, Requirement: "2025.8.3", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "cfgv", Change: model.ChangeChanged, Requirement: "3.4.0", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "fd264efa3702c6de0231b8235741881d70bb4e3e", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "charset-normalizer", Change: model.ChangeChanged, Requirement: "3.4.3", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "click", Change: model.ChangeChanged, Requirement: "8.1.8", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "colorama", Change: model.ChangeChanged, Requirement: "0.4.6", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "coverage", Change: model.ChangeChanged, Requirement: "7.6.10", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "distlib", Change: model.ChangeChanged, Requirement: "0.3.9", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "fd264efa3702c6de0231b8235741881d70bb4e3e", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "docutils", Change: model.ChangeChanged, Requirement: "0.21.2", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "exceptiongroup", Change: model.ChangeChanged, Requirement: "1.2.2", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "filelock", Change: model.ChangeChanged, Requirement: "3.16.1", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "fd264efa3702c6de0231b8235741881d70bb4e3e", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "furo", Change: model.ChangeChanged, Requirement: "2023.9.10", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "identify", Change: model.ChangeChanged, Requirement: "2.6.4", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "fd264efa3702c6de0231b8235741881d70bb4e3e", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "idna", Change: model.ChangeChanged, Requirement: "3.10", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "fd264efa3702c6de0231b8235741881d70bb4e3e", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "imagesize", Change: model.ChangeChanged, Requirement: "1.4.1", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "importlib-metadata", Change: model.ChangeChanged, Requirement: "8.5.0", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "importlib-resources", Change: model.ChangeChanged, Requirement: "6.4.5", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "iniconfig", Change: model.ChangeChanged, Requirement: "2.0.0", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "jinja2", Change: model.ChangeChanged, Requirement: "3.1.5", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "markupsafe", Change: model.ChangeChanged, Requirement: "3.0.2", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "mypy", Change: model.ChangeChanged, Requirement: "1.14.1", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "mypy-extensions", Change: model.ChangeChanged, Requirement: "1.0.0", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "nodeenv", Change: model.ChangeChanged, Requirement: "1.9.1", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "packaging", Change: model.ChangeChanged, Requirement: "24.2", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "platformdirs", Change: model.ChangeChanged, Requirement: "4.3.6", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "pluggy", Change: model.ChangeChanged, Requirement: "1.5.0", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "pre-commit", Change: model.ChangeChanged, Requirement: "3.8.0", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "pytest", Change: model.ChangeChanged, Requirement: "8.3.4", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "pytest-cov", Change: model.ChangeChanged, Requirement: "4.1.0", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "pytest-mock", Change: model.ChangeChanged, Requirement: "3.14.0", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "pyyaml", Change: model.ChangeChanged, Requirement: "6.0.2", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "fd264efa3702c6de0231b8235741881d70bb4e3e", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "requests", Change: model.ChangeChanged, Requirement: "2.32.5", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "fd264efa3702c6de0231b8235741881d70bb4e3e", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "snowballstemmer", Change: model.ChangeChanged, Requirement: "3.0.1", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "fd264efa3702c6de0231b8235741881d70bb4e3e", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "soupsieve", Change: model.ChangeChanged, Requirement: "2.8", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "fd264efa3702c6de0231b8235741881d70bb4e3e", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "sphinx", Change: model.ChangeChanged, Requirement: "7.4.7", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "fd264efa3702c6de0231b8235741881d70bb4e3e", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "sphinx-basic-ng", Change: model.ChangeChanged, Requirement: "1.0.0b2", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "fd264efa3702c6de0231b8235741881d70bb4e3e", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "sphinxcontrib-applehelp", Change: model.ChangeChanged, Requirement: "2.0.0", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "fd264efa3702c6de0231b8235741881d70bb4e3e", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "sphinxcontrib-devhelp", Change: model.ChangeChanged, Requirement: "2.0.0", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "fd264efa3702c6de0231b8235741881d70bb4e3e", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "sphinxcontrib-htmlhelp", Change: model.ChangeChanged, Requirement: "2.1.0", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "fd264efa3702c6de0231b8235741881d70bb4e3e", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "sphinxcontrib-jsmath", Change: model.ChangeChanged, Requirement: "1.0.1", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "fd264efa3702c6de0231b8235741881d70bb4e3e", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "sphinxcontrib-qthelp", Change: model.ChangeChanged, Requirement: "2.0.0", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "fd264efa3702c6de0231b8235741881d70bb4e3e", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "sphinxcontrib-serializinghtml", Change: model.ChangeChanged, Requirement: "2.0.0", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "tomli", Change: model.ChangeChanged, Requirement: "2.2.1", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "towncrier", Change: model.ChangeChanged, Requirement: "24.8.0", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "typing-extensions", Change: model.ChangeChanged, Requirement: "4.12.2", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "fd264efa3702c6de0231b8235741881d70bb4e3e", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "urllib3", Change: model.ChangeChanged, Requirement: "2.5.0", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "virtualenv", Change: model.ChangeChanged, Requirement: "20.28.1", Reason: reasonScopeOnly},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "acb16a775cdb98a909ade6fdc4c5476ccf2ce7f6", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "zipp", Change: model.ChangeChanged, Requirement: "3.21.0", Reason: reasonScopeOnly},
}

// parityEcosystems are the git-pkgs ecosystems upstreamwatch indexes.
var parityEcosystems = []string{"golang", "pypi", "docker", "github-actions"}

// toolchainPURL is the pseudo-package git-pkgs has no counterpart for.
const toolchainPURL = "pkg:golang/toolchain"

// maxListedDiffs caps the differing events printed per side.
const maxListedDiffs = 50

// minParityEvents is the fewest normalised events each side must have per
// fixture, so a run in which both sides parse nothing cannot pass.
const minParityEvents = 100

// gitPkgsEvent is one object of git pkgs history --format json, keeping only
// the fields the comparison reads.
type gitPkgsEvent struct {
	SHA          string `json:"sha"`
	Name         string `json:"name"`
	Ecosystem    string `json:"ecosystem"`
	ChangeType   string `json:"change_type"`
	Requirement  string `json:"requirement"`
	ManifestPath string `json:"manifest_path"`
}

// parityEvent is the normalised form both sides are compared in.
// Requirement is empty for a removal.
type parityEvent struct {
	SHA          string
	ManifestPath string
	Ecosystem    string
	Name         string
	Change       model.Change
	Requirement  string
}

func (e parityEvent) String() string {
	s := fmt.Sprintf("%s %s %s %s %s", e.SHA, e.ManifestPath, e.Ecosystem, e.Name, e.Change)
	if e.Requirement != "" {
		s += " " + e.Requirement
	}
	return s
}

// paritySide names the side a differing event appears on.
type paritySide string

const (
	sideUpw     paritySide = "upw"
	sideGitPkgs paritySide = "git-pkgs"
)

// parityException excuses differing events of one fixture that appear only
// on Side. A non-empty field must equal the event's; an empty one matches
// any value. Reason names the decision in docs/decisions.md that explains
// the difference.
type parityException struct {
	Fixture      string
	Side         paritySide
	SHA          string
	ManifestPath string
	Ecosystem    string
	Name         string
	Change       model.Change
	Requirement  string
	Reason       string
}

func (x parityException) matches(side paritySide, e parityEvent) bool {
	field := func(want, got string) bool { return want == "" || want == got }
	return x.Side == side &&
		field(x.SHA, e.SHA) &&
		field(x.ManifestPath, e.ManifestPath) &&
		field(x.Ecosystem, e.Ecosystem) &&
		field(x.Name, e.Name) &&
		field(string(x.Change), string(e.Change)) &&
		field(x.Requirement, e.Requirement)
}

// ecosystemPyPI is the git-pkgs and upstreamwatch name of the PyPI ecosystem.
const ecosystemPyPI = "pypi"

// pep503Separators are the runs PEP 503 replaces with one "-".
var pep503Separators = regexp.MustCompile(`[-_.]+`)

// packageName returns name as both sides are compared on. A package is
// identified by its version-less package URL, and the PyPI package URL
// normalises the name as PEP 503 does (docs/decisions.md, "Lead decisions
// for M1"), so PyPI names are lower-cased with separator runs as "-".
func packageName(ecosystem, name string) string {
	if ecosystem != ecosystemPyPI {
		return name
	}
	return pep503Separators.ReplaceAllString(strings.ToLower(name), "-")
}

// collapseRespellings folds each git-pkgs removed plus added pair at one
// commit and manifest whose PyPI names normalise equal, as git-pkgs reports
// a lockfile respelling Jinja2 as jinja2. Under the same decision as
// packageName that is one package: the pair becomes nothing when the
// requirement is unchanged and one modified event otherwise. Several pairs
// under one key are matched in order. events must already carry normalised
// names; the result keeps their order.
func collapseRespellings(events []gitPkgsEvent) []gitPkgsEvent {
	type key struct{ sha, path, name string }
	removed := map[key][]int{}
	added := map[key][]int{}
	for i, e := range events {
		if e.Ecosystem != ecosystemPyPI {
			continue
		}
		k := key{e.SHA, e.ManifestPath, e.Name}
		switch e.ChangeType {
		case "removed":
			removed[k] = append(removed[k], i)
		case "added":
			added[k] = append(added[k], i)
		}
	}
	drop := map[int]bool{}
	out := slices.Clone(events)
	for k, rs := range removed {
		as := added[k]
		for j := range min(len(rs), len(as)) {
			r, a := rs[j], as[j]
			drop[r] = true
			if out[r].Requirement == out[a].Requirement {
				drop[a] = true
			} else {
				out[a].ChangeType = "modified"
			}
		}
	}
	kept := out[:0]
	for i, e := range out {
		if !drop[i] {
			kept = append(kept, e)
		}
	}
	return kept
}

// normaliseGitPkgs keeps the git-pkgs events on first-parent commits that
// are not merges, in an ecosystem upstreamwatch indexes and a manifest path
// it handles, normalises PyPI names and collapses respellings, and maps
// modified to changed.
func normaliseGitPkgs(events []gitPkgsEvent, firstParent, merges map[string]bool, handled func(path string) bool) ([]parityEvent, error) {
	var kept []gitPkgsEvent
	for _, e := range events {
		if !firstParent[e.SHA] || merges[e.SHA] ||
			!slices.Contains(parityEcosystems, e.Ecosystem) || !handled(e.ManifestPath) {
			continue
		}
		e.Name = packageName(e.Ecosystem, e.Name)
		kept = append(kept, e)
	}
	var out []parityEvent
	for _, e := range collapseRespellings(kept) {
		var change model.Change
		switch e.ChangeType {
		case "added":
			change = model.ChangeAdded
		case "modified":
			change = model.ChangeChanged
		case "removed":
			change = model.ChangeRemoved
		default:
			return nil, fmt.Errorf("git-pkgs event %s %s %s: unknown change_type %q", e.SHA, e.ManifestPath, e.Name, e.ChangeType)
		}
		req := e.Requirement
		if change == model.ChangeRemoved {
			req = ""
		}
		out = append(out, parityEvent{
			SHA:          e.SHA,
			ManifestPath: e.ManifestPath,
			Ecosystem:    e.Ecosystem,
			Name:         e.Name,
			Change:       change,
			Requirement:  req,
		})
	}
	return out, nil
}

// normaliseUpw drops upstreamwatch's events on merge commits, whose changes
// git-pkgs attributes to the branch commits, and the toolchain
// pseudo-package, and normalises PyPI names.
func normaliseUpw(events []model.DepEvent, merges map[string]bool) []parityEvent {
	var out []parityEvent
	for _, e := range events {
		if merges[e.CommitSHA] || e.Package.PURL == toolchainPURL {
			continue
		}
		req := e.Requirement
		if e.Change == model.ChangeRemoved {
			req = ""
		}
		out = append(out, parityEvent{
			SHA:          e.CommitSHA,
			ManifestPath: e.ManifestPath,
			Ecosystem:    e.Package.Ecosystem,
			Name:         packageName(e.Package.Ecosystem, e.Package.Name),
			Change:       e.Change,
			Requirement:  req,
		})
	}
	return out
}

// parityResult is the outcome of comparing one fixture's two event lists.
type parityResult struct {
	upwCount, gitPkgsCount int
	// onlyUpw and onlyGitPkgs are the differing events no exception
	// excuses, sorted.
	onlyUpw, onlyGitPkgs []parityEvent
	// excused counts the differing events exceptions excused.
	excused int
	// unused are the fixture's exceptions that matched no difference.
	unused []parityException
}

func (r parityResult) ok() bool {
	return len(r.onlyUpw) == 0 && len(r.onlyGitPkgs) == 0 && len(r.unused) == 0
}

// compareParity compares upw and gitPkgs as multisets and excuses the
// differences that fixture's exceptions match.
func compareParity(fixture string, upw, gitPkgs []parityEvent, exceptions []parityException) parityResult {
	counts := make(map[parityEvent]int, len(upw))
	for _, e := range upw {
		counts[e]++
	}
	for _, e := range gitPkgs {
		counts[e]--
	}

	var fixtureExceptions []parityException
	for _, x := range exceptions {
		if x.Fixture == fixture {
			fixtureExceptions = append(fixtureExceptions, x)
		}
	}
	used := make([]bool, len(fixtureExceptions))
	excuse := func(side paritySide, e parityEvent) bool {
		for i, x := range fixtureExceptions {
			if x.matches(side, e) {
				used[i] = true
				return true
			}
		}
		return false
	}

	r := parityResult{upwCount: len(upw), gitPkgsCount: len(gitPkgs)}
	for e, n := range counts {
		side, list := sideUpw, &r.onlyUpw
		if n < 0 {
			side, list, n = sideGitPkgs, &r.onlyGitPkgs, -n
		}
		for range n {
			if excuse(side, e) {
				r.excused++
				continue
			}
			*list = append(*list, e)
		}
	}
	for i, x := range fixtureExceptions {
		if !used[i] {
			r.unused = append(r.unused, x)
		}
	}
	slices.SortFunc(r.onlyUpw, compareParityEvents)
	slices.SortFunc(r.onlyGitPkgs, compareParityEvents)
	return r
}

func compareParityEvents(a, b parityEvent) int {
	return cmp.Or(
		cmp.Compare(a.ManifestPath, b.ManifestPath),
		cmp.Compare(a.Ecosystem, b.Ecosystem),
		cmp.Compare(a.Name, b.Name),
		cmp.Compare(a.SHA, b.SHA),
		cmp.Compare(a.Change, b.Change),
		cmp.Compare(a.Requirement, b.Requirement),
	)
}

// report describes a failed comparison: the counts on each side, up to
// maxListedDiffs differing events from each side, and unused exceptions.
func (r parityResult) report(fixture string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: upw %d events, git-pkgs %d events (normalised); %d differences excused\n",
		fixture, r.upwCount, r.gitPkgsCount, r.excused)
	list := func(name string, events []parityEvent) {
		fmt.Fprintf(&b, "only in %s (%d):\n", name, len(events))
		for i, e := range events {
			if i == maxListedDiffs {
				fmt.Fprintf(&b, "  ... %d more\n", len(events)-maxListedDiffs)
				break
			}
			fmt.Fprintf(&b, "  %s\n", e)
		}
	}
	list(string(sideUpw), r.onlyUpw)
	list(string(sideGitPkgs), r.onlyGitPkgs)
	for _, x := range r.unused {
		fmt.Fprintf(&b, "exception matches no difference: %+v\n", x)
	}
	return b.String()
}

func TestParity(t *testing.T) {
	if _, err := exec.LookPath("git-pkgs"); err != nil {
		t.Skipf("git-pkgs not on PATH, timeline parity not checked: %v", err)
	}
	for _, fx := range parityFixtures {
		t.Run(fx.slug, func(t *testing.T) {
			env := parityEnv(t)
			clone := filepath.Join(t.TempDir(), fx.slug)
			runTool(t, env, "", "git", "clone", "--quiet", "--", fx.url, clone)
			runTool(t, env, clone, "git", "reset", "--hard", "--quiet", fx.commit)

			firstParent := revSet(t, env, clone, "--first-parent", "HEAD")
			merges := revSet(t, env, clone, "--first-parent", "--merges", "HEAD")

			runTool(t, env, clone, "git-pkgs", "init")
			var raw []gitPkgsEvent
			if err := json.Unmarshal(runTool(t, env, clone, "git-pkgs", "history", "--format", "json"), &raw); err != nil {
				t.Fatalf("decode git pkgs history output: %v", err)
			}

			logger := slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelWarn}))
			ext := extract.New(extract.Options{DeriveToolchainFromImages: false}, logger)
			handled := func(path string) bool {
				_, err := ext.Parse(path, nil)
				return !errors.Is(err, model.ErrUnsupportedFile)
			}
			theirs, err := normaliseGitPkgs(raw, firstParent, merges, handled)
			if err != nil {
				t.Fatal(err)
			}
			ours := normaliseUpw(indexWithUpw(t, logger, ext, fx.slug, clone), merges)

			r := compareParity(fx.slug, ours, theirs, parityExceptions)
			t.Logf("%s: %d first-parent commits, %d merges; git-pkgs %d raw events; upw %d, git-pkgs %d normalised; %d excused",
				fx.slug, len(firstParent), len(merges), len(raw), r.upwCount, r.gitPkgsCount, r.excused)
			if r.upwCount < minParityEvents || r.gitPkgsCount < minParityEvents {
				t.Errorf("too few events to compare: upw %d, git-pkgs %d normalised, want at least %d on each side",
					r.upwCount, r.gitPkgsCount, minParityEvents)
			}
			if !r.ok() {
				t.Errorf("timeline differs from git pkgs history\n%s", r.report(fx.slug))
			}
		})
	}
}

// indexWithUpw syncs the clone at dir through its file:// URL with the
// ingest job, builds its timeline with the timeline job and returns the
// stored events.
func indexWithUpw(t *testing.T, logger *slog.Logger, ext *extract.Extractor, slug, dir string) []model.DepEvent {
	t.Helper()
	ctx := t.Context()

	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "upw.sqlite"), logger)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	git := gitsrc.New(gitsrc.Options{
		ReposDir:      t.TempDir(),
		HomeDir:       t.TempDir(),
		AllowFileURLs: true,
	}, logger)
	targets := []model.TargetRepo{{Slug: slug, URL: "file://" + filepath.ToSlash(dir)}}
	if _, err := ingest.NewService(git, db, ingest.Options{Targets: targets}, logger).Run(ctx, model.Scope{}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	tl := timeline.NewService(git, db, db, []timeline.Extractor{ext}, unity.New(),
		timeline.Options{Targets: []timeline.TargetSettings{{Slug: slug}}}, logger)
	stats, err := tl.Run(ctx, model.Scope{})
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	t.Logf("%s: timeline stats %v", slug, stats)

	repo, err := db.GetRepo(ctx, slug)
	if err != nil {
		t.Fatalf("get repo: %v", err)
	}
	events, err := db.ListEvents(ctx, repo.ID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	return events
}

// parityEnv is the environment git and git-pkgs run with: the caller's,
// with an empty HOME and no system or global git config, so local settings
// cannot change what either side sees.
func parityEnv(t *testing.T) []string {
	t.Helper()
	home := t.TempDir()
	return append(os.Environ(),
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
	)
}

// runTool runs name with args in dir and returns its standard output. Any
// failure fails the test.
func runTool(t *testing.T, env []string, dir, name string, args ...string) []byte {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, stderr.String())
	}
	return stdout.Bytes()
}

// revSet returns the commits git rev-list prints for args in the clone at
// dir, as a set.
func revSet(t *testing.T, env []string, dir string, args ...string) map[string]bool {
	t.Helper()
	out := runTool(t, env, dir, "git", append([]string{"rev-list"}, args...)...)
	set := map[string]bool{}
	for line := range strings.Lines(string(out)) {
		if sha := strings.TrimSpace(line); sha != "" {
			set[sha] = true
		}
	}
	return set
}

func TestParityCompare(t *testing.T) {
	t.Parallel()

	const (
		fp1   = "1111111111111111111111111111111111111111"
		fp2   = "2222222222222222222222222222222222222222"
		merge = "3333333333333333333333333333333333333333"
		side  = "4444444444444444444444444444444444444444"
	)
	firstParent := map[string]bool{fp1: true, fp2: true, merge: true}
	merges := map[string]bool{merge: true}
	handled := func(path string) bool {
		_, err := extract.New(extract.Options{}, slog.New(slog.DiscardHandler)).Parse(path, nil)
		return !errors.Is(err, model.ErrUnsupportedFile)
	}

	raw := []gitPkgsEvent{
		{SHA: fp1, ManifestPath: "go.mod", Ecosystem: "golang", Name: "example.com/lib", ChangeType: "added", Requirement: "v1.0.0"},
		{SHA: fp2, ManifestPath: "go.mod", Ecosystem: "golang", Name: "example.com/lib", ChangeType: "modified", Requirement: "v1.1.0"},
		{SHA: fp2, ManifestPath: "go.mod", Ecosystem: "golang", Name: "example.com/old", ChangeType: "removed", Requirement: "v0.9.0"},
		// Dropped: a branch commit, a merge, an ecosystem upstreamwatch does
		// not index, and a manifest path no extractor handles.
		{SHA: side, ManifestPath: "go.mod", Ecosystem: "golang", Name: "example.com/branch", ChangeType: "added", Requirement: "v1.0.0"},
		{SHA: merge, ManifestPath: "go.mod", Ecosystem: "golang", Name: "example.com/merged", ChangeType: "added", Requirement: "v1.0.0"},
		{SHA: fp1, ManifestPath: "package.json", Ecosystem: "npm", Name: "left-pad", ChangeType: "added", Requirement: "^1.0.0"},
		{SHA: fp1, ManifestPath: "Pipfile", Ecosystem: "pypi", Name: "requests", ChangeType: "added", Requirement: "*"},
	}
	upw := []model.DepEvent{
		{CommitSHA: fp1, ManifestPath: "go.mod", Package: model.Package{PURL: "pkg:golang/example.com/lib", Ecosystem: "golang", Name: "example.com/lib"}, Change: model.ChangeAdded, Requirement: "v1.0.0"},
		{CommitSHA: fp1, ManifestPath: "go.mod", Package: model.Package{PURL: toolchainPURL, Ecosystem: "golang", Name: "toolchain"}, Change: model.ChangeAdded, Requirement: "1.27"},
		{CommitSHA: fp2, ManifestPath: "go.mod", Package: model.Package{PURL: "pkg:golang/example.com/lib", Ecosystem: "golang", Name: "example.com/lib"}, Change: model.ChangeChanged, Requirement: "v1.1.0"},
		{CommitSHA: fp2, ManifestPath: "go.mod", Package: model.Package{PURL: "pkg:golang/example.com/old", Ecosystem: "golang", Name: "example.com/old"}, Change: model.ChangeRemoved, Requirement: "v0.9.0"},
		{CommitSHA: merge, ManifestPath: "go.mod", Package: model.Package{PURL: "pkg:golang/example.com/merged", Ecosystem: "golang", Name: "example.com/merged"}, Change: model.ChangeAdded, Requirement: "v1.0.0"},
	}

	theirs, err := normaliseGitPkgs(raw, firstParent, merges, handled)
	if err != nil {
		t.Fatalf("normaliseGitPkgs: %v", err)
	}
	ours := normaliseUpw(upw, merges)
	wantNormalised := []parityEvent{
		{SHA: fp1, ManifestPath: "go.mod", Ecosystem: "golang", Name: "example.com/lib", Change: model.ChangeAdded, Requirement: "v1.0.0"},
		{SHA: fp2, ManifestPath: "go.mod", Ecosystem: "golang", Name: "example.com/lib", Change: model.ChangeChanged, Requirement: "v1.1.0"},
		{SHA: fp2, ManifestPath: "go.mod", Ecosystem: "golang", Name: "example.com/old", Change: model.ChangeRemoved},
	}
	if !slices.Equal(theirs, wantNormalised) {
		t.Errorf("normaliseGitPkgs = %v, want %v", theirs, wantNormalised)
	}
	if !slices.Equal(ours, wantNormalised) {
		t.Errorf("normaliseUpw = %v, want %v", ours, wantNormalised)
	}

	if _, err := normaliseGitPkgs([]gitPkgsEvent{{SHA: fp1, ManifestPath: "go.mod", Ecosystem: "golang", ChangeType: "renamed"}},
		firstParent, merges, handled); err == nil {
		t.Errorf("normaliseGitPkgs(unknown change_type) error = nil, want an error")
	}

	changedReq := parityEvent{SHA: fp2, ManifestPath: "go.mod", Ecosystem: "golang", Name: "example.com/lib", Change: model.ChangeChanged, Requirement: "v1.2.0"}
	dup := wantNormalised[0]

	tests := []struct {
		name            string
		upw, gitPkgs    []parityEvent
		exceptions      []parityException
		wantOK          bool
		wantOnlyUpw     []parityEvent
		wantOnlyGitPkgs []parityEvent
		wantExcused     int
		wantUnused      int
	}{
		{
			name:    "equal",
			upw:     wantNormalised,
			gitPkgs: wantNormalised,
			wantOK:  true,
		},
		{
			name:            "requirement differs",
			upw:             []parityEvent{wantNormalised[0], changedReq},
			gitPkgs:         wantNormalised[:2],
			wantOnlyUpw:     []parityEvent{changedReq},
			wantOnlyGitPkgs: []parityEvent{wantNormalised[1]},
		},
		{
			name:        "multiset counts duplicates",
			upw:         []parityEvent{dup, dup},
			gitPkgs:     []parityEvent{dup},
			wantOnlyUpw: []parityEvent{dup},
		},
		{
			name:    "exception excuses its side only",
			upw:     []parityEvent{wantNormalised[0], changedReq},
			gitPkgs: wantNormalised[:2],
			exceptions: []parityException{
				{Fixture: "fx", Side: sideUpw, ManifestPath: "go.mod", Name: "example.com/lib", Requirement: "v1.2.0", Reason: "test"},
			},
			wantOnlyGitPkgs: []parityEvent{wantNormalised[1]},
			wantExcused:     1,
		},
		{
			name:    "exceptions excuse both sides",
			upw:     []parityEvent{wantNormalised[0], changedReq},
			gitPkgs: wantNormalised[:2],
			exceptions: []parityException{
				{Fixture: "fx", Side: sideUpw, SHA: fp2, Reason: "test"},
				{Fixture: "fx", Side: sideGitPkgs, SHA: fp2, Reason: "test"},
			},
			wantOK:      true,
			wantExcused: 2,
		},
		{
			name:    "exception that matches nothing fails",
			upw:     wantNormalised,
			gitPkgs: wantNormalised,
			exceptions: []parityException{
				{Fixture: "fx", Side: sideUpw, Name: "example.com/never", Reason: "test"},
			},
			wantUnused: 1,
		},
		{
			name:    "other fixture's exception is ignored",
			upw:     []parityEvent{dup},
			gitPkgs: nil,
			exceptions: []parityException{
				{Fixture: "other", Side: sideUpw, Reason: "test"},
			},
			wantOnlyUpw: []parityEvent{dup},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := compareParity("fx", tt.upw, tt.gitPkgs, tt.exceptions)
			if got.ok() != tt.wantOK {
				t.Errorf("ok() = %v, want %v\n%s", got.ok(), tt.wantOK, got.report("fx"))
			}
			if !slices.Equal(got.onlyUpw, tt.wantOnlyUpw) {
				t.Errorf("onlyUpw = %v, want %v", got.onlyUpw, tt.wantOnlyUpw)
			}
			if !slices.Equal(got.onlyGitPkgs, tt.wantOnlyGitPkgs) {
				t.Errorf("onlyGitPkgs = %v, want %v", got.onlyGitPkgs, tt.wantOnlyGitPkgs)
			}
			if got.excused != tt.wantExcused {
				t.Errorf("excused = %d, want %d", got.excused, tt.wantExcused)
			}
			if len(got.unused) != tt.wantUnused {
				t.Errorf("len(unused) = %d, want %d", len(got.unused), tt.wantUnused)
			}
		})
	}
}

func TestParityPackageName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		ecosystem, name, want string
	}{
		{"pypi", "Jinja2", "jinja2"},
		{"pypi", "zope.interface", "zope-interface"},
		{"pypi", "Typing_Extensions", "typing-extensions"},
		{"pypi", "a__b-.c", "a-b-c"},
		{"golang", "github.com/BurntSushi/toml", "github.com/BurntSushi/toml"},
		{"docker", "Library/Some_Image", "Library/Some_Image"},
	}
	for _, tt := range tests {
		t.Run(tt.ecosystem+"/"+tt.name, func(t *testing.T) {
			t.Parallel()
			if got := packageName(tt.ecosystem, tt.name); got != tt.want {
				t.Errorf("packageName(%q, %q) = %q, want %q", tt.ecosystem, tt.name, got, tt.want)
			}
		})
	}
}

func TestParityNormalisePyPIRespellings(t *testing.T) {
	t.Parallel()

	const (
		fp1 = "1111111111111111111111111111111111111111"
		fp2 = "2222222222222222222222222222222222222222"
	)
	firstParent := map[string]bool{fp1: true, fp2: true}
	handled := func(string) bool { return true }

	raw := []gitPkgsEvent{
		// Respelled with the same version: no event.
		{SHA: fp1, ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "Jinja2", ChangeType: "removed", Requirement: "3.1.2"},
		{SHA: fp1, ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "jinja2", ChangeType: "added", Requirement: "3.1.2"},
		// Respelled with a new version: one changed.
		{SHA: fp1, ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "Babel", ChangeType: "removed", Requirement: "2.10.3"},
		{SHA: fp1, ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "babel", ChangeType: "added", Requirement: "2.11.0"},
		// A lone addition only has its name normalised.
		{SHA: fp1, ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "Zope.Interface", ChangeType: "added", Requirement: "5.0"},
		// Removal and addition in different manifests stay apart.
		{SHA: fp2, ManifestPath: "requirements.txt", Ecosystem: "pypi", Name: "Flask", ChangeType: "removed", Requirement: "==2.0"},
		{SHA: fp2, ManifestPath: "pyproject.toml", Ecosystem: "pypi", Name: "flask", ChangeType: "added", Requirement: "^2.0"},
		// Other ecosystems keep their names and are never collapsed.
		{SHA: fp2, ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/BurntSushi/toml", ChangeType: "removed", Requirement: "v1.0.0"},
		{SHA: fp2, ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/burntsushi/toml", ChangeType: "added", Requirement: "v1.0.0"},
	}
	got, err := normaliseGitPkgs(raw, firstParent, nil, handled)
	if err != nil {
		t.Fatalf("normaliseGitPkgs: %v", err)
	}
	want := []parityEvent{
		{SHA: fp1, ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "babel", Change: model.ChangeChanged, Requirement: "2.11.0"},
		{SHA: fp1, ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "zope-interface", Change: model.ChangeAdded, Requirement: "5.0"},
		{SHA: fp2, ManifestPath: "requirements.txt", Ecosystem: "pypi", Name: "flask", Change: model.ChangeRemoved},
		{SHA: fp2, ManifestPath: "pyproject.toml", Ecosystem: "pypi", Name: "flask", Change: model.ChangeAdded, Requirement: "^2.0"},
		{SHA: fp2, ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/BurntSushi/toml", Change: model.ChangeRemoved},
		{SHA: fp2, ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/burntsushi/toml", Change: model.ChangeAdded, Requirement: "v1.0.0"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("normaliseGitPkgs =\n%v\nwant\n%v", got, want)
	}

	upw := []model.DepEvent{
		{CommitSHA: fp1, ManifestPath: "poetry.lock", Package: model.Package{PURL: "pkg:pypi/babel", Ecosystem: "pypi", Name: "Babel"}, Change: model.ChangeChanged, Requirement: "2.11.0"},
		{CommitSHA: fp2, ManifestPath: "go.mod", Package: model.Package{PURL: "pkg:golang/github.com/BurntSushi/toml", Ecosystem: "golang", Name: "github.com/BurntSushi/toml"}, Change: model.ChangeAdded, Requirement: "v1.0.0"},
	}
	wantUpw := []parityEvent{
		{SHA: fp1, ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "babel", Change: model.ChangeChanged, Requirement: "2.11.0"},
		{SHA: fp2, ManifestPath: "go.mod", Ecosystem: "golang", Name: "github.com/BurntSushi/toml", Change: model.ChangeAdded, Requirement: "v1.0.0"},
	}
	if gotUpw := normaliseUpw(upw, nil); !slices.Equal(gotUpw, wantUpw) {
		t.Errorf("normaliseUpw =\n%v\nwant\n%v", gotUpw, wantUpw)
	}
}

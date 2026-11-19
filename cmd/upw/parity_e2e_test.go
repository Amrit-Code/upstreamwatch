//go:build e2e

package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/extract"
	"github.com/Amrit-Code/upstreamwatch/internal/extract/unity"
	"github.com/Amrit-Code/upstreamwatch/internal/gitsrc"
	"github.com/Amrit-Code/upstreamwatch/internal/ingest"
	"github.com/Amrit-Code/upstreamwatch/internal/model"
	"github.com/Amrit-Code/upstreamwatch/internal/store"
	"github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo"
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
	// cleo, merge 765562a8 (net change of 36993d5f): poetry.lock lists
	// coverage (4.5.4, then 5.0.4) and importlib-metadata (0.23, then 1.5.2)
	// twice; only the second entry moved (to 5.1 and 1.6.0), so the first,
	// which upstreamwatch keeps, is unchanged at the merge.
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "765562a8421e5a9016ddff7e75fb88f5ec7fee28", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "coverage", Change: model.ChangeChanged, Requirement: "5.1", Reason: reasonFirstWins},
	{Fixture: "cleo", Side: sideGitPkgs, SHA: "765562a8421e5a9016ddff7e75fb88f5ec7fee28", ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "importlib-metadata", Change: model.ChangeChanged, Requirement: "1.6.0", Reason: reasonFirstWins},

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

// minMergeEvents is the fewest merge-dated events each side must have per
// fixture, so a run in which the merge check compares nothing cannot pass.
// Both fixtures have a first-parent merge that brought in a dependency change.
const minMergeEvents = 1

// minHeadDeps is the fewest dependencies the state at HEAD must hold per
// fixture, so a HEAD check in which both sides are empty cannot pass.
const minHeadDeps = 10

// gitPkgsEvent is one object of git pkgs history --format json, keeping only
// the fields the comparison reads. For a removal git-pkgs puts the
// requirement the package had in Requirement and leaves PreviousRequirement
// empty.
type gitPkgsEvent struct {
	SHA                 string `json:"sha"`
	Name                string `json:"name"`
	Ecosystem           string `json:"ecosystem"`
	ChangeType          string `json:"change_type"`
	Requirement         string `json:"requirement"`
	PreviousRequirement string `json:"previous_requirement"`
	ManifestPath        string `json:"manifest_path"`
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
	return toParityEvents(collapseRespellings(keepIndexed(events, handled, func(sha string) bool {
		return firstParent[sha] && !merges[sha]
	})))
}

// keepIndexed returns the events whose commit keepSHA accepts, in an
// ecosystem upstreamwatch indexes and a manifest path it handles, with PyPI
// names normalised, in their order.
func keepIndexed(events []gitPkgsEvent, handled func(path string) bool, keepSHA func(sha string) bool) []gitPkgsEvent {
	var kept []gitPkgsEvent
	for _, e := range events {
		if !keepSHA(e.SHA) || !slices.Contains(parityEcosystems, e.Ecosystem) || !handled(e.ManifestPath) {
			continue
		}
		e.Name = packageName(e.Ecosystem, e.Name)
		kept = append(kept, e)
	}
	return kept
}

// normaliseMerge returns the net change the commits a first-parent merge
// brought in made, dated at the merge, for comparison with upstreamwatch's
// events at the merge. brought lists those commits (reachable from the
// merge's second parent and not from its first) in topological order,
// oldest first. The events of those commits are filtered and named as
// normaliseGitPkgs does, respellings are collapsed per commit, and the rest
// are netted per manifest and package by collapseMerge.
func normaliseMerge(events []gitPkgsEvent, merge string, brought []string, handled func(path string) bool) ([]parityEvent, error) {
	pos := make(map[string]int, len(brought))
	for i, sha := range brought {
		pos[sha] = i
	}
	kept := keepIndexed(events, handled, func(sha string) bool {
		_, ok := pos[sha]
		return ok
	})
	// git-pkgs prints commits in its own order; the net change needs them in
	// commit order. A stable sort keeps each commit's events in file order.
	slices.SortStableFunc(kept, func(a, b gitPkgsEvent) int { return cmp.Compare(pos[a.SHA], pos[b.SHA]) })
	collapsed, err := collapseMerge(merge, collapseRespellings(kept))
	if err != nil {
		return nil, err
	}
	return toParityEvents(collapsed)
}

// collapseMerge nets events, in commit order and with normalised names, into
// at most one event per manifest path, ecosystem and name, carrying SHA
// merge. The package's state before the events is inferred from its first
// event: absent before an added, present with the previous requirement
// before a modified or removed. Its state after is that of its last event.
// Absent to present is added at the last requirement; present to absent is
// removed; present to present is modified from the first previous to the
// last requirement, or nothing when the two are equal; absent to absent (added
// then removed) is nothing. The result is ordered by first appearance. An
// unknown change type is an error.
func collapseMerge(merge string, events []gitPkgsEvent) ([]gitPkgsEvent, error) {
	type key struct{ path, ecosystem, name string }
	type net struct {
		first, last gitPkgsEvent
	}
	var order []key
	nets := map[key]*net{}
	for _, e := range events {
		if !slices.Contains([]string{"added", "modified", "removed"}, e.ChangeType) {
			return nil, fmt.Errorf("git-pkgs event %s %s %s: unknown change_type %q", e.SHA, e.ManifestPath, e.Name, e.ChangeType)
		}
		k := key{e.ManifestPath, e.Ecosystem, e.Name}
		if n, ok := nets[k]; ok {
			n.last = e
			continue
		}
		nets[k] = &net{first: e, last: e}
		order = append(order, k)
	}

	var out []gitPkgsEvent
	for _, k := range order {
		n := nets[k]
		before := n.first.ChangeType != "added"
		// A removal carries the old requirement in Requirement.
		beforeReq := cmp.Or(n.first.PreviousRequirement, n.first.Requirement)
		if n.first.ChangeType == "modified" {
			beforeReq = n.first.PreviousRequirement
		}
		after := n.last.ChangeType != "removed"
		afterReq := n.last.Requirement

		e := gitPkgsEvent{SHA: merge, ManifestPath: k.path, Ecosystem: k.ecosystem, Name: k.name}
		switch {
		case !before && !after:
			continue
		case !before:
			e.ChangeType, e.Requirement = "added", afterReq
		case !after:
			e.ChangeType, e.Requirement = "removed", beforeReq
		case beforeReq == afterReq:
			continue
		default:
			e.ChangeType, e.PreviousRequirement, e.Requirement = "modified", beforeReq, afterReq
		}
		out = append(out, e)
	}
	return out, nil
}

// toParityEvents maps git-pkgs events to the compared form: modified becomes
// changed, and a removal's requirement is dropped.
func toParityEvents(events []gitPkgsEvent) ([]parityEvent, error) {
	var out []parityEvent
	for _, e := range events {
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

// normaliseUpw drops upstreamwatch's events for the toolchain
// pseudo-package and normalises PyPI names. Events at merges are kept: they
// are compared with the net change normaliseMerge computes.
func normaliseUpw(events []model.DepEvent) []parityEvent {
	var out []parityEvent
	for _, e := range events {
		if e.Package.PURL == toolchainPURL {
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
			// Each merge on the first-parent chain is compared as the net
			// change of the commits it brought in.
			var theirsAtMerges int
			for m := range merges {
				brought := revList(t, env, clone, "--topo-order", "--reverse", m+"^2", "^"+m+"^1")
				net, err := normaliseMerge(raw, m, brought, handled)
				if err != nil {
					t.Fatal(err)
				}
				theirsAtMerges += len(net)
				theirs = append(theirs, net...)
			}
			events, intervals := indexWithUpw(t, logger, ext, fx.slug, clone)
			ours := normaliseUpw(events)
			oursAtMerges := 0
			for _, e := range ours {
				if merges[e.SHA] {
					oursAtMerges++
				}
			}

			r := compareParity(fx.slug, ours, theirs, parityExceptions)
			t.Logf("%s: %d first-parent commits, %d merges; git-pkgs %d raw events; upw %d, git-pkgs %d normalised; %d excused; at merges: upw %d, git-pkgs %d net, %d differing",
				fx.slug, len(firstParent), len(merges), len(raw), r.upwCount, r.gitPkgsCount, r.excused,
				oursAtMerges, theirsAtMerges, r.differingAt(merges))
			if r.upwCount < minParityEvents || r.gitPkgsCount < minParityEvents {
				t.Errorf("too few events to compare: upw %d, git-pkgs %d normalised, want at least %d on each side",
					r.upwCount, r.gitPkgsCount, minParityEvents)
			}
			if oursAtMerges < minMergeEvents || theirsAtMerges < minMergeEvents {
				t.Errorf("too few merge-dated events to compare: upw %d, git-pkgs %d net, want at least %d on each side",
					oursAtMerges, theirsAtMerges, minMergeEvents)
			}
			if !r.ok() {
				t.Errorf("timeline differs from git pkgs history\n%s", r.report(fx.slug))
			}

			// The state at HEAD, parsed straight from the tree, must equal
			// the open intervals: independent of the walk, so a stale state
			// fails here even when every event matches.
			upwHead, err := headStateFromIntervals(intervals)
			if err != nil {
				t.Errorf("open intervals at HEAD: %v", err)
			}
			treeHead := headStateFromTree(t, env, clone, fx.commit, ext)
			t.Logf("%s: state at HEAD: upw %d open intervals, tree %d dependencies", fx.slug, len(upwHead), len(treeHead))
			if len(upwHead) < minHeadDeps || len(treeHead) < minHeadDeps {
				t.Errorf("too few dependencies at HEAD to compare: upw %d, tree %d, want at least %d on each side",
					len(upwHead), len(treeHead), minHeadDeps)
			}
			if diffs := compareHeadState(upwHead, treeHead); len(diffs) > 0 {
				t.Errorf("open intervals differ from the manifests at HEAD %s (%d differences):\n%s",
					fx.commit, len(diffs), listCapped(diffs))
			}
		})
	}
}

// differingAt counts the differing events no exception excused whose commit
// is in shas.
func (r parityResult) differingAt(shas map[string]bool) int {
	n := 0
	for _, e := range slices.Concat(r.onlyUpw, r.onlyGitPkgs) {
		if shas[e.SHA] {
			n++
		}
	}
	return n
}

// headKey identifies one dependency in the state at HEAD.
type headKey struct {
	ManifestPath string
	PURL         string
}

// headDep is what the state at HEAD compares for one dependency.
type headDep struct {
	Version     string
	Requirement string
}

// headStateFromIntervals returns the open intervals as the state at HEAD. Two
// open intervals for one manifest and package are an error.
func headStateFromIntervals(intervals []model.DepInterval) (map[headKey]headDep, error) {
	st := map[headKey]headDep{}
	var errs []error
	for _, iv := range intervals {
		if !iv.IsOpen() {
			continue
		}
		k := headKey{iv.ManifestPath, iv.Package.PURL}
		if _, ok := st[k]; ok {
			errs = append(errs, fmt.Errorf("%s %s: more than one open interval", k.ManifestPath, k.PURL))
			continue
		}
		st[k] = headDep{Version: iv.Version, Requirement: iv.Requirement}
	}
	return st, errors.Join(errs...)
}

// paritySkippedDirs are the directory names whose manifests the timeline
// never indexes, at any depth (docs/decisions.md, "Lead decisions for M1").
// The fixtures configure no exclude_paths.
var paritySkippedDirs = []string{"testdata", "vendor", "node_modules"}

// parityExcluded reports whether one of the slash-separated path's directory
// segments, not its file name, is in paritySkippedDirs.
func parityExcluded(path string) bool {
	segments := strings.Split(path, "/")
	for _, dir := range segments[:len(segments)-1] {
		if slices.Contains(paritySkippedDirs, dir) {
			return true
		}
	}
	return false
}

// headStateFromTree parses every file at commit in the clone at dir that the
// extractor handles and the timeline does not exclude, read with git show,
// and returns the dependencies keyed by manifest path and package URL,
// keeping the first occurrence of a package URL in each file. A file that
// fails to parse contributes nothing and is logged.
func headStateFromTree(t *testing.T, env []string, dir, commit string, ext *extract.Extractor) map[headKey]headDep {
	t.Helper()
	st := map[headKey]headDep{}
	out := runTool(t, env, dir, "git", "ls-tree", "-r", "-z", "--full-tree", commit)
	for entry := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		// "<mode> <type> <object>\t<path>"
		meta, path, ok := strings.Cut(entry, "\t")
		if !ok {
			t.Fatalf("git ls-tree: unexpected entry %q", entry)
		}
		if fields := strings.Fields(meta); len(fields) != 3 || fields[1] != "blob" {
			continue
		}
		if parityExcluded(path) {
			continue
		}
		if _, err := ext.Parse(path, nil); errors.Is(err, model.ErrUnsupportedFile) {
			continue
		}
		content := runTool(t, env, dir, "git", "show", "--no-textconv", commit+":"+path)
		pm, err := ext.Parse(path, content)
		if err != nil {
			t.Logf("%s at %s does not parse, contributes nothing: %v", path, commit, err)
			continue
		}
		for _, d := range pm.Dependencies {
			k := headKey{path, d.Package.PURL}
			if _, ok := st[k]; ok {
				continue
			}
			st[k] = headDep{Version: d.Version, Requirement: d.Requirement}
		}
	}
	return st
}

// compareHeadState returns one sorted line per dependency that differs
// between upw's state at HEAD and the state parsed from the tree.
func compareHeadState(upw, tree map[headKey]headDep) []string {
	var diffs []string
	for k, u := range upw {
		tr, ok := tree[k]
		switch {
		case !ok:
			diffs = append(diffs, fmt.Sprintf("%s %s: only in upw (version %q, requirement %q)", k.ManifestPath, k.PURL, u.Version, u.Requirement))
		case u != tr:
			diffs = append(diffs, fmt.Sprintf("%s %s: upw version %q requirement %q, tree version %q requirement %q",
				k.ManifestPath, k.PURL, u.Version, u.Requirement, tr.Version, tr.Requirement))
		}
	}
	for k, tr := range tree {
		if _, ok := upw[k]; !ok {
			diffs = append(diffs, fmt.Sprintf("%s %s: only in tree (version %q, requirement %q)", k.ManifestPath, k.PURL, tr.Version, tr.Requirement))
		}
	}
	slices.Sort(diffs)
	return diffs
}

// listCapped indents lines, one per line, listing at most maxListedDiffs.
func listCapped(lines []string) string {
	var b strings.Builder
	for i, l := range lines {
		if i == maxListedDiffs {
			fmt.Fprintf(&b, "  ... %d more\n", len(lines)-maxListedDiffs)
			break
		}
		fmt.Fprintf(&b, "  %s\n", l)
	}
	return b.String()
}

// indexWithUpw syncs the clone at dir through its file:// URL with the
// ingest job, builds its timeline with the timeline job and returns the
// stored events and intervals.
func indexWithUpw(t *testing.T, logger *slog.Logger, ext *extract.Extractor, slug, dir string) ([]model.DepEvent, []model.DepInterval) {
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
	intervals, err := db.ListIntervals(ctx, repo.ID)
	if err != nil {
		t.Fatalf("list intervals: %v", err)
	}
	return events, intervals
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
	set := map[string]bool{}
	for _, sha := range revList(t, env, dir, args...) {
		set[sha] = true
	}
	return set
}

// revList returns the commits git rev-list prints for args in the clone at
// dir, in its order.
func revList(t *testing.T, env []string, dir string, args ...string) []string {
	t.Helper()
	out := runTool(t, env, dir, "git", append([]string{"rev-list"}, args...)...)
	var shas []string
	for line := range strings.Lines(string(out)) {
		if sha := strings.TrimSpace(line); sha != "" {
			shas = append(shas, sha)
		}
	}
	return shas
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
	ours := normaliseUpw(upw)
	wantNormalised := []parityEvent{
		{SHA: fp1, ManifestPath: "go.mod", Ecosystem: "golang", Name: "example.com/lib", Change: model.ChangeAdded, Requirement: "v1.0.0"},
		{SHA: fp2, ManifestPath: "go.mod", Ecosystem: "golang", Name: "example.com/lib", Change: model.ChangeChanged, Requirement: "v1.1.0"},
		{SHA: fp2, ManifestPath: "go.mod", Ecosystem: "golang", Name: "example.com/old", Change: model.ChangeRemoved},
	}
	if !slices.Equal(theirs, wantNormalised) {
		t.Errorf("normaliseGitPkgs = %v, want %v", theirs, wantNormalised)
	}
	// upstreamwatch's event at the merge is kept, for comparison with the
	// net change of the commits the merge brought in; only the toolchain
	// pseudo-package is dropped.
	wantUpw := append(slices.Clone(wantNormalised),
		parityEvent{SHA: merge, ManifestPath: "go.mod", Ecosystem: "golang", Name: "example.com/merged", Change: model.ChangeAdded, Requirement: "v1.0.0"})
	if !slices.Equal(ours, wantUpw) {
		t.Errorf("normaliseUpw = %v, want %v", ours, wantUpw)
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
	if gotUpw := normaliseUpw(upw); !slices.Equal(gotUpw, wantUpw) {
		t.Errorf("normaliseUpw =\n%v\nwant\n%v", gotUpw, wantUpw)
	}
}

func TestParityCollapseMerge(t *testing.T) {
	t.Parallel()

	const (
		merge = "9999999999999999999999999999999999999999"
		b1    = "1111111111111111111111111111111111111111"
		b2    = "2222222222222222222222222222222222222222"
		b3    = "3333333333333333333333333333333333333333"
	)
	lib := func(sha, change, prev, req string) gitPkgsEvent {
		return gitPkgsEvent{SHA: sha, ManifestPath: "go.mod", Ecosystem: "golang", Name: "example.com/lib",
			ChangeType: change, PreviousRequirement: prev, Requirement: req}
	}
	atMerge := func(change, prev, req string) gitPkgsEvent { return lib(merge, change, prev, req) }

	tests := []struct {
		name   string
		events []gitPkgsEvent
		want   []gitPkgsEvent
	}{
		{
			// The package was absent before the first event, so the net
			// change is an addition, carrying the last version.
			name:   "added then changed is one addition at the last version",
			events: []gitPkgsEvent{lib(b1, "added", "", "v1.0.0"), lib(b2, "modified", "v1.0.0", "v1.1.0")},
			want:   []gitPkgsEvent{atMerge("added", "", "v1.1.0")},
		},
		{
			name:   "added then removed cancels",
			events: []gitPkgsEvent{lib(b1, "added", "", "v1.0.0"), lib(b2, "removed", "", "v1.0.0")},
			want:   nil,
		},
		{
			name:   "two changes are one change from the first previous to the last version",
			events: []gitPkgsEvent{lib(b1, "modified", "v1.0.0", "v1.1.0"), lib(b2, "modified", "v1.1.0", "v1.2.0")},
			want:   []gitPkgsEvent{atMerge("modified", "v1.0.0", "v1.2.0")},
		},
		{
			name:   "changes back to the first version cancel",
			events: []gitPkgsEvent{lib(b1, "modified", "v1.0.0", "v1.1.0"), lib(b2, "modified", "v1.1.0", "v1.0.0")},
			want:   nil,
		},
		{
			name:   "changed then removed is a removal",
			events: []gitPkgsEvent{lib(b1, "modified", "v1.0.0", "v1.1.0"), lib(b2, "removed", "", "v1.1.0")},
			want:   []gitPkgsEvent{atMerge("removed", "", "v1.0.0")},
		},
		{
			name:   "removed then added at another version is a change",
			events: []gitPkgsEvent{lib(b1, "removed", "", "v1.0.0"), lib(b2, "added", "", "v2.0.0")},
			want:   []gitPkgsEvent{atMerge("modified", "v1.0.0", "v2.0.0")},
		},
		{
			name:   "removed then added at the same version cancels",
			events: []gitPkgsEvent{lib(b1, "removed", "", "v1.0.0"), lib(b2, "added", "", "v1.0.0")},
			want:   nil,
		},
		{
			name:   "added, removed, added again is one addition at the last version",
			events: []gitPkgsEvent{lib(b1, "added", "", "v1.0.0"), lib(b2, "removed", "", "v1.0.0"), lib(b3, "added", "", "v1.2.0")},
			want:   []gitPkgsEvent{atMerge("added", "", "v1.2.0")},
		},
		{
			name:   "a single event keeps its change, dated at the merge",
			events: []gitPkgsEvent{lib(b1, "modified", "v1.0.0", "v1.1.0")},
			want:   []gitPkgsEvent{atMerge("modified", "v1.0.0", "v1.1.0")},
		},
		{
			name: "packages and manifests are netted apart, in order of first appearance",
			events: []gitPkgsEvent{
				{SHA: b1, ManifestPath: "requirements.txt", Ecosystem: "pypi", Name: "flask", ChangeType: "added", Requirement: "==2.0"},
				{SHA: b1, ManifestPath: "pyproject.toml", Ecosystem: "pypi", Name: "flask", ChangeType: "added", Requirement: "^2.0"},
				lib(b1, "added", "", "v1.0.0"),
				{SHA: b2, ManifestPath: "requirements.txt", Ecosystem: "pypi", Name: "flask", ChangeType: "modified", PreviousRequirement: "==2.0", Requirement: "==2.1"},
				{SHA: b2, ManifestPath: "pyproject.toml", Ecosystem: "pypi", Name: "flask", ChangeType: "removed", Requirement: "^2.0"},
			},
			want: []gitPkgsEvent{
				{SHA: merge, ManifestPath: "requirements.txt", Ecosystem: "pypi", Name: "flask", ChangeType: "added", Requirement: "==2.1"},
				atMerge("added", "", "v1.0.0"),
			},
		},
		{
			name:   "no events",
			events: nil,
			want:   nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := collapseMerge(merge, tt.events)
			if err != nil {
				t.Fatalf("collapseMerge(%v): %v", tt.events, err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("collapseMerge(%v) =\n%v\nwant\n%v", tt.events, got, tt.want)
			}
		})
	}

	if _, err := collapseMerge(merge, []gitPkgsEvent{lib(b1, "renamed", "", "")}); err == nil {
		t.Errorf("collapseMerge(unknown change_type) error = nil, want an error")
	}
}

func TestParityNormaliseMerge(t *testing.T) {
	t.Parallel()

	const (
		merge = "9999999999999999999999999999999999999999"
		b1    = "1111111111111111111111111111111111111111"
		b2    = "2222222222222222222222222222222222222222"
		other = "5555555555555555555555555555555555555555"
	)
	handled := func(path string) bool {
		_, err := extract.New(extract.Options{}, slog.New(slog.DiscardHandler)).Parse(path, nil)
		return !errors.Is(err, model.ErrUnsupportedFile)
	}
	// git-pkgs' own order puts b2 before b1; the net change follows commit
	// order, so lib is added at its last version.
	raw := []gitPkgsEvent{
		{SHA: b2, ManifestPath: "go.mod", Ecosystem: "golang", Name: "example.com/lib", ChangeType: "modified", PreviousRequirement: "v1.0.0", Requirement: "v1.1.0"},
		{SHA: b2, ManifestPath: "go.mod", Ecosystem: "golang", Name: "example.com/gone", ChangeType: "removed", Requirement: "v0.1.0"},
		{SHA: b1, ManifestPath: "go.mod", Ecosystem: "golang", Name: "example.com/lib", ChangeType: "added", Requirement: "v1.0.0"},
		// A respelling within one commit is one package left unchanged.
		{SHA: b1, ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "Jinja2", ChangeType: "removed", Requirement: "3.1.2"},
		{SHA: b1, ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "jinja2", ChangeType: "added", Requirement: "3.1.2"},
		// PyPI names are normalised.
		{SHA: b2, ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "Zope.Interface", ChangeType: "added", Requirement: "5.0"},
		// Dropped: a commit the merge did not bring in, an ecosystem
		// upstreamwatch does not index, a manifest path it does not handle.
		{SHA: other, ManifestPath: "go.mod", Ecosystem: "golang", Name: "example.com/elsewhere", ChangeType: "added", Requirement: "v1.0.0"},
		{SHA: b1, ManifestPath: "package.json", Ecosystem: "npm", Name: "left-pad", ChangeType: "added", Requirement: "^1.0.0"},
		{SHA: b1, ManifestPath: "Pipfile", Ecosystem: "pypi", Name: "requests", ChangeType: "added", Requirement: "*"},
	}
	got, err := normaliseMerge(raw, merge, []string{b1, b2}, handled)
	if err != nil {
		t.Fatalf("normaliseMerge: %v", err)
	}
	want := []parityEvent{
		{SHA: merge, ManifestPath: "go.mod", Ecosystem: "golang", Name: "example.com/lib", Change: model.ChangeAdded, Requirement: "v1.1.0"},
		{SHA: merge, ManifestPath: "go.mod", Ecosystem: "golang", Name: "example.com/gone", Change: model.ChangeRemoved},
		{SHA: merge, ManifestPath: "poetry.lock", Ecosystem: "pypi", Name: "zope-interface", Change: model.ChangeAdded, Requirement: "5.0"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("normaliseMerge =\n%v\nwant\n%v", got, want)
	}

	if got, err := normaliseMerge(raw, merge, nil, handled); err != nil || got != nil {
		t.Errorf("normaliseMerge(no commits brought in) = %v, %v, want nil, nil", got, err)
	}
	if _, err := normaliseMerge([]gitPkgsEvent{{SHA: b1, ManifestPath: "go.mod", Ecosystem: "golang", Name: "example.com/lib", ChangeType: "renamed"}},
		merge, []string{b1}, handled); err == nil {
		t.Errorf("normaliseMerge(unknown change_type) error = nil, want an error")
	}
}

func TestParityHeadStateFromIntervals(t *testing.T) {
	t.Parallel()

	day := func(d int) time.Time { return time.Date(2026, 3, d, 12, 0, 0, 0, time.UTC) }
	lib := model.Package{PURL: "pkg:golang/example.com/lib", Ecosystem: "golang", Name: "example.com/lib"}
	other := model.Package{PURL: "pkg:golang/example.com/other", Ecosystem: "golang", Name: "example.com/other"}

	intervals := []model.DepInterval{
		// Closed intervals are not in force at HEAD.
		{ManifestPath: "go.mod", Package: lib, Version: "v1.0.0", Requirement: "v1.0.0", FromSHA: "a", From: day(1), ToSHA: "b", To: day(2)},
		{ManifestPath: "go.mod", Package: lib, Version: "v1.1.0", Requirement: "v1.1.0", FromSHA: "b", From: day(2)},
		{ManifestPath: "tools/go.mod", Package: lib, Version: "v0.9.0", Requirement: "v0.9.0", FromSHA: "a", From: day(1)},
		{ManifestPath: "go.mod", Package: other, Version: "v2.0.0", Requirement: "v2.0.0", FromSHA: "a", From: day(1), ToSHA: "c", To: day(3)},
	}
	got, err := headStateFromIntervals(intervals)
	if err != nil {
		t.Fatalf("headStateFromIntervals: %v", err)
	}
	want := map[headKey]headDep{
		{"go.mod", lib.PURL}:       {Version: "v1.1.0", Requirement: "v1.1.0"},
		{"tools/go.mod", lib.PURL}: {Version: "v0.9.0", Requirement: "v0.9.0"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("headStateFromIntervals = %v, want %v", got, want)
	}

	twoOpen := append(slices.Clone(intervals),
		model.DepInterval{ManifestPath: "go.mod", Package: lib, Version: "v1.2.0", Requirement: "v1.2.0", FromSHA: "c", From: day(3)})
	if _, err := headStateFromIntervals(twoOpen); err == nil {
		t.Errorf("headStateFromIntervals(two open intervals for one package) error = nil, want an error")
	}

	if got, err := headStateFromIntervals(nil); err != nil || len(got) != 0 {
		t.Errorf("headStateFromIntervals(nil) = %v, %v, want empty, nil", got, err)
	}
}

func TestParityCompareHeadState(t *testing.T) {
	t.Parallel()

	lib := headKey{ManifestPath: "go.mod", PURL: "pkg:golang/example.com/lib"}
	img := headKey{ManifestPath: "Dockerfile", PURL: "pkg:docker/golang"}
	base := map[headKey]headDep{
		lib: {Version: "v1.1.0", Requirement: "v1.1.0"},
		img: {Version: "1.27", Requirement: "1.27"},
	}
	with := func(key headKey, d headDep) map[headKey]headDep {
		m := maps.Clone(base)
		m[key] = d
		return m
	}
	without := func(key headKey) map[headKey]headDep {
		m := maps.Clone(base)
		delete(m, key)
		return m
	}

	tests := []struct {
		name      string
		upw, tree map[headKey]headDep
		want      []string
	}{
		{name: "equal", upw: base, tree: maps.Clone(base), want: nil},
		{name: "both empty", upw: nil, tree: map[headKey]headDep{}, want: nil},
		{
			// A stale interval: the walk missed the last change.
			name: "version differs",
			upw:  with(lib, headDep{Version: "v1.0.0", Requirement: "v1.0.0"}),
			tree: base,
			want: []string{`go.mod pkg:golang/example.com/lib: upw version "v1.0.0" requirement "v1.0.0", tree version "v1.1.0" requirement "v1.1.0"`},
		},
		{
			name: "requirement differs",
			upw:  with(img, headDep{Version: "1.27", Requirement: "1.27@sha256:00"}),
			tree: base,
			want: []string{`Dockerfile pkg:docker/golang: upw version "1.27" requirement "1.27@sha256:00", tree version "1.27" requirement "1.27"`},
		},
		{
			name: "open interval for a dependency no longer in the tree",
			upw:  base,
			tree: without(lib),
			want: []string{`go.mod pkg:golang/example.com/lib: only in upw (version "v1.1.0", requirement "v1.1.0")`},
		},
		{
			name: "dependency in the tree with no open interval",
			upw:  without(img),
			tree: base,
			want: []string{`Dockerfile pkg:docker/golang: only in tree (version "1.27", requirement "1.27")`},
		},
		{
			name: "differences are sorted",
			upw:  without(lib),
			tree: without(img),
			want: []string{
				`Dockerfile pkg:docker/golang: only in upw (version "1.27", requirement "1.27")`,
				`go.mod pkg:golang/example.com/lib: only in tree (version "v1.1.0", requirement "v1.1.0")`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := compareHeadState(tt.upw, tt.tree); !slices.Equal(got, tt.want) {
				t.Errorf("compareHeadState =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestParityExcluded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want bool
	}{
		{"go.mod", false},
		{"tools/go.mod", false},
		{"testdata/go.mod", true},
		{"internal/x/testdata/requirements.txt", true},
		{"vendor/example.com/lib/go.mod", true},
		{"web/node_modules/pkg/Dockerfile", true},
		// Only directory segments count, and only whole segments.
		{"vendor", false},
		{"testdata2/go.mod", false},
		{"myvendor/go.mod", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			if got := parityExcluded(tt.path); got != tt.want {
				t.Errorf("parityExcluded(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestParityHeadStateFromTree(t *testing.T) {
	t.Parallel()

	day := func(d int) time.Time { return time.Date(2026, 3, d, 12, 0, 0, 0, time.UTC) }
	ext := extract.New(extract.Options{}, slog.New(slog.DiscardHandler))

	const (
		goMod1 = "module example.com/app\n\ngo 1.27\n\nrequire example.com/lib v1.2.0\n"
		goMod2 = "module example.com/app\n\ngo 1.27\n\nrequire example.com/lib v1.3.0\n"
		reqs   = "requests==2.31.0\nflask==3.0.0\n"
		broken = "module example.com/broken\n\nrequire (\n"
	)
	if _, err := ext.Parse("broken/go.mod", []byte(broken)); err == nil {
		t.Fatalf("precondition: Parse(broken go.mod) error = nil, want a parse error")
	}

	r := gitrepo.New(t, "main")
	r.Write("go.mod", goMod1)
	r.Write("app/requirements.txt", reqs)
	r.Write("broken/go.mod", broken)
	r.Write("README.md", "not a manifest\n")
	// Excluded by the timeline's rules.
	r.Write("vendor/example.com/x/go.mod", "module example.com/x\n\nrequire example.com/y v0.1.0\n")
	r.Write("internal/testdata/requirements.txt", "django==1.0\n")
	r.Write("web/node_modules/pkg/Dockerfile", "FROM alpine:3.19\n")
	first := r.Commit("first", day(1))
	// A later commit, and an uncommitted change, are not part of first.
	r.Write("go.mod", goMod2)
	r.Commit("bump lib", day(2))
	r.Write("app/requirements.txt", "requests==9.9.9\n")

	want := map[headKey]headDep{}
	for path, content := range map[string]string{"go.mod": goMod1, "app/requirements.txt": reqs} {
		pm, err := ext.Parse(path, []byte(content))
		if err != nil {
			t.Fatalf("Parse(%s): %v", path, err)
		}
		for _, d := range pm.Dependencies {
			want[headKey{path, d.Package.PURL}] = headDep{Version: d.Version, Requirement: d.Requirement}
		}
	}
	if len(want) < 3 {
		t.Fatalf("precondition: %d dependencies parsed from the fixture, want at least 3", len(want))
	}

	got := headStateFromTree(t, parityEnv(t), r.Path(), first, ext)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("headStateFromTree(first) =\n%v\nwant\n%v", got, want)
	}
}

func TestParityMergeBroughtIn(t *testing.T) {
	t.Parallel()

	day := func(d int) time.Time { return time.Date(2026, 3, d, 12, 0, 0, 0, time.UTC) }
	r := gitrepo.New(t, "main")
	r.Write("go.mod", "module example.com/app\n")
	r.Commit("root", day(1))
	r.Branch("feature")
	r.Write("a.txt", "a\n")
	f1 := r.Commit("feature 1", day(2))
	r.Write("b.txt", "b\n")
	f2 := r.Commit("feature 2", day(3))
	r.Switch("main")
	r.Write("c.txt", "c\n")
	r.Commit("main moves on", day(4))
	m := r.Merge("feature", "merge feature", day(5))

	env := parityEnv(t)
	got := revList(t, env, r.Path(), "--topo-order", "--reverse", m+"^2", "^"+m+"^1")
	if want := []string{f1, f2}; !slices.Equal(got, want) {
		t.Errorf("commits brought in by the merge = %v, want %v (oldest first)", got, want)
	}
	if merges := revSet(t, env, r.Path(), "--first-parent", "--merges", "HEAD"); !reflect.DeepEqual(merges, map[string]bool{m: true}) {
		t.Errorf("first-parent merges = %v, want only %s", merges, m)
	}
}

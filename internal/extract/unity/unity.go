// Package unity extracts the Unity editor version and the UPM packages of a
// Unity project from ProjectSettings/ProjectVersion.txt,
// Packages/manifest.json and Packages/packages-lock.json.
package unity

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const (
	ecosystem = "upm"

	editorPrefix         = "m_EditorVersion:"
	editorRevisionPrefix = "m_EditorVersionWithRevision:"
)

// Extractor parses the three Unity project files. It holds no state.
type Extractor struct{}

// New returns a Unity extractor.
func New() *Extractor {
	return &Extractor{}
}

// Patterns returns the Unity project files the extractor reads, in git's glob
// pathspec syntax.
func (e *Extractor) Patterns() []string {
	return []string{
		"**/ProjectSettings/ProjectVersion.txt",
		"**/Packages/manifest.json",
		"**/Packages/packages-lock.json",
	}
}

// Parse parses one Unity project file, chosen by the last two elements of
// path. Any other path returns an error wrapping model.ErrUnsupportedFile.
func (e *Extractor) Parse(p string, content []byte) (model.ParsedManifest, error) {
	var (
		pm  model.ParsedManifest
		err error
	)
	switch path.Base(path.Dir(p)) + "/" + path.Base(p) {
	case "ProjectSettings/ProjectVersion.txt":
		pm, err = parseProjectVersion(content)
	case "Packages/manifest.json":
		pm, err = parseManifest(content)
	case "Packages/packages-lock.json":
		pm, err = parseLock(content)
	default:
		err = model.ErrUnsupportedFile
	}
	if err != nil {
		return model.ParsedManifest{}, fmt.Errorf("parse %s: %w", p, err)
	}
	return pm, nil
}

func parseProjectVersion(content []byte) (model.ParsedManifest, error) {
	var version, withRevision string
	sc := bufio.NewScanner(bytes.NewReader(content))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if v, ok := strings.CutPrefix(line, editorPrefix); ok {
			version = strings.TrimSpace(v)
		} else if v, ok := strings.CutPrefix(line, editorRevisionPrefix); ok {
			withRevision = strings.TrimSpace(v)
		}
	}
	if err := sc.Err(); err != nil {
		return model.ParsedManifest{}, err
	}
	pm := model.ParsedManifest{Ecosystem: ecosystem, Kind: model.ManifestKindManifest}
	if version == "" {
		return pm, nil
	}
	requirement := withRevision
	if requirement == "" {
		requirement = version
	}
	pm.Dependencies = []model.Dependency{{
		Package: model.Package{
			PURL:      "pkg:generic/unity-editor",
			Ecosystem: "generic",
			Name:      "unity-editor",
		},
		Version:     version,
		Requirement: requirement,
		Direct:      model.DirectYes,
		Scope:       "build",
	}}
	return pm, nil
}

func parseManifest(content []byte) (model.ParsedManifest, error) {
	var doc struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(content, &doc); err != nil {
		return model.ParsedManifest{}, err
	}
	pm := model.ParsedManifest{Ecosystem: ecosystem, Kind: model.ManifestKindManifest}
	for _, name := range sortedKeys(doc.Dependencies) {
		value := doc.Dependencies[name]
		var version string
		switch {
		case strings.HasPrefix(value, "file:"):
			version = ""
		case isGitURL(value):
			version = fragment(value)
		default:
			version = stripUserinfo(value)
		}
		pm.Dependencies = append(pm.Dependencies, model.Dependency{
			Package:     upmPackage(name),
			Version:     version,
			Requirement: stripUserinfo(value),
			Direct:      model.DirectYes,
			Scope:       "runtime",
		})
	}
	return pm, nil
}

type lockEntry struct {
	Version string `json:"version"`
	Depth   *int   `json:"depth"`
	Source  string `json:"source"`
	Hash    string `json:"hash"`
}

func parseLock(content []byte) (model.ParsedManifest, error) {
	var doc struct {
		Dependencies map[string]lockEntry `json:"dependencies"`
	}
	if err := json.Unmarshal(content, &doc); err != nil {
		return model.ParsedManifest{}, err
	}
	pm := model.ParsedManifest{Ecosystem: ecosystem, Kind: model.ManifestKindLockfile}
	for _, name := range sortedKeys(doc.Dependencies) {
		entry := doc.Dependencies[name]
		var version string
		switch entry.Source {
		case "git":
			version = entry.Hash
			if version == "" {
				version = fragment(entry.Version)
			}
		case "registry", "builtin":
			version = stripUserinfo(entry.Version)
		default:
			version = ""
		}
		pm.Dependencies = append(pm.Dependencies, model.Dependency{
			Package:     upmPackage(name),
			Version:     version,
			Requirement: stripUserinfo(entry.Version),
			Direct:      lockDirectness(entry.Depth),
			Scope:       "runtime",
		})
	}
	return pm, nil
}

// lockDirectness maps a packages-lock.json depth to Directness. A negative
// depth is not a value Unity writes, so it is treated like an absent one.
func lockDirectness(depth *int) model.Directness {
	switch {
	case depth == nil || *depth < 0:
		return model.DirectUnknown
	case *depth == 0:
		return model.DirectYes
	default:
		return model.DirectNo
	}
}

func upmPackage(name string) model.Package {
	return model.Package{PURL: "pkg:upm/" + name, Ecosystem: ecosystem, Name: name}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// isGitURL reports whether a manifest.json value is a git dependency rather
// than a registry version or a file: reference.
func isGitURL(v string) bool {
	for _, prefix := range []string{"https://", "http://", "git+", "git@", "ssh://"} {
		if strings.HasPrefix(v, prefix) {
			return true
		}
	}
	base, _, _ := strings.Cut(v, "#")
	return strings.HasSuffix(base, ".git")
}

// fragment returns the part of v after the first '#', or "" when there is
// none.
func fragment(v string) string {
	_, ref, _ := strings.Cut(v, "#")
	return ref
}

// stripUserinfo removes credentials from a URL. A URL with a scheme loses its
// whole userinfo. An scp-like reference (git@host:org/x.git) keeps a bare user
// name, which is not a secret, and loses a user:password pair. Values that
// are not URLs come back unchanged.
func stripUserinfo(v string) string {
	if scheme, rest, ok := strings.Cut(v, "://"); ok {
		authorityEnd := strings.IndexAny(rest, "/?#")
		if authorityEnd < 0 {
			authorityEnd = len(rest)
		}
		at := strings.LastIndex(rest[:authorityEnd], "@")
		if at < 0 {
			return v
		}
		return scheme + "://" + rest[at+1:]
	}
	at := strings.Index(v, "@")
	if at < 0 {
		return v
	}
	userinfo := v[:at]
	if strings.ContainsAny(userinfo, "/#?") || !strings.Contains(userinfo, ":") {
		return v
	}
	return v[at+1:]
}

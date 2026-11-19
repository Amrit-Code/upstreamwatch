package extract

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/git-pkgs/manifests"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// parseWithLibrary parses Python requirements, pyproject.toml, uv.lock,
// poetry.lock and GitHub Actions workflows through the manifests library.
// It is called without Options.FSRoot, so the library reads only content.
func (e *Extractor) parseWithLibrary(p string, content []byte, kind model.ManifestKind) (model.ParsedManifest, error) {
	res, err := manifests.Parse(p, content)
	if _, ok := errors.AsType[*manifests.UnknownFileError](err); ok {
		return model.ParsedManifest{}, fmt.Errorf("parse %s: %w", p, model.ErrUnsupportedFile)
	}
	if err != nil {
		return model.ParsedManifest{}, fmt.Errorf("parse %s: %w", p, err)
	}

	poetryLock := path.Base(p) == "poetry.lock"
	var uvLocal map[string]bool
	if path.Base(p) == "uv.lock" {
		uvLocal = uvLocalPackages(content)
	}
	pm := model.ParsedManifest{Ecosystem: res.Ecosystem, Kind: kind}
	for _, dep := range res.Dependencies {
		if dep.PURL == "" {
			continue
		}
		// A local package is part of the project, not a published one. For
		// poetry.lock the library puts [package.source].url in RegistryURL,
		// which for a directory or file package is a local path; for uv.lock
		// it gives no source kind, so the lockfile is scanned for it.
		if (poetryLock && isLocalSource(dep.RegistryURL)) || uvLocal[dep.Name] {
			e.logger.Debug("local package skipped", "path", p, "package", dep.Name)
			continue
		}
		if res.Ecosystem == ecosystemActions && strings.HasPrefix(dep.Name, "docker://") {
			// The library gives a docker:// step a malformed githubactions
			// PURL instead of none; it names an image, not an action.
			e.logger.Debug("docker action skipped", "path", p, "uses", dep.Name)
			continue
		}
		d := model.Dependency{
			Package: model.Package{
				PURL:      versionless(dep.PURL),
				Ecosystem: res.Ecosystem,
				Name:      dep.Name,
			},
			Scope: string(dep.Scope),
		}
		switch {
		case kind == model.ManifestKindLockfile:
			d.Version = dep.Version
			d.Requirement = dep.Version
			d.Direct = model.DirectUnknown
		case res.Ecosystem == ecosystemPyPI:
			d.Version = exactPin(dep.Version)
			d.Requirement = dep.Version
			d.Direct = directness(dep.Direct)
		default:
			d.Version = dep.Version
			d.Requirement = dep.Version
			d.Direct = directness(dep.Direct)
		}
		pm.Dependencies = append(pm.Dependencies, d)
	}
	return pm, nil
}

// exactPin returns X for a Python constraint "==X" that pins one version: no
// wildcard, no second clause and not the arbitrary-equality "===". Any other
// constraint returns "".
func exactPin(constraint string) string {
	c := strings.TrimSpace(constraint)
	v, ok := strings.CutPrefix(c, "==")
	if !ok || strings.HasPrefix(v, "=") || strings.ContainsAny(v, "*,") {
		return ""
	}
	return strings.TrimSpace(v)
}

// isLocalSource reports whether a package source location is a filesystem
// path rather than a URL: a relative or absolute path, or a file:// URL. An
// empty location is the default registry.
func isLocalSource(location string) bool {
	if location == "" {
		return false
	}
	return strings.HasPrefix(location, "file://") || !strings.Contains(location, "://")
}

// directness maps the library's direct flag.
func directness(direct bool) model.Directness {
	if direct {
		return model.DirectYes
	}
	return model.DirectNo
}

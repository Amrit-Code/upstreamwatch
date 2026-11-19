package extract

import (
	"fmt"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// replaceKey identifies the module a replace directive applies to. An empty
// version applies to every version of the path.
type replaceKey struct {
	path    string
	version string
}

// parseGoMod reads require lines, applies replace directives and records the
// go and toolchain directives as pkg:golang/toolchain.
func parseGoMod(p string, content []byte) (model.ParsedManifest, error) {
	f, err := modfile.Parse(p, content, nil)
	if err != nil {
		return model.ParsedManifest{}, fmt.Errorf("parse %s: %w", p, err)
	}

	replaces := make(map[replaceKey]*modfile.Replace, len(f.Replace))
	for _, r := range f.Replace {
		replaces[replaceKey{r.Old.Path, r.Old.Version}] = r
	}

	deps := make([]model.Dependency, 0, len(f.Require)+1)
	for _, req := range f.Require {
		d := model.Dependency{
			Package:     golangPackage(req.Mod.Path),
			Version:     req.Mod.Version,
			Requirement: req.Mod.Version,
			Direct:      model.DirectYes,
			Scope:       scopeRuntime,
		}
		if req.Indirect {
			d.Direct = model.DirectNo
		}
		// As in the go command, a replace naming the exact version wins over
		// one naming the path alone.
		r, ok := replaces[replaceKey{req.Mod.Path, req.Mod.Version}]
		if !ok {
			r, ok = replaces[replaceKey{req.Mod.Path, ""}]
		}
		if ok {
			if r.New.Version == "" {
				d.Version = ""
				d.Requirement = "=> " + r.New.Path
			} else {
				d.Package = golangPackage(r.New.Path)
				d.Version = r.New.Version
				d.Requirement = req.Mod.Path + " => " + r.New.Path + " " + r.New.Version
			}
		}
		deps = append(deps, d)
	}

	if tc, ok := goToolchain(f); ok {
		deps = append(deps, tc)
	}

	return model.ParsedManifest{
		Ecosystem:    ecosystemGolang,
		Kind:         model.ManifestKindManifest,
		Dependencies: deps,
	}, nil
}

// goToolchain returns the pkg:golang/toolchain dependency for the go and
// toolchain directives, and false when neither gives a version.
func goToolchain(f *modfile.File) (model.Dependency, bool) {
	goVersion := ""
	if f.Go != nil {
		goVersion = f.Go.Version
	}
	toolchain := ""
	if f.Toolchain != nil && f.Toolchain.Name != "default" {
		toolchain = f.Toolchain.Name
	}

	d := model.Dependency{
		Package: model.Package{PURL: toolchainPURL, Ecosystem: ecosystemGolang, Name: "toolchain"},
		Direct:  model.DirectYes,
		Scope:   scopeBuild,
	}
	switch {
	case goVersion != "" && toolchain != "":
		d.Version = strings.TrimPrefix(toolchain, "go")
		d.Requirement = "go " + goVersion
	case goVersion != "":
		d.Version = goVersion
		d.Requirement = "go " + goVersion
	case toolchain != "":
		d.Version = strings.TrimPrefix(toolchain, "go")
		d.Requirement = "toolchain " + toolchain
	default:
		return model.Dependency{}, false
	}
	return d, true
}

// golangPackage returns the package for a Go module path.
func golangPackage(modulePath string) model.Package {
	return model.Package{
		PURL:      "pkg:golang/" + modulePath,
		Ecosystem: ecosystemGolang,
		Name:      modulePath,
	}
}

package extract

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/git-pkgs/manifests"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// goReleaseTag matches the Go release number at the start of a golang image
// tag: "1.26" in "1.26-alpine", "1.26.3" in "1.26.3-bookworm".
var goReleaseTag = regexp.MustCompile(`^[0-9]+\.[0-9]+(\.[0-9]+)?`)

// imageRef is a parsed image reference [registry[:port]/]path[:tag][@digest].
type imageRef struct {
	name   string
	tag    string
	digest string
}

// parseDockerfile reads the FROM instructions of a Dockerfile.
func (e *Extractor) parseDockerfile(p string, content []byte) (model.ParsedManifest, error) {
	var (
		deps      []model.Dependency
		stages    = make(map[string]bool)
		toolchain bool
	)
	for _, line := range dockerfileLines(string(content)) {
		image, stage, ok, err := fromInstruction(line)
		if err != nil {
			return model.ParsedManifest{}, fmt.Errorf("parse %s: %w", p, err)
		}
		if !ok {
			continue
		}
		// The image is checked against the stages named before this line, so
		// "FROM x AS x" still refers to the image x.
		isStage := stages[strings.ToLower(image)]
		if stage != "" {
			stages[strings.ToLower(stage)] = true
		}
		switch {
		case isStage, strings.EqualFold(image, "scratch"):
			continue
		case strings.Contains(image, "$"):
			e.logger.Debug("image with a variable skipped", "path", p, "image", image)
			continue
		}

		ref, err := parseImageRef(image)
		if err != nil {
			return model.ParsedManifest{}, fmt.Errorf("parse %s: %w", p, err)
		}
		d, err := imageDependency(ref)
		if err != nil {
			return model.ParsedManifest{}, fmt.Errorf("parse %s: %w", p, err)
		}
		deps = append(deps, d)

		if e.opts.DeriveToolchainFromImages && !toolchain {
			if tc, ok := derivedToolchain(ref); ok {
				deps = append(deps, tc)
				toolchain = true
			}
		}
	}
	return model.ParsedManifest{
		Ecosystem:    ecosystemDocker,
		Kind:         model.ManifestKindManifest,
		Dependencies: deps,
	}, nil
}

// heredocMarker matches one heredoc word as BuildKit reads it: an optional
// file descriptor, << or <<-, and a name that may be quoted.
var heredocMarker = regexp.MustCompile(`^[0-9]*<<(-?)(["']?)([A-Za-z_][A-Za-z0-9_]*)(["']?)$`)

// heredoc is a pending heredoc body: the terminator line that ends it, and
// whether tabs may precede the terminator (<<-).
type heredoc struct {
	word      string
	stripTabs bool
}

// dockerfileLines returns the logical lines of a Dockerfile: comment lines
// dropped, backslash continuations joined and heredoc bodies of RUN, COPY
// and ADD skipped, as the Docker builder reads them. Comment and blank lines
// inside a continuation are skipped too.
func dockerfileLines(content string) []string {
	var (
		lines   []string
		cur     strings.Builder
		cont    bool
		pending []heredoc
	)
	for raw := range strings.SplitSeq(content, "\n") {
		if len(pending) > 0 {
			// Inside a heredoc body every line is content, up to and
			// including the terminator line.
			body := strings.TrimSuffix(raw, "\r")
			if pending[0].stripTabs {
				body = strings.TrimLeft(body, "\t")
			}
			if body == pending[0].word {
				pending = pending[1:]
			}
			continue
		}
		t := strings.TrimSpace(raw)
		if strings.HasPrefix(t, "#") || (cont && t == "") {
			continue
		}
		if rest, ok := strings.CutSuffix(t, `\`); ok {
			cur.WriteString(strings.TrimSpace(rest))
			cur.WriteByte(' ')
			cont = true
			continue
		}
		cur.WriteString(t)
		line := cur.String()
		lines = append(lines, line)
		pending = heredocs(line)
		cur.Reset()
		cont = false
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}

// heredocs returns the heredocs a RUN, COPY or ADD line opens, in the order
// their bodies follow it.
func heredocs(line string) []heredoc {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil
	}
	switch strings.ToUpper(fields[0]) {
	case "RUN", "COPY", "ADD":
	default:
		return nil
	}
	var docs []heredoc
	for _, f := range fields[1:] {
		m := heredocMarker.FindStringSubmatch(f)
		if m == nil || m[2] != m[4] {
			continue
		}
		docs = append(docs, heredoc{word: m[3], stripTabs: m[1] == "-"})
	}
	return docs
}

// fromInstruction reads one logical line. It returns ok false for any
// instruction other than FROM, and otherwise the image and the stage name
// given with AS, if any. Flags such as --platform are skipped.
func fromInstruction(line string) (image, stage string, ok bool, err error) {
	fields := strings.Fields(line)
	if len(fields) == 0 || !strings.EqualFold(fields[0], "FROM") {
		return "", "", false, nil
	}
	args := fields[1:]
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		args = args[1:]
	}
	switch {
	case len(args) == 1:
		return args[0], "", true, nil
	case len(args) == 3 && strings.EqualFold(args[1], "AS"):
		return args[0], args[2], true, nil
	}
	return "", "", false, fmt.Errorf("malformed FROM instruction %q", line)
}

// parseImageRef splits [registry[:port]/]path[:tag][@digest]. A colon is a
// tag separator only after the last slash, so a registry port is kept in the
// name.
func parseImageRef(s string) (imageRef, error) {
	var ref imageRef
	name, digest, hasDigest := strings.Cut(s, "@")
	if hasDigest {
		if digest == "" {
			return imageRef{}, fmt.Errorf("invalid image reference %q: empty digest", s)
		}
		ref.digest = digest
	}
	if i := strings.LastIndex(name, ":"); i >= 0 && !strings.Contains(name[i+1:], "/") {
		ref.tag = name[i+1:]
		name = name[:i]
		if ref.tag == "" {
			return imageRef{}, fmt.Errorf("invalid image reference %q: empty tag", s)
		}
	}
	if name == "" {
		return imageRef{}, fmt.Errorf("invalid image reference %q: empty name", s)
	}
	ref.name = name
	return ref, nil
}

// imageDependency returns the docker dependency for an image reference.
func imageDependency(ref imageRef) (model.Dependency, error) {
	purl, err := dockerPURL(ref.name)
	if err != nil {
		return model.Dependency{}, err
	}
	d := model.Dependency{
		Package:     model.Package{PURL: purl, Ecosystem: ecosystemDocker, Name: ref.name},
		Version:     ref.tag,
		Requirement: ref.tag,
		Direct:      model.DirectYes,
		Scope:       scopeRuntime,
	}
	switch {
	case ref.digest != "":
		d.Requirement = ref.digest
	case ref.tag == "":
		d.Version = "latest"
		d.Requirement = "latest"
	}
	return d, nil
}

// dockerPURL returns the package URL the manifests library gives an untagged
// reference to the image, so images from Dockerfiles and compose files get
// the same identity as images the library parses elsewhere, escaping
// included.
func dockerPURL(name string) (string, error) {
	res, err := manifests.Parse("Dockerfile", []byte("FROM "+name+"\n"))
	if err != nil {
		return "", fmt.Errorf("package URL for image %q: %w", name, err)
	}
	if len(res.Dependencies) != 1 || res.Dependencies[0].Name != name || res.Dependencies[0].PURL == "" {
		return "", fmt.Errorf("package URL for image %q: invalid image name", name)
	}
	return versionless(res.Dependencies[0].PURL), nil
}

// derivedToolchain returns pkg:golang/toolchain for an official golang
// image whose tag starts with a Go release number, and false otherwise.
func derivedToolchain(ref imageRef) (model.Dependency, bool) {
	switch ref.name {
	case "golang", "library/golang", "docker.io/library/golang":
	default:
		return model.Dependency{}, false
	}
	version := goReleaseTag.FindString(ref.tag)
	if version == "" {
		return model.Dependency{}, false
	}
	return model.Dependency{
		Package:     model.Package{PURL: toolchainPURL, Ecosystem: ecosystemGolang, Name: "toolchain"},
		Version:     version,
		Requirement: "golang:" + ref.tag,
		Direct:      model.DirectYes,
		Scope:       scopeBuild,
	}, true
}

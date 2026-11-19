package extract

import (
	"reflect"
	"testing"

	"github.com/git-pkgs/manifests"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const distrolessDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000001"

const dockerfileSample = `# syntax=docker/dockerfile:1
ARG BASE=alpine:3.20

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
RUN go build \
    -o /out/app .

FROM golang:1.25.3-bookworm AS tester
FROM builder AS packager
FROM ${BASE}
FROM scratch AS empty

from \
  # the runtime image
  --platform=linux/amd64 \
  gcr.io/distroless/static@` + distrolessDigest + ` \
  as runtime

FROM registry.example.com:5000/team/app:2.1
FROM Builder
FROM alpine
`

func dockerDep(purl, name, version, requirement string) model.Dependency {
	return model.Dependency{
		Package:     model.Package{PURL: purl, Ecosystem: "docker", Name: name},
		Version:     version,
		Requirement: requirement,
		Direct:      model.DirectYes,
		Scope:       "runtime",
	}
}

func TestParse_Dockerfile(t *testing.T) {
	t.Parallel()
	images := []model.Dependency{
		dockerDep("pkg:docker/golang", "golang", "1.26-alpine", "1.26-alpine"),
		dockerDep("pkg:docker/gcr.io/distroless/static", "gcr.io/distroless/static", "", distrolessDigest),
		dockerDep("pkg:docker/registry.example.com:5000/team/app", "registry.example.com:5000/team/app", "2.1", "2.1"),
		dockerDep("pkg:docker/alpine", "alpine", "latest", "latest"),
	}
	withToolchain := []model.Dependency{images[0], toolchainDep("1.26", "golang:1.26-alpine"), images[1], images[2], images[3]}

	tests := []struct {
		name    string
		derive  bool
		content string
		want    []model.Dependency
	}{
		{name: "multi-stage, derivation on", derive: true, content: dockerfileSample, want: withToolchain},
		{name: "multi-stage, derivation off", derive: false, content: dockerfileSample, want: images},
		{
			name:    "first golang tag with a release number derives",
			derive:  true,
			content: "FROM golang:latest\nFROM golang:alpine AS a\nFROM library/golang:1.26.3-bookworm AS b\nFROM docker.io/library/golang:1.24\n",
			want: []model.Dependency{
				dockerDep("pkg:docker/golang", "golang", "latest", "latest"),
				dockerDep("pkg:docker/library/golang", "library/golang", "1.26.3-bookworm", "1.26.3-bookworm"),
				toolchainDep("1.26.3", "golang:1.26.3-bookworm"),
				dockerDep("pkg:docker/docker.io/library/golang", "docker.io/library/golang", "1.24", "1.24"),
			},
		},
		{
			name:    "image of another name derives nothing",
			derive:  true,
			content: "FROM example-org/golang:1.26\n",
			want:    []model.Dependency{dockerDep("pkg:docker/example-org/golang", "example-org/golang", "1.26", "1.26")},
		},
		{
			name:    "tag and digest",
			derive:  false,
			content: "FROM node:22@sha256:abc\n",
			want:    []model.Dependency{dockerDep("pkg:docker/node", "node", "22", "sha256:abc")},
		},
		{
			name:   "RUN heredoc body with from lines",
			derive: false,
			content: "FROM python:3.13-slim AS app\nRUN python3 - <<EOF\nfrom os import path\nFROM x\n# not a comment\nprint(path.sep)\nEOF\n" +
				"FROM alpine:3.20\n",
			want: []model.Dependency{
				dockerDep("pkg:docker/python", "python", "3.13-slim", "3.13-slim"),
				dockerDep("pkg:docker/alpine", "alpine", "3.20", "3.20"),
			},
		},
		{
			name:    "COPY heredoc body",
			derive:  false,
			content: "FROM alpine:3.20\nCOPY <<EOF /etc/notes.txt\nFROM is a word here\nfrom a b c\nEOF\nFROM busybox:1.37\n",
			want: []model.Dependency{
				dockerDep("pkg:docker/alpine", "alpine", "3.20", "3.20"),
				dockerDep("pkg:docker/busybox", "busybox", "1.37", "1.37"),
			},
		},
		{
			name:   "two heredocs on one RUN line",
			derive: false,
			content: "FROM alpine:3.20\nRUN cat > /a <<FIRST && cat > /b <<-'SECOND'\nfrom first body\nFROM y\nFIRST\n" +
				"\tfrom second body\n\tFROM z\n\tSECOND\nFROM busybox:1.37\n",
			want: []model.Dependency{
				dockerDep("pkg:docker/alpine", "alpine", "3.20", "3.20"),
				dockerDep("pkg:docker/busybox", "busybox", "1.37", "1.37"),
			},
		},
		{
			name:    "only stages, scratch and variables",
			derive:  true,
			content: "FROM ${GO_IMAGE} AS build\nFROM build\nFROM scratch\nFROM $BASE:1.0\n",
			want:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := newExtractor(tt.derive).Parse("build/Dockerfile", []byte(tt.content))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			checkParsed(t, "build/Dockerfile", got, model.ParsedManifest{
				Ecosystem:    "docker",
				Kind:         model.ManifestKindManifest,
				Dependencies: tt.want,
			})
		})
	}
}

func TestDockerPURLMatchesLibrary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		image string
		want  string
	}{
		{"golang", "pkg:docker/golang"},
		{"gcr.io/distroless/static", "pkg:docker/gcr.io/distroless/static"},
		{"ghcr.io/owner/img", "pkg:docker/ghcr.io/owner/img"},
	}
	e := newExtractor(false)
	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			t.Parallel()
			lib, err := manifests.Parse("Dockerfile", []byte("FROM "+tt.image+"\n"))
			if err != nil || len(lib.Dependencies) != 1 {
				t.Fatalf("manifests.Parse(FROM %s) = %+v, %v", tt.image, lib, err)
			}
			if lib.Dependencies[0].PURL != tt.want {
				t.Errorf("library PURL for %s = %q, want %q", tt.image, lib.Dependencies[0].PURL, tt.want)
			}
			for _, content := range []string{"FROM " + tt.image + ":1.0\n", "services:\n  s:\n    image: " + tt.image + ":1.0\n"} {
				path := "Dockerfile"
				if content[0] == 's' {
					path = "compose.yaml"
				}
				pm, err := e.Parse(path, []byte(content))
				if err != nil || len(pm.Dependencies) != 1 {
					t.Fatalf("Parse(%s, %q) = %+v, %v", path, content, pm, err)
				}
				if got := pm.Dependencies[0].Package.PURL; got != lib.Dependencies[0].PURL {
					t.Errorf("Parse(%s) PURL for %s = %q, library gives %q", path, tt.image, got, lib.Dependencies[0].PURL)
				}
			}
		})
	}
}

func TestParseImageRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    imageRef
		wantErr bool
	}{
		{in: "golang", want: imageRef{name: "golang"}},
		{in: "golang:1.26-alpine", want: imageRef{name: "golang", tag: "1.26-alpine"}},
		{in: "alpine@sha256:abc", want: imageRef{name: "alpine", digest: "sha256:abc"}},
		{in: "node:22@sha256:abc", want: imageRef{name: "node", tag: "22", digest: "sha256:abc"}},
		{in: "registry.example.com:5000/team/app", want: imageRef{name: "registry.example.com:5000/team/app"}},
		{in: "registry.example.com:5000/team/app:2.1", want: imageRef{name: "registry.example.com:5000/team/app", tag: "2.1"}},
		{in: "golang:", wantErr: true},
		{in: "golang@", wantErr: true},
		{in: ":1.0", wantErr: true},
		{in: "@sha256:abc", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := parseImageRef(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseImageRef(%q) error = %v, want error %v", tt.in, err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseImageRef(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestHeredocs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line string
		want []heredoc
	}{
		{"RUN <<EOF", []heredoc{{word: "EOF"}}},
		{"run python3 - <<-PY", []heredoc{{word: "PY", stripTabs: true}}},
		{`COPY <<"A" /a <<'B' /b`, []heredoc{{word: "A"}, {word: "B"}}},
		{"ADD 2<<EOF /x", []heredoc{{word: "EOF"}}},
		{`RUN cat <<"EOF'`, nil},
		{"RUN echo $((1<<2))", nil},
		{`RUN grep x <<< "$v"`, nil},
		{"ENV X=<<EOF", nil},
		{"FROM alpine", nil},
		{"", nil},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			t.Parallel()
			if got := heredocs(tt.line); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("heredocs(%q) = %+v, want %+v", tt.line, got, tt.want)
			}
		})
	}
}

func TestDockerfileLines(t *testing.T) {
	t.Parallel()
	content := "# comment\nFROM a \\\n  # inside\n\n  AS b\nRUN x \\\n"
	want := []string{"FROM a AS b", "RUN x "}
	if got := dockerfileLines(content); !reflect.DeepEqual(got, want) {
		t.Errorf("dockerfileLines() = %q, want %q", got, want)
	}
}

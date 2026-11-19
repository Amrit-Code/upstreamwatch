package extract

import (
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const composeSample = `x-cache: &cache
  image: redis:7

services:
  web:
    image: ghcr.io/example-org/web:1.4
  db:
    image: postgres:16@sha256:abc
  cache:
    <<: *cache
  worker:
    build: .
  templated:
    image: ${WEB_IMAGE}
  web-next:
    image: ghcr.io/example-org/web:1.5
  toolchain:
    image: golang:1.26-alpine
  proxy:
    image: nginx
  empty:
`

func TestParse_Compose(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		want    []model.Dependency
	}{
		{
			name:    "services in file order",
			content: composeSample,
			want: []model.Dependency{
				dockerDep("pkg:docker/ghcr.io/example-org/web", "ghcr.io/example-org/web", "1.4", "1.4"),
				dockerDep("pkg:docker/postgres", "postgres", "16", "sha256:abc"),
				dockerDep("pkg:docker/redis", "redis", "7", "7"),
				// No toolchain is derived from a compose image.
				dockerDep("pkg:docker/golang", "golang", "1.26-alpine", "1.26-alpine"),
				dockerDep("pkg:docker/nginx", "nginx", "latest", "latest"),
			},
		},
		{name: "no services key", content: "version: '3'\nvolumes:\n  data: {}\n", want: nil},
		{name: "null services", content: "services:\n", want: nil},
		{name: "empty file", content: "", want: nil},
	}
	e := newExtractor(true)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := e.Parse("deploy/docker-compose.yml", []byte(tt.content))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			checkParsed(t, "deploy/docker-compose.yml", got, model.ParsedManifest{
				Ecosystem:    "docker",
				Kind:         model.ManifestKindManifest,
				Dependencies: tt.want,
			})
		})
	}
}

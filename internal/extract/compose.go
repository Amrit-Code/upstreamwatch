package extract

import (
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// composeFile is the part of a compose file the extractor reads. Services is
// kept as a node so services are visited in file order.
type composeFile struct {
	Services yaml.Node `yaml:"services"`
}

// composeService is the part of one service the extractor reads. Decoding
// each service into it resolves anchors, aliases and merge keys.
type composeService struct {
	Image string `yaml:"image"`
}

// parseCompose reads services.<name>.image from a compose file, in file
// order.
func (e *Extractor) parseCompose(p string, content []byte) (model.ParsedManifest, error) {
	pm := model.ParsedManifest{Ecosystem: ecosystemDocker, Kind: model.ManifestKindManifest}

	var f composeFile
	if err := yaml.Unmarshal(content, &f); err != nil {
		return model.ParsedManifest{}, fmt.Errorf("parse %s: %w", p, err)
	}
	services := &f.Services
	if services.Kind == yaml.AliasNode {
		services = services.Alias
	}
	switch {
	case services.Kind == 0, services.Kind == yaml.ScalarNode && services.Tag == "!!null":
		return pm, nil
	case services.Kind != yaml.MappingNode:
		return model.ParsedManifest{}, fmt.Errorf("parse %s: services is not a mapping", p)
	}

	for i := 0; i+1 < len(services.Content); i += 2 {
		name := services.Content[i].Value
		var svc composeService
		if err := services.Content[i+1].Decode(&svc); err != nil {
			return model.ParsedManifest{}, fmt.Errorf("parse %s: service %q: %w", p, name, err)
		}
		image := strings.TrimSpace(svc.Image)
		if image == "" {
			continue
		}
		if strings.Contains(image, "$") {
			e.logger.Debug("image with a variable skipped", "path", p, "image", image)
			continue
		}
		ref, err := parseImageRef(image)
		if err != nil {
			return model.ParsedManifest{}, fmt.Errorf("parse %s: service %q: %w", p, name, err)
		}
		d, err := imageDependency(ref)
		if err != nil {
			return model.ParsedManifest{}, fmt.Errorf("parse %s: service %q: %w", p, name, err)
		}
		pm.Dependencies = append(pm.Dependencies, d)
	}
	return pm, nil
}

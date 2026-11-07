package clusterconfig

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

type BootstrapConfig struct {
	CRD   []string        `yaml:"crd"`
	Steps []BootstrapStep `yaml:"steps"`
}

// BootstrapStep is either a HelmRelease metadata.name or a manifest path.
type BootstrapStep struct {
	Chart    string
	Manifest string
}

func (step *BootstrapStep) UnmarshalYAML(node *yaml.Node) error {
	*step = BootstrapStep{}
	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" && node.Value != "" {
		step.Chart = node.Value
		return nil
	}
	if node.Kind == yaml.MappingNode && len(node.Content) == 2 && node.Content[0].Value == "manifest" {
		value := node.Content[1]
		if value.Kind == yaml.ScalarNode && value.Tag == "!!str" && value.Value != "" {
			step.Manifest = value.Value
			return nil
		}
	}
	return fmt.Errorf("bootstrap.steps line %d: expected a HelmRelease name or {manifest: path}", node.Line)
}

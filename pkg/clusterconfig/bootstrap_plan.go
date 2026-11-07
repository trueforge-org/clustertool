package clusterconfig

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type BootstrapConfig []BootstrapStep

// BootstrapStep selects one chart, CRD source or manifest.
type BootstrapStep struct {
	Chart    string
	CRD      string
	Manifest string
	Wait     bool
}

func (config *BootstrapConfig) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("bootstrap line %d: expected a list of steps", node.Line)
	}
	steps := make(BootstrapConfig, len(node.Content))
	for i, entry := range node.Content {
		if err := steps[i].UnmarshalYAML(entry); err != nil {
			return fmt.Errorf("bootstrap[%d]: %w", i, err)
		}
	}
	*config = steps
	return nil
}

func (step *BootstrapStep) UnmarshalYAML(node *yaml.Node) error {
	*step = BootstrapStep{Wait: true}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: expected a chart, crd or manifest entry", node.Line)
	}
	action, hasWait := "", false
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		switch key.Value {
		case "chart", "crd", "manifest":
			if action != "" {
				return fmt.Errorf("line %d: expected exactly one of chart, crd or manifest", key.Line)
			}
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" || strings.TrimSpace(value.Value) == "" {
				return fmt.Errorf("line %d: %s requires a non-empty string", value.Line, key.Value)
			}
			action = key.Value
			switch action {
			case "chart":
				step.Chart = value.Value
			case "crd":
				step.CRD = value.Value
			case "manifest":
				step.Manifest = value.Value
			}
		case "wait":
			if hasWait {
				return fmt.Errorf("line %d: duplicate wait field", key.Line)
			}
			if value.Kind != yaml.ScalarNode || value.Tag != "!!bool" {
				return fmt.Errorf("line %d: wait must be true or false", value.Line)
			}
			if err := value.Decode(&step.Wait); err != nil {
				return err
			}
			hasWait = true
		default:
			return fmt.Errorf("line %d: unknown bootstrap field %q", key.Line, key.Value)
		}
	}
	if action == "" {
		return fmt.Errorf("line %d: expected exactly one of chart, crd or manifest", node.Line)
	}
	if hasWait && action != "chart" {
		return fmt.Errorf("line %d: wait is only supported for chart steps", node.Line)
	}
	return nil
}

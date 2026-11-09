package clusterconfig

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// BootstrapConfig separates CRDs applied before the base charts from later application steps.
type BootstrapConfig struct {
	CRD   []string        `yaml:"crd"`
	Steps []BootstrapStep `yaml:"steps"`
}

// BootstrapStep selects one chart or manifest.
type BootstrapStep struct {
	Chart    string
	Manifest string
	Wait     bool
}

// Keep the section, step and field position so errors can use the original source line.
type bootstrapConfigError struct {
	section string
	step    int
	field   int
	line    int
	message string
}

func (err *bootstrapConfigError) Error() string {
	location := "bootstrap"
	if err.section != "" {
		location += "." + err.section
	}
	if err.step > 0 {
		if err.section == "crd" {
			location = fmt.Sprintf("bootstrap.crd entry %d", err.step)
		} else {
			location = fmt.Sprintf("bootstrap step %d", err.step)
		}
	}
	return fmt.Sprintf("%s, line %d: %s", location, err.line, err.message)
}

func (err *bootstrapConfigError) useSourceLine(source, rendered []byte) {
	var original, expanded yaml.Node
	if yaml.Unmarshal(source, &original) != nil || yaml.Unmarshal(rendered, &expanded) != nil {
		return
	}
	if len(original.Content) == 0 || len(expanded.Content) == 0 {
		return
	}
	node, renderedNode := original.Content[0], expanded.Content[0]
	for _, name := range []string{"bootstrap", err.section} {
		if name == "" {
			continue
		}
		for node.Kind == yaml.AliasNode {
			node = node.Alias
		}
		for renderedNode.Kind == yaml.AliasNode {
			renderedNode = renderedNode.Alias
		}
		if node.Kind != yaml.MappingNode || renderedNode.Kind != yaml.MappingNode {
			return
		}
		found := false
		for i := 0; i+1 < len(renderedNode.Content) && i+1 < len(node.Content); i += 2 {
			if renderedNode.Content[i].Value == name {
				node, renderedNode = node.Content[i+1], renderedNode.Content[i+1]
				found = true
				break
			}
		}
		if !found {
			return
		}
	}
	for node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	if err.step > 0 {
		if node.Kind != yaml.SequenceNode || err.step > len(node.Content) {
			return
		}
		node = node.Content[err.step-1]
	}
	if err.field >= 0 {
		if err.field >= len(node.Content) {
			return
		}
		node = node.Content[err.field]
	}
	err.line = node.Line
}

func (config *BootstrapConfig) UnmarshalYAML(node *yaml.Node) error {
	*config = BootstrapConfig{}
	if node.Kind != yaml.MappingNode {
		return &bootstrapConfigError{field: -1, line: node.Line, message: "expected a mapping with crd and steps lists"}
	}
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.Value != "crd" && key.Value != "steps" {
			return &bootstrapConfigError{field: i, line: key.Line, message: fmt.Sprintf("unknown bootstrap field '%s'; expected crd or steps", key.Value)}
		}
		if seen[key.Value] {
			return &bootstrapConfigError{field: i, line: key.Line, message: fmt.Sprintf("duplicate bootstrap field '%s'", key.Value)}
		}
		seen[key.Value] = true
		if value.Kind != yaml.SequenceNode {
			return &bootstrapConfigError{section: key.Value, field: -1, line: value.Line, message: "expected a list; use [] for an empty list"}
		}
		switch key.Value {
		case "crd":
			config.CRD = make([]string, len(value.Content))
			for j, entry := range value.Content {
				if entry.Kind != yaml.ScalarNode || entry.Tag != "!!str" || strings.TrimSpace(entry.Value) == "" {
					return &bootstrapConfigError{section: "crd", step: j + 1, field: -1, line: entry.Line, message: "expected a non-empty HelmRelease name"}
				}
				config.CRD[j] = entry.Value
			}
		case "steps":
			config.Steps = make([]BootstrapStep, len(value.Content))
			for j, entry := range value.Content {
				if err := config.Steps[j].UnmarshalYAML(entry); err != nil {
					if configErr, ok := err.(*bootstrapConfigError); ok {
						configErr.section, configErr.step = "steps", j+1
						return configErr
					}
					return fmt.Errorf("bootstrap step %d: %w", j+1, err)
				}
			}
		}
	}
	return nil
}

func (step *BootstrapStep) UnmarshalYAML(node *yaml.Node) error {
	*step = BootstrapStep{Wait: true}
	invalid := func(field int, node *yaml.Node, message string) error {
		return &bootstrapConfigError{field: field, line: node.Line, message: message}
	}
	if node.Kind != yaml.MappingNode {
		return invalid(-1, node, "expected a chart or manifest entry")
	}
	action, hasWait := "", false
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		switch key.Value {
		case "chart", "manifest":
			if action != "" {
				return invalid(i, key, "expected exactly one of chart or manifest")
			}
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" || strings.TrimSpace(value.Value) == "" {
				return invalid(i+1, value, fmt.Sprintf("%s requires a non-empty string", key.Value))
			}
			action = key.Value
			switch action {
			case "chart":
				step.Chart = value.Value
			case "manifest":
				step.Manifest = value.Value
			}
		case "wait":
			if hasWait {
				return invalid(i, key, "duplicate wait field")
			}
			if value.Kind != yaml.ScalarNode || value.Tag != "!!bool" {
				return invalid(i+1, value, "wait must be true or false")
			}
			if err := value.Decode(&step.Wait); err != nil {
				return invalid(i+1, value, err.Error())
			}
			hasWait = true
		default:
			return invalid(i, key, fmt.Sprintf("unknown bootstrap field '%s'; expected chart, manifest or wait", key.Value))
		}
	}
	if action == "" {
		return invalid(-1, node, "expected exactly one of chart or manifest")
	}
	if hasWait && action != "chart" {
		return invalid(-1, node, "wait is only supported for chart steps")
	}
	return nil
}

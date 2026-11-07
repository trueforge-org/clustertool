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

// Keep the step and field position so errors can use the original source line.
type bootstrapConfigError struct {
	step    int
	field   int
	line    int
	message string
}

func (err *bootstrapConfigError) Error() string {
	location := "bootstrap"
	if err.step > 0 {
		location = fmt.Sprintf("bootstrap step %d", err.step)
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
	originalRoot, expandedRoot := original.Content[0], expanded.Content[0]
	for i := 0; i+1 < len(expandedRoot.Content) && i+1 < len(originalRoot.Content); i += 2 {
		if expandedRoot.Content[i].Value != "bootstrap" {
			continue
		}
		node := originalRoot.Content[i+1]
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
		return
	}
}

func (config *BootstrapConfig) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return &bootstrapConfigError{field: -1, line: node.Line, message: "expected a list of steps"}
	}
	steps := make(BootstrapConfig, len(node.Content))
	for i, entry := range node.Content {
		if err := steps[i].UnmarshalYAML(entry); err != nil {
			if configErr, ok := err.(*bootstrapConfigError); ok {
				configErr.step = i + 1
				return configErr
			}
			return fmt.Errorf("bootstrap step %d: %w", i+1, err)
		}
	}
	*config = steps
	return nil
}

func (step *BootstrapStep) UnmarshalYAML(node *yaml.Node) error {
	*step = BootstrapStep{Wait: true}
	invalid := func(field int, node *yaml.Node, message string) error {
		return &bootstrapConfigError{field: field, line: node.Line, message: message}
	}
	if node.Kind != yaml.MappingNode {
		return invalid(-1, node, "expected a chart, crd or manifest entry")
	}
	action, hasWait := "", false
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		switch key.Value {
		case "chart", "crd", "manifest":
			if action != "" {
				return invalid(i, key, "expected exactly one of chart, crd or manifest")
			}
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" || strings.TrimSpace(value.Value) == "" {
				return invalid(i+1, value, fmt.Sprintf("%s requires a non-empty string", key.Value))
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
			return invalid(i, key, fmt.Sprintf("unknown bootstrap field %q; expected chart, crd, manifest or wait", key.Value))
		}
	}
	if action == "" {
		return invalid(-1, node, "expected exactly one of chart, crd or manifest")
	}
	if hasWait && action != "chart" {
		return invalid(-1, node, "wait is only supported for chart steps")
	}
	return nil
}

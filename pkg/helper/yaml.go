package helper

import (
	"bytes"
	"fmt"
	"io"
	"regexp"

	"gopkg.in/yaml.v3"
)

var yamlVariable = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// RenderYAML substitutes variables in scalar values without changing YAML structure.
// Values may contain quotes, colons or newlines.
func RenderYAML(data []byte, env map[string]string) ([]byte, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		var walk func(*yaml.Node) error
		walk = func(n *yaml.Node) error {
			if n.Kind == yaml.ScalarNode {
				var missing string
				n.Value = yamlVariable.ReplaceAllStringFunc(n.Value, func(s string) string {
					key := s[2 : len(s)-1]
					value, ok := env[key]
					if !ok {
						missing = key
					}
					return value
				})
				if missing != "" {
					return fmt.Errorf("unresolved variable %s", missing)
				}
			}
			for _, child := range n.Content {
				if err := walk(child); err != nil {
					return err
				}
			}
			return nil
		}
		if err := walk(&doc); err != nil {
			return nil, err
		}
		if err := enc.Encode(&doc); err != nil {
			return nil, err
		}
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

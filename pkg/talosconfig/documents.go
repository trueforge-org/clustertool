package talosconfig

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/trueforge-org/clustertool/pkg/helper"
	"gopkg.in/yaml.v3"
)

func ValidateNode(node string) error {
	inv, err := LoadInventory()
	if err != nil {
		return err
	}
	_, err = inv.Select(node)
	return err
}

func checkExistingIdentity() error {
	for _, path := range []string{TalosconfigPath()} {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("existing Talos cluster detected at %s: refusing to generate new CAs; restore secrets.sops.yaml before continuing", path)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func publish(staged string) error {
	// Both validated files are published together. Never replace an older backup.
	backup := helper.TalosGenerated + ".previous"
	if _, err := os.Stat(backup); err == nil {
		return fmt.Errorf("generation backup exists at %s; inspect it before retrying", backup)
	} else if !os.IsNotExist(err) {
		return err
	}
	hadPrevious := false
	if _, err := os.Stat(helper.TalosGenerated); err == nil {
		if err = os.Rename(helper.TalosGenerated, backup); err != nil {
			return err
		}
		hadPrevious = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(staged, helper.TalosGenerated); err != nil {
		if hadPrevious {
			if restoreErr := os.Rename(backup, helper.TalosGenerated); restoreErr != nil {
				return fmt.Errorf("publish: %v; restore %s: %w", err, backup, restoreErr)
			}
		}
		return err
	}
	if hadPrevious {
		return os.RemoveAll(backup)
	}
	return nil
}

var variable = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Replace YAML scalar values, not YAML syntax: credentials may contain quotes,
// colons or newlines. Talos remains the authority for the document schema.
func render(data []byte, env map[string]string) ([]byte, error) {
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
				n.Value = variable.ReplaceAllStringFunc(n.Value, func(s string) string {
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

// Read only the official fields needed for CLI orchestration from validated
// output. This does not define or validate another Talos schema.

func GeneratedNodeValue(path, kind string, fields ...string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var doc map[string]interface{}
		if err := dec.Decode(&doc); err == io.EOF {
			break
		} else if err != nil {
			return "", err
		}
		if kind != "" && doc["kind"] != kind {
			continue
		}
		if kind == "" && doc["kind"] != nil {
			continue
		}
		var value interface{} = doc
		for _, key := range fields {
			m, ok := value.(map[string]interface{})
			if !ok {
				value = nil
				break
			}
			value = m[key]
		}
		if s, ok := value.(string); ok && s != "" {
			return s, nil
		}
	}
	return "", fmt.Errorf("generated Talos configuration has no %s %s", kind, strings.Join(fields, "."))
}

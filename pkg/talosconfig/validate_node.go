package talosconfig

import (
	"bytes"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"net/netip"
	"os"
)

// Validate orchestration invariants after Talos has validated its own schema.
func validateNodeOutput(node Node, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	foundStatic, matches := false, false
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		kind, _ := doc["kind"].(string)
		if kind == "" {
			machine, _ := doc["machine"].(map[string]any)
			expected := "worker"
			if node.Role == "control-plane" {
				expected = "controlplane"
			}
			if machine["type"] != expected {
				return fmt.Errorf("node %s: generated machine type does not match clustertool.yaml role %s", node.Name, node.Role)
			}
		}
		if node.Role == "worker" {
			switch kind {
			case "Layer2VIPConfig", "KubeAPIServerConfig", "KubeControllerManagerConfig", "KubeSchedulerConfig":
				return fmt.Errorf("worker %s inherits control-plane document %s; move it to the control-plane or node layer", node.Name, kind)
			}
		}
		if kind == "LinkConfig" {
			addresses, _ := doc["addresses"].([]any)
			for _, a := range addresses {
				m, _ := a.(map[string]any)
				value, _ := m["address"].(string)
				prefix, e := netip.ParsePrefix(value)
				if e != nil {
					continue
				}
				foundStatic = true
				if prefix.Addr().String() == node.Address {
					matches = true
				}
			}
		}
	}
	if foundStatic && !matches {
		return fmt.Errorf("node %s management address %s does not match its static LinkConfig addresses", node.Name, node.Address)
	}
	return nil
}

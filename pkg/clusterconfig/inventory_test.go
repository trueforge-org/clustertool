package clusterconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trueforge-org/clustertool/embed"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"gopkg.in/yaml.v3"
)

func fixture(t *testing.T) {
	t.Helper()
	oldPath, oldEnv := helper.TalosPath, helper.TalEnv
	helper.TalosPath = filepath.Join(t.TempDir(), "talos")
	helper.TalEnv = map[string]string{"CONTROL1IP": "192.168.20.210"}
	t.Cleanup(func() { helper.TalosPath, helper.TalEnv = oldPath, oldEnv })
	if err := os.MkdirAll(filepath.Join(helper.TalosPath, "patches", "nodes", "control-1"), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := embed.GenericFiles.ReadFile("generic/base/talos/clustertool.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(helper.TalosPath, "clustertool.yaml"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapConfigDoesNotResolveApplications(t *testing.T) {
	fixture(t)
	// Only Talos files exist: day-2 configuration loading must not inspect app files.
	inv, err := LoadInventory()
	if err != nil {
		t.Fatal(err)
	}
	config := inv.BootstrapConfig
	if len(config) < 2 || config[0].CRD != "kube-prometheus-stack" ||
		config[1].Manifest != "kubernetes/config/cluster-secrets.sops.yaml" {
		t.Fatalf("bootstrap configuration not retained: %+v", config)
	}
	last := config[len(config)-1]
	if last.Chart != "flux-instance" || last.Wait {
		t.Fatalf("default FluxInstance step must not wait: %+v", last)
	}
	for _, step := range config[:len(config)-1] {
		if step.Chart != "" && !step.Wait {
			t.Fatalf("chart must wait by default: %+v", step)
		}
	}
}

func TestBootstrapStepValidation(t *testing.T) {
	var config BootstrapConfig
	if err := yaml.Unmarshal([]byte("- chart: arbitrary-chart\n- manifest: custom.yaml\n- crd: monitoring\n- chart: custom-instance\n  wait: false\n"), &config); err != nil ||
		len(config) != 4 || config[0].Chart != "arbitrary-chart" || !config[0].Wait ||
		config[1].Manifest != "custom.yaml" || config[2].CRD != "monitoring" || config[3].Wait {
		t.Fatalf("parse steps: %+v, %v", config, err)
	}
	for _, value := range []string{
		"chart-name", "42", "null", "{}",
		"{chart: []}", "{crd: ''}", "{manifest: test.yaml, extra: true}",
		"{chrat: test}", "{chart: test, crd: test}", "{chart: first, chart: second}",
		"{chart: test, wait: 'false'}", "{chart: test, wait: true, wait: false}",
		"{crd: test, wait: false}", "{manifest: test.yaml, wait: false}",
	} {
		if err := yaml.Unmarshal([]byte("["+value+"]"), &config); err == nil {
			t.Fatalf("accepted invalid step %s", value)
		}
	}
	if err := yaml.Unmarshal([]byte("steps: []"), &config); err == nil {
		t.Fatal("accepted a mapping instead of the bootstrap list")
	}
}

func TestRequiredClusterConfig(t *testing.T) {
	fixture(t)
	path := filepath.Join(helper.TalosPath, "clustertool.yaml")
	valid, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, replacement := range [][2]string{
		{"clustertool/v1", "clustertool/v2"},
		{"kind: ClusterConfig", "kind: Other"},
		{"clusterName: main", ""},
		{"clusterName: main", "clusterName: \"\""},
		{"clusterName: main", "clusterName: \"   \""},
		{"apiVersion: clustertool/v1", "version: 1"},
		{"${CONTROL1IP}", "${UNDEFINED}"},
		{"${CONTROL1IP}", "192.0.2.1/24"},
	} {
		if err := os.WriteFile(path, []byte(strings.Replace(string(valid), replacement[0], replacement[1], 1)), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadInventory(); err == nil {
			t.Fatalf("accepted invalid config: %v", replacement)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	// Old files and environment values must not silently select a node.
	if err := os.WriteFile(filepath.Join(helper.TalosPath, "inventory.yaml"), valid, 0600); err != nil {
		t.Fatal(err)
	}
	helper.TalEnv["MASTER1IP_IP"] = "192.0.2.1"
	if _, err := LoadInventory(); err == nil || !strings.Contains(err.Error(), "required clustertool.yaml") {
		t.Fatalf("missing config fell back: %v", err)
	}
}

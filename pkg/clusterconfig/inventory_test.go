package clusterconfig

import (
	"fmt"
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
	if _, err := LoadInventory(); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapConfigValidation(t *testing.T) {
	var config BootstrapConfig
	if err := yaml.Unmarshal([]byte(`crd: [monitoring, other]
steps:
  - chart: arbitrary-chart
  - manifest: custom.yaml
  - chart: custom-instance
    wait: false
`), &config); err != nil ||
		len(config.CRD) != 2 || config.CRD[0] != "monitoring" || config.CRD[1] != "other" ||
		len(config.Steps) != 3 || config.Steps[0].Chart != "arbitrary-chart" || !config.Steps[0].Wait ||
		config.Steps[1].Manifest != "custom.yaml" || config.Steps[2].Wait {
		t.Fatalf("parse bootstrap: %+v, %v", config, err)
	}
	for _, value := range []string{
		"chart-name", "42", "null", "{}",
		"{chart: []}", "{manifest: test.yaml, extra: true}",
		"{chrat: test}", "{chart: test, manifest: test.yaml}", "{chart: first, chart: second}",
		"{chart: test, wait: 'false'}", "{chart: test, wait: true, wait: false}",
		"{crd: test}", "{manifest: test.yaml, wait: false}",
	} {
		if err := yaml.Unmarshal([]byte("steps: ["+value+"]"), &config); err == nil {
			t.Fatalf("accepted invalid step %s", value)
		}
	}
	for _, value := range []string{
		"[]", "[chart: test]", "crd: monitoring", "crd: null", "crd: [42]", "crd: ['']",
		"crd: [{chart: test}]", "steps: {}", "steps: null", "charts: []", "crd: []\ncrd: []",
	} {
		if err := yaml.Unmarshal([]byte(value), &config); err == nil {
			t.Fatalf("accepted invalid bootstrap %s", value)
		}
	}
	for _, value := range []string{"{crd: [], steps: []}", "{crd: []}", "{steps: []}", "{}"} {
		if err := yaml.Unmarshal([]byte(value), &config); err != nil || len(config.CRD) != 0 || len(config.Steps) != 0 {
			t.Fatalf("empty bootstrap %s: %+v, %v", value, config, err)
		}
	}
}

func TestBootstrapErrorsUseSourceLines(t *testing.T) {
	fixture(t)
	helper.TalEnv["EMPTY_CHART"] = ""
	const config = `# Config with variables, comments and blank lines.
apiVersion: clustertool/v1
kind: ClusterConfig

clusterName: test
talosVersion: v1.14.5
kubernetesVersion: v1.37.2

nodes:
  - name: control-1
    role: control-plane
    address: ${CONTROL1IP}

# Each bootstrap list runs in order.
bootstrap:
%s
`
	const steps = "  crd: [monitoring]\n  steps:\n    - chart: example-controller\n\n"
	for _, tc := range []struct{ name, bootstrap, marker, location, message string }{
		{"unknown field", steps + "    - chrt: example-instance", "chrt:", "bootstrap step 2", `unknown bootstrap field 'chrt'`},
		{"invalid wait", steps + "    - chart: example-instance\n      wait: invalid", "wait:", "bootstrap step 2", "wait must be true or false"},
		{"empty variable", steps + "    - chart: ${EMPTY_CHART}", "- chart: ${EMPTY_CHART}", "bootstrap step 2", "chart requires a non-empty string"},
		{"invalid CRD", "  crd:\n    - monitoring\n\n    - 42\n  steps: []", "- 42", "bootstrap.crd entry 2", "expected a non-empty HelmRelease name"},
		{"invalid list", "  crd: monitoring\n  steps: []", "crd:", "bootstrap.crd", "expected a list"},
		{"unknown section", "  crd: []\n\n  charts: []", "charts:", "bootstrap", "unknown bootstrap field 'charts'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := fmt.Sprintf(config, tc.bootstrap)
			line := strings.Count(data[:strings.Index(data, tc.marker)], "\n") + 1
			path := filepath.Join(helper.TalosPath, "clustertool.yaml")
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadInventory()
			want := fmt.Sprintf("read clustertool.yaml: %s, line %d: %s", tc.location, line, tc.message)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("got %v, want %s", err, want)
			}
		})
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

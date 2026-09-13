package talosconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trueforge-org/clustertool/pkg/helper"
)

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

func TestExamplesAreInactive(t *testing.T) {
	fixture(t)
	helper.TalEnv["DOCKERHUB_USER"] = "only-one-variable"
	if err := os.WriteFile(filepath.Join(helper.TalosPath, "examples", "invalid.yaml"), []byte("invalid: ["), 0600); err != nil {
		t.Fatal(err)
	}
	env := make(map[string]string, len(helper.TalEnv)+1)
	for key, value := range helper.TalEnv {
		env[key] = value
	}
	env["TALOS_VERSION"] = "v1.14.0"
	dirs := []string{"all", "control-plane", filepath.Join("nodes", "control-1")}
	if _, err := renderPatchDirs(t.TempDir(), dirs, env); err != nil {
		t.Fatalf("inactive example or credentials affected generation: %v", err)
	}
	auth, err := os.ReadFile(filepath.Join(helper.TalosPath, "examples", "44-registry-auth.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(helper.TalosPath, "patches", "all", "44-registry-auth.yaml"), auth, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := renderPatchDirs(t.TempDir(), dirs, env); err == nil || !strings.Contains(err.Error(), "DOCKERHUB_PASSWORD") {
		t.Fatalf("active patch did not validate missing variable: %v", err)
	}
}

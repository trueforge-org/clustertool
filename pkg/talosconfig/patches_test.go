package talosconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trueforge-org/clustertool/pkg/helper"
)

func TestExamplesAreInactive(t *testing.T) {
	fixture(t)
	helper.TalEnv["DOCKERHUB_USER"] = "only-one-variable"
	if err := os.WriteFile(filepath.Join(helper.TalosPath, "example-patches", "invalid.yaml"), []byte("invalid: ["), 0600); err != nil {
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
	auth, err := os.ReadFile(filepath.Join(helper.TalosPath, "example-patches", "44-registry-auth.yaml"))
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

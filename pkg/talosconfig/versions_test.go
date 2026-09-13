package talosconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trueforge-org/clustertool/pkg/helper"
)

func TestRequiredVersions(t *testing.T) {
	fixture(t)
	path := filepath.Join(helper.TalosPath, "clustertool.yaml")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []struct{ name, value string }{{"talosVersion", "v1.14.0"}, {"kubernetesVersion", "v1.37.0"}} {
		for _, value := range []string{"", "1.14.0", "v1.14", "v01.14.0", "latest", "v1.14.0+build", "v1.14.0 --help"} {
			t.Run(field.name+"/"+value, func(t *testing.T) {
				changed := strings.Replace(string(source), field.name+": "+field.value, field.name+": \""+value+"\"", 1)
				if err := os.WriteFile(path, []byte(changed), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := LoadInventory(); err == nil || !strings.Contains(err.Error(), field.name) {
					t.Fatalf("expected %s validation error, got %v", field.name, err)
				}
			})
		}
		changed := strings.Replace(string(source), field.name+": "+field.value, field.name+": v1.38.0-rc.1", 1)
		if err := os.WriteFile(path, []byte(changed), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadInventory(); err != nil {
			t.Fatalf("valid prerelease rejected: %v", err)
		}
	}
}

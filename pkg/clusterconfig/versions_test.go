package clusterconfig

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/trueforge-org/clustertool/pkg/helper"
)

func replaceVersion(source, field, value string) string {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(field) + `:\s*\S+`)
	return re.ReplaceAllString(source, field+`: "`+value+`"`)
}

func TestRequiredVersions(t *testing.T) {
	fixture(t)

	path := filepath.Join(helper.TalosPath, "clustertool.yaml")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	for _, field := range []string{"talosVersion", "kubernetesVersion"} {
		for _, value := range []string{
			"",
			"1.14.0",
			"v1.14",
			"v01.14.0",
			"latest",
			"v1.14.0+build",
			"v1.14.0 --help",
		} {
			t.Run(field+"/"+value, func(t *testing.T) {
				changed := replaceVersion(string(source), field, value)

				if err := os.WriteFile(path, []byte(changed), 0600); err != nil {
					t.Fatal(err)
				}

				if _, err := LoadInventory(); err == nil || !strings.Contains(err.Error(), field) {
					t.Fatalf("expected %s validation error, got %v", field, err)
				}
			})
		}

		changed := replaceVersion(string(source), field, "v1.38.0-rc.1")
		if err := os.WriteFile(path, []byte(changed), 0600); err != nil {
			t.Fatal(err)
		}

		if _, err := LoadInventory(); err != nil {
			t.Fatalf("valid prerelease rejected: %v", err)
		}
	}
}

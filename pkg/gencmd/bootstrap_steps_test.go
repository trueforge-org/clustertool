package gencmd

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/trueforge-org/clustertool/pkg/clusterconfig"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"gopkg.in/yaml.v3"
)

func TestBootstrapLookupUsesMetadataAndCachesRelease(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"storage", "first", "second"} {
		path := filepath.Join(root, dir)
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		name := "duplicate"
		if dir == "storage" {
			name = "storage"
		}
		if err := os.WriteFile(filepath.Join(path, "helm-release.yaml"), []byte("metadata:\n  name: "+name+"\n  namespace: system\nspec:\n  releaseName: actual-name\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	index, err := loadHelmReleaseIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "storage", "helm-release.yaml")); err != nil {
		t.Fatal(err)
	}
	chart, err := index.lookup("storage")
	if err != nil || chart.Release.Spec.ReleaseName != "actual-name" || !chart.Wait || !chart.WaitForJobs {
		t.Fatalf("lookup lost the parsed release or waiting policy: %+v, %v", chart, err)
	}
	for _, name := range []string{"missing", "actual-name", "duplicate"} {
		if _, err := index.lookup(name); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("expected a lookup error for %s: %v", name, err)
		}
	}
}

func TestBootstrapOrderAndStopOnError(t *testing.T) {
	var steps []clusterconfig.BootstrapStep
	if err := yaml.Unmarshal([]byte(`- manifest: secrets.yaml
- chart: storage
- manifest: snapshot.yaml
- chart: custom-instance
  wait: false
`), &steps); err != nil {
		t.Fatal(err)
	}
	want := []string{"manifest:secrets.yaml", "chart:storage", "manifest:snapshot.yaml", "chart:custom-instance"}
	cause := errors.New("operation failed")
	for failAt := 0; failAt <= len(want); failAt++ {
		var got []string
		run := func(event string) error {
			got = append(got, event)
			if len(got) == failAt {
				return cause
			}
			return nil
		}
		err := runBootstrapSteps(steps, func(name string, wait bool) error {
			if wait != (name == "storage") {
				t.Fatalf("wrong wait setting for %s: %v", name, wait)
			}
			return run("chart:" + name)
		}, func(path string) error { return run("manifest:" + path) })
		expected := want
		if failAt > 0 {
			expected = want[:failAt]
			if !errors.Is(err, cause) {
				t.Fatalf("lost failure at %d: %v", failAt, err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("execution order: got %v, want %v", got, expected)
		}
	}
}

func TestBootstrapRequiresExplicitConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, bootstrap string
		emptyConfig     bool
	}{
		{"missing", "", false},
		{"blank", "bootstrap:\n", false},
		{"null", "bootstrap: null\n", false},
		{"empty lists", "bootstrap: {crd: [], steps: []}\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withSingleNodeFixture(t)
			t.Chdir(t.TempDir())
			if err := os.WriteFile(".sops.yaml", []byte("creation_rules: []\n"), 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(helper.TalosPath, "clustertool.yaml")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(data, []byte(tc.bootstrap)...), 0600); err != nil {
				t.Fatal(err)
			}
			// Day-2 inventory loading must still accept all four forms.
			if _, err := clusterconfig.LoadInventory(); err != nil {
				t.Fatal(err)
			}
			// A rejected forwarded flag stops the accepted empty configuration before any cluster action.
			args := []string{"--nodes"}
			want := "requires an explicit bootstrap configuration"
			if tc.emptyConfig {
				want = ValidateExtraArgs(args).Error()
			}
			if err := RunBootstrap(args); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("got %v, want %s", err, want)
			}
		})
	}
}

package gencmd

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/trueforge-org/clustertool/pkg/clusterconfig"
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
	var config clusterconfig.BootstrapConfig
	if err := yaml.Unmarshal([]byte(`- crd: monitoring
- manifest: secrets.yaml
- chart: storage
- crd: other
- manifest: snapshot.yaml
- chart: custom-instance
  wait: false
`), &config); err != nil {
		t.Fatal(err)
	}
	want := []string{"crd:monitoring", "manifest:secrets.yaml", "chart:storage", "crd:other", "manifest:snapshot.yaml", "chart:custom-instance"}
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
		err := runBootstrapSteps(config, func(name string, wait bool) error {
			if wait != (name == "storage") {
				t.Fatalf("wrong wait setting for %s: %v", name, wait)
			}
			return run("chart:" + name)
		}, func(name string) error { return run("crd:" + name) },
			func(path string) error { return run("manifest:" + path) })
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

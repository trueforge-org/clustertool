package helmhandler

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/trueforge-org/clustertool/pkg/helper"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"
)

func TestCRDRenderingSharesValuesAndExcludesHooksAndWorkloads(t *testing.T) {
	root := t.TempDir()
	oldEnv := helper.TalEnv
	helper.TalEnv = map[string]string{"TEST_BOOTSTRAP_VALUE": "substituted"}
	t.Cleanup(func() { helper.TalEnv = oldEnv })
	files := map[string]string{
		"Chart.yaml":                   "apiVersion: v2\nname: test\nversion: 1.2.3\n",
		"values.yaml":                  "choice: defaults\nfromDefault: preserved\nfromHR: default\n",
		"crds/plain.yaml":              "apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n  name: packaged.example.test\n",
		"charts/child/Chart.yaml":      "apiVersion: v2\nname: child\nversion: 1.0.0\n",
		"charts/child/crds/plain.yaml": "apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n  name: child.example.test\n",
		"templates/crd.yaml": `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: templated.example.test
  annotations:
    choice: {{ .Values.choice | quote }}
    default: {{ .Values.fromDefault | quote }}
    hr: {{ .Values.fromHR | quote }}
    file: {{ .Values.fromFile | quote }}
    identity: {{ printf "%s/%s" .Release.Namespace .Release.Name | quote }}
`,
		"templates/hooks.yaml":             "apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n  name: hook.example.test\n  annotations:\n    helm.sh/hook: pre-install\n",
		"templates/workload.yaml":          "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: excluded\n",
		"release/values.yaml":              "choice: file\nfromFile: preserved\n",
		"release/bootstrap-values.yaml.ct": "choice: bootstrap\n",
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	release := &HelmRelease{Metadata: Metadata{Name: "lookup-name", Namespace: "system"}, Spec: Spec{
		ReleaseName: "actual-name", Values: map[string]interface{}{"choice": "hr", "fromHR": "${TEST_BOOTSTRAP_VALUE}"},
	}}
	chart, vals, err := prepareChart(t.TempDir(), root, "test", "1.2.3", filepath.Join(root, "release"), release)
	if err != nil {
		t.Fatal(err)
	}
	data, err := renderChartCRDs(chart, release, vals, "v1.37.2")
	if err != nil {
		t.Fatal(err)
	}
	decoder := kubeyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	objects := map[string]unstructured.Unstructured{}
	for {
		var object unstructured.Unstructured
		if err := decoder.Decode(&object); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if object.GetKind() != "CustomResourceDefinition" || object.GetLabels()["app.kubernetes.io/managed-by"] != "Helm" ||
			object.GetAnnotations()["meta.helm.sh/release-name"] != "actual-name" || object.GetAnnotations()["meta.helm.sh/release-namespace"] != "system" {
			t.Fatalf("unexpected resource or ownership: %v", object.Object)
		}
		objects[object.GetName()] = object
	}
	if len(objects) != 3 || objects["packaged.example.test"].Object == nil || objects["child.example.test"].Object == nil {
		t.Fatalf("expected packaged, subchart and templated CRDs only: %v", objects)
	}
	templated := objects["templated.example.test"]
	for key, value := range map[string]string{"choice": "bootstrap", "default": "preserved", "hr": "substituted", "file": "preserved", "identity": "system/actual-name"} {
		if templated.GetAnnotations()[key] != value {
			t.Fatalf("incorrect effective value %s: %v", key, templated.GetAnnotations())
		}
	}
}

func TestCRDTemporaryFilesRemovedOnPreparationFailure(t *testing.T) {
	oldCache := helper.HelmCache
	helper.HelmCache = t.TempDir()
	t.Cleanup(func() { helper.HelmCache = oldCache })
	release := &HelmRelease{Metadata: Metadata{Name: "broken"}, Spec: Spec{Chart: Chart{Spec: ChartSpec{SourceRef: SourceRef{Name: "local"}}}}}
	repos := map[string]*HelmRepo{"local": {Spec: HelmRepoSpec{URL: filepath.Join(t.TempDir(), "missing-chart")}}}
	if err := ApplyChartCRDs(context.Background(), HelmChart{Release: release}, repos, "v1.37.2"); err == nil {
		t.Fatal("expected preparation failure")
	}
	entries, err := os.ReadDir(helper.HelmCache)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary files retained: %v, %v", entries, err)
	}
}

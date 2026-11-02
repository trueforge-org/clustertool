package cmd

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trueforge-org/clustertool/embed"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"github.com/trueforge-org/clustertool/pkg/initfiles"
	"github.com/trueforge-org/clustertool/pkg/sops"
	"gopkg.in/yaml.v3"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

func TestSingleClusterRepositoryLayout(t *testing.T) {
	cache := t.TempDir()
	t.Chdir(t.TempDir())
	if err := os.CopyFS(cache, embed.GenericFiles); err != nil {
		t.Fatal(err)
	}
	oldRoot, oldBase, oldKube, oldEnv := helper.RootCache, helper.BaseCache, helper.KubeCache, helper.TalEnv
	t.Cleanup(func() {
		helper.RootCache, helper.BaseCache, helper.KubeCache, helper.TalEnv = oldRoot, oldBase, oldKube, oldEnv
	})
	helper.RootCache = filepath.Join(cache, "generic", "root")
	helper.BaseCache = filepath.Join(cache, "generic", "base")
	helper.KubeCache = filepath.Join(cache, "generic", "kubernetes")
	if err := initfiles.InitFiles(); err != nil {
		t.Fatal(err)
	}
	if !initfiles.CheckRunAgainFileExists() {
		t.Fatal("first init did not request completion")
	}
	settingsPath := "kubernetes/config/cluster-secrets.sops.yaml"
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := yaml.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if settings["metadata"].(map[string]any)["name"] != "cluster-secrets" {
		t.Fatal("shared configuration Secret has the wrong name")
	}
	values := settings["stringData"].(map[string]any)
	for key, value := range map[string]string{
		"DOMAIN_0": "dummy.local", "DOMAIN_0_EMAIL": "admin@dummy.local",
		"DOMAIN_0_CLOUDFLARE_TOKEN": "dummy-token", "GITHUB_REPOSITORY": "https://github.com/example/cluster.git",
	} {
		values[key] = value
	}
	for key := range values {
		t.Setenv(key, os.Getenv(key))
	}
	t.Setenv("CLUSTERNAME", os.Getenv("CLUSTERNAME"))
	t.Setenv("SOPS_AGE_KEY_FILE", os.Getenv("SOPS_AGE_KEY_FILE"))
	data, err = yaml.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	writeLayoutFile(t, settingsPath, data)
	// A dummy existing identity avoids invoking Talos in this layout test.
	writeLayoutFile(t, "talos/secrets.sops.yaml", []byte("dummy: retained-identity\n"))
	if err := initfiles.InitFiles(); err != nil {
		t.Fatal(err)
	}
	if initfiles.CheckRunAgainFileExists() {
		t.Fatal("completed init retained RUNAGAIN")
	}
	for _, path := range []string{
		"talos/clustertool.yaml", "talos/example-patches/44-registry-auth.yaml",
		"talos/patches/nodes/control-1/00-install.yaml", "kubernetes/apps/flux-system/sops-age.sops.yaml",
		"kubernetes/flux-entry/ks.yaml", "kubernetes/repositories/helm/cilium.yaml",
		"kubernetes/repositories/oci/flux-instance.yaml",
		"kubernetes/apps/other/librespeed/app/namespace.yaml",
		"kubernetes/apps/system/longhorn/jobs/kustomization.yaml",
		"kubernetes/apps/system/system-upgrade/tuppr/plans/kustomization.yaml",
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing generated path %s: %v", path, err)
		}
	}
	for _, path := range []string{"clusters", "repositories", "secrets", "flux-entry", "bootstrap", "talos/examples", "kubernetes/apps/apps", "kubernetes/apps/flux-system/flux-instance/app/sops-age.sops.yaml"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("unexpected old path %s: %v", path, err)
		}
	}
	cilium, err := os.ReadFile("kubernetes/apps/kube-system/cilium/app/helm-release.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cilium, []byte("    cluster:")) || bytes.Contains(cilium, []byte("REPLACEWITHCLUSTERNAME")) {
		t.Fatal("Cilium still configures cluster identity")
	}
	flux, err := os.ReadFile("kubernetes/apps/flux-system/flux-instance/app/helm-release.yaml")
	if err != nil || !bytes.Contains(flux, []byte("path: kubernetes/flux-entry")) || !bytes.Contains(flux, []byte("ssh://git@github.com/example/cluster.git")) {
		t.Fatalf("Flux sync configuration was not rendered: %v", err)
	}

	// Components are optional user content and must remain outside genks discovery.
	component := "kubernetes/components/example/kustomization.yaml"
	writeLayoutFile(t, component, []byte("apiVersion: kustomize.config.k8s.io/v1alpha1\nkind: Component\nlabels:\n  - pairs:\n      example.test/component: enabled\n"))
	app := "kubernetes/apps/other/librespeed/app"
	appKustomization, err := os.ReadFile(app + "/kustomization.yaml")
	if err != nil {
		t.Fatal(err)
	}
	writeLayoutFile(t, app+"/kustomization.yaml", append(appKustomization, []byte("components:\n  - ../../../../components/example\n")...))
	// New app content exercises discovery rather than only rerunning a finished tree.
	writeLayoutFile(t, "kubernetes/apps/other/custom/app/namespace.yaml", []byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: custom\n"))
	before := layoutSnapshot(t)
	outside := make(map[string]string)
	for path, data := range before {
		if !strings.HasPrefix(path, "kubernetes/apps/") || strings.HasSuffix(path, "/ks.yaml") {
			outside[path] = data
		}
	}
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"genks", "kustomizations"} {
		if out, err := commandProcess(t, dir, command); err != nil {
			t.Fatalf("%s failed: %v %s", command, err, out)
		}
		after := layoutSnapshot(t)
		for path, data := range outside {
			if after[path] != data {
				t.Fatalf("%s modified protected file %s", command, path)
			}
		}
		for path := range after {
			if _, existed := before[path]; !existed && !strings.HasPrefix(path, "kubernetes/apps/") {
				t.Fatalf("%s created file outside kubernetes/apps: %s", command, path)
			}
		}
	}
	ks, err := os.ReadFile("kubernetes/apps/other/custom/ks.yaml")
	if err != nil || !bytes.Contains(ks, []byte("path: kubernetes/apps/other/custom/app")) {
		t.Fatalf("new application was not discovered: %v", err)
	}

	// Flux generates this wrapper when the sync directory only contains manifests.
	writeLayoutFile(t, "kubernetes/flux-entry/kustomization.yaml", []byte("apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n  - ks.yaml\n"))
	entry, err := krusty.MakeKustomizer(krusty.MakeDefaultOptions()).Run(filesys.MakeFsOnDisk(), "kubernetes/flux-entry")
	if err != nil {
		t.Fatal(err)
	}
	wantPaths := map[string]string{"flux-entry-secrets": "./kubernetes/config", "flux-entry-repos": "./kubernetes/repositories", "flux-entry": "./kubernetes/apps"}
	if entry.Size() != len(wantPaths) {
		t.Fatalf("expected three entry resources, got %d", entry.Size())
	}
	for _, resource := range entry.Resources() {
		path, err := resource.GetString("spec.path")
		if err != nil || path != wantPaths[resource.GetName()] {
			t.Fatalf("unexpected path for %s: %q %v", resource.GetName(), path, err)
		}
		if resource.GetName() != "flux-entry-secrets" {
			data, err := resource.AsYAML()
			// Both the entry and its child Kustomizations substitute the shared Secret.
			if err != nil || bytes.Count(data, []byte("name: cluster-secrets")) != 2 {
				t.Fatalf("wrong substitution Secret for %s: %v", resource.GetName(), err)
			}
		}
		built, err := krusty.MakeKustomizer(krusty.MakeDefaultOptions()).Run(filesys.MakeFsOnDisk(), path)
		if err != nil {
			t.Fatalf("build %s: %v", path, err)
		}
		// Each child Flux path must also resolve, including jobs and plans.
		for _, child := range built.Resources() {
			if child.GetKind() == "Secret" && child.GetName() == "sops-age" {
				t.Fatal("bootstrap age-key Secret was included in Flux resources")
			}
			if child.GetApiVersion() != "kustomize.toolkit.fluxcd.io/v1" || child.GetKind() != "Kustomization" {
				continue
			}
			childPath, err := child.GetString("spec.path")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := krusty.MakeKustomizer(krusty.MakeDefaultOptions()).Run(filesys.MakeFsOnDisk(), childPath); err != nil {
				t.Fatalf("build child %s: %v", childPath, err)
			}
		}
	}

	writeLayoutFile(t, "kubernetes/components/example/dummy.sops.yaml", []byte("apiVersion: v1\nkind: Secret\nmetadata:\n  name: dummy\nstringData:\n  token: dummy-component-token\n"))
	if err := sops.EncryptAllFiles(); err != nil {
		t.Fatal(err)
	}
	files, err := sops.ExecuteCheck(false)
	if err != nil || len(files) != 5 {
		t.Fatalf("expected settings, Talos, age, deploy key and component secrets: %d %v", len(files), err)
	}
	for _, file := range files {
		if !file.Encrypted {
			t.Errorf("not encrypted: %s", file.Path)
		}
	}
	if out, err := commandProcess(t, dir, "checkcrypt"); err != nil {
		t.Fatalf("checkcrypt rejected encrypted fixture: %v %s", err, out)
	}
	if err := sops.DecryptFiles(); err != nil {
		t.Fatal(err)
	}
	if out, err := commandProcess(t, dir, "checkcrypt"); err == nil {
		t.Fatalf("checkcrypt accepted plaintext fixture: %s", out)
	}
	if data, err := os.ReadFile("talos/secrets.sops.yaml"); err != nil || !bytes.Contains(data, []byte("retained-identity")) {
		t.Fatalf("Talos identity changed: %v", err)
	}
	if out, err := exec.Command("git", "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture Git repository: %v %s", err, out)
	}
	for _, path := range []string{"talos/generated/control-1.yaml", "talos/.bootstrap-in-progress.json", "age.agekey", "talosconfig"} {
		if out, err := exec.Command("git", "check-ignore", path).CombinedOutput(); err != nil {
			t.Errorf("generated .gitignore does not cover %s: %v %s", path, err, out)
		}
	}
	if out, err := exec.Command("git", "add", "--", settingsPath, "kubernetes/apps/flux-system/sops-age.sops.yaml").CombinedOutput(); err != nil {
		t.Fatalf("stage dummy secrets: %v %s", err, out)
	}
	if out, err := commandProcess(t, dir, "precommit"); err != nil {
		t.Fatalf("precommit rejected new settings path: %v %s", err, out)
	}
	for _, path := range []string{settingsPath, "kubernetes/apps/flux-system/sops-age.sops.yaml"} {
		index, err := exec.Command("git", "show", ":"+path).Output()
		if err != nil || !bytes.Contains(index, []byte("ENC[")) {
			t.Fatalf("precommit did not encrypt staged %s: %v", path, err)
		}
	}
}

func writeLayoutFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func layoutSnapshot(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	if err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(path)] = string(data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return files
}

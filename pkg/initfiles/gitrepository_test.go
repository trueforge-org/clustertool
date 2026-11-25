package initfiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	appembed "github.com/trueforge-org/clustertool/embed"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"gopkg.in/yaml.v3"
)

func TestGitRepositoryFluxTemplate(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings map[string]string
		want     string
	}{
		{"github", map[string]string{"GIT_REPOSITORY": "https://github.com/owner/repo.git"}, "ssh://git@github.com/owner/repo.git"},
		{"gitlab", map[string]string{"GIT_REPOSITORY": "git@gitlab.com:group/subgroup/repo.git"}, "ssh://git@gitlab.com/group/subgroup/repo.git"},
		{"custom port", map[string]string{"GIT_REPOSITORY": "ssh://custom@192.0.2.10:2222/group/subgroup/repo.git"}, "ssh://custom@192.0.2.10:2222/group/subgroup/repo.git"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			oldFile, oldKube, oldApps, oldEnv := helper.ClusterSettingsFile, helper.KubernetesPath, helper.KubernetesAppsPath, helper.TalEnv
			t.Cleanup(func() {
				helper.ClusterSettingsFile, helper.KubernetesPath, helper.KubernetesAppsPath, helper.TalEnv = oldFile, oldKube, oldApps, oldEnv
			})
			t.Setenv("GIT_REPOSITORY", "")
			helper.ClusterSettingsFile = "cluster-secrets.sops.yaml"
			helper.KubernetesPath = "kubernetes"
			helper.KubernetesAppsPath = filepath.Join("kubernetes", "apps")
			settings, err := yaml.Marshal(map[string]interface{}{"stringData": tc.settings})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(helper.ClusterSettingsFile, settings, 0600); err != nil {
				t.Fatal(err)
			}
			if err := LoadTalEnv(false); err != nil {
				t.Fatal(err)
			}
			template, err := appembed.GenericFiles.ReadFile("generic/kubernetes/apps/flux-system/flux-instance/app/helm-release.yaml")
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(helper.KubernetesAppsPath, "flux-system", "flux-instance", "app", "helm-release.yaml")
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, template, 0600); err != nil {
				t.Fatal(err)
			}
			if err := UpdateFluxConfig(); err != nil {
				t.Fatal(err)
			}
			result, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(result), tc.want) || strings.Contains(string(result), "REPLACEWITHGITREPO") {
				t.Fatalf("wrong generated Flux URL, want %s", tc.want)
			}
			// Once rendered, init must preserve the user's Flux configuration.
			helper.TalEnv["GIT_REPOSITORY"] = "ssh://git@example.test/different/repo.git"
			if err := UpdateFluxConfig(); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(target)
			if err != nil || string(after) != string(result) {
				t.Fatal("existing Flux URL overwritten")
			}
		})
	}
}

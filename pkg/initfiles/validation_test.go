package initfiles

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trueforge-org/clustertool/pkg/helper"
	"gopkg.in/yaml.v3"
)

func TestEnvironmentErrorsReturn(t *testing.T) {
	old := helper.ClusterPath
	helper.ClusterPath = t.TempDir()
	t.Cleanup(func() { helper.ClusterPath = old })
	if err := LoadTalEnv(true); err != nil {
		t.Fatal(err)
	}
	if err := LoadTalEnv(false); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing environment: %v", err)
	}
	if err := os.WriteFile(filepath.Join(helper.ClusterPath, "clusterenv.yaml"), []byte("key: [\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := LoadTalEnv(true); err == nil {
		t.Fatal("malformed environment accepted")
	}
}

func TestNetworkValidationRetainsAllNodeChecks(t *testing.T) {
	oldPath, oldTalos, oldEnv := helper.ClusterPath, helper.TalosPath, helper.TalEnv
	helper.ClusterPath = t.TempDir()
	helper.TalosPath = filepath.Join(helper.ClusterPath, "talos")
	t.Cleanup(func() { helper.ClusterPath, helper.TalosPath, helper.TalEnv = oldPath, oldTalos, oldEnv })
	for _, name := range []string{"cp", "worker"} {
		if err := os.MkdirAll(filepath.Join(helper.TalosPath, "patches", "nodes", name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	config := "apiVersion: clustertool/v1\nkind: ClusterConfig\ntalosVersion: v1.14.0\nkubernetesVersion: v1.37.0\nbootstrapNode: cp\nnodes:\n  - name: cp\n    role: control-plane\n    address: 192.0.2.11\n  - name: worker\n    role: worker\n    address: ${WORKER_IP}\n"
	if err := os.WriteFile(filepath.Join(helper.TalosPath, "clustertool.yaml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	base := map[string]string{"VIP": "192.0.2.10", "GATEWAY": "192.0.2.1", "HEADLAMP_IP": "192.0.2.101", "METALLB_RANGE": "192.0.2.100-192.0.2.120", "PODNET": "198.51.100.0/24", "SVCNET": "203.0.113.0/24", "WORKER_IP": "192.0.2.21", "DOMAIN_0": "example.test", "DOMAIN_0_EMAIL": "operator@example.test", "DOMAIN_0_CLOUDFLARE_TOKEN": "test-value"}
	for key, value := range base {
		t.Setenv(key, value)
	}
	t.Setenv("CLUSTERNAME", helper.ClusterName)
	for _, tc := range []struct{ key, value, want string }{
		{"WORKER_IP", "192.0.2.21", ""},
		{"WORKER_IP", "192.0.2.10", "overlaps VIP"},
		{"WORKER_IP", "192.0.2.1", "gateway"},
		{"WORKER_IP", "192.0.2.102", "METALLB_RANGE"},
		{"WORKER_IP", "198.51.100.10", "PODNET"},
		{"WORKER_IP", "203.0.113.10", "SVCNET"},
		{"VIP", "192.0.2.103", "METALLB_RANGE"},
		{"GATEWAY", "192.0.2.103", "METALLB_RANGE"},
		{"VIP", "198.51.100.10", "PODNET"},
		{"GATEWAY", "203.0.113.10", "SVCNET"},
		{"HEADLAMP_IP", "192.0.2.50", "HEADLAMP_IP"},
		{"METALLB_RANGE", "invalid", "METALLB_RANGE"},
		{"METALLB_RANGE", "192.0.2.120-192.0.2.100", "METALLB_RANGE"},
		{"PODNET", "192.0.2.104/30", "METALLB_RANGE"},
		{"PODNET", "invalid", "PODNET"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			values := map[string]string{}
			for k, v := range base {
				values[k] = v
			}
			values[tc.key] = tc.value
			data, err := yaml.Marshal(values)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(helper.ClusterPath, "clusterenv.yaml"), data, 0600); err != nil {
				t.Fatal(err)
			}
			err = CheckEnvVariables()
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %s, got %v", tc.want, err)
			}
		})
	}
}

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
	old := helper.ClusterSettingsFile
	helper.ClusterSettingsFile = filepath.Join(t.TempDir(), "cluster-secrets.sops.yaml")
	t.Cleanup(func() { helper.ClusterSettingsFile = old })
	if err := LoadTalEnv(true); err != nil {
		t.Fatal(err)
	}
	if err := LoadTalEnv(false); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing environment: %v", err)
	}
	if err := os.WriteFile(helper.ClusterSettingsFile, []byte("key: [\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := LoadTalEnv(true); err == nil {
		t.Fatal("malformed environment accepted")
	}
}

func TestClusterSettingsRejectInvalidStringData(t *testing.T) {
	old := helper.ClusterSettingsFile
	helper.ClusterSettingsFile = filepath.Join(t.TempDir(), "cluster-secrets.sops.yaml")
	t.Cleanup(func() { helper.ClusterSettingsFile = old })
	for _, source := range []string{
		"data:\n  PASSWORD: c2VjcmV0\n",
		"stringData:\n  PORT: 443\n",
		"stringData:\n  ENABLED: true\n",
		"stringData:\n  ENVOY_INTERNAL_IP:\n",
	} {
		if err := os.WriteFile(helper.ClusterSettingsFile, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		if err := LoadTalEnv(false); err == nil {
			t.Fatalf("invalid cluster settings accepted: %s", source)
		}
	}
}

func TestOptionalSettingsValidation(t *testing.T) {
	oldPath, oldFile, oldTalos, oldEnv := helper.ClusterPath, helper.ClusterSettingsFile, helper.TalosPath, helper.TalEnv
	helper.ClusterPath = t.TempDir()
	helper.ClusterSettingsFile = filepath.Join(helper.ClusterPath, "cluster-secrets.sops.yaml")
	helper.TalosPath = filepath.Join(helper.ClusterPath, "talos")
	t.Cleanup(func() {
		helper.ClusterPath, helper.ClusterSettingsFile, helper.TalosPath, helper.TalEnv = oldPath, oldFile, oldTalos, oldEnv
	})
	if err := os.MkdirAll(filepath.Join(helper.TalosPath, "patches", "nodes", "cp"), 0700); err != nil {
		t.Fatal(err)
	}
	config := "apiVersion: clustertool/v1\nkind: ClusterConfig\nclusterName: homelab\ntalosVersion: v1.14.0\nkubernetesVersion: v1.37.0\nnodes:\n  - name: cp\n    role: control-plane\n    address: ${CONTROL1IP}\n"
	if err := os.WriteFile(filepath.Join(helper.TalosPath, "clustertool.yaml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	base := map[string]string{
		"VIP": "192.0.2.10", "GATEWAY": "192.0.2.1", "CONTROL1IP": "192.0.2.11",
		"PODNET": "198.51.100.0/24", "SVCNET": "203.0.113.0/24",
		"METALLB_RANGE":     "192.0.2.100-192.0.2.120",
		"ENVOY_INTERNAL_IP": "192.0.2.103", "ENVOY_EXTERNAL_IP": "192.0.2.104",
		"BLOCKY_IP": "192.0.2.102", "HEADLAMP_IP": "192.0.2.101",
		"DOMAIN_0": "example.test", "DOMAIN_0_EMAIL": "operator@example.test",
		"DOMAIN_0_CLOUDFLARE_TOKEN": "test-token", "GITHUB_REPOSITORY": "https://github.com/example/cluster.git",
	}
	for key, value := range base {
		t.Setenv(key, value)
	}
	optionalKeys := []string{"METALLB_RANGE", "ENVOY_INTERNAL_IP", "ENVOY_EXTERNAL_IP", "BLOCKY_IP", "HEADLAMP_IP", "DOMAIN_0", "DOMAIN_0_EMAIL", "DOMAIN_0_CLOUDFLARE_TOKEN", "GITHUB_REPOSITORY"}
	type testCase struct {
		name   string
		values map[string]string
		omit   []string
		want   string
	}
	cases := []testCase{
		{name: "all settings present"},
		{name: "all optional settings absent", omit: optionalKeys},
		{name: "range absent", omit: []string{"METALLB_RANGE"}},
		{name: "VIP in former range", omit: []string{"METALLB_RANGE"}, values: map[string]string{"VIP": "192.0.2.105"}},
		{name: "gateway in former range", omit: []string{"METALLB_RANGE"}, values: map[string]string{"GATEWAY": "192.0.2.105"}},
		{name: "node in former range", omit: []string{"METALLB_RANGE"}, values: map[string]string{"CONTROL1IP": "192.0.2.105"}},
		{name: "VIP node conflict without range", omit: []string{"METALLB_RANGE"}, values: map[string]string{"VIP": "192.0.2.11"}, want: "overlaps VIP"},
		{name: "gateway node conflict without range", omit: []string{"METALLB_RANGE"}, values: map[string]string{"GATEWAY": "192.0.2.11"}, want: "gateway"},
		{name: "node pod network conflict without range", omit: []string{"METALLB_RANGE"}, values: map[string]string{"PODNET": "192.0.2.11/32"}, want: "node cp management address conflicts with PODNET"},
		{name: "VIP service network conflict without range", omit: []string{"METALLB_RANGE"}, values: map[string]string{"SVCNET": "192.0.2.10/32"}, want: "VIP cannot be in SVCNET"},
		{name: "invalid pod network without range", omit: []string{"METALLB_RANGE"}, values: map[string]string{"PODNET": "invalid"}, want: "invalid PODNET"},
		{name: "mixed range families", values: map[string]string{"METALLB_RANGE": "192.0.2.100-2001:db8::120"}, want: "METALLB_RANGE"},
		{name: "range with subnet prefix", values: map[string]string{"METALLB_RANGE": "192.0.2.100/24-192.0.2.120"}, want: "METALLB_RANGE"},
		{name: "non-empty text only", values: map[string]string{"DOMAIN_0": "placeholder", "DOMAIN_0_EMAIL": "placeholder", "DOMAIN_0_CLOUDFLARE_TOKEN": "placeholder", "GITHUB_REPOSITORY": "placeholder"}},
	}
	for _, key := range optionalKeys {
		cases = append(cases,
			testCase{name: key + " absent", omit: []string{key}},
			testCase{name: key + " empty", values: map[string]string{key: ""}, want: key},
		)
	}
	for _, key := range []string{"VIP", "GATEWAY", "PODNET", "SVCNET"} {
		cases = append(cases,
			testCase{name: key + " still required", omit: []string{key}, want: key + " cannot be empty"},
		)
	}
	for _, key := range []string{"ENVOY_INTERNAL_IP", "ENVOY_EXTERNAL_IP", "BLOCKY_IP", "HEADLAMP_IP"} {
		for _, tc := range []struct{ name, value, want string }{
			{"first range address", "192.0.2.100", ""},
			{"last range address", "192.0.2.120", ""},
			{"below range", "192.0.2.99", "must be in METALLB_RANGE"},
			{"above range", "192.0.2.121", "must be in METALLB_RANGE"},
			{"different IP family", "2001:db8::101", "must be in METALLB_RANGE"},
			{"invalid IP", "invalid", "must be an IP address without a subnet prefix"},
			{"subnet prefix", "192.0.2.101/24", "must be an IP address without a subnet prefix"},
			{"zone", "fe80::1%eth0", "must be an IP address without a subnet prefix"},
		} {
			want := tc.want
			if want != "" {
				want = key + " " + want
			}
			cases = append(cases, testCase{name: key + " " + tc.name, values: map[string]string{key: tc.value}, want: want})
		}
		cases = append(cases,
			testCase{name: key + " outside former range", omit: []string{"METALLB_RANGE"}, values: map[string]string{key: "192.0.2.50"}},
			testCase{name: key + " invalid without range", omit: []string{"METALLB_RANGE"}, values: map[string]string{key: "invalid"}, want: key + " must be an IP address"},
		)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{}
			for key, value := range base {
				values[key] = value
			}
			for key, value := range tc.values {
				values[key] = value
			}
			for _, key := range tc.omit {
				delete(values, key)
			}
			data, err := yaml.Marshal(map[string]interface{}{"stringData": values})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(helper.ClusterSettingsFile, data, 0600); err != nil {
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

func TestNetworkValidationRetainsAllNodeChecks(t *testing.T) {
	oldPath, oldFile, oldTalos, oldEnv := helper.ClusterPath, helper.ClusterSettingsFile, helper.TalosPath, helper.TalEnv
	helper.ClusterPath = t.TempDir()
	helper.ClusterSettingsFile = filepath.Join(helper.ClusterPath, "cluster-secrets.sops.yaml")
	helper.TalosPath = filepath.Join(helper.ClusterPath, "talos")
	t.Cleanup(func() {
		helper.ClusterPath, helper.ClusterSettingsFile, helper.TalosPath, helper.TalEnv = oldPath, oldFile, oldTalos, oldEnv
	})
	for _, name := range []string{"cp", "worker"} {
		if err := os.MkdirAll(filepath.Join(helper.TalosPath, "patches", "nodes", name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	config := "apiVersion: clustertool/v1\nkind: ClusterConfig\nclusterName: homelab\ntalosVersion: v1.14.0\nkubernetesVersion: v1.37.0\nnodes:\n  - name: cp\n    role: control-plane\n    address: 192.0.2.11\n  - name: worker\n    role: worker\n    address: ${WORKER_IP}\n"
	if err := os.WriteFile(filepath.Join(helper.TalosPath, "clustertool.yaml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	base := map[string]string{"VIP": "192.0.2.10", "GATEWAY": "192.0.2.1", "HEADLAMP_IP": "192.0.2.101", "METALLB_RANGE": "192.0.2.100-192.0.2.120", "PODNET": "198.51.100.0/24", "SVCNET": "203.0.113.0/24", "WORKER_IP": "192.0.2.21", "DOMAIN_0": "example.test", "DOMAIN_0_EMAIL": "operator@example.test", "DOMAIN_0_CLOUDFLARE_TOKEN": "test-value"}
	for key, value := range base {
		t.Setenv(key, value)
	}
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
			data, err := yaml.Marshal(map[string]interface{}{"stringData": values})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(helper.ClusterSettingsFile, data, 0600); err != nil {
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

package talosconfig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trueforge-org/clustertool/pkg/helper"
)

func multiFixture(t *testing.T) {
	fixture(t)
	write := func(path, content string) {
		t.Helper()
		p := filepath.Join(helper.TalosPath, path)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	install, err := os.ReadFile(filepath.Join(helper.TalosPath, "patches", "nodes", "control-1", "00-install.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	write("clustertool.yaml", "apiVersion: clustertool/v1\nkind: ClusterConfig\ntalosVersion: v1.14.0\nkubernetesVersion: v1.37.0\nbootstrapNode: control-1\nnodes:\n  - {name: control-1, role: control-plane, address: 192.0.2.11}\n  - {name: control-2, role: control-plane, address: 192.0.2.12}\n  - {name: worker-1, role: worker, address: 192.0.2.21}\n")
	for index, name := range []string{"control-1", "control-2", "worker-1"} {
		// Synthetic schematic hashes are used only for offline generation, never for image pulls.
		id := strings.Repeat(fmt.Sprint(index+1), 64)
		image := strings.Replace(string(install), "4c4acaf75b4a51d6ec95b38dc8b49fb0af5f699e7fbd12fbf246821c649b5312", id, 1)
		start := strings.Index(image, "    match:")
		end := strings.Index(image[start:], "\n") + start
		image = image[:start] + fmt.Sprintf("    match: disk.dev_path == '/dev/sd%c'", 'a'+index) + image[end:]
		write("patches/nodes/"+name+"/00-install.yaml", image)
		write("patches/nodes/"+name+"/10-hostname.yaml", "apiVersion: v1alpha1\nkind: HostnameConfig\nauto: off\nhostname: "+name+"\n")
		address := []string{"192.0.2.11", "192.0.2.12", "192.0.2.21"}[index]
		write("patches/nodes/"+name+"/20-network.yaml", "apiVersion: v1alpha1\nkind: LinkConfig\nname: eth0\nup: true\naddresses:\n  - address: "+address+"/24\n")
		write("patches/nodes/"+name+"/90-label.yaml", "apiVersion: v1alpha1\nkind: KubeNodeConfig\nlabels:\n  clustertool.test/node: "+name+"\n")
	}
	write("patches/all/99-label.yaml", "apiVersion: v1alpha1\nkind: KubeNodeConfig\nlabels:\n  clustertool.test/layer: shared\n")
	write("patches/control-plane/00-label.yaml", "apiVersion: v1alpha1\nkind: KubeNodeConfig\nlabels:\n  clustertool.test/layer: role\n")
}

func TestInventorySelection(t *testing.T) {
	multiFixture(t)
	inv, err := LoadInventory()
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"", "all"} {
		nodes, err := inv.Select(target)
		if err != nil || len(nodes) != 3 {
			t.Fatal(nodes, err)
		}
	}
	for _, target := range []string{"worker-1", "192.0.2.21"} {
		nodes, err := inv.Select(target)
		if err != nil || len(nodes) != 1 || nodes[0].Role != "worker" {
			t.Fatal(nodes, err)
		}
	}
	if _, err := inv.Select("unknown"); err == nil {
		t.Fatal("unknown node accepted")
	}
	data, _ := os.ReadFile(filepath.Join(helper.TalosPath, "clustertool.yaml"))
	for _, bad := range []string{strings.Replace(string(data), "192.0.2.12", "192.0.2.11", 1), strings.Replace(string(data), "bootstrapNode: control-1", "bootstrapNode: worker-1", 1), strings.Replace(string(data), "name: worker-1", "name: ../worker", 1)} {
		os.WriteFile(filepath.Join(helper.TalosPath, "clustertool.yaml"), []byte(bad), 0600)
		if _, err := LoadInventory(); err == nil {
			t.Fatal("invalid inventory accepted")
		}
	}
}

func TestMultiNodeGenerationIntegration(t *testing.T) {
	if os.Getenv("TALOSCTL_INTEGRATION") != "1" {
		t.Skip("requires real talosctl")
	}
	multiFixture(t)
	// Use non-default versions to prove CLI propagation, not merely defaults.
	configPath := filepath.Join(helper.TalosPath, "clustertool.yaml")
	source, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	source = bytes.ReplaceAll(source, []byte("v1.37.0"), []byte("v1.36.3"))
	source = bytes.ReplaceAll(source, []byte("v1.14.0"), []byte("v1.14.1"))
	if err := os.WriteFile(configPath, source, 0600); err != nil {
		t.Fatal(err)
	}
	// A stale environment value must not override clustertool.yaml.
	helper.TalEnv["TALOS_VERSION"] = "v1.13.0"
	if err := EnsureSecrets(); err != nil {
		t.Fatal(err)
	}
	secrets, _ := os.ReadFile(SecretsPath())
	if err := Generate(); err != nil {
		t.Fatal(err)
	}
	previous := map[string][]byte{}
	for index, name := range []string{"control-1", "control-2", "worker-1"} {
		path := filepath.Join(helper.TalosGenerated, name+".yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		previous[name] = data
		docs := readDocuments(t, data)
		if machine, ok := docs["/"]["machine"].(map[string]any); ok && machine["kubelet"] != nil {
			t.Fatal("legacy kubelet remains", name)
		}
		for _, volume := range []string{"longhorn", "openebs"} {
			if docs["UserVolumeConfig/"+volume]["volumeType"] != "directory" {
				t.Fatal("missing directory volume", name, volume)
			}
		}
		kinds := []string{"KubeletConfig"}
		if index < 2 {
			kinds = append(kinds, "KubeAPIServerConfig", "KubeControllerManagerConfig", "KubeSchedulerConfig")
		}
		for _, kind := range kinds {
			image, err := GeneratedNodeValue(path, kind, "image")
			if err != nil || !strings.HasSuffix(image, ":v1.36.3") {
				t.Fatal(name, kind, image, err)
			}
		}
		selector := docs["UnattendedInstallConfig/"]["provisioning"].(map[string]any)["diskSelector"].(map[string]any)["match"]
		if selector != fmt.Sprintf("disk.dev_path == '/dev/sd%c'", 'a'+index) {
			t.Fatal("disk selector changed", selector)
		}
		if index == 2 {
			for _, forbidden := range []string{"kind: Layer2VIPConfig", "kind: KubeAPIServerConfig", "kind: KubeTalosAPIAccessConfig", "kind: KubeProxyConfig"} {
				if strings.Contains(string(data), forbidden) {
					t.Fatalf("worker inherited %s", forbidden)
				}
			}
		}
		image, err := GeneratedNodeValue(path, "UnattendedInstallConfig", "installer", "image")
		if err != nil || !strings.Contains(image, strings.Repeat(fmt.Sprint(index+1), 64)+":v1.14.1") {
			t.Fatal(name, image, err)
		}
		role, err := GeneratedNodeValue(path, "", "machine", "type")
		expected := "controlplane"
		if index == 2 {
			expected = "worker"
		}
		if err != nil || role != expected {
			t.Fatal(name, role, err)
		}
		value, err := GeneratedNodeValue(path, "KubeNodeConfig", "labels", "clustertool.test/layer")
		expected = "role"
		if index == 2 {
			expected = "shared"
		}
		if err != nil || value != expected {
			t.Fatal(name, value, err)
		}
	}
	if helper.TalEnv["TALOS_VERSION"] != "v1.13.0" {
		t.Fatal("generation mutated source environment")
	}
	clientConfig, err := os.ReadFile(TalosconfigPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{"192.0.2.11", "192.0.2.12"} {
		if !strings.Contains(string(clientConfig), address) {
			t.Fatalf("missing control-plane endpoint %s", address)
		}
	}
	if strings.Contains(string(clientConfig), "192.0.2.21") {
		t.Fatal("worker used as client endpoint")
	}
	networkPath := filepath.Join(helper.TalosPath, "patches", "nodes", "worker-1", "20-network.yaml")
	network, err := os.ReadFile(networkPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(networkPath, bytes.ReplaceAll(network, []byte("192.0.2.21"), []byte("192.0.2.99")), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Generate(); err == nil || !strings.Contains(err.Error(), "management address") {
		t.Fatalf("wrong static IP accepted: %v", err)
	}
	if err := os.WriteFile(networkPath, network, 0600); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(helper.TalosPath, "patches", "nodes", "worker-1", "99-invalid.yaml"), []byte("apiVersion: v1alpha1\nkind: UnattendedInstallConfig\nprovisioning:\n  diskSelector:\n    match: unknown.field > 0\n"), 0600)
	if err := Generate(); err == nil {
		t.Fatal("invalid worker accepted")
	}
	for name, old := range previous {
		now, _ := os.ReadFile(filepath.Join(helper.TalosGenerated, name+".yaml"))
		if !bytes.Equal(old, now) {
			t.Fatal("partial publication", name)
		}
	}
	now, _ := os.ReadFile(SecretsPath())
	if !bytes.Equal(secrets, now) {
		t.Fatal("cluster identity changed")
	}
}

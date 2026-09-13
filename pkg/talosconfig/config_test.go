package talosconfig

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/trueforge-org/clustertool/embed"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"gopkg.in/yaml.v3"
)

func fixture(t *testing.T) {
	t.Helper()
	oldPath, oldGenerated, oldEnv, oldName := helper.TalosPath, helper.TalosGenerated, helper.TalEnv, helper.ClusterName
	helper.TalosPath = filepath.Join(t.TempDir(), "talos")
	helper.TalosGenerated = filepath.Join(helper.TalosPath, "generated")
	helper.ClusterName = "main"
	helper.TalEnv = map[string]string{"CLUSTERNAME": "main", "CONTROL1IP": "192.168.20.210", "VIP": "192.168.20.200", "GATEWAY": "192.168.20.1", "PODNET": "172.16.0.0/16", "SVCNET": "172.17.0.0/16"}
	t.Cleanup(func() {
		helper.TalosPath = oldPath
		helper.TalosGenerated = oldGenerated
		helper.TalEnv = oldEnv
		helper.ClusterName = oldName
	})
	sub, err := fs.Sub(embed.GenericFiles, "generic/base/talos")
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dest := filepath.Join(helper.TalosPath, path)
		if d.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		data, err := fs.ReadFile(sub, path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0600)
	}); err != nil {
		t.Fatal(err)
	}

}

func TestExistingSecretsAndIdentityGuard(t *testing.T) {
	fixture(t)
	for _, path := range []string{TalosconfigPath()} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("existing identity"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := EnsureSecrets(); err == nil {
			t.Fatal("generated a new identity for a legacy cluster")
		}
		if _, err := os.Stat(SecretsPath()); !os.IsNotExist(err) {
			t.Fatal("created new secrets")
		}
		os.Remove(path)
	}
	original := []byte("opaque encrypted secrets must remain unchanged")
	os.WriteFile(SecretsPath(), original, 0600)
	if err := EnsureSecrets(); err != nil {
		t.Fatal(err)
	}
	actual, _ := os.ReadFile(SecretsPath())
	if !bytes.Equal(original, actual) {
		t.Fatal("existing identity overwritten")
	}
}

func TestYAMLSubstitution(t *testing.T) {
	secret := "quote\": value\npassword: surprise"
	data, err := render([]byte("password: \"${PASSWORD}\"\n"), map[string]string{"PASSWORD": secret})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]string
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc) != 1 || doc["password"] != secret {
		t.Fatal("credential changed YAML structure")
	}
	if _, err := render([]byte("value: ${MISSING}\n"), nil); err == nil {
		t.Fatal("unresolved variable accepted")
	}
}

func TestNativeGenerationIntegration(t *testing.T) {
	if os.Getenv("TALOSCTL_INTEGRATION") != "1" {
		t.Skip("set TALOSCTL_INTEGRATION=1 with talosctl 1.14 on PATH")
	}
	fixture(t)
	if err := EnsureSecrets(); err != nil {
		t.Fatal(err)
	}
	secrets, _ := os.ReadFile(SecretsPath())
	if err := Generate(); err != nil {
		t.Fatal(err)
	}
	config, _ := os.ReadFile(NodeConfigPath(Node{Name: "control-1"}))
	tc, _ := os.ReadFile(TalosconfigPath())
	docs := readDocuments(t, config)
	legacy := docs["/"]
	at := func(v any, keys ...string) any {
		for _, key := range keys {
			m, ok := v.(map[string]any)
			if !ok {
				return nil
			}
			v = m[key]
		}
		return v
	}
	require := func(got, want any) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
	require(at(docs["KubeletConfig/"], "config", "imageGCHighThresholdPercent"), 50)
	require(at(docs["KubeletConfig/"], "config", "imageGCLowThresholdPercent"), 30)
	require(at(docs["KubeletConfig/"], "config", "imageMinimumGCAge"), "30m")
	require(at(legacy, "machine", "certSANs"), []any{"127.0.0.1", "192.168.20.200"})
	require(at(docs["KubeletConfig/"], "config", "maxPods"), 250)
	require(at(docs["KubeletConfig/"], "config", "shutdownGracePeriod"), "90s")
	require(at(docs["KubeletConfig/"], "config", "shutdownGracePeriodCriticalPods"), "60s")
	require(at(docs["KubeletConfig/"], "extraArgs", "rotate-server-certificates"), "true")
	require(at(docs["TimeSyncConfig/"], "ntp", "servers"), []any{"time.cloudflare.com"})
	require(at(docs["ResolverConfig/"], "nameservers"), []any{map[string]any{"address": "1.1.1.1"}, map[string]any{"address": "8.8.8.8"}})
	if at(docs["KubeNodeConfig/"], "taints", "node-role.kubernetes.io/control-plane") != nil {
		t.Fatal("control-plane scheduling is disabled")
	}
	require(at(legacy, "machine", "kubelet"), nil)
	for _, name := range []string{"longhorn", "openebs"} {
		require(at(docs["UserVolumeConfig/"+name], "volumeType"), "directory")
	}
	require(at(legacy, "cluster", "etcd", "extraArgs", "listen-metrics-urls"), "http://0.0.0.0:2381")
	require(at(docs["HostnameConfig/"], "hostname"), "k8s-control-1")
	for _, key := range []string{"enabled", "forwardKubeDNSToHost", "resolveMemberNames"} {
		require(at(docs["ResolverConfig/"], "hostDNS", key), true)
	}
	for _, name := range []string{"nvme_tcp", "vfio_pci", "uio_pci_generic"} {
		if docs["KernelModuleConfig/"+name] == nil {
			t.Fatal("missing module " + name)
		}
	}
	for _, name := range []string{"cgr.dev", "docker.io", "factory.talos.dev", "gcr.io", "ghcr.io", "k8s.gcr.io", "mcr.microsoft.com", "public.ecr.aws", "quay.io", "registry-1.docker.io", "registry.k8s.io", "tccr.io", "oci.trueforge.org"} {
		if docs["RegistryMirrorConfig/"+name] == nil {
			t.Fatal("missing mirror " + name)
		}
	}
	require(at(docs["KubeProxyConfig/"], "enabled"), false)
	if docs["KubeFlannelCNIConfig/"] != nil {
		t.Fatal("conflicting generated document remains")
	}
	if !bytes.Contains(tc, []byte("192.168.20.210")) {
		t.Fatal("management endpoint not configured")
	}
	if !strings.Contains(at(docs["UnattendedInstallConfig/"], "installer", "image").(string), "4c4acaf75b4a51d6ec95b38dc8b49fb0af5f699e7fbd12fbf246821c649b5312:v1.14.0") {
		t.Fatal("wrong schematic")
	}
	if err := EnsureSecrets(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(SecretsPath())
	if !bytes.Equal(secrets, after) {
		t.Fatal("PKI changed")
	}
	// A valid document with invalid Talos semantics must not publish either file.
	bad := filepath.Join(helper.TalosPath, "patches", "nodes", "control-1", "99-invalid.yaml")
	os.WriteFile(bad, []byte("apiVersion: v1alpha1\nkind: UnattendedInstallConfig\nprovisioning:\n  diskSelector:\n    match: nonexistent.field > 0\n"), 0600)
	if err := Generate(); err == nil {
		t.Fatal("invalid Talos document accepted")
	}
	after, _ = os.ReadFile(NodeConfigPath(Node{Name: "control-1"}))
	if !bytes.Equal(config, after) {
		t.Fatal("failed generation replaced valid machine config")
	}
	after, _ = os.ReadFile(TalosconfigPath())
	if !bytes.Equal(tc, after) {
		t.Fatal("failed generation replaced valid client config")
	}
	os.Remove(bad)
	helper.TalEnv["DOCKERHUB_USER"] = "test-user"
	helper.TalEnv["DOCKERHUB_PASSWORD"] = "a:\"b#c\\d"
	// Credentials alone must never activate an example.
	if err := Generate(); err != nil {
		t.Fatal(err)
	}
	withoutAuth, _ := os.ReadFile(NodeConfigPath(Node{Name: "control-1"}))
	if bytes.Contains(withoutAuth, []byte("kind: RegistryAuthConfig")) {
		t.Fatal("example activated automatically")
	}
	auth, err := os.ReadFile(filepath.Join(helper.TalosPath, "examples", "44-registry-auth.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(helper.TalosPath, "patches", "nodes", "control-1", "44-registry-auth.yaml"), auth, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Generate(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(NodeConfigPath(Node{Name: "control-1"}))
	docs = readDocuments(t, data)
	for _, name := range []string{"docker.io", "registry-1.docker.io"} {
		require(at(docs["RegistryAuthConfig/"+name], "password"), helper.TalEnv["DOCKERHUB_PASSWORD"])
	}
}

func readDocuments(t *testing.T, data []byte) map[string]map[string]any {
	t.Helper()
	docs := map[string]map[string]any{}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		kind, _ := doc["kind"].(string)
		name, _ := doc["name"].(string)
		docs[kind+"/"+name] = doc
	}
	return docs
}

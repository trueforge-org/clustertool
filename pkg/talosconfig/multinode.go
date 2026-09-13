package talosconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/trueforge-org/clustertool/pkg/helper"
)

func generateInventory(inv *Inventory) error {
	if _, err := os.Stat(SecretsPath()); err != nil {
		return fmt.Errorf("existing cluster secrets are required: %w", err)
	}
	work, err := os.MkdirTemp(helper.TalosPath, ".generate-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	endpoint := helper.TalEnv["VIP"]
	if endpoint == "" {
		endpoint = inv.Bootstrap().Address
	}
	if strings.Contains(endpoint, ":") {
		endpoint = "[" + endpoint + "]"
	}
	if err = runTalosctl("gen", "config", helper.ClusterName, "https://"+endpoint+":6443", "--kubernetes-version", strings.TrimPrefix(inv.KubernetesVersion, "v"), "--with-secrets", SecretsPath(), "--output", work, "--output-types", "controlplane,worker,talosconfig", "--with-docs=false", "--with-examples=false"); err != nil {
		return err
	}
	staged := filepath.Join(work, "validated")
	if err = os.Mkdir(staged, 0700); err != nil {
		return err
	}
	// Keep reserved version substitution local to this generation, not in TalEnv.
	env := make(map[string]string, len(helper.TalEnv)+1)
	for key, value := range helper.TalEnv {
		env[key] = value
	}
	env["TALOS_VERSION"] = inv.TalosVersion
	names := map[string]bool{}
	for _, node := range inv.Nodes {
		nodeWork := filepath.Join(work, node.Name+"-patches")
		if err = os.Mkdir(nodeWork, 0700); err != nil {
			return err
		}
		patches, err := renderPatchDirs(nodeWork, []string{"all", node.Role, filepath.Join("nodes", node.Name)}, env)
		if err != nil {
			return fmt.Errorf("node %s: %w", node.Name, err)
		}
		role := "worker"
		if node.Role == "control-plane" {
			role = "controlplane"
		}
		output := filepath.Join(staged, node.Name+".yaml")
		args := []string{"machineconfig", "patch", filepath.Join(work, role+".yaml")}
		for _, p := range patches {
			args = append(args, "--patch", "@"+p)
		}
		args = append(args, "--output", output)
		if err = runTalosctl(args...); err != nil {
			return fmt.Errorf("node %s: %w", node.Name, err)
		}
		if err = os.Chmod(output, 0600); err != nil {
			return err
		}
		if err = runTalosctl("validate", "--config", output, "--mode", "metal"); err != nil {
			return fmt.Errorf("node %s: %w", node.Name, err)
		}
		if err = validateNodeOutput(node, output); err != nil {
			return err
		}
		hostname, err := GeneratedNodeValue(output, "HostnameConfig", "hostname")
		if err != nil || names[hostname] {
			return fmt.Errorf("node %s requires a unique explicit HostnameConfig hostname", node.Name)
		}
		names[hostname] = true
		if _, err = GeneratedNodeValue(output, "UnattendedInstallConfig", "installer", "image"); err != nil {
			return fmt.Errorf("node %s: %w", node.Name, err)
		}
	}
	tc := filepath.Join(work, "talosconfig")
	args := append([]string{"--talosconfig", tc, "config", "endpoint"}, inv.Endpoints()...)
	if err = runTalosctl(args...); err != nil {
		return err
	}
	if err = runTalosctl("--talosconfig", tc, "config", "node", inv.Bootstrap().Address); err != nil {
		return err
	}
	data, err := os.ReadFile(tc)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(staged, "talosconfig"), data, 0600); err != nil {
		return err
	}
	return publish(staged)
}

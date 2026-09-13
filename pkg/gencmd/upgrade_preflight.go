package gencmd

import (
	"encoding/json"
	"fmt"
	"github.com/Masterminds/semver/v3"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
	"strings"
)

func imageVersion(image string) (*semver.Version, error) {
	image, _, _ = strings.Cut(image, "@")
	pos := strings.LastIndex(image, ":")
	if pos < 0 || strings.Contains(image[pos+1:], "/") {
		return nil, fmt.Errorf("image must contain an explicit version tag: %s", image)
	}
	return semver.NewVersion(image[pos+1:])
}

// PreflightUpgrade completes read-only checks for the entire selected set before
// changing any node. Talos' own dry run validates the cluster-wide Kubernetes plan.
func PreflightUpgrade(commands []Command, kube Command) error {
	inv, err := talosconfig.LoadInventory()
	if err != nil {
		return err
	}
	for _, cmd := range commands {
		nodes, err := inv.Select(cmd.Node)
		if err != nil {
			return err
		}
		node := nodes[0]
		if err := waitReady(node.Address); err != nil {
			return fmt.Errorf("node %s upgrade preflight: %w", node.Name, err)
		}
		canReboot, err := guardControlPlane(node.Address)
		if err != nil {
			return fmt.Errorf("node %s upgrade quorum preflight: %w", node.Name, err)
		}
		if !canReboot {
			return fmt.Errorf("node %s: rolling upgrade would lose etcd quorum; add a third healthy control plane", node.Name)
		}
		image, err := talosconfig.GeneratedNodeValue(talosconfig.NodeConfigPath(node), "UnattendedInstallConfig", "installer", "image")
		if err != nil {
			return err
		}
		desired, err := imageVersion(image)
		if err != nil {
			return err
		}
		out, err := runCommand(nodeCommand("version", node, "--json", "-e", node.Address).Args, true)
		if err != nil {
			return fmt.Errorf("read live Talos version on %s: %w", node.Name, err)
		}
		var response struct {
			Version struct {
				Tag string `json:"tag"`
			} `json:"version"`
		}
		if err = json.Unmarshal(out, &response); err != nil {
			return fmt.Errorf("read Talos version on %s: %w", node.Name, err)
		}
		live, err := semver.NewVersion(response.Version.Tag)
		if err != nil {
			return fmt.Errorf("invalid live Talos version on %s: %w", node.Name, err)
		}
		if desired.LessThan(live) {
			return fmt.Errorf("node %s runs Talos %s, but its installer requests %s; update the source after Tuppr upgrades (automatic downgrades are refused)", node.Name, live, desired)
		}
		if desired.Major() != live.Major() || desired.Minor() > live.Minor()+1 {
			return fmt.Errorf("node %s: upgrade Talos one minor release at a time (%s -> %s)", node.Name, live, desired)
		}
	}
	if len(kube.Args) != 0 {
		kube, err = resolveControlPlane(kube)
		if err != nil {
			return err
		}
		args := append(append([]string{}, kube.Args...), "--dry-run")
		if _, err := runCommand(args, false); err != nil {
			return fmt.Errorf("Kubernetes upgrade preflight failed; resolve compatibility before changing nodes (upgrade Talos first with --talos-only when required): %w", err)
		}
	}
	return nil
}

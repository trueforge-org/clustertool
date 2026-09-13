package gencmd

import (
	"fmt"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
)

// ExistingControlPlane proves authenticated etcd access before choosing a join
// path. Failure is not evidence that bootstrapping a new cluster is safe.
func ExistingControlPlane(inv *talosconfig.Inventory) (string, error) {
	for _, n := range inv.Nodes {
		if n.Role != "control-plane" {
			continue
		}
		cmd := nodeCommand("etcd", n, "members", "-e", n.Address)
		if out, err := runCommand(cmd.Args, true); err == nil {
			if _, err := parseEtcdMembers(string(out), true); err == nil {
				return n.Address, nil
			}
		}
	}
	return "", fmt.Errorf("no control-plane node has authenticated etcd access")
}

func NeedsBootstrap(inv *talosconfig.Inventory) (bool, error) {
	if _, err := ExistingControlPlane(inv); err == nil {
		return false, nil
	}
	for _, n := range inv.Nodes {
		if n.Role != "control-plane" {
			continue
		}
		stage, err := checkStatus(n.Address)
		if err != nil {
			return false, fmt.Errorf("cannot establish cluster state at %s: %w", n.Name, err)
		}
		if stage != "maintenance" {
			return false, fmt.Errorf("node %s is %s but etcd is unreachable; inspect the existing installation before retrying bootstrap", n.Name, stage)
		}
	}
	return true, nil
}

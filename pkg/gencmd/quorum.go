package gencmd

import (
	"fmt"
	"github.com/trueforge-org/clustertool/pkg/nodestatus"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
	"strings"
)

// etcdMembers parses the pinned Talos CLI's table, failing closed if its format
// changes. Learners cannot be counted as voting members.
func etcdMembers(output string) ([]string, error) {
	return parseEtcdMembers(output, false)
}

func parseEtcdMembers(output string, allowLearners bool) ([]string, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 || strings.Join(strings.Fields(lines[0]), " ") != "NODE ID HOSTNAME PEER URLS CLIENT URLS LEARNER" {
		return nil, fmt.Errorf("unrecognized or empty etcd membership response")
	}
	var hosts []string
	seen := map[string]bool{}
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) != 6 || (fields[5] != "false" && (!allowLearners || fields[5] != "true")) || seen[fields[2]] {
			return nil, fmt.Errorf("etcd membership is incomplete, contains learners, or is ambiguous")
		}
		seen[fields[2]] = true
		hosts = append(hosts, fields[2])
	}
	return hosts, nil
}

// controlPlaneGuard verifies every live member, not merely the number of
// inventory entries (which may include nodes that have not joined yet).
// false means only an apply with no-reboot is safe, e.g. a two-member cluster.
func controlPlaneGuard(address string) (bool, error) {
	inv, err := talosconfig.LoadInventory()
	if err != nil {
		return false, err
	}
	selected, err := inv.Select(address)
	if err != nil {
		return false, err
	}
	if selected[0].Role != "control-plane" {
		return true, nil
	}
	out, err := runCommand(nodeCommand("etcd", selected[0], "members", "-e", address).Args, true)
	if err != nil {
		return false, fmt.Errorf("read etcd membership: %w", err)
	}
	hosts, err := etcdMembers(string(out))
	if err != nil {
		return false, err
	}
	known := map[string]talosconfig.Node{}
	for _, n := range inv.Nodes {
		if n.Role != "control-plane" {
			continue
		}
		host, err := talosconfig.GeneratedNodeValue(talosconfig.NodeConfigPath(n), "HostnameConfig", "hostname")
		if err != nil {
			return false, err
		}
		known[host] = n
	}
	for _, host := range hosts {
		n, ok := known[host]
		if !ok {
			return false, fmt.Errorf("live etcd member %s is absent from generated clustertool.yaml nodes; reconcile clustertool.yaml before maintenance", host)
		}
		if _, err := nodestatus.CheckReadyStatus(n.Address, true); err != nil {
			return false, fmt.Errorf("etcd member %s is not ready: %w", host, err)
		}
	}
	// Single-node operation necessarily has downtime. With multiple members,
	// losing one voter must retain a strict majority.
	return len(hosts) == 1 || len(hosts)-1 >= len(hosts)/2+1, nil
}

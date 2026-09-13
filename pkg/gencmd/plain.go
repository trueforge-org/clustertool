package gencmd

import (
	"fmt"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
	"strings"
)

func GenPlain(operation, target string, extra []string) []Command {
	for _, arg := range extra {
		if operation == "kubeconfig" && (arg == "-f" || arg == "--force") {
			continue
		}
		if err := ValidateExtraArgs([]string{arg}); err != nil {
			return []Command{{Err: err}}
		}
	}
	inv, err := talosconfig.LoadInventory()
	if err != nil {
		return []Command{{Err: err}}
	}
	nodes, err := inv.Select(target)
	if err != nil {
		return []Command{{Err: err}}
	}
	if operation == "kubeconfig" && (target == "" || target == "all") {
		nodes = []talosconfig.Node{inv.Bootstrap()}
	}
	if operation == "health" {
		var workers []string
		for _, n := range inv.Nodes {
			if n.Role == "worker" {
				workers = append(workers, n.Address)
			}
		}
		flags := []string{"--control-plane-nodes", strings.Join(inv.Endpoints(), ",")}
		if len(workers) > 0 {
			flags = append(flags, "--worker-nodes", strings.Join(workers, ","))
		}
		command := nodeCommand(operation, inv.Bootstrap(), append(flags, extra...)...)
		command.Failover = true
		return []Command{command}
	}
	var commands []Command
	for _, node := range nodes {
		if operation == "kubeconfig" && node.Role != "control-plane" {
			return []Command{{Err: fmt.Errorf("kubeconfig requires a control-plane node")}}
		}
		command := nodeCommand(operation, node, extra...)
		command.Failover = operation == "kubeconfig" && (target == "" || target == "all")
		commands = append(commands, command)
	}
	return commands
}

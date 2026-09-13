package gencmd

import "github.com/trueforge-org/clustertool/pkg/talosconfig"

func GenApply(target string, extra []string) []Command {
	if err := ValidateExtraArgs(extra); err != nil {
		return []Command{{Err: err}}
	}
	inv, err := talosconfig.LoadInventory()
	if err != nil {
		return []Command{{Err: err}}
	}
	nodes, err := inv.Select(target)
	if err != nil {
		return []Command{{Err: err}}
	}
	var commands []Command
	for _, node := range nodes {
		args := append([]string{"-f", talosconfig.NodeConfigPath(node)}, extra...)
		command := nodeCommand("apply-config", node, args...)
		commands = append(commands, command)
	}
	return commands
}

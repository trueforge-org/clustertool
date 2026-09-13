package gencmd

import (
	"fmt"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
	"strings"
)

func GenUpgrade(target string, extra []string) ([]Command, error) {
	inv, err := talosconfig.LoadInventory()
	if err != nil {
		return nil, err
	}
	nodes, err := inv.Select(target)
	if err != nil {
		return nil, err
	}
	if err := ValidateExtraArgs(extra); err != nil {
		return nil, err
	}
	var commands []Command
	for _, node := range nodes {
		image, err := talosconfig.GeneratedNodeValue(talosconfig.NodeConfigPath(node), "UnattendedInstallConfig", "installer", "image")
		if err != nil {
			return nil, fmt.Errorf("node %s: %w", node.Name, err)
		}
		args := append([]string{"--wait", "--image", image}, extra...)
		command := nodeCommand("upgrade", node, args...)
		commands = append(commands, command)
	}
	return commands, nil
}

func GenKubeUpgrade(target string) (Command, error) {
	inv, err := talosconfig.LoadInventory()
	if err != nil {
		return Command{}, err
	}
	node := inv.Bootstrap()
	if target != "" && target != "all" {
		nodes, e := inv.Select(target)
		if e != nil {
			return Command{}, e
		}
		node = nodes[0]
	}
	if node.Role != "control-plane" {
		return Command{}, fmt.Errorf("Kubernetes upgrade requires a control-plane target")
	}
	var version string
	for _, n := range inv.Nodes {
		image, e := talosconfig.GeneratedNodeValue(talosconfig.NodeConfigPath(n), "KubeletConfig", "image")
		if e != nil {
			return Command{}, e
		}
		tag := image[strings.LastIndex(image, ":")+1:]
		tag, _, _ = strings.Cut(tag, "@")
		if !strings.HasPrefix(tag, "v") {
			return Command{}, fmt.Errorf("kubelet image requires a version tag")
		}
		if version != "" && version != tag {
			return Command{}, fmt.Errorf("all nodes must have the same desired Kubernetes version")
		}
		version = tag
	}
	command := nodeCommand("upgrade-k8s", node, "--to", strings.TrimPrefix(version, "v"))
	command.Failover = target == "" || target == "all"
	return command, nil
}

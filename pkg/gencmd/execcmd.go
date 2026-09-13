package gencmd

import (
	"fmt"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"github.com/trueforge-org/clustertool/pkg/nodestatus"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
	"strings"
	"time"
)

var runCommand = helper.RunBoundedCommand
var checkStatus = nodestatus.CheckStatus
var waitReady = func(node string) error { _, err := nodestatus.WaitForHealth(node, nil); return err }
var guardControlPlane = controlPlaneGuard
var recoveryDelay = func() { time.Sleep(5 * time.Second) }
var readBootID = func(node string) (string, error) {
	inv, err := talosconfig.LoadInventory()
	if err != nil {
		return "", err
	}
	nodes, err := inv.Select(node)
	if err != nil {
		return "", err
	}
	out, err := runCommand(nodeCommand("read", nodes[0], "/proc/sys/kernel/random/boot_id", "-e", node).Args, true)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		return "", fmt.Errorf("empty boot ID on %s", node)
	}
	return id, nil
}

func ExecCmd(cmd Command) error {
	_, err := executeCommand(cmd)
	return err
}

func executeCommand(cmd Command) ([]byte, error) {
	if cmd.Err != nil {
		return nil, cmd.Err
	}
	if len(cmd.Args) < 2 {
		return nil, fmt.Errorf("empty Talos command")
	}
	var err error
	cmd, err = resolveControlPlane(cmd)
	if err != nil {
		return nil, err
	}
	out, err := runCommand(cmd.Args, false)
	if err != nil {
		return out, fmt.Errorf("node %s, %s: %w: %s", cmd.Node, cmd.Args[1], err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func resolveControlPlane(cmd Command) (Command, error) {
	if !cmd.Failover {
		return cmd, nil
	}
	inv, err := talosconfig.LoadInventory()
	if err != nil {
		return cmd, err
	}
	address, err := ExistingControlPlane(inv)
	if err != nil {
		return cmd, err
	}
	cmd.Args = append([]string{}, cmd.Args...)
	for i := 0; i+1 < len(cmd.Args); i++ {
		if cmd.Args[i] == "-n" {
			cmd.Args[i+1] = address
		}
	}
	cmd.Node = address
	return cmd, nil
}

// Execute the already validated generated files. Never regenerate during execution.
func ExecCmds(commands []Command, healthcheck bool) error {
	if len(commands) == 0 {
		return fmt.Errorf("no node commands generated")
	}
	for _, cmd := range commands {
		if cmd.Err != nil {
			return cmd.Err
		}
	}
	for index, cmd := range commands {
		if len(cmd.Args) < 2 {
			return fmt.Errorf("empty Talos command")
		}
		operation := cmd.Args[1]
		var bootID string
		disruptive := operation == "apply-config" || operation == "upgrade" || operation == "reset"
		if disruptive {
			stage, err := checkStatus(cmd.Node)
			if err != nil {
				return fmt.Errorf("node %s preflight (%d completed, %d pending): %w", cmd.Node, index, len(commands)-index, err)
			}
			if stage == "maintenance" {
				if operation != "apply-config" {
					return fmt.Errorf("node %s is in maintenance; apply its configuration first", cmd.Node)
				}
				// Maintenance uses a direct endpoint. Never ask another configured endpoint
				// to proxy an unauthenticated request to an unconfigured node.
				cmd.Args = append(append([]string{}, cmd.Args...), "-e", cmd.Node, "--insecure")
			} else if healthcheck {
				if err := waitReady(cmd.Node); err != nil {
					return fmt.Errorf("node %s preflight: %w", cmd.Node, err)
				}
				if operation == "apply-config" {
					bootID, err = readBootID(cmd.Node)
					if err != nil {
						return fmt.Errorf("node %s boot identity: %w", cmd.Node, err)
					}
				}
				if operation != "reset" {
					canReboot, err := guardControlPlane(cmd.Node)
					if err != nil {
						return fmt.Errorf("node %s control-plane preflight: %w", cmd.Node, err)
					}
					if !canReboot {
						if operation != "apply-config" {
							return fmt.Errorf("node %s: reboot would lose etcd quorum; add a third healthy control plane before rolling upgrades", cmd.Node)
						}
						cmd.Args = append(append([]string{}, cmd.Args...), "--mode=no-reboot")
					}
				}
			}
		}
		out, err := executeCommand(cmd)
		if err != nil {
			return fmt.Errorf("%d completed; stopped with %d pending: %w", index, len(commands)-index-1, err)
		}
		if healthcheck && disruptive && operation != "reset" {
			if bootID != "" && strings.Contains(strings.ToLower(string(out)), "with a reboot") {
				deadline := time.Now().Add(15 * time.Minute)
				for {
					current, err := readBootID(cmd.Node)
					if err == nil && current != bootID {
						break
					}
					if time.Now().After(deadline) {
						return fmt.Errorf("node %s did not complete its requested reboot", cmd.Node)
					}
					recoveryDelay()
				}
			}
			if err := waitReady(cmd.Node); err != nil {
				return fmt.Errorf("node %s recovery; %d pending: %w", cmd.Node, len(commands)-index-1, err)
			}
			if _, err := guardControlPlane(cmd.Node); err != nil {
				return fmt.Errorf("node %s membership after recovery: %w", cmd.Node, err)
			}
		}
	}
	return nil
}

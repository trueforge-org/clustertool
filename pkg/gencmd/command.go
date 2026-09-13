package gencmd

import (
	"github.com/trueforge-org/clustertool/embed"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
	"strings"
)

// Command retains argument boundaries from planning through execution.
type Command struct {
	Args     []string
	Node     string
	Err      error
	Failover bool
}

func (c Command) String() string { return strings.Join(c.Args, " ") }

func nodeCommand(operation string, node talosconfig.Node, extra ...string) Command {
	args := []string{embed.GetTalosExec(), operation, "--talosconfig", talosconfig.TalosconfigPath(), "-n", node.Address}
	return Command{Args: append(args, extra...), Node: node.Address}
}

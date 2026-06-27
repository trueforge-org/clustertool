package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func hasSubcommand(parent *cobra.Command, use string) bool {
	for _, c := range parent.Commands() {
		if c.Use == use || c.Name() == use {
			return true
		}
	}
	return false
}

func findSubcommand(parent *cobra.Command, use string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Use == use || c.Name() == use {
			return c
		}
	}
	return nil
}

func TestRootCommandSurface(t *testing.T) {
	if !hasSubcommand(RootCmd, "precommit") {
		t.Fatalf("expected root command to expose precommit")
	}

	if hasSubcommand(RootCmd, "adv") {
		t.Fatalf("did not expect removed adv command group")
	}

	if hasSubcommand(RootCmd, "help") {
		t.Fatalf("did not expect explicit help command")
	}
}

func TestTalosSubcommands(t *testing.T) {
	talos := findSubcommand(RootCmd, "talos")
	if talos == nil {
		t.Fatalf("expected talos command to be registered")
	}

	if hasSubcommand(talos, "bootstrap") {
		t.Fatalf("did not expect removed talos bootstrap subcommand")
	}

	for _, expected := range []string{"apply", "health", "kubeconfig", "reset", "upgrade"} {
		if !hasSubcommand(talos, expected) {
			t.Fatalf("expected talos subcommand %q", expected)
		}
	}
}

package cmd

import (
	"reflect"
	"sort"
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

	apply := findSubcommand(talos, "apply")
	if apply == nil {
		t.Fatalf("expected talos apply command to be registered")
	}

	hasAlias := false
	for _, alias := range apply.Aliases {
		if alias == "apply-config" {
			hasAlias = true
			break
		}
	}
	if !hasAlias {
		t.Fatalf("expected talos apply to include alias %q", "apply-config")
	}
}

func TestRootCommandSnapshot(t *testing.T) {
	var got []string
	for _, c := range RootCmd.Commands() {
		got = append(got, c.Name())
	}
	sort.Strings(got)

	want := []string{
		"checkcrypt",
		"decrypt",
		"encrypt",
		"flux",
		"genconfig",
		"info",
		"init",
		"precommit",
		"talos",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("root command snapshot mismatch\n got: %v\nwant: %v", got, want)
	}
}

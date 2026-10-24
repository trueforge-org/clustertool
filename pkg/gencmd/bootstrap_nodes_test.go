package gencmd

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/trueforge-org/clustertool/pkg/talosconfig"
)

func bootstrapNodesFixture() *talosconfig.Inventory {
	return &talosconfig.Inventory{Nodes: []talosconfig.Node{
		{Name: "cp1", Role: "control-plane", Address: "192.0.2.11"},
		{Name: "cp2", Role: "control-plane", Address: "192.0.2.12"},
		{Name: "worker", Role: "worker", Address: "192.0.2.21"},
	}}
}

func mockBootstrapNodes(t *testing.T, inv *talosconfig.Inventory, stages map[string]string, existing bool) (*[]string, func(string) error) {
	t.Helper()
	mockExecution(t)
	oldWait, oldBootstrapRun := waitForBootstrapHealth, runBootstrapCommand
	t.Cleanup(func() { waitForBootstrapHealth, runBootstrapCommand = oldWait, oldBootstrapRun })
	var events []string
	checkStatus = func(node string) (string, error) { return stages[node], nil }
	waitReady = func(string) error {
		t.Fatal("full readiness must not be required before bootstrap and CNI")
		return nil
	}
	guardControlPlane = func(string) (bool, error) {
		t.Fatal("initial configuration must not use day-2 quorum checks")
		return false, nil
	}
	runCommand = func(args []string, _ bool) (string, string, error) {
		if args[1] == "etcd" {
			if existing {
				return "NODE ID HOSTNAME PEER URLS CLIENT URLS LEARNER\n192.0.2.11 1 cp1 https://192.0.2.11:2380 https://192.0.2.11:2379 false\n", "", nil
			}
			return "", "", errors.New("etcd unavailable")
		}
		if args[1] != "apply-config" {
			t.Fatalf("unexpected command: %v", args)
		}
		var node, endpoint string
		for index, arg := range args {
			if arg == "-n" {
				node = args[index+1]
			}
			if arg == "-e" {
				endpoint = args[index+1]
			}
		}
		if endpoint != node || !strings.Contains(strings.Join(args, " "), "--insecure") {
			t.Fatalf("maintenance apply must use its own endpoint: %v", args)
		}
		events = append(events, "apply:"+node)
		return "", "", nil
	}
	waitForBootstrapHealth = func(node string, stages []string) (string, error) {
		if node != inv.Bootstrap().Address || !reflect.DeepEqual(stages, []string{"booting", "running"}) {
			t.Fatalf("unexpected bootstrap readiness requirement: %s %v", node, stages)
		}
		events = append(events, "installation:"+node)
		return "booting", nil
	}
	runBootstrapCommand = func(_ context.Context, args []string) ([]byte, error) {
		if args[1] == "etcd" {
			return nil, errors.New("etcd unavailable")
		}
		if args[1] != "bootstrap" {
			t.Fatalf("unexpected bootstrap command: %v", args)
		}
		events = append(events, "bootstrap:"+inv.Bootstrap().Address)
		return nil, nil
	}
	applyNode := func(address string) error {
		for _, node := range inv.Nodes {
			if node.Address == address {
				return ExecCmds([]Command{nodeCommand("apply-config", node, "-f", talosconfig.NodeConfigPath(node))}, false)
			}
		}
		t.Fatalf("unexpected apply target: %s", address)
		return nil
	}
	return &events, applyNode
}

func TestBootstrapAppliesAllNodesBeforeRPC(t *testing.T) {
	for _, single := range []bool{false, true} {
		t.Run(map[bool]string{false: "multiple-control-planes-and-worker", true: "single-control-plane"}[single], func(t *testing.T) {
			inv := bootstrapNodesFixture()
			if single {
				inv.Nodes = inv.Nodes[:1]
			}
			stages := map[string]string{}
			var want []string
			for _, node := range inv.Nodes {
				stages[node.Address] = "maintenance"
				want = append(want, "apply:"+node.Address)
			}
			want = append(want, "installation:192.0.2.11", "bootstrap:192.0.2.11")
			events, applyNode := mockBootstrapNodes(t, inv, stages, false)
			if err := bootstrapTalosNodes(inv, applyNode); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(*events, want) {
				t.Fatalf("bootstrap started before all configurations were applied: %v, want %v", *events, want)
			}
		})
	}
}

func TestBootstrapResumePreparesUnconfiguredNodes(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-etcd-bootstrap", true: "after-etcd-bootstrap"}[existing], func(t *testing.T) {
			inv := bootstrapNodesFixture()
			stages := map[string]string{"192.0.2.11": "running", "192.0.2.12": "booting", "192.0.2.21": "maintenance"}
			events, applyNode := mockBootstrapNodes(t, inv, stages, existing)
			if err := bootstrapTalosNodes(inv, applyNode); err != nil {
				t.Fatal(err)
			}
			want := []string{"apply:192.0.2.21"}
			if !existing {
				want = append(want, "installation:192.0.2.11", "bootstrap:192.0.2.11")
			}
			if !reflect.DeepEqual(*events, want) {
				t.Fatalf("resume reapplied configured nodes or repeated bootstrap: %v, want %v", *events, want)
			}
		})
	}
}

func TestBootstrapNodeFailureStopsBeforeRPC(t *testing.T) {
	for _, failure := range []string{"status", "stage", "apply"} {
		t.Run(failure, func(t *testing.T) {
			inv := bootstrapNodesFixture()
			stages := map[string]string{"192.0.2.11": "maintenance", "192.0.2.12": "maintenance", "192.0.2.21": "maintenance"}
			events, applyNode := mockBootstrapNodes(t, inv, stages, false)
			cause := errors.New("node unavailable")
			if failure == "stage" {
				stages["192.0.2.12"] = "resetting"
			}
			if failure == "status" {
				checkStatus = func(node string) (string, error) {
					if node == "192.0.2.12" {
						return "", cause
					}
					return stages[node], nil
				}
			}
			if failure == "apply" {
				previous := runCommand
				runCommand = func(args []string, silent bool) (string, string, error) {
					out, stderr, err := previous(args, silent)
					if args[1] == "apply-config" && strings.Contains(strings.Join(args, " "), " -n 192.0.2.12") {
						return "", "installation failed", cause
					}
					return out, stderr, err
				}
			}
			err := bootstrapTalosNodes(inv, applyNode)
			if err == nil || !strings.Contains(err.Error(), "cp2") {
				t.Fatalf("missing failure context: %v", err)
			}
			if failure != "stage" && !errors.Is(err, cause) {
				t.Fatalf("lost underlying failure: %v", err)
			}
			want := []string{"apply:192.0.2.11"}
			if failure == "apply" {
				want = append(want, "apply:192.0.2.12")
			}
			if !reflect.DeepEqual(*events, want) {
				t.Fatalf("continued after node failure: %v, want %v", *events, want)
			}
		})
	}
}

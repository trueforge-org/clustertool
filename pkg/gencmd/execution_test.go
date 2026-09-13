package gencmd

import (
	"errors"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func mockExecution(t *testing.T) {
	t.Helper()
	oldRun, oldStatus, oldReady, oldGuard, oldDelay := runCommand, checkStatus, waitReady, guardControlPlane, recoveryDelay
	oldBoot := readBootID
	t.Cleanup(func() {
		readBootID = oldBoot
		runCommand, checkStatus, waitReady, guardControlPlane, recoveryDelay = oldRun, oldStatus, oldReady, oldGuard, oldDelay
	})
	checkStatus = func(string) (string, error) { return "running", nil }
	waitReady = func(string) error { return nil }
	guardControlPlane = func(string) (bool, error) { return true, nil }
	recoveryDelay = func() {}
	readBootID = func(string) (string, error) { return "initial-boot", nil }
}

func TestApplyWaitsForChangedBootID(t *testing.T) {
	mockExecution(t)
	readCalls := 0
	readBootID = func(string) (string, error) {
		readCalls++
		if readCalls < 4 {
			return "old", nil
		}
		return "new", nil
	}
	runCommand = func([]string, bool) ([]byte, error) { return []byte("Applied configuration with a reboot"), nil }
	if err := ExecCmds([]Command{{Args: []string{"talosctl", "apply-config"}, Node: "cp"}}, true); err != nil {
		t.Fatal(err)
	}
	if readCalls < 4 {
		t.Fatal("accepted readiness from the old boot")
	}
}

func TestExecutionPreservesArgumentsAndMaintenanceEndpoint(t *testing.T) {
	mockExecution(t)
	checkStatus = func(string) (string, error) { return "maintenance", nil }
	original := []string{"/tools with spaces/talosctl", "apply-config", "-n", "192.0.2.21", "-f", "/cluster with spaces/worker.yaml"}
	var actual []string
	runCommand = func(args []string, _ bool) ([]byte, error) { actual = append([]string{}, args...); return nil, nil }
	if err := ExecCmds([]Command{{Args: original, Node: "192.0.2.21"}}, true); err != nil {
		t.Fatal(err)
	}
	expected := append(append([]string{}, original...), "-e", "192.0.2.21", "--insecure")
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("argv changed: %#v", actual)
	}
	if len(original) != 6 {
		t.Fatal("mutated planned command")
	}
}

func TestExecutionStopsBeforeNextNodeOnFailure(t *testing.T) {
	for _, failure := range []string{"command", "recovery", "preflight"} {
		t.Run(failure, func(t *testing.T) {
			mockExecution(t)
			var executed []string
			runCommand = func(args []string, _ bool) ([]byte, error) {
				executed = append(executed, args[2])
				if failure == "command" {
					return []byte("failure"), errors.New("exit 1")
				}
				return nil, nil
			}
			calls := 0
			waitReady = func(string) error {
				calls++
				if failure == "preflight" || failure == "recovery" && calls == 2 {
					return errors.New("not ready")
				}
				return nil
			}
			err := ExecCmds([]Command{{Args: []string{"talosctl", "apply-config", "first"}, Node: "first"}, {Args: []string{"talosctl", "apply-config", "second"}, Node: "second"}}, true)
			if err == nil || strings.Contains(strings.Join(executed, ","), "second") {
				t.Fatalf("continued after %s: %v %v", failure, executed, err)
			}
		})
	}
}

func TestTwoMemberApplyCannotReboot(t *testing.T) {
	mockExecution(t)
	guardControlPlane = func(string) (bool, error) { return false, nil }
	var executed []string
	runCommand = func(args []string, _ bool) ([]byte, error) { executed = args; return nil, nil }
	if err := ExecCmds([]Command{{Args: []string{"talosctl", "apply-config"}, Node: "cp"}}, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(executed, " "), "--mode=no-reboot") {
		t.Fatal(executed)
	}
	executed = nil
	if err := ExecCmds([]Command{{Args: []string{"talosctl", "upgrade"}, Node: "cp"}}, true); err == nil || executed != nil {
		t.Fatal("unsafe upgrade executed")
	}
}

func TestBootstrapRequiresClusterEvidence(t *testing.T) {
	mockExecution(t)
	inv := &talosconfig.Inventory{BootstrapNode: "cp", Nodes: []talosconfig.Node{{Name: "cp", Role: "control-plane", Address: "192.0.2.11"}, {Name: "worker", Role: "worker", Address: "192.0.2.21"}}}
	runCommand = func([]string, bool) ([]byte, error) {
		return []byte("NODE ID HOSTNAME PEER URLS CLIENT URLS LEARNER\n192.0.2.11 1 cp https://192.0.2.11:2380 https://192.0.2.11:2379 false\n"), nil
	}
	checkStatus = func(string) (string, error) {
		t.Fatal("existing cluster must not depend on worker maintenance")
		return "", nil
	}
	if needed, err := NeedsBootstrap(inv); err != nil || needed {
		t.Fatal(needed, err)
	}
	runCommand = func([]string, bool) ([]byte, error) { return nil, errors.New("unreachable") }
	checkStatus = func(string) (string, error) { return "maintenance", nil }
	if needed, err := NeedsBootstrap(inv); err != nil || !needed {
		t.Fatal(needed, err)
	}
	checkStatus = func(string) (string, error) { return "booting", nil }
	if needed, err := NeedsBootstrap(inv); err == nil || needed {
		t.Fatal("unknown state allowed bootstrap")
	}
}

func TestEtcdMemberParsingFailsClosed(t *testing.T) {
	valid := "NODE ID HOSTNAME PEER URLS CLIENT URLS LEARNER\n192.0.2.11 1 cp https://192.0.2.11:2380 https://192.0.2.11:2379 false"
	if hosts, err := etcdMembers(valid); err != nil || len(hosts) != 1 {
		t.Fatal(hosts, err)
	}
	if _, err := parseEtcdMembers(strings.Replace(valid, "false", "true", 1), true); err != nil {
		t.Fatal("learner must still prove an existing cluster", err)
	}
	for _, bad := range []string{"", strings.Replace(valid, "false", "true", 1), strings.Replace(valid, "HOSTNAME", "CHANGED", 1)} {
		if _, err := etcdMembers(bad); err == nil {
			t.Fatal("accepted", bad)
		}
	}
}

func TestUpgradeRefusesLiveDowngradeBeforeMutation(t *testing.T) {
	mockExecution(t)
	withSingleNodeFixture(t)
	previous := helper.TalosGenerated
	helper.TalosGenerated = t.TempDir()
	t.Cleanup(func() { helper.TalosGenerated = previous })
	if err := os.WriteFile(filepath.Join(helper.TalosGenerated, "control-1.yaml"), []byte("apiVersion: v1alpha1\nkind: UnattendedInstallConfig\ninstaller:\n  image: factory.talos.dev/metal-installer/test:v1.14.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runCommand = func(args []string, _ bool) ([]byte, error) {
		if args[1] != "version" {
			t.Fatalf("mutation during preflight: %v", args)
		}
		return []byte(`{"version":{"tag":"v1.14.1"}}`), nil
	}
	err := PreflightUpgrade([]Command{{Node: "10.0.0.1"}}, Command{})
	if err == nil || !strings.Contains(err.Error(), "downgrades") {
		t.Fatal(err)
	}
}

func TestForwardedFlagsCannotOverridePlan(t *testing.T) {
	for _, arg := range []string{"--nodes=other", "-nother", "-i", "--config-patch=x", "--mode=staged", "--wait=false", "--image=x", "--talosconfig=x"} {
		if err := ValidateExtraArgs([]string{arg}); err == nil {
			t.Fatal(arg)
		}
	}
}

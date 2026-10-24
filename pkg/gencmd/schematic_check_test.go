package gencmd

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
)

func schematicCheckFixture(t *testing.T, nodes []talosconfig.Node, image string) *bytes.Buffer {
	t.Helper()
	mockExecution(t)
	previousGenerated, previousLogger := helper.TalosGenerated, log.Logger
	helper.TalosGenerated = t.TempDir()
	warnings := &bytes.Buffer{}
	log.Logger = zerolog.New(warnings)
	t.Cleanup(func() {
		helper.TalosGenerated, log.Logger = previousGenerated, previousLogger
	})
	for _, node := range nodes {
		config := "apiVersion: v1alpha1\nkind: UnattendedInstallConfig\ninstaller:\n  image: " + image + "\n"
		if err := os.WriteFile(talosconfig.NodeConfigPath(node), []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
	}
	checkStatus = func(string) (string, error) { return "maintenance", nil }
	return warnings
}

func schematicGetTarget(t *testing.T, args []string) (string, string) {
	t.Helper()
	if len(args) < 3 || args[1] != "get" {
		t.Fatalf("schematic check performed a mutation: %v", args)
	}
	var node, endpoint, output, resource string
	insecure := false
	for i, arg := range args {
		switch arg {
		case "-n":
			node = args[i+1]
		case "-e":
			endpoint = args[i+1]
		case "-o":
			output = args[i+1]
		case "--insecure":
			insecure = true
		case "volumestatus", "systemdisk", "imagefactoryschematic":
			resource = arg
			if arg == "systemdisk" && (i+1 >= len(args) || args[i+1] != "system-disk") {
				t.Fatalf("expected the SystemDisk singleton: %v", args)
			}
		}
	}
	if node == "" || endpoint != node || !insecure || output != "json" || resource == "" {
		t.Fatalf("expected direct maintenance JSON query: %v", args)
	}
	return node, resource
}

func TestConfirmTalosInstallationSkipsSafeNodes(t *testing.T) {
	wanted := strings.Repeat("a", 64)
	other := strings.Repeat("b", 64)
	nodes := []talosconfig.Node{{Name: "control-1", Role: "control-plane", Address: "192.0.2.11"}}
	for _, tc := range []struct {
		name, stage, phase, current string
		wantGets                    int
	}{
		{"fresh-ISO-with-other-schematic", "maintenance", "missing", other, 1},
		{"running-node", "running", "ready", other, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schematicCheckFixture(t, nodes, "factory.talos.dev/metal-installer/"+wanted+":v1.14.2")
			checkStatus = func(string) (string, error) { return tc.stage, nil }
			gets := 0
			runCommand = func(args []string, _ bool) (string, string, error) {
				gets++
				_, resource := schematicGetTarget(t, args)
				if resource == "systemdisk" {
					return `{"spec":{"diskID":"sda","devPath":"/dev/sda"}}`, "", nil
				}
				if resource == "volumestatus" {
					return `{"metadata":{"phase":"running"},"spec":{"phase":"` + tc.phase + `"}}`, "", nil
				}
				return `{"spec":{"schematicId":"` + tc.current + `"}}`, "", nil
			}
			if err := ConfirmTalosInstallation(nodes, func(string, bool) bool {
				t.Fatal("safe node required confirmation")
				return false
			}); err != nil {
				t.Fatal(err)
			}
			if gets != tc.wantGets {
				t.Fatalf("got %d resource queries, want %d", gets, tc.wantGets)
			}
		})
	}
}

func TestConfirmTalosInstallationInspectsAllNodesBeforeOnePrompt(t *testing.T) {
	wanted, current := strings.Repeat("a", 64), strings.Repeat("b", 64)
	nodes := []talosconfig.Node{
		{Name: "control-1", Role: "control-plane", Address: "192.0.2.11"},
		{Name: "worker-1", Role: "worker", Address: "192.0.2.21"},
		{Name: "worker-2", Role: "worker", Address: "192.0.2.22"},
	}
	for _, proceed := range []bool{false, true} {
		t.Run(map[bool]string{false: "decline", true: "continue"}[proceed], func(t *testing.T) {
			warnings := schematicCheckFixture(t, nodes, "factory.talos.dev/metal-installer/"+wanted+":v1.14.2")
			inspected, gets, prompts := 0, 0, 0
			checkStatus = func(node string) (string, error) {
				inspected++
				if node == nodes[1].Address {
					return "running", nil
				}
				return "maintenance", nil
			}
			runCommand = func(args []string, _ bool) (string, string, error) {
				gets++
				node, resource := schematicGetTarget(t, args)
				if node == nodes[1].Address {
					t.Fatal("queried a running node through maintenance API")
				}
				if resource == "systemdisk" {
					return `{"spec":{"diskID":"sda","devPath":"/dev/sda"}}`, "", nil
				}
				if resource == "volumestatus" {
					return `{"spec":{"phase":"ready"}}`, "", nil
				}
				return `{"spec":{"schematicId":"` + current + `"}}`, "", nil
			}
			err := ConfirmTalosInstallation(nodes, func(_ string, defaultAnswer bool) bool {
				prompts++
				if inspected != len(nodes) || gets != 6 {
					t.Fatalf("prompted before inspecting all nodes: statuses=%d, gets=%d", inspected, gets)
				}
				if defaultAnswer {
					t.Fatal("continuing after a warning must not be the default")
				}
				return proceed
			})
			if (err == nil) != proceed || prompts != 1 {
				t.Fatalf("proceed=%v: prompts=%d, error=%v", proceed, prompts, err)
			}
			if !strings.Contains(warnings.String(), wanted) || !strings.Contains(warnings.String(), current) {
				t.Fatalf("warning did not identify both schematics: %s", warnings)
			}
		})
	}
}

func TestConfirmTalosInstallationWarnsWhenInspectionIsInconclusive(t *testing.T) {
	wanted := strings.Repeat("a", 64)
	nodes := []talosconfig.Node{{Name: "worker-1", Role: "worker", Address: "192.0.2.21"}}
	for _, tc := range []struct {
		name, meta, schematic, failedResource, image string
	}{
		{name: "unknown-META-phase", meta: `{"spec":{"phase":"waiting"}}`},
		{name: "invalid-META-JSON", meta: "not JSON"},
		{name: "META-query-failed", failedResource: "volumestatus"},
		{name: "schematic-query-failed", failedResource: "imagefactoryschematic"},
		{name: "invalid-schematic-JSON", schematic: "not JSON"},
		{name: "missing-schematic-ID", schematic: `{"spec":{}}`},
		{name: "non-factory-installer", image: "ghcr.io/siderolabs/installer:v1.14.2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			image := tc.image
			if image == "" {
				image = "factory.talos.dev/metal-installer/" + wanted + ":v1.14.2"
			}
			warnings := schematicCheckFixture(t, nodes, image)
			runCommand = func(args []string, _ bool) (string, string, error) {
				_, resource := schematicGetTarget(t, args)
				if resource == tc.failedResource {
					return "", "API unavailable", errors.New("exit status 1")
				}
				if resource == "systemdisk" {
					return `{"spec":{"diskID":"sda","devPath":"/dev/sda"}}`, "", nil
				}
				if resource == "volumestatus" {
					if tc.meta != "" {
						return tc.meta, "", nil
					}
					return `{"spec":{"phase":"ready"}}`, "", nil
				}
				if tc.schematic != "" {
					return tc.schematic, "", nil
				}
				return `{"spec":{"schematicId":"` + wanted + `"}}`, "", nil
			}
			prompts := 0
			err := ConfirmTalosInstallation(nodes, func(string, bool) bool { prompts++; return true })
			if err != nil || prompts != 1 {
				t.Fatalf("inconclusive inspection did not allow an explicit choice: prompts=%d, error=%v", prompts, err)
			}
			if !strings.Contains(warnings.String(), `"level":"warn"`) {
				t.Fatalf("missing warning: %s", warnings)
			}
			if tc.meta != "" || tc.failedResource == "volumestatus" {
				if strings.Contains(warnings.String(), "Unattended installation will be skipped") {
					t.Fatalf("inconclusive META inspection was reported as installed: %s", warnings)
				}
				if !strings.Contains(warnings.String(), "Installation state: unknown") ||
					!strings.Contains(warnings.String(), "/dev/sda") {
					t.Fatalf("missing unknown state or detected disk: %s", warnings)
				}
			}
		})
	}
}

func TestConfirmTalosInstallationWarnsWithMatchingSchematic(t *testing.T) {
	wanted := strings.Repeat("a", 64)
	nodes := []talosconfig.Node{{Name: "control-1", Role: "control-plane", Address: "192.0.2.11"}}
	for _, tc := range []struct {
		name, disk string
		diskError  bool
		proceed    bool
	}{
		{name: "decline-existing-USB-installation", disk: "/dev/sdb"},
		{name: "accept-existing-installation", disk: "/dev/sda", proceed: true},
		{name: "disk-query-failed", diskError: true},
		{name: "disk-path-missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			warnings := schematicCheckFixture(t, nodes, "factory.talos.dev/metal-installer/"+wanted+":v1.14.2")
			gets, prompts := 0, 0
			runCommand = func(args []string, _ bool) (string, string, error) {
				gets++
				_, resource := schematicGetTarget(t, args)
				switch resource {
				case "volumestatus":
					return `{"spec":{"phase":"ready"}}`, "", nil
				case "systemdisk":
					if tc.diskError {
						return "", "API unavailable", errors.New("exit status 1")
					}
					return `{"spec":{"devPath":"` + tc.disk + `"}}`, "", nil
				default:
					return `{"spec":{"schematicId":"` + wanted + `"}}`, "", nil
				}
			}
			err := ConfirmTalosInstallation(nodes, func(_ string, defaultAnswer bool) bool {
				prompts++
				if defaultAnswer || gets != 3 {
					t.Fatalf("confirmation must default to no after inspection: gets=%d, default=%v", gets, defaultAnswer)
				}
				return tc.proceed
			})
			if (err == nil) != tc.proceed || prompts != 1 {
				t.Fatalf("proceed=%v: prompts=%d, error=%v", tc.proceed, prompts, err)
			}
			disk := tc.disk
			if disk == "" {
				disk = "unknown"
			}
			if !strings.Contains(warnings.String(), "Talos is already installed on "+disk) ||
				!strings.Contains(warnings.String(), "Unattended installation will be skipped") ||
				!strings.Contains(warnings.String(), wanted) {
				t.Fatalf("matching schematic hid the existing installation or disk: %s", warnings)
			}
		})
	}
}

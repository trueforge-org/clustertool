package gencmd

import (
	"path/filepath"
	"strings"
	"testing"

	talhelperCfg "github.com/budimanjojo/talhelper/v3/pkg/config"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"github.com/trueforge-org/clustertool/pkg/talassist"
)

func withTalConfigFixture(t *testing.T, cfg *talhelperCfg.TalhelperConfig) {
	t.Helper()
	prev := talassist.TalConfig
	talassist.TalConfig = cfg
	t.Cleanup(func() {
		talassist.TalConfig = prev
	})
}

func TestGenPlainAllNodesWithExtraArgs(t *testing.T) {
	withTalConfigFixture(t, &talhelperCfg.TalhelperConfig{
		ClusterName: "main",
		Nodes: []talhelperCfg.Node{
			{Hostname: "cp1", IPAddress: "10.0.0.1"},
			{Hostname: "cp2", IPAddress: "10.0.0.2"},
		},
	})

	cmds := GenPlain("health", "", []string{"-f"})
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cmds))
	}

	for _, cmd := range cmds {
		if !strings.Contains(cmd, " health ") {
			t.Fatalf("expected health command, got %q", cmd)
		}
		if !strings.Contains(cmd, "--talosconfig "+helper.TalosConfigFile) {
			t.Fatalf("expected talosconfig path, got %q", cmd)
		}
		if !strings.HasSuffix(cmd, " -f") {
			t.Fatalf("expected extra args suffix, got %q", cmd)
		}
	}
}

func TestGenApplySingleNode(t *testing.T) {
	withTalConfigFixture(t, &talhelperCfg.TalhelperConfig{
		ClusterName: "main",
		Nodes: []talhelperCfg.Node{
			{Hostname: "cp1", IPAddress: "10.0.0.1"},
			{Hostname: "cp2", IPAddress: "10.0.0.2"},
		},
	})

	cmds := GenApply("10.0.0.2", nil)
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}

	cmd := cmds[0]
	if !strings.Contains(cmd, " apply machineconfig ") {
		t.Fatalf("expected apply machineconfig command, got %q", cmd)
	}
	if !strings.Contains(cmd, " -n 10.0.0.2") {
		t.Fatalf("expected selected node in command, got %q", cmd)
	}
	expectedFile := filepath.Join(helper.TalosGenerated, "main-cp2.yaml")
	if !strings.Contains(cmd, " -f "+expectedFile) {
		t.Fatalf("expected generated config path %q in command, got %q", expectedFile, cmd)
	}
}

func TestGenPlainSingleNodeWithoutExtraArgs(t *testing.T) {
	withTalConfigFixture(t, &talhelperCfg.TalhelperConfig{
		ClusterName: "main",
		Nodes: []talhelperCfg.Node{
			{Hostname: "cp1", IPAddress: "10.0.0.1"},
			{Hostname: "cp2", IPAddress: "10.0.0.2"},
		},
	})

	cmds := GenPlain("kubeconfig", "10.0.0.1", nil)
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}

	cmd := cmds[0]
	if !strings.Contains(cmd, " kubeconfig ") {
		t.Fatalf("expected kubeconfig command, got %q", cmd)
	}
	if !strings.Contains(cmd, " -n 10.0.0.1") {
		t.Fatalf("expected selected node in command, got %q", cmd)
	}
	if strings.HasSuffix(cmd, " ") {
		t.Fatalf("did not expect trailing space in command, got %q", cmd)
	}
}

func TestGenApplyAllNodes(t *testing.T) {
	withTalConfigFixture(t, &talhelperCfg.TalhelperConfig{
		ClusterName: "main",
		Nodes: []talhelperCfg.Node{
			{Hostname: "cp1", IPAddress: "10.0.0.1"},
			{Hostname: "cp2", IPAddress: "10.0.0.2"},
		},
	})

	cmds := GenApply("", nil)
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cmds))
	}

	if !strings.Contains(cmds[0], " -f "+filepath.Join(helper.TalosGenerated, "main-cp1.yaml")) {
		t.Fatalf("expected first node config path in first command, got %q", cmds[0])
	}
	if !strings.Contains(cmds[1], " -f "+filepath.Join(helper.TalosGenerated, "main-cp2.yaml")) {
		t.Fatalf("expected second node config path in second command, got %q", cmds[1])
	}
}

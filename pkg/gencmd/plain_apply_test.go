package gencmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trueforge-org/clustertool/pkg/helper"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
)

func withSingleNodeFixture(t *testing.T) {
	t.Helper()
	previous := helper.TalEnv
	previousPath := helper.TalosPath
	helper.TalosPath = t.TempDir()
	helper.TalEnv = map[string]string{"CONTROL1IP": "10.0.0.1"}
	t.Cleanup(func() { helper.TalEnv = previous; helper.TalosPath = previousPath })
	if err := os.MkdirAll(filepath.Join(helper.TalosPath, "patches", "nodes", "control-1"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(helper.TalosPath, "clustertool.yaml"), []byte("apiVersion: clustertool/v1\nkind: ClusterConfig\ntalosVersion: v1.14.0\nkubernetesVersion: v1.37.0\nbootstrapNode: control-1\nnodes:\n  - name: control-1\n    role: control-plane\n    address: ${CONTROL1IP}\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestGenPlainUsesSingleConfiguredNode(t *testing.T) {
	withSingleNodeFixture(t)
	cmds := GenPlain("health", "", []string{"--wait-timeout=1m"})
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}
	if !strings.Contains(cmds[0].String(), " -n 10.0.0.1") || !strings.Contains(cmds[0].String(), "--talosconfig "+talosconfig.TalosconfigPath()) {
		t.Fatalf("unexpected command %q", cmds[0])
	}
	if !strings.HasSuffix(cmds[0].String(), " --wait-timeout=1m") {
		t.Fatalf("expected extra flag, got %q", cmds[0])
	}
}

func TestGenApplySingleNode(t *testing.T) {
	withSingleNodeFixture(t)
	cmds := GenApply("", []string{"--timeout=1m"})
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}
	if !strings.Contains(cmds[0].String(), " apply-config ") || !strings.Contains(cmds[0].String(), " -f "+talosconfig.NodeConfigPath(talosconfig.Node{Name: "control-1"})) {
		t.Fatalf("unexpected command %q", cmds[0])
	}
	if !strings.Contains(cmds[0].String(), " -n 10.0.0.1") || !strings.HasSuffix(cmds[0].String(), " --timeout=1m") {
		t.Fatalf("unexpected node or flags in %q", cmds[0])
	}
}

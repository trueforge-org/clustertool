package gencmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trueforge-org/clustertool/pkg/helper"
)

func TestGenKubeUpgrade(t *testing.T) {
	withSingleNodeFixture(t)
	previous := helper.TalosGenerated
	helper.TalosGenerated = t.TempDir()
	t.Cleanup(func() { helper.TalosGenerated = previous })
	if err := os.WriteFile(filepath.Join(helper.TalosGenerated, "control-1.yaml"), []byte("apiVersion: v1alpha1\nkind: KubeletConfig\nimage: ghcr.io/siderolabs/kubelet:v1.37.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	node := "10.0.0.1"
	cmd, err := GenKubeUpgrade(node)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cmd.String(), "--to 1.37.0") {
		t.Fatalf("Kubernetes version missing: %s", cmd)
	}

	if !strings.Contains(cmd.String(), " upgrade-k8s ") {
		t.Fatalf("expected upgrade-k8s in command, got %q", cmd)
	}

	if !strings.Contains(cmd.String(), "--talosconfig "+filepath.Join(helper.TalosGenerated, "talosconfig")) {
		t.Fatalf("expected talosconfig path in command, got %q", cmd)
	}

	if !strings.Contains(cmd.String(), " -n "+node) {
		t.Fatalf("expected node argument in command, got %q", cmd)
	}
}

func TestUpgradeKeepsConfiguredSchematic(t *testing.T) {
	withSingleNodeFixture(t)
	previous := helper.TalosGenerated
	helper.TalosGenerated = t.TempDir()
	t.Cleanup(func() { helper.TalosGenerated = previous })
	image := "factory.talos.dev/metal-installer/4c4acaf75b4a51d6ec95b38dc8b49fb0af5f699e7fbd12fbf246821c649b5312:v1.14.0"
	if err := os.WriteFile(filepath.Join(helper.TalosGenerated, "control-1.yaml"), []byte("apiVersion: v1alpha1\nkind: UnattendedInstallConfig\ninstaller:\n  image: "+image+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	commands, err := GenUpgrade("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(commands[0].String(), "--image "+image) || !strings.Contains(commands[0].String(), "--wait") {
		t.Fatal(commands)
	}
	if _, err := GenUpgrade("10.0.0.2", nil); err == nil {
		t.Fatal("accepted an unconfigured node")
	}
	if _, err := GenUpgrade("", []string{"--image=other"}); err == nil {
		t.Fatal("accepted an installer override")
	}
}

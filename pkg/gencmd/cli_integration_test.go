package gencmd

import (
	"github.com/trueforge-org/clustertool/pkg/helper"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Asking the real CLI for help parses flags without contacting any node. This
// catches removed flags such as --preserve that string-only tests cannot detect.
func TestGeneratedFlagsMatchTalosCLI(t *testing.T) {
	if os.Getenv("TALOSCTL_INTEGRATION") != "1" {
		t.Skip("requires real talosctl")
	}
	withSingleNodeFixture(t)
	previous := helper.TalosGenerated
	helper.TalosGenerated = t.TempDir()
	t.Cleanup(func() { helper.TalosGenerated = previous })
	data := []byte("apiVersion: v1alpha1\nkind: KubeletConfig\nimage: ghcr.io/siderolabs/kubelet:v1.37.0\n---\napiVersion: v1alpha1\nkind: UnattendedInstallConfig\ninstaller:\n  image: factory.talos.dev/metal-installer/test:v1.14.0\n")
	if err := os.WriteFile(filepath.Join(helper.TalosGenerated, "control-1.yaml"), data, 0600); err != nil {
		t.Fatal(err)
	}
	upgrades, err := GenUpgrade("", nil)
	if err != nil {
		t.Fatal(err)
	}
	kube, err := GenKubeUpgrade("")
	if err != nil {
		t.Fatal(err)
	}
	commands := append(upgrades, GenApply("", nil)...)
	commands = append(commands, GenPlain("health", "", nil)...)
	commands = append(commands, GenPlain("kubeconfig", "", []string{"-f"})...)
	commands = append(commands, kube)
	for _, command := range commands {
		if command.Err != nil {
			t.Fatal(command.Err)
		}
		args := append(append([]string{}, command.Args[1:]...), "--help")
		if output, err := exec.Command(command.Args[0], args...).CombinedOutput(); err != nil {
			t.Fatalf("invalid %s flags: %v: %s", command.Args[1], err, output)
		}
	}
}

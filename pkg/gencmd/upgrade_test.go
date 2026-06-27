package gencmd

import (
	"strings"
	"testing"

	"github.com/trueforge-org/clustertool/pkg/helper"
)

func TestGenKubeUpgrade(t *testing.T) {
	node := "10.1.2.3"
	cmd := GenKubeUpgrade(node)

	if !strings.Contains(cmd, " upgrade-k8s ") {
		t.Fatalf("expected upgrade-k8s in command, got %q", cmd)
	}

	if !strings.Contains(cmd, "--talosconfig "+helper.TalosConfigFile) {
		t.Fatalf("expected talosconfig path in command, got %q", cmd)
	}

	if !strings.Contains(cmd, " -n "+node) {
		t.Fatalf("expected node argument in command, got %q", cmd)
	}
}

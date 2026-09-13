package gencmd

import (
	"github.com/trueforge-org/clustertool/pkg/helper"
	"os"
	"path/filepath"
	"testing"
)

func TestBootstrapCheckpointBindsIdentity(t *testing.T) {
	withSingleNodeFixture(t)
	secrets := filepath.Join(helper.TalosPath, "secrets.sops.yaml")
	if err := os.WriteFile(secrets, []byte("identity-one"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := beginBootstrap(); err != nil {
		t.Fatal(err)
	}
	if pending, err := BootstrapPending(); err != nil || !pending {
		t.Fatal(pending, err)
	}
	if err := os.WriteFile(secrets, []byte("identity-two"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := BootstrapPending(); err == nil {
		t.Fatal("resumed with different identity")
	}
}

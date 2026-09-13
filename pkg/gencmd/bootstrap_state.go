package gencmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
)

type bootstrapState struct {
	Node     string `json:"node"`
	Identity string `json:"identity"`
}

func bootstrapStatePath() string {
	return filepath.Join(helper.TalosPath, ".bootstrap-in-progress.json")
}

func currentBootstrapState() (bootstrapState, error) {
	inv, err := talosconfig.LoadInventory()
	if err != nil {
		return bootstrapState{}, err
	}
	data, err := os.ReadFile(talosconfig.SecretsPath())
	if err != nil {
		return bootstrapState{}, err
	}
	var identity any
	if err := yaml.Unmarshal(data, &identity); err != nil {
		return bootstrapState{}, err
	}
	canonical, err := json.Marshal(identity)
	if err != nil {
		return bootstrapState{}, err
	}
	hash := sha256.Sum256(canonical)
	return bootstrapState{Node: inv.Bootstrap().Address, Identity: hex.EncodeToString(hash[:])}, nil
}

// A local checkpoint records explicit initial-bootstrap consent, bound to the
// cluster identity and bootstrap address. Live etcd still decides whether the
// bootstrap RPC is necessary on resume.
func BootstrapPending() (bool, error) {
	data, err := os.ReadFile(bootstrapStatePath())
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var saved bootstrapState
	if err := json.Unmarshal(data, &saved); err != nil {
		return false, err
	}
	current, err := currentBootstrapState()
	if err != nil {
		return false, err
	}
	if saved != current {
		return false, fmt.Errorf("bootstrap checkpoint does not match the current identity/address; inspect it before continuing")
	}
	return true, nil
}

func beginBootstrap() error {
	pending, err := BootstrapPending()
	if err != nil || pending {
		return err
	}
	state, err := currentBootstrapState()
	if err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(bootstrapStatePath(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

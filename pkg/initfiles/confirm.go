package initfiles

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/trueforge-org/clustertool/pkg/helper"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
	fthelper "github.com/trueforge-org/forgetool/v4/pkg/helper"
)

// ConfirmInit checks the cluster before any repository files are changed.
func ConfirmInit() error {
	for _, file := range []string{
		filepath.Join(helper.TalosPath, "talconfig.yaml"),
		filepath.Join(helper.ClusterPath, "clusterenv.yaml"),
		filepath.Join("clusters", helper.ClusterName, "talos", "talconfig.yaml"),
		filepath.Join("clusters", helper.ClusterName, "talos", "clustertool.yaml"),
		filepath.Join("clusters", helper.ClusterName, "clusterenv.yaml"),
	} {
		if _, err := os.Stat(file); err == nil {
			return fmt.Errorf("Legacy cluster layout found. Initialize ClusterTool in a new folder.")
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check legacy configuration: %w", err)
		}
	}
	if _, err := os.Stat("RUNAGAIN"); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check RUNAGAIN: %w", err)
	}
	if _, err := os.Stat(talosconfig.SecretsPath()); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("check Talos secrets: %w", err)
	}
	fmt.Println("WARNING: This cluster appears to have already been initialized.\nRunning init again may add or modify files.\n\nWe recommend initializing in a new folder and comparing the files\nwith your existing configuration.")
	if !fthelper.GetYesOrNo("\nContinue with init? [y/n]: ", false) {
		return fmt.Errorf("init cancelled")
	}
	return nil
}

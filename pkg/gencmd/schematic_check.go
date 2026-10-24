package gencmd

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
)

// ConfirmTalosInstallation checks maintenance nodes before any configuration is applied.
// The detected schematic describes the current boot image, not a separate disk inspection.
func ConfirmTalosInstallation(nodes []talosconfig.Node, confirm func(string, bool) bool) error {
	warnings := false
	for _, node := range nodes {
		stage, err := checkStatus(node.Address)
		if err != nil {
			return fmt.Errorf("inspect node %s before apply: %w", node.Name, err)
		}
		if stage != "maintenance" {
			continue
		}

		image, err := talosconfig.GeneratedNodeValue(talosconfig.NodeConfigPath(node), "UnattendedInstallConfig", "installer", "image")
		if err != nil {
			return err
		}
		desired := "unknown"
		reference, _, _ := strings.Cut(image, "@")
		id, _, _ := strings.Cut(reference[strings.LastIndex(reference, "/")+1:], ":")
		if len(id) == 64 {
			if _, err := hex.DecodeString(id); err == nil {
				desired = id
			}
		}

		var volume struct {
			Spec struct {
				Phase string `json:"phase"`
			} `json:"spec"`
		}
		out, stderr, err := runCommand(nodeCommand("get", node, "volumestatus", "META", "-e", node.Address, "--insecure", "-o", "json").Args, true)
		if err == nil {
			err = json.Unmarshal([]byte(out), &volume)
		}
		if err != nil {
			log.Warn().Str("node", node.Name).Err(err).Msgf("Cannot determine Talos installation state: %s", strings.TrimSpace(stderr))
		}
		if err == nil && volume.Spec.Phase == "missing" {
			// A fresh ISO may use a different schematic; the installer still has to run.
			continue
		}
		installation := "unknown"
		if err == nil && volume.Spec.Phase == "ready" {
			installation = "installed"
		}

		var systemDisk struct {
			Spec struct {
				Path string `json:"devPath"`
			} `json:"spec"`
		}
		out, stderr, err = runCommand(nodeCommand("get", node, "systemdisk", "system-disk", "-e", node.Address, "--insecure", "-o", "json").Args, true)
		if err == nil {
			err = json.Unmarshal([]byte(out), &systemDisk)
		}
		if err != nil {
			log.Warn().Str("node", node.Name).Err(err).Msgf("Cannot read the detected Talos installation disk: %s", strings.TrimSpace(stderr))
		}
		disk := "unknown"
		if err == nil && strings.TrimSpace(systemDisk.Spec.Path) != "" {
			disk = strings.TrimSpace(systemDisk.Spec.Path)
		}

		var schematic struct {
			Spec struct {
				ID string `json:"schematicId"`
			} `json:"spec"`
		}
		out, stderr, err = runCommand(nodeCommand("get", node, "imagefactoryschematic", "-e", node.Address, "--insecure", "-o", "json").Args, true)
		if err == nil {
			err = json.Unmarshal([]byte(out), &schematic)
		}
		if err != nil {
			log.Warn().Str("node", node.Name).Err(err).Msgf("Cannot read current Talos schematic: %s", strings.TrimSpace(stderr))
		}
		detected := "unknown"
		if err == nil && strings.TrimSpace(schematic.Spec.ID) != "" {
			detected = strings.TrimSpace(schematic.Spec.ID)
		}

		warnings = true
		if installation == "installed" {
			log.Warn().Msgf("Talos is already installed on %s for %s (%s).", disk, node.Name, node.Address)
			log.Warn().Msg("Unattended installation will be skipped.")
		} else {
			log.Warn().Msgf("Cannot verify the Talos installation state for %s (%s).", node.Name, node.Address)
			log.Warn().Msg("Installation state: unknown")
			log.Warn().Msgf("Detected Talos installation disk: %s", disk)
		}
		log.Warn().Msgf("Detected schematic: %s", detected)
		log.Warn().Msgf("Desired schematic:  %s", desired)
		if desired == "unknown" {
			log.Warn().Msgf("Configured installer image: %s", image)
		}
	}

	if warnings {
		log.Warn().Msg("Applying configuration does not replace an existing Talos image or reinstall it on the configured target disk.")
		log.Warn().Msg("Tip: If you intend to reinstall, verify the node and disk before removing the existing Talos installation. See \"talosctl wipe disk --help\" for disk-wiping options.")
		log.Warn().Msg("Wiping destroys data. Disks in use may require a different reset/wipe procedure.")
		if !confirm("Continue applying configuration despite these warnings? [y/n]: ", false) {
			return fmt.Errorf("apply cancelled: Talos installation warnings were not accepted")
		}
	}
	return nil
}

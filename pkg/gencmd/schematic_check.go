package gencmd

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
)

// ConfirmInstallSchematic checks maintenance nodes before any configuration is applied.
// The detected schematic describes the current boot image, not a separate disk inspection.
func ConfirmInstallSchematic(nodes []talosconfig.Node, confirm func(string, bool) bool) error {
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
		if installation == "installed" && desired != "unknown" && detected == desired {
			continue
		}

		warnings = true
		log.Warn().Msgf("Node %s (%s): verify the Talos image before applying configuration.", node.Name, node.Address)
		log.Warn().Msgf("Installation state: %s", installation)
		log.Warn().Msgf("Detected schematic: %s", detected)
		log.Warn().Msgf("Desired schematic:  %s", desired)
		if desired == "unknown" {
			log.Warn().Msgf("Configured installer image: %s", image)
		}
	}

	if warnings {
		log.Warn().Msg("Applying configuration does not replace an existing Talos installation. The current kernel arguments and system extensions may remain in use.")
		if !confirm("Continue applying configuration despite these warnings? [y/n]: ", false) {
			return fmt.Errorf("apply cancelled: Talos schematic was not verified")
		}
	}
	return nil
}

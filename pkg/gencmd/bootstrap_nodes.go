package gencmd

import (
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/trueforge-org/clustertool/pkg/nodestatus"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
)

var waitForBootstrapHealth = nodestatus.WaitForHealth

func bootstrapTalosNodes(inv *talosconfig.Inventory, applyNode func(string) error) error {
	// Configure every node before bootstrap or chart readiness can depend on it.
	// On resume, keep configurations already applied to booting/running nodes.
	for _, node := range inv.Nodes {
		log.Info().Msgf("Bootstrap: preparing node %s (%s)", node.Name, node.Address)
		stage, err := checkStatus(node.Address)
		if err != nil {
			return fmt.Errorf("inspect node %s for bootstrap: %w", node.Name, err)
		}
		switch stage {
		case "maintenance":
			if err := applyNode(node.Address); err != nil {
				return fmt.Errorf("apply node %s for bootstrap: %w", node.Name, err)
			}
		case "booting", "running":
		default:
			return fmt.Errorf("node %s is in unexpected stage %s during bootstrap", node.Name, stage)
		}
	}

	log.Info().Msg("Bootstrap: checking for an existing bootstrapped control plane; this may take up to 30 seconds per control-plane node")
	if _, err := ExistingControlPlane(inv); err == nil {
		return nil
	}
	bootstrapNode := inv.Bootstrap().Address
	// Only installation progress is required here, not full node readiness.
	if _, err := waitForBootstrapHealth(bootstrapNode, []string{"booting", "running"}); err != nil {
		return err
	}
	log.Info().Msgf("Bootstrap: waiting for installation/bootstrap availability on %s; inspect the disk selector and installation logs if it remains unavailable", bootstrapNode)
	// Check again after installation: a prior interrupted RPC may have succeeded.
	return bootstrapIfNeeded(inv)
}

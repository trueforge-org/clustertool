package nodestatus

import (
	"fmt"
	"path"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/trueforge-org/clustertool/embed"
	"github.com/trueforge-org/clustertool/pkg/helper"
)

var runCommand = helper.RunCommandWithTimeout

func baseStatusCMD(node string) []string {
	argsslice := [...]string{embed.GetTalosExec(), "--talosconfig=" + path.Join(helper.ClusterPath, "/talos/generated/talosconfig"), "-n", node, "-e", node, "get", "machinestatus"}

	log.Debug().Strs("command", argsslice[:]).Msg("Constructed base command for machine status")
	return argsslice[:]
}

func CheckStatus(node string) (string, error) {
	log.Info().Str("node", node).Msg("Checking node status")

	argsslice := append(baseStatusCMD(node), "-o", "jsonpath={.spec.stage}")
	out, stderr, err := runCommand(argsslice, true)
	if err != nil {
		log.Debug().Err(err).Str("stderr", stderr).Msg("Error running command, checking for certificate issue")
		if strings.Contains(stderr, "certificate signed by unknown authority") {
			log.Debug().Msg("Certificate signed by unknown authority; retrying with insecure flag")
			argsslice = append(baseStatusCMD(node), "-o", "jsonpath={.spec.stage}", "--insecure")
			out2, stderr2, err2 := runCommand(argsslice, true)
			if err2 != nil {
				return "ERROR", fmt.Errorf("node %s status: %w: %s", node, err2, strings.TrimSpace(stderr2))
			}
			if strings.TrimSpace(stderr2) != "" {
				log.Warn().Str("node", node).Msg(strings.TrimSpace(stderr2))
			}
			log.Info().Msg("Successfully retrieved node status with insecure flag")
			if strings.TrimSpace(out2) != "maintenance" {
				return "ERROR", fmt.Errorf("node %s reported %q; refusing insecure access outside maintenance", node, strings.TrimSpace(out2))
			}
			return "maintenance", nil
		} else {
			return "ERROR", fmt.Errorf("node %s status: %w: %s", node, err, strings.TrimSpace(stderr))
		}
	}
	if strings.TrimSpace(stderr) != "" {
		log.Warn().Str("node", node).Msg(strings.TrimSpace(stderr))
	}
	log.Info().Str("status", strings.TrimSpace(out)).Msg("Node status retrieved successfully")
	return strings.TrimSpace(out), nil
}

func CheckReadyStatus(node string, silent bool) (string, error) {
	log.Info().Str("node", node).Msg("Checking node readiness status")

	argsslice := append(baseStatusCMD(node), "-o", "jsonpath={.spec.status.ready}")
	out, stderr, err := runCommand(argsslice, true)

	if err != nil {
		return "ERROR", fmt.Errorf("node %s readiness: %w: %s", node, err, strings.TrimSpace(stderr))
	}
	if strings.TrimSpace(stderr) != "" {
		log.Warn().Str("node", node).Msg(strings.TrimSpace(stderr))
	}
	if strings.TrimSpace(out) == "true" {
		log.Info().Msg("Node is ready")
	} else {
		return strings.TrimSpace(out), fmt.Errorf("node %s is not ready", node)
	}
	return strings.TrimSpace(out), nil
}

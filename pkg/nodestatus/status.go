package nodestatus

import (
	"errors"
	"path"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/trueforge-org/clustertool/embed"
	"github.com/trueforge-org/clustertool/pkg/helper"
)

func baseStatusCMD(node string) []string {
	argsslice := [...]string{embed.GetTalosExec(), "--talosconfig=" + path.Join(helper.ClusterPath, "/talos/generated/talosconfig"), "-n", node, "-e", node, "get", "machinestatus"}

	log.Debug().Strs("command", argsslice[:]).Msg("Constructed base command for machine status")
	return argsslice[:]
}

func CheckStatus(node string) (string, error) {
	log.Info().Str("node", node).Msg("Checking node status")

	argsslice := append(baseStatusCMD(node), "-o", "jsonpath={.spec.stage}")
	out, err := helper.RunBoundedCommand(argsslice, true)
	if err != nil {
		log.Debug().Err(err).Str("output", string(out)).Msg("Error running command, checking for certificate issue")
		if strings.Contains(string(out), "certificate signed by unknown authority") {
			log.Debug().Msg("Certificate signed by unknown authority; retrying with insecure flag")
			argsslice = append(baseStatusCMD(node), "-o", "jsonpath={.spec.stage}", "--insecure")
			out2, err2 := helper.RunBoundedCommand(argsslice, true)
			if err2 != nil {
				errstring := "status: " + string(out) + " error: " + err2.Error()
				log.Error().Msg(errstring)
				return "ERROR", errors.New(errstring)
			}
			log.Info().Msg("Successfully retrieved node status with insecure flag")
			if strings.TrimSpace(string(out2)) != "maintenance" {
				return "ERROR", errors.New("unauthenticated node did not report maintenance; refusing insecure access")
			}
			return "maintenance", nil
		} else {
			errstring := "status: " + string(out) + " error: " + err.Error()
			log.Error().Msg(errstring)
			return "ERROR", errors.New(errstring)
		}
	}
	log.Info().Str("status", strings.TrimSpace(string(out))).Msg("Node status retrieved successfully")
	return strings.TrimSpace(string(out)), nil
}

func CheckReadyStatus(node string, silent bool) (string, error) {
	log.Info().Str("node", node).Msg("Checking node readiness status")

	argsslice := append(baseStatusCMD(node), "-o", "jsonpath={.spec.status.ready}")
	out, err := helper.RunBoundedCommand(argsslice, true)

	if err != nil {
		errstring := "status: " + string(out) + " error: " + err.Error()
		if !silent {
			log.Error().Msg(errstring)
		}
		return "ERROR", errors.New(errstring)
	}
	if strings.TrimSpace(string(out)) == "true" {
		log.Info().Msg("Node is ready")
	} else {
		return strings.TrimSpace(string(out)), errors.New("node is not ready")
	}
	return strings.TrimSpace(string(out)), nil
}

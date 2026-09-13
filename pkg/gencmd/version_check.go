package gencmd

import (
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/rs/zerolog/log"
	"github.com/trueforge-org/clustertool/embed"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
)

// Check the binary actually used for generation, rather than a template version.
func confirmTalosVersion(confirm func(string, bool) bool) error {
	inv, err := talosconfig.LoadInventory()
	if err != nil {
		return err
	}
	out, err := runCommand([]string{embed.GetTalosExec(), "version", "--client", "--short"}, true)
	if err != nil {
		return fmt.Errorf("read talosctl version: %w: %s", err, strings.TrimSpace(string(out)))
	}
	clientVersion := ""
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "Talos" {
			clientVersion = fields[1]
			break
		}
	}
	client, err := semver.StrictNewVersion(strings.TrimPrefix(clientVersion, "v"))
	if err != nil {
		return fmt.Errorf("cannot determine talosctl version from client output")
	}
	target, err := semver.StrictNewVersion(strings.TrimPrefix(inv.TalosVersion, "v"))
	if err != nil {
		return fmt.Errorf("invalid configured Talos version: %w", err)
	}
	if client.Major() == target.Major() && client.Minor() == target.Minor() {
		return nil
	}
	log.Warn().Msgf("Bundled talosctl %s differs from configured Talos %s. Configuration compatibility is not guaranteed.", clientVersion, inv.TalosVersion)
	if !confirm("Continue anyway? [y/n]: ", false) {
		return fmt.Errorf("configuration generation cancelled: Talos version mismatch")
	}
	return nil
}

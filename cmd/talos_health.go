package cmd

import (
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/trueforge-org/clustertool/pkg/gencmd"
	"github.com/trueforge-org/clustertool/pkg/initfiles"
	"github.com/trueforge-org/clustertool/pkg/sops"
)

var advHealthLongHelp = strings.TrimSpace(`

`)

var health = &cobra.Command{
	Use:     "health",
	Short:   "Check Talos Cluster Health",
	Example: "clustertool talos health",
	Long:    advHealthLongHelp,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := sops.DecryptFiles(); err != nil {
			return err
		}
		if err := initfiles.LoadTalEnv(false); err != nil {
			return err
		}
		log.Info().Msg("Running Cluster HealthCheck")
		healthcmd := gencmd.GenPlain("health", "", []string{})
		if len(healthcmd) == 0 {
			return fmt.Errorf("no nodes configured for health check")
		}
		for _, command := range healthcmd {
			if err := gencmd.ExecCmd(command); err != nil {
				return err
			}
		}
		return nil
	},
}

func init() {
	talosCmd.AddCommand(health)
}

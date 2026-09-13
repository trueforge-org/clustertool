package cmd

import (
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/trueforge-org/clustertool/pkg/gencmd"
	"github.com/trueforge-org/clustertool/pkg/initfiles"
	"github.com/trueforge-org/clustertool/pkg/sops"
)

var kubeconfig = &cobra.Command{
	Use:     "kubeconfig",
	Short:   "kubeconfig for Talos Cluster",
	Example: "clustertool talos kubeconfig <NodeIP>",
	Long:    advResetLongHelp,
	RunE: func(cmd *cobra.Command, args []string) error {
		var extraArgs []string
		node := ""

		if len(args) > 1 {
			extraArgs = args[1:]
		}
		if len(args) >= 1 {
			node = args[0]
			if args[0] == "all" {
				node = ""
			}
		}

		if err := sops.DecryptFiles(); err != nil {
			return err
		}
		if err := initfiles.LoadTalEnv(false); err != nil {
			return err
		}
		log.Info().Msg("Running Cluster kubeconfig")

		taloscmds := gencmd.GenPlain("kubeconfig", node, extraArgs)
		if err := gencmd.ExecCmds(taloscmds, true); err != nil {
			return err
		}

		return nil
	},
}

func init() {
	talosCmd.AddCommand(kubeconfig)
}

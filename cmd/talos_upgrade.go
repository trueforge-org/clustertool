package cmd

import (
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/trueforge-org/clustertool/pkg/gencmd"

	"github.com/trueforge-org/clustertool/pkg/initfiles"
	"github.com/trueforge-org/clustertool/pkg/sops"
)

var upgradeLongHelp = strings.TrimSpace(`
The "upgrade" command updates nodes selected in clustertool.yaml sequentially using each node's validated installer image and schematic ID. With no target, all nodes are selected.

After upgrading Talos, it upgrades Kubernetes to the configured kubelet version.

`)

var upgrade = &cobra.Command{
	Use:     "upgrade",
	Short:   "Upgrade Talos Nodes and Kubernetes",
	Example: "clustertool talos upgrade <NodeIP>",
	Long:    upgradeLongHelp,
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

		log.Info().Msg("Running Cluster Upgrade")

		if err := gencmd.GenConfig(nil); err != nil {
			return err
		}
		taloscmds, err := gencmd.GenUpgrade(node, extraArgs)
		if err != nil {
			return err
		}
		talosOnly, _ := cmd.Flags().GetBool("talos-only")
		var kubeUpgradeCmd gencmd.Command
		if !talosOnly {
			kubeUpgradeCmd, err = gencmd.GenKubeUpgrade("")
			if err != nil {
				return err
			}
		}
		if err := gencmd.PreflightUpgrade(taloscmds, kubeUpgradeCmd); err != nil {
			return err
		}
		if err := gencmd.ExecCmds(taloscmds, true); err != nil {
			return err
		}

		if talosOnly {
			return nil
		}
		log.Info().Msg("Running one cluster-wide Kubernetes upgrade after the selected Talos nodes")
		if err := gencmd.ExecCmd(kubeUpgradeCmd); err != nil {
			return err
		}

		log.Info().Msg("(re)Loading KubeConfig)")
		kubeconfigcmds := gencmd.GenPlain("kubeconfig", "", []string{"-f"})
		if err := gencmd.ExecCmd(kubeconfigcmds[0]); err != nil {
			return err
		}

		return nil
	},
}

func init() {
	upgrade.Flags().Bool("talos-only", false, "Skip the cluster-wide Kubernetes upgrade")
	talosCmd.AddCommand(upgrade)
}

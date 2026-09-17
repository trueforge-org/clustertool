package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/trueforge-org/clustertool/pkg/gencmd"
)

var genConfigLongHelp = strings.TrimSpace(`
Reads secrets/cluster-settings.sops.yaml and the native Talos documents to generate and validate the cluster configuration with talosctl.

Run genconfig after changing settings. Flux uses the same cluster-settings Secret directly.
`)

var genConfig = &cobra.Command{
	Use:     "genconfig",
	Short:   "generate Cluster Configuration files",
	Long:    genConfigLongHelp,
	Example: "clustertool genconfig",
	RunE: func(cmd *cobra.Command, args []string) error {
		return gencmd.GenConfig(args)
	},
}

func init() {
	RootCmd.AddCommand(genConfig)
}

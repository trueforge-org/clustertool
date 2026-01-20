package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/trueforge-org/forgetool/pkg/gencmd"
)

var genConfigLongHelp = strings.TrimSpace(`
After all your settings are entered into talconfig.yaml and clusterenv.yaml, Clustertool generates a complete clusterconfiguration using TalHelper and various other tools.

It's important to note that running clustertool genconfig, again after each settings change, is absolutely imperative to be able to deploy said settings to your cluster.

This does not only generate the Talos "Machine Config" files, but also ensures an updated configmap containing your "clusterenv.yaml" settings, is added to the /manifests/ directory, for consumption by FluxCD when added.
It also ensures the same configmap is always added by updating the patches.
`)

var genConfig = &cobra.Command{
	Use:     "genconfig",
	Short:   "generate Cluster Configuration files",
	Long:    genConfigLongHelp,
	Example: "forgetool cluster genconfig",
	Run: func(cmd *cobra.Command, args []string) {

		gencmd.GenConfig(args)
	},
}

func init() {
	clusterCmd.AddCommand(genConfig)
}

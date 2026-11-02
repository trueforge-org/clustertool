package cmd

import (
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/trueforge-org/clustertool/pkg/fluxhandler"
	"github.com/trueforge-org/clustertool/pkg/helper"
)

var genks = &cobra.Command{
	Use:     "genks",
	Aliases: []string{"kustomizations"},
	Short:   "Generate Kubernetes Kustomization files",
	Long:    "Create missing ks.yaml files and add resources to kustomization.yaml files under kubernetes/apps. Existing ks.yaml files are preserved.",
	Example: "clustertool genks",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := fluxhandler.ProcessDirectory(helper.KubernetesAppsPath); err != nil {
			return err
		}
		// The second pass lets parent directories reference newly created ks.yaml files.
		if err := fluxhandler.ProcessDirectory(helper.KubernetesAppsPath); err != nil {
			return err
		}
		log.Info().Msg("Kustomizations processed successfully.")
		return nil
	},
}

func init() {
	RootCmd.AddCommand(genks)
}

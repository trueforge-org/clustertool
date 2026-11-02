package cmd

import (
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/trueforge-org/clustertool/pkg/helper"
)

var thisversion string

var RootCmd = &cobra.Command{
	Use:           "clustertool",
	Short:         "A tool to help with creating Talos cluster",
	Long:          infoLongHelp,
	SilenceUsage:  true,
	SilenceErrors: true,
	Version:       thisversion,
}

func init() {
	RootCmd.SetHelpCommand(&cobra.Command{Hidden: true})
}

func Execute() error {
	log.Info().Msgf("Cluster name: %s", helper.ClusterName)

	// Execute the root command and all subcommands
	return RootCmd.Execute()
}

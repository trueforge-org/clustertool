package cmd

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"gopkg.in/yaml.v3"
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
	clusterName := "<not configured>"
	if data, err := os.ReadFile(filepath.Join(helper.TalosPath, "clustertool.yaml")); err == nil {
		var config struct {
			ClusterName string `yaml:"clusterName"`
		}
		if err := yaml.Unmarshal(data, &config); err == nil && strings.TrimSpace(config.ClusterName) != "" {
			clusterName = config.ClusterName
		}
	}
	log.Info().Msgf("Cluster name: %s", clusterName)

	// Execute the root command and all subcommands
	return RootCmd.Execute()
}

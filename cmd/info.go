package cmd

import (
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/trueforge-org/forgetool/v4/pkg/info"
)

var description = strings.TrimSpace(`Clustertool is a tool to help you easily deploy and maintain a Talos Kubernetes Cluster.
`)

var infoLongHelp = strings.TrimSpace(description + `

Workflow:
  Configure clusterenv.yaml and the native Talos documents:

 Available commands
  > clustertool init
  > clustertool genconfig

`)

var infoCmd = &cobra.Command{
	Use:     "info",
	Short:   "Prints information about the clustertool binary",
	Long:    infoLongHelp,
	Example: "clustertool info",
	Run: func(cmd *cobra.Command, args []string) {
		log.Info().Msg(description)
		info.NewInfo().Print()
	},
}

func init() {
	RootCmd.AddCommand(infoCmd)
}

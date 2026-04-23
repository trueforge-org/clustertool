package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/trueforge-org/clustertool/pkg/info"
)

var infoLongHelp = strings.TrimSpace(`
Forgetool is a tool to help you easily deploy and maintain a Talos Kubernetes Cluster.


Workflow:
  Create talconfig.yaml file defining your nodes information like so:

 Available commands
  > forgetool cluster init
  > forgetool cluster genconfig

`)

var infoCmd = &cobra.Command{
	Use:     "info",
	Short:   "Prints information about the forgetool binary",
	Long:    infoLongHelp,
	Example: "forgetool info",
	Run: func(cmd *cobra.Command, args []string) {
		info.NewInfo().Print()
	},
}

func init() {
	RootCmd.AddCommand(infoCmd)
}

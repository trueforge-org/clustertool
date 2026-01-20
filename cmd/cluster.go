package cmd

import (
	"strings"

	"github.com/spf13/cobra"
)

var clusterLongHelp = strings.TrimSpace(`
These are all commands that can be used to maintain cluster OS

`)

var clusterCmd = &cobra.Command{
	Use:           "cluster",
	Short:         "Commands for handling cluster management",
	Example:       "clustertool cluster init",
	Long:          clusterLongHelp,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	RootCmd.AddCommand(clusterCmd)
}

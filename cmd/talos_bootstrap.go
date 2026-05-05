package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/trueforge-org/clustertool/pkg/gencmd"
	"github.com/trueforge-org/clustertool/pkg/initfiles"
	"github.com/trueforge-org/clustertool/pkg/talassist"
	fthelper "github.com/trueforge-org/forgetool/v4/pkg/helper"
)

var advBootstrapLongHelp = strings.TrimSpace(`

`)

var bootstrap = &cobra.Command{
	Use:     "bootstrap",
	Short:   "bootstrap first Talos Node",
	Example: "clustertool talos bootstrap",
	Long:    advBootstrapLongHelp,
	Run:     bootstrapfunc,
}

func bootstrapfunc(cmd *cobra.Command, args []string) {
	if fthelper.GetYesOrNo("Do you want to also run the complete Clustertool Bootstrap, besides just talos? (yes/no) [y/n]: ", false) {
		initfiles.LoadTalEnv(false)
		talassist.LoadTalConfig()
		gencmd.RunBootstrap(args)
	} else {
		bootstrapcmds := gencmd.GenPlain("bootstrap", talassist.TalConfig.Nodes[0].IPAddress, []string{})
		gencmd.ExecCmd(bootstrapcmds[0])
	}
}

func init() {
	talosCmd.AddCommand(bootstrap)
}

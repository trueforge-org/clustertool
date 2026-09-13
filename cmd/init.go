package cmd

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/trueforge-org/clustertool/pkg/initfiles"
	"github.com/trueforge-org/clustertool/pkg/sops"
)

var initLongHelp = strings.TrimSpace(`
Clustertool requires a specific directory layout to ensure smooth operators and standardised environments.

To ensure smooth deployment, the init function can pre-generate all required files in the right places.
Afterwards, edit clusterenv.yaml and the native Talos documents to reflect your personal settings.

When done, please run clustertool genconfig to generate all configurations based on your personal settings.
`)

var initFiles = &cobra.Command{
	Use:     "init",
	Short:   "generate Basic cluster file-and-folder structure in current folder",
	Long:    initLongHelp,
	Example: "clustertool init",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := sops.DecryptFiles(); err != nil {
			// A missing SOPS config is expected during the first initialization.
			if _, statErr := os.Stat(".sops.yaml"); !os.IsNotExist(statErr) {
				return err
			}
		}

		return initfiles.InitFiles()
	},
}

func init() {
	RootCmd.AddCommand(initFiles)
}

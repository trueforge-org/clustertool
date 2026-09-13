package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/trueforge-org/clustertool/pkg/sops"
)

var decryptLongHelp = strings.TrimSpace(`
The decryption feature of clustertool goes over all config files and, if encrypted, checks if ".sops.yaml" specifies that they should be decrypted.
If so, they are decrypted using your "age.agekey" file as specified in ".sops.yaml".

`)

var decrypt = &cobra.Command{
	Use:     "decrypt",
	Short:   "Decrypt all high-risk data using sops",
	Example: "clustertool decrypt",
	Long:    decryptLongHelp,
	RunE: func(cmd *cobra.Command, args []string) error {
		return sops.DecryptFiles()
	},
}

func init() {
	RootCmd.AddCommand(decrypt)
}

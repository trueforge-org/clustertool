package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/trueforge-org/clustertool/pkg/sops"
)

var encryptLongHelp = strings.TrimSpace(`
The encryption feature of clustertool goes over all config files and, if not encrypted already, checks if ".sops.yaml" mandates that they should be encrypted.
Afterwards, they are encrypted using your "age.agekey" file as specified in ".sops.yaml".

`)

var encrypt = &cobra.Command{
	Use:     "encrypt",
	Short:   "Encrypt all high-risk data using sops",
	Example: "clustertool encrypt",
	Long:    encryptLongHelp,
	RunE: func(cmd *cobra.Command, args []string) error {
		return sops.EncryptAllFiles()
	},
}

func init() {
	RootCmd.AddCommand(encrypt)
}

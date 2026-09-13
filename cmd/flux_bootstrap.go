package cmd

import (
	"context"
	"strings"

	"github.com/spf13/cobra"
	"github.com/trueforge-org/clustertool/pkg/fluxhandler"
	"github.com/trueforge-org/clustertool/pkg/initfiles"
	"github.com/trueforge-org/clustertool/pkg/sops"
)

var fluxBootstrapLongHelp = strings.TrimSpace(`

`)

var fluxbootstrap = &cobra.Command{
	Use:     "bootstrap",
	Short:   "Manually bootstrap fluxcd on existing cluster",
	Example: "clustertool flux bootstrap",
	Long:    fluxBootstrapLongHelp,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		if err := sops.DecryptFiles(); err != nil {
			return err
		}
		if err := initfiles.LoadTalEnv(false); err != nil {
			return err
		}
		return fluxhandler.FluxBootstrap(ctx)
	},
}

func init() {
	fluxCmd.AddCommand(fluxbootstrap)
}

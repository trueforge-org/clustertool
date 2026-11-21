package cmd

import (
	"github.com/spf13/cobra"
	"github.com/trueforge-org/clustertool/pkg/helper"
)

var githook = &cobra.Command{
	Use:     "githook",
	Short:   "Install the Git pre-commit encryption hook",
	Long:    "Install or replace the Git pre-commit encryption hook for the current environment. Run from your cluster repository root after switching environments or rebuilding a devcontainer. Put custom checks in .git/hooks/pre-commit.local; this file is preserved and runs after the encryption check succeeds. A local hook failure aborts the commit. On Linux, make it executable with chmod +x .git/hooks/pre-commit.local.",
	Example: "clustertool githook",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return helper.CreateEncrPreCommitHook()
	},
}

func init() {
	RootCmd.AddCommand(githook)
}

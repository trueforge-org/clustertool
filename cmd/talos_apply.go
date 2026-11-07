package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/trueforge-org/clustertool/pkg/clusterconfig"
	"github.com/trueforge-org/clustertool/pkg/gencmd"

	"github.com/trueforge-org/clustertool/pkg/initfiles"

	"github.com/trueforge-org/clustertool/pkg/sops"
	fthelper "github.com/trueforge-org/forgetool/v4/pkg/helper"
)

var applyLongHelp = strings.TrimSpace(`
The "apply" command validates and applies your Talos configuration to the nodes selected in clustertool.yaml, existing or new.

This is the recommended command for both initial cluster bootstrap and day-2 Talos config maintenance.

## Bootstrapping
If the cluster has not been bootstrapped yet, Apply will automatically detect this and ask if you want to bootstrap the cluster

Bootstrapping applies the generated native Talos configuration to all configured nodes and then bootstraps the cluster through the alphabetically first control-plane node by name.

ClusterTool installs Cilium and the kubelet CSR approver, creates namespaces and completes the base health checks.
It then executes the bootstrap list from talos/clustertool.yaml in the order listed.
Each step selects exactly one action: chart, crd or manifest.
Chart and CRD entries refer to HelmRelease metadata.name; manifest entries refer to repository-relative files.
By default, charts wait for their resources and Jobs with a 15-minute timeout before the next step starts.
Set wait: false on a chart step to skip these waits; Helm installation hooks retain their own waiting behavior.
The supplied configuration ends with flux-operator and flux-instance, with wait: false for flux-instance, so Flux can continue the rollout.
These steps run only during initial bootstrap or an explicitly confirmed resume, never during ordinary Talos configuration changes.

## About Bootstrapping

While we load a lot of helm-charts during bootstrap, we will *never* manage them for you.
You're responsible for maintaining and configuring your cluster after bootstrapping.

Apply and *all other* commands, are just for maintaining Talos itself.
Not any contained helm-charts

`)

var apply = &cobra.Command{
	Use:     "apply",
	Short:   "apply",
	Aliases: []string{"apply-config"},
	Example: "clustertool talos apply <NodeIP>",
	Long:    applyLongHelp,
	RunE: func(cmd *cobra.Command, args []string) error {
		var extraArgs []string
		node := ""

		if len(args) > 1 {
			extraArgs = args[1:]
		}
		if len(args) >= 1 {
			node = args[0]
			if args[0] == "all" {
				node = ""
			}
		}

		if err := sops.DecryptFiles(); err != nil {
			return err
		}

		if err := initfiles.LoadTalEnv(false); err != nil {
			return err

		}
		if err := clusterconfig.ValidateNode(node); err != nil {
			return err
		}
		if err := gencmd.ValidateExtraArgs(extraArgs); err != nil {
			return err
		}
		if err := gencmd.GenConfig(nil); err != nil {
			return err
		}
		inv, err := clusterconfig.LoadInventory()
		if err != nil {
			return err
		}
		pending, err := gencmd.BootstrapPending()
		if err != nil {
			return err
		}
		if pending {
			if node != "" {
				return fmt.Errorf("initial cluster setup is incomplete; run talos apply all to resume")
			}
			if err := gencmd.ConfirmTalosInstallation(inv.Nodes, fthelper.GetYesOrNo); err != nil {
				return err
			}
			return resumeBootstrap(inv.Bootstrap().Address, extraArgs, fthelper.GetYesOrNo, gencmd.RunBootstrap)
		}
		needed, err := gencmd.NeedsBootstrap(inv)
		if err != nil {
			return err
		}
		if needed {
			if node != "" && node != inv.Bootstrap().Name && node != inv.Bootstrap().Address {
				return fmt.Errorf("initialize the cluster with talos apply all before joining a selected node")
			}
			if !fthelper.GetYesOrNo("All configured control planes report maintenance. Bootstrap this new cluster? [y/n]: ", false) {
				return fmt.Errorf("bootstrap cancelled")
			}
			if err := gencmd.ConfirmTalosInstallation(inv.Nodes, fthelper.GetYesOrNo); err != nil {
				return err
			}
			return gencmd.RunBootstrap(extraArgs)
		}
		nodes, err := inv.Select(node)
		if err != nil {
			return err
		}
		if err := gencmd.ConfirmTalosInstallation(nodes, fthelper.GetYesOrNo); err != nil {
			return err
		}
		return RunApply(true, node, extraArgs)
	},
}

func RunApply(kubeconfig bool, node string, extraArgs []string) error {
	taloscmds := gencmd.GenApply(node, extraArgs)
	if err := gencmd.ExecCmds(taloscmds, true); err != nil {
		return err
	}

	if kubeconfig {
		kubeconfigcmds := gencmd.GenPlain("kubeconfig", "", []string{"-f"})
		return gencmd.ExecCmd(kubeconfigcmds[0])
	}

	return nil
}

func init() {
	talosCmd.AddCommand(apply)
}

func resumeBootstrap(address string, args []string, confirm func(string, bool) bool, run func([]string) error) error {
	fmt.Printf("An unfinished cluster bootstrap was found for node %s.\n", address)
	if !confirm("Resume cluster setup? [y/n]: ", false) {
		return fmt.Errorf("bootstrap resume cancelled")
	}
	return run(args)
}

package gencmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/trueforge-org/clustertool/pkg/clusterconfig"
	"github.com/trueforge-org/clustertool/pkg/helmhandler"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"github.com/trueforge-org/clustertool/pkg/kubectlcmds"
	"github.com/trueforge-org/clustertool/pkg/nodestatus"
	"github.com/trueforge-org/clustertool/pkg/sops"
)

var HelmRepos map[string]*helmhandler.HelmRepo

func RunBootstrap(args []string) error {
	extraArgs := args

	if err := sops.DecryptFiles(); err != nil {
		return err
	}

	// Load the configured phases before preparing any nodes.
	inv, err := clusterconfig.LoadInventory()
	if err != nil {
		return err
	}
	if inv.BootstrapConfig == nil {
		return fmt.Errorf("clustertool.yaml requires an explicit bootstrap configuration; use bootstrap: {crd: [], steps: []} to skip application steps")
	}
	bootstrapNode := inv.Bootstrap().Address
	applyCommands := GenApply("", extraArgs)
	for _, command := range applyCommands {
		if command.Err != nil {
			return command.Err
		}
	}
	applyNode := func(address string) error {
		for _, command := range applyCommands {
			if command.Err != nil {
				return command.Err
			}
			if command.Node == address {
				return ExecCmds([]Command{command}, false)
			}
		}
		return fmt.Errorf("node %s missing from bootstrap command plan", address)
	}
	if err := beginBootstrap(); err != nil {
		return err
	}

	// Configure all nodes before bootstrapping the first control plane.
	if err := bootstrapTalosNodes(inv, applyNode); err != nil {
		return err
	}

	log.Info().Msgf("Bootstrap: waiting for control plane %v to come online...", bootstrapNode)
	if _, err := nodestatus.WaitForHealth(bootstrapNode, []string{"running"}); err != nil {
		return err
	}

	log.Info().Msgf("Bootstrap: retrieving kubeconfig through control plane %v", bootstrapNode)
	// Ensure kubeconfig is loaded

	if err := ExecCmd(bootstrapCommand(inv, "kubeconfig", "-f")); err != nil {
		return err
	}

	// Wait for the Kubernetes control plane before applying cluster resources.
	requiredPods := []string{
		"kube-controller-manager",
		"kube-scheduler",
		"kube-apiserver",
	}

	log.Info().Msgf("Bootstrap: Waiting for system Pods to be running for: %v", bootstrapNode)
	if err := kubectlcmds.CheckStatus(requiredPods, []string{}, 15); err != nil {
		log.Error().Err(err).Msgf("Error: %v\n", err)

		return fmt.Errorf("bootstrap failed: %w", err)
	}

	log.Info().Msg("Bootstrap: Starting Cluster configuration...")
	// Keep approving node certificates while the base cluster starts.
	stopCh := make(chan struct{})
	var stopOnce sync.Once
	stopApprover := func() { stopOnce.Do(func() { close(stopCh) }) }
	defer stopApprover()

	// Get Kubernetes clientset
	clientset, err := kubectlcmds.GetClientset()
	if err != nil {
		log.Info().Msgf("Error getting Kubernetes clientset: %v", err)
		return err
	}
	ctx := context.Background()

	helmRepoPath := filepath.Join(helper.KubernetesPath, "repositories", "helm")
	HelmRepos, err = helmhandler.LoadAllHelmRepos(helmRepoPath)

	// Added by Boemeltrein, for linting purposes
	if err != nil {
		log.Error().Err(err).Msg("Failed to load Helm repositories")
		return err
	}

	// Call ApprovePendingCertificates with clientset and stopCh
	go kubectlcmds.ApprovePendingCertificates(clientset, stopCh)

	// Read HelmReleases once for both CRD extraction and later chart installation.
	index, err := loadHelmReleaseIndex(helper.KubernetesAppsPath)
	if err != nil {
		return err
	}

	// Apply the configured CRDs in order before installing Cilium.
	for i, name := range inv.BootstrapConfig.CRD {
		chart, err := index.lookup(name)
		if err != nil {
			return fmt.Errorf("bootstrap.crd entry %d, %s: %w", i+1, name, err)
		}
		log.Info().Msgf("Bootstrap: Applying CRDs from %s", name)
		if err := helmhandler.ApplyChartCRDs(ctx, chart, HelmRepos, inv.KubernetesVersion); err != nil {
			return fmt.Errorf("bootstrap.crd entry %d, %s: %w", i+1, name, err)
		}
	}

	// Install the fixed base charts before waiting for all nodes to become ready.
	baseCharts := []helmhandler.HelmChart{
		// Pulled directly from upstream, due to this being very complex and important
		{ChartPath: filepath.Join(helper.KubernetesAppsPath, "kube-system/cilium/app"), Wait: true},
		{ChartPath: filepath.Join(helper.KubernetesAppsPath, "kube-system/kubelet-csr-approver/app"), Wait: true},
	}

	if err := helmhandler.InstallCharts(baseCharts, HelmRepos, true); err != nil {
		return err
	}
	// Full node readiness is checked only after the CNI is installed.
	{
		for _, node := range inv.Nodes {
			if node.Address == bootstrapNode {
				continue
			}
			log.Info().Msgf("Bootstrap: waiting for joining node %s (%s)", node.Name, node.Address)
			if _, err := nodestatus.WaitForHealth(node.Address, nil); err != nil {
				return fmt.Errorf("wait for joining node %s: %w", node.Name, err)
			}
		}
	}

	log.Info().Msg("Bootstrap: Creating Namespaces...")

	var namespaceFilePaths []string

	// Walk through the directory recursively and find all namespace.yaml files
	err = filepath.WalkDir(helper.KubernetesAppsPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) == "namespace.yaml" {
			namespaceFilePaths = append(namespaceFilePaths, path)
		}
		return nil
	})

	if err != nil {
		log.Info().Msgf("Error walking the path: %v\n", err)
		return err
	}

	for _, filePath := range namespaceFilePaths {
		log.Info().Msgf("Bootstrap: Loading namespace: %v", filePath)
		if err := kubectlcmds.KubectlApply(ctx, filePath); err != nil {
			log.Info().Msgf("Error applying manifest for %s: %v\n", filepath.Base(filePath), err)
			return fmt.Errorf("bootstrap failed: %w", err)
		}
	}

	log.Info().Msg("Bootstrap: Base Cluster Configuration Completed, continuing setup...")
	// Confirm base cluster health before running application steps.
	log.Info().Msg("Bootstrap: Confirming cluster health...")
	healthcmd := GenPlain("health", bootstrapNode, []string{})
	if err := ExecCmd(healthcmd[0]); err != nil {
		return err
	}
	stopApprover()

	// Run the configured manifests and charts in order, using the same release index.
	if err := RunBootstrapSteps(ctx, inv.BootstrapConfig.Steps, index, HelmRepos); err != nil {
		return err
	}

	log.Info().Msg("Bootstrap: Completed Successfully!")
	for _, step := range inv.BootstrapConfig.Steps {
		if step.Chart == "flux-instance" {
			log.Info().Msg("Flux configuration installed. Reconciliation continues in the background.")
			log.Info().Msg("Ensure the public key from ssh-public-key.txt is added to your GitHub account under Settings > SSH and GPG keys.")
			log.Info().Msg("Check progress with: kubectl get fluxinstance,gitrepository,kustomization -n flux-system")
			break
		}
	}
	return os.Remove(bootstrapStatePath())
}

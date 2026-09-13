package gencmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/trueforge-org/clustertool/pkg/fluxhandler"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"github.com/trueforge-org/clustertool/pkg/kubectlcmds"
	"github.com/trueforge-org/clustertool/pkg/nodestatus"
	"github.com/trueforge-org/clustertool/pkg/sops"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
)

var HelmRepos map[string]*fluxhandler.HelmRepo

func RunBootstrap(args []string) error {
	extraArgs := args
	manifestPaths := []string{
		filepath.Join(helper.KubernetesPath, "flux-system", "flux", "sopssecret.secret.yaml"),
		filepath.Join(helper.KubernetesPath, "flux-system", "flux", "deploykey.secret.yaml"),
		filepath.Join(helper.KubernetesPath, "flux-system", "flux", "clustersettings.secret.yaml"),
	}

	if err := sops.DecryptFiles(); err != nil {
		return err
	}

	inv, err := talosconfig.LoadInventory()
	if err != nil {
		return err
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

	if _, err := ExistingControlPlane(inv); err != nil {
		stage, err := checkStatus(bootstrapNode)
		if err != nil {
			return err
		}
		if stage == "maintenance" {
			if err := applyNode(bootstrapNode); err != nil {
				return err
			}
		} else if stage != "booting" && stage != "running" {
			return fmt.Errorf("bootstrap node is in unexpected stage %s", stage)
		}
		if _, err := nodestatus.WaitForHealth(bootstrapNode, []string{"booting", "running"}); err != nil {
			return err
		}
		log.Info().Msgf("Bootstrap: waiting for installation/bootstrap availability on %s; inspect the disk selector and installation logs if it remains unavailable", bootstrapNode)
		// Check again after installation: a prior interrupted RPC may have succeeded.
		if err := bootstrapIfNeeded(inv); err != nil {
			return err
		}
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

	// Desired pod names
	requiredPods := []string{
		"kube-controller-manager",
		"kube-scheduler",
		"kube-apiserver",
	}

	log.Info().Msgf("Bootstrap: Waiting for system Pods to be running for: %v", bootstrapNode)
	if err := kubectlcmds.CheckStatus(requiredPods, []string{}, 600); err != nil {
		log.Error().Err(err).Msgf("Error: %v\n", err)

		return fmt.Errorf("bootstrap failed: %w", err)
	}

	log.Info().Msg("Bootstrap: Starting Cluster configuration...")
	// Start process to approve any cert requests till our manifests are loaded
	// Set up a signal handler to handle termination gracefully
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

	helmRepoPath := filepath.Join("./repositories", "helm")
	HelmRepos, err = fluxhandler.LoadAllHelmRepos(helmRepoPath)

	// Added by Boemeltrein, for linting purposes
	if err != nil {
		log.Error().Err(err).Msg("Failed to load Helm repositories")
		return err
	}

	// Call ApprovePendingCertificates with clientset and stopCh
	go kubectlcmds.ApprovePendingCertificates(clientset, stopCh)

	baseCharts := []fluxhandler.HelmChart{
		// Pulled directly from upstream, due to this being very complex and important
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/kube-system/cilium/app"), Retry: false, Wait: true},
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/kube-system/kubelet-csr-approver/app"), Retry: false, Wait: true},
	}

	if err := fluxhandler.InstallCharts(baseCharts, HelmRepos, true); err != nil {
		return err
	}
	// Bootstrap is performed once. All remaining inventory nodes join the
	// established cluster with their own generated configuration afterwards.
	{
		for _, node := range inv.Nodes {
			if node.Address == bootstrapNode {
				continue
			}
			log.Info().Msgf("Bootstrap: applying configuration to joining node %s (%s)", node.Name, node.Address)
			stage, err := checkStatus(node.Address)
			if err != nil {
				return fmt.Errorf("inspect joining node %s: %w", node.Name, err)
			}
			if stage == "maintenance" {
				if err := applyNode(node.Address); err != nil {
					return fmt.Errorf("apply joining node %s: %w", node.Name, err)
				}
			} else if stage != "running" && stage != "booting" {
				return fmt.Errorf("joining node %s is in unexpected stage %s", node.Name, stage)
			}
			if _, err := nodestatus.WaitForHealth(node.Address, nil); err != nil {
				return fmt.Errorf("wait for joining node %s: %w", node.Name, err)
			}
		}
	}

	log.Info().Msg("Bootstrap: Creating Namespaces...")

	var namespaceFilePaths []string
	var VSCfilePaths []string

	// Walk through the directory recursively and find all namespace.yaml files
	err = filepath.WalkDir(helper.ClusterPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) == "namespace.yaml" {
			namespaceFilePaths = append(namespaceFilePaths, path)
		}
		if filepath.Base(path) == "volumeSnapshotClass.yaml" {
			VSCfilePaths = append(VSCfilePaths, path)
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

	for _, filePath := range manifestPaths {
		log.Info().Msgf("Bootstrap: Loading Manifest: %v", filePath)
		if err := kubectlcmds.KubectlApply(ctx, filePath); err != nil {
			log.Info().Msgf("Error applying manifest for %s: %v\n", filepath.Base(filePath), err)
			return fmt.Errorf("bootstrap failed: %w", err)
		}
	}

	log.Info().Msg("Bootstrap: Base Cluster Configuration Completed, continuing setup...")
	log.Info().Msg("Bootstrap: Confirming cluster health...")
	healthcmd := GenPlain("health", bootstrapNode, []string{})
	if err := ExecCmd(healthcmd[0]); err != nil {
		return err
	}
	stopApprover()

	prioCharts := []fluxhandler.HelmChart{
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/observability/kube-prometheus-stack/app"), Retry: false, Wait: false},
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/system/cert-manager/app"), Retry: false, Wait: false},
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/system/kubernetes-reflector/app"), Retry: false, Wait: false},
	}
	if err := fluxhandler.InstallCharts(prioCharts, HelmRepos, false); err != nil {
		return err
	}

	intermediateCharts := []fluxhandler.HelmChart{
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/system/metallb/app"), Retry: false, Wait: false},
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/core/clusterissuer/app"), Retry: false, Wait: false},
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/system/cloudnative-pg/app"), Retry: false, Wait: false},
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/kube-system/node-feature-discovery/app"), Retry: false, Wait: false},
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/kube-system/metrics-server/app"), Retry: false, Wait: false},
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/system/volsync/app"), Retry: false, Wait: true},
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/system/snapshot-controller/app"), Retry: false, Wait: true},
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/system/openebs/app"), Retry: false, Wait: true},
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/system/longhorn/app"), Retry: false, Wait: true},
	}

	if err := fluxhandler.InstallCharts(intermediateCharts, HelmRepos, true); err != nil {
		return err
	}

	// Desired pod names
	requiredMLBPods := []string{
		"metallb-controller",
		"metallb-speaker",
	}

	log.Info().Msgf("Bootstrap: Waiting for MetalLB Pods to be running for: %v", bootstrapNode)
	if err := kubectlcmds.CheckStatus(requiredMLBPods, []string{}, 600); err != nil {
		log.Error().Err(err).Msgf("Error: %v\n", err)

		return fmt.Errorf("bootstrap failed: %w", err)
	}

	lateCharts := []fluxhandler.HelmChart{
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/core/metallb-config/app"), Retry: false, Wait: false},
	}

	log.Info().Msgf("Bootstrap: Loading VolumeSnapshotClasses")

	for _, filePath := range VSCfilePaths {
		log.Info().Msgf("Bootstrap: Loading VolumeSnapshotClass: %v", filePath)
		if err := kubectlcmds.KubectlApply(ctx, filePath); err != nil {
			log.Info().Msgf("Error applying manifest for %s: %v\n", filepath.Base(filePath), err)
			return fmt.Errorf("bootstrap failed: %w", err)
		}
	}

	if err := fluxhandler.InstallCharts(lateCharts, HelmRepos, true); err != nil {
		return err
	}

	log.Info().Msg("Bootstrap: Installing included applications")
	postCharts := []fluxhandler.HelmChart{
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/networking/nginx-internal/app"), Retry: false, Wait: true},
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/networking/nginx-external/app"), Retry: false, Wait: true},
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/core/blocky/app"), Retry: false, Wait: true},
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/observability/headlamp/app"), Retry: false, Wait: true},
	}

	if err := fluxhandler.InstallCharts(postCharts, HelmRepos, true); err != nil {
		return err
	}

	log.Info().Msg("------")

	if err := fluxhandler.FluxBootstrap(ctx); err != nil {
		return err
	}

	log.Info().Msg("Bootstrap: Completed Successfully!")
	return os.Remove(bootstrapStatePath())
}

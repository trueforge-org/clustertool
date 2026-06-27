package gencmd

import (
	"context"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"

	"github.com/trueforge-org/clustertool/pkg/fluxhandler"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"github.com/trueforge-org/clustertool/pkg/kubectlcmds"
	"github.com/trueforge-org/clustertool/pkg/nodestatus"
	"github.com/trueforge-org/clustertool/pkg/sops"
	"github.com/trueforge-org/clustertool/pkg/talassist"
)

var HelmRepos map[string]*fluxhandler.HelmRepo

var manifestPaths = []string{
	filepath.Join(helper.KubernetesPath, "flux-system", "flux", "sopssecret.secret.yaml"),
	filepath.Join(helper.KubernetesPath, "flux-system", "flux", "deploykey.secret.yaml"),
	filepath.Join(helper.KubernetesPath, "flux-system", "flux", "clustersettings.secret.yaml"),
}

func RunBootstrap(args []string) {
	var extraArgs []string
	if len(args) > 1 {
		extraArgs = args[1:]
	}

	if err := sops.DecryptFiles(); err != nil {
		log.Info().Msgf("Error decrypting files: %v\n", err)
	}

	bootstrapNode := talassist.TalConfig.Nodes[0].IPAddress

	nodestatus.WaitForHealth(bootstrapNode, []string{"maintenance"})

	taloscmds := GenApply(bootstrapNode, extraArgs)

	ExecCmds(taloscmds, false)

	nodestatus.WaitForHealth(bootstrapNode, []string{"booting"})

	log.Info().Msgf("Bootstrap: At this point your system is installed to disk, please make sure not to reboot into the installer ISO/USB  %s", bootstrapNode)

	log.Info().Msgf("Bootstrap: running bootstrap on node:  %s", bootstrapNode)
	bootstrapcmds := GenPlain("bootstrap", bootstrapNode, extraArgs)

	ExecCmd(bootstrapcmds[0])

	log.Info().Msgf("Bootstrap: waiting for VIP %v to come online...", helper.TalEnv["VIP_IP"])
	nodestatus.WaitForHealth(helper.TalEnv["VIP_IP"], []string{"running"})

	log.Info().Msgf("Bootstrap: Configuring kubeconfig/kubectl for VIP: %v", helper.TalEnv["VIP_IP"])
	// Ensure kubeconfig is loaded

	kubeconfigcmds := GenPlain("kubeconfig", helper.TalEnv["VIP_IP"], []string{"-f"})
	ExecCmd(kubeconfigcmds[0])

	// Desired pod names
	requiredPods := []string{
		"kube-controller-manager",
		"kube-scheduler",
		"kube-apiserver",
	}

	log.Info().Msgf("Bootstrap: Waiting for system Pods to be running for: %v", helper.TalEnv["VIP_IP"])
	if err := kubectlcmds.CheckStatus(requiredPods, []string{}, 600); err != nil {
		log.Error().Err(err).Msgf("Error: %v\n", err)

		os.Exit(1)
	}

	log.Info().Msg("Bootstrap: Starting Cluster configuration...")
	// Start process to approve any cert requests till our manifests are loaded
	// Set up a signal handler to handle termination gracefully
	stopCh := make(chan struct{})

	// Get Kubernetes clientset
	clientset, err := kubectlcmds.GetClientset()
	if err != nil {
		log.Info().Msgf("Error getting Kubernetes clientset: %v", err)
		return
	}
	ctx := context.Background()

	helmRepoPath := filepath.Join("./repositories", "helm")
	HelmRepos, err = fluxhandler.LoadAllHelmRepos(helmRepoPath)

	// Added by Boemeltrein, for linting purposes
	if err != nil {
		log.Error().Err(err).Msg("Failed to load Helm repositories")
		return
	}

	// Call ApprovePendingCertificates with clientset and stopCh
	go kubectlcmds.ApprovePendingCertificates(clientset, stopCh)

	baseCharts := []fluxhandler.HelmChart{
		// Pulled directly from upstream, due to this being very complex and important
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/kube-system/cilium/app"), Retry: false, Wait: true},
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/kube-system/kubelet-csr-approver/app"), Retry: false, Wait: true},
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/observability/kube-prometheus-stack/app"), Retry: false, Wait: false},
	}

	fluxhandler.InstallCharts(baseCharts, HelmRepos, true)

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
		return
	}

	for _, filePath := range namespaceFilePaths {
		log.Info().Msgf("Bootstrap: Loading namespace: %v", filePath)
		if err := kubectlcmds.KubectlApply(ctx, filePath); err != nil {
			log.Info().Msgf("Error applying manifest for %s: %v\n", filepath.Base(filePath), err)
			os.Exit(1)
		}
	}

	for _, filePath := range manifestPaths {
		log.Info().Msgf("Bootstrap: Loading Manifest: %v", filePath)
		if err := kubectlcmds.KubectlApply(ctx, filePath); err != nil {
			log.Info().Msgf("Error applying manifest for %s: %v\n", filepath.Base(filePath), err)
			os.Exit(1)
		}
	}

	log.Info().Msg("Bootstrap: Base Cluster Configuration Completed, continuing setup...")
	log.Info().Msg("Bootstrap: Confirming cluster health...")
	healthcmd := GenPlain("health", helper.TalEnv["VIP_IP"], []string{})
	ExecCmd(healthcmd[0])
	close(stopCh)

	prioCharts := []fluxhandler.HelmChart{
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/system/cert-manager/app"), Retry: false, Wait: false},
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/system/kubernetes-reflector/app"), Retry: false, Wait: false},
	}
	fluxhandler.InstallCharts(prioCharts, HelmRepos, false)

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

	fluxhandler.InstallCharts(intermediateCharts, HelmRepos, true)

	// Desired pod names
	requiredMLBPods := []string{
		"metallb-controller",
		"metallb-speaker",
	}

	log.Info().Msgf("Bootstrap: Waiting for MetalLB Pods to be running for: %v", helper.TalEnv["VIP_IP"])
	if err := kubectlcmds.CheckStatus(requiredMLBPods, []string{}, 600); err != nil {
		log.Error().Err(err).Msgf("Error: %v\n", err)

		os.Exit(1)
	}

	lateCharts := []fluxhandler.HelmChart{
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/core/metallb-config/app"), Retry: false, Wait: false},
	}

	log.Info().Msgf("Bootstrap: Loading VolumeSnapshotClasses")

	for _, filePath := range VSCfilePaths {
		log.Info().Msgf("Bootstrap: Loading VolumeSnapshotClass: %v", filePath)
		if err := kubectlcmds.KubectlApply(ctx, filePath); err != nil {
			log.Info().Msgf("Error applying manifest for %s: %v\n", filepath.Base(filePath), err)
			os.Exit(1)
		}
	}

	fluxhandler.InstallCharts(lateCharts, HelmRepos, true)

	log.Info().Msg("Bootstrap: Installing included applications")
	postCharts := []fluxhandler.HelmChart{
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/networking/nginx-internal/app"), Retry: false, Wait: true},
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/networking/nginx-external/app"), Retry: false, Wait: true},
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/core/blocky/app"), Retry: false, Wait: true},
		{ChartPath: filepath.Join(helper.ClusterPath, "/kubernetes/observability/headlamp/app"), Retry: false, Wait: true},
	}

	fluxhandler.InstallCharts(postCharts, HelmRepos, true)

	log.Info().Msg("------")

	fluxhandler.FluxBootstrap(ctx)

	log.Info().Msg("Bootstrap: Completed Successfully!")
}

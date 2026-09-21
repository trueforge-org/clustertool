package fluxhandler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"github.com/trueforge-org/clustertool/pkg/kubectlcmds"
	fthelper "github.com/trueforge-org/forgetool/v4/pkg/helper"
)

func init() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()
}

// FluxBootstrap is shared by Talos bootstrap and the standalone Flux command.
func FluxBootstrap(ctx context.Context) error {
	if helper.TalEnv["GITHUB_REPOSITORY"] == "" {
		return nil
	}
	log.Info().Msg("GITHUB_Repository for Flux configured.")
	if !fthelper.GetYesOrNo("Do you want to bootstrap Flux? [y/n]: ", false) {
		return nil
	}
	if err := bootstrapFluxCD(ctx); err != nil {
		return fmt.Errorf("bootstrap Flux: %w", err)
	}
	log.Info().Msg("Flux configuration installed. Reconciliation continues in the background.")
	log.Info().Msg("Ensure the public key from ssh-public-key.txt is added to your GitHub account under Settings > SSH and GPG keys.")
	log.Info().Msg("Check progress with: kubectl get fluxinstance,gitrepository,kustomization -n flux-system")
	return nil
}

func bootstrapFluxCD(ctx context.Context) error {
	isRepo, err := helper.IsCurrentDirGitRepo()
	if err != nil {
		return fmt.Errorf("check Git repository: %w", err)
	}
	if !isRepo {
		return fmt.Errorf("current directory is not a Git repository")
	}
	repos, err := LoadAllHelmRepos(filepath.Join("repositories", "helm"))
	if err != nil {
		return fmt.Errorf("load Helm repositories: %w", err)
	}

	fluxPath := filepath.Join(helper.ClusterPath, "kubernetes", "flux-system")
	manifestPaths := []string{
		filepath.Join(fluxPath, "namespace.yaml"),
		filepath.Join(fluxPath, "flux-instance", "app", "deploy-key.sops.yaml"),
		filepath.Join(fluxPath, "flux-instance", "app", "sops-age.sops.yaml"),
		helper.ClusterSettingsFile,
	}
	for _, filePath := range manifestPaths {
		log.Info().Msgf("Bootstrap: Loading Manifest: %s", filePath)
		if err := kubectlcmds.KubectlApply(ctx, filePath); err != nil {
			return fmt.Errorf("apply Flux manifest %s: %w", filePath, err)
		}
	}

	log.Info().Msg("Bootstrap: Installing Flux charts")
	fluxCharts := []HelmChart{
		{ChartPath: filepath.Join(fluxPath, "flux-operator", "app"), Retry: false, Wait: true},
		{ChartPath: filepath.Join(fluxPath, "flux-instance", "app"), Retry: false, Wait: false},
	}
	return InstallCharts(fluxCharts, repos, false)
}

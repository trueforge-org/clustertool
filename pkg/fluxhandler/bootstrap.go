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
	// Configure zerolog to output to stdout with a timestamp and log level
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()
}

// FluxBootstrap initializes the FluxCD bootstrapping process if GITHUB_REPOSITORY is set in TalEnv.
func FluxBootstrap(ctx context.Context) error {
	if helper.TalEnv["GITHUB_REPOSITORY"] == "" {
		return nil
	}
	log.Info().Msg("GITHUB_Repository for Flux configured.")
	if !fthelper.GetYesOrNo("Do you want to (re)bootstrap FluxCD as well? (yes/no) [y/n]: ", false) {
		return nil
	}
	if err := bootstrapFluxCD(ctx); err != nil {
		return fmt.Errorf("bootstrap FluxCD: %w", err)
	}
	log.Info().Msg("FluxCD Bootstrapped successfully")
	return nil
}

// bootstrapFluxCD handles the entire FluxCD bootstrapping process.
func bootstrapFluxCD(ctx context.Context) error {
	if err := checkGitRepo(); err != nil {
		return err
	}

	fluxPath := filepath.Join(helper.ClusterPath, "kubernetes", "flux-system", "flux")
	if err := setupFluxCD(ctx, fluxPath); err != nil {
		return err
	}

	reposFilePath := "repositories"
	if err := setupRepositories(ctx, reposFilePath); err != nil {
		return err
	}

	clusterEntryFile := filepath.Join(helper.ClusterPath, "kubernetes", "flux-entry.yaml")
	if err := kubectlcmds.KubectlApply(ctx, clusterEntryFile); err != nil {
		return fmt.Errorf("apply cluster Flux entry %s: %w", clusterEntryFile, err)
	}

	return nil
}

// checkGitRepo verifies if the current directory is a valid Git repository.
func checkGitRepo() error {
	isRepo, err := helper.IsCurrentDirGitRepo()
	if err != nil {
		return fmt.Errorf("check Git repository: %w", err)
	}
	if !isRepo {
		errMsg := "current directory is not a Git repository"
		return fmt.Errorf("%s", errMsg)
	}
	log.Info().Msg("Bootstrap: The current directory is a valid GIT repository, continuing...")
	return nil
}

// setupFluxCD handles the setup of FluxCD manifests.
func setupFluxCD(ctx context.Context, fluxPath string) error {
	bootstrapFile := "bootstrap.yaml.ct"
	kustomFile := "kustomization.yaml"
	tmpFile := "placeholder"

	log.Info().Msg("Bootstrap: Loading fluxcd onto the cluster...")

	// Rename files for kustomize application
	if err := os.Rename(filepath.Join(fluxPath, kustomFile), filepath.Join(fluxPath, tmpFile)); err != nil {
		return fmt.Errorf("save Flux kustomization: %w", err)
	}
	if err := os.Rename(filepath.Join(fluxPath, bootstrapFile), filepath.Join(fluxPath, kustomFile)); err != nil {
		return fmt.Errorf("activate Flux bootstrap manifest: %w", err)
	}

	if err := kubectlcmds.KubectlApplyKustomize(ctx, fluxPath); err != nil {
		log.Error().Err(err).Str("path", fluxPath).Msg("Error applying FluxCD manifest")
		log.Debug().Msg("Reverting renamed files for fluxbootstrap")
		if err := os.Rename(filepath.Join(fluxPath, kustomFile), filepath.Join(fluxPath, bootstrapFile)); err != nil {
			return fmt.Errorf("restore Flux bootstrap manifest: %w", err)
		}
		if err := os.Rename(filepath.Join(fluxPath, tmpFile), filepath.Join(fluxPath, kustomFile)); err != nil {
			return fmt.Errorf("restore Flux kustomization: %w", err)
		}
		return fmt.Errorf("apply Flux manifests %s: %w", fluxPath, err)
	}

	// Revert file renames
	if err := os.Rename(filepath.Join(fluxPath, kustomFile), filepath.Join(fluxPath, bootstrapFile)); err != nil {
		return fmt.Errorf("restore Flux bootstrap manifest: %w", err)
	}
	if err := os.Rename(filepath.Join(fluxPath, tmpFile), filepath.Join(fluxPath, kustomFile)); err != nil {
		return fmt.Errorf("restore Flux kustomization: %w", err)
	}

	return nil
}

// setupRepositories handles the setup of repository manifests.
func setupRepositories(ctx context.Context, reposFilePath string) error {
	log.Info().Msg("Bootstrap: Loading git-repo manifests onto the cluster...")

	gitRepoFile := filepath.Join(reposFilePath, "git", "this-repo.yaml")
	if err := kubectlcmds.KubectlApply(ctx, gitRepoFile); err != nil {
		return fmt.Errorf("apply repositories manifest %s: %w", gitRepoFile, err)
	}

	log.Info().Msg("Bootstrap: Loading repositories flux-entry onto the cluster...")
	reposEntryFile := filepath.Join(reposFilePath, "flux-entry.yaml")
	if err := kubectlcmds.KubectlApply(ctx, reposEntryFile); err != nil {
		return fmt.Errorf("apply repositories Flux entry %s: %w", reposEntryFile, err)
	}

	return nil
}

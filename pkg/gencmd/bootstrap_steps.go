package gencmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/trueforge-org/clustertool/pkg/clusterconfig"
	"github.com/trueforge-org/clustertool/pkg/helmhandler"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"github.com/trueforge-org/clustertool/pkg/kubectlcmds"
)

// Keep parsed releases for this bootstrap run; duplicate names are resolved only when selected.
type helmReleaseIndex map[string][]helmhandler.HelmChart

func loadHelmReleaseIndex(root string) (helmReleaseIndex, error) {
	index := make(helmReleaseIndex)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "helm-release.yaml" {
			return nil
		}
		release, err := helmhandler.LoadHelmRelease(path)
		if err != nil {
			return fmt.Errorf("load HelmRelease %s: %w", path, err)
		}
		index[release.Metadata.Name] = append(index[release.Metadata.Name], helmhandler.HelmChart{
			ChartPath: filepath.Dir(path), Release: release, Wait: true, WaitForJobs: true,
		})
		return nil
	})
	return index, err
}

func (index helmReleaseIndex) lookup(name string) (helmhandler.HelmChart, error) {
	matches := index[name]
	if len(matches) == 0 {
		return helmhandler.HelmChart{}, fmt.Errorf("no helm-release.yaml with metadata.name %q", name)
	}
	if len(matches) > 1 {
		paths := make([]string, 0, len(matches))
		for _, match := range matches {
			paths = append(paths, filepath.Join(match.ChartPath, "helm-release.yaml"))
		}
		return helmhandler.HelmChart{}, fmt.Errorf("ambiguous HelmRelease metadata.name %q: %s", name, strings.Join(paths, ", "))
	}
	return matches[0], nil
}

// RunBootstrapSteps is called only during initial setup or an explicit bootstrap resume.
func RunBootstrapSteps(ctx context.Context, config clusterconfig.BootstrapConfig, repos map[string]*helmhandler.HelmRepo, kubeVersion string) error {
	index, err := loadHelmReleaseIndex(helper.KubernetesAppsPath)
	if err != nil {
		return err
	}
	return runBootstrapSteps(config, func(name string, wait bool) error {
		chart, err := index.lookup(name)
		if err != nil {
			return err
		}
		chart.Wait, chart.WaitForJobs = wait, wait
		return helmhandler.InstallCharts([]helmhandler.HelmChart{chart}, repos, false)
	}, func(name string) error {
		chart, err := index.lookup(name)
		if err != nil {
			return err
		}
		log.Info().Msgf("Bootstrap: Applying CRDs from %s", name)
		return helmhandler.ApplyChartCRDs(ctx, chart, repos, kubeVersion)
	}, func(path string) error {
		log.Info().Msgf("Bootstrap: Loading Manifest: %s", path)
		return kubectlcmds.KubectlApply(ctx, filepath.Join(helper.ClusterPath, filepath.FromSlash(path)))
	})
}

func runBootstrapSteps(config clusterconfig.BootstrapConfig, runChart func(string, bool) error, applyCRDs func(string) error, applyManifest func(string) error) error {
	for i, step := range config {
		switch {
		case step.Chart != "":
			if err := runChart(step.Chart, step.Wait); err != nil {
				return fmt.Errorf("bootstrap step %d, chart %s: %w", i+1, step.Chart, err)
			}
		case step.CRD != "":
			if err := applyCRDs(step.CRD); err != nil {
				return fmt.Errorf("bootstrap step %d, crd %s: %w", i+1, step.CRD, err)
			}
		case step.Manifest != "":
			if err := applyManifest(step.Manifest); err != nil {
				return fmt.Errorf("bootstrap step %d, manifest %s: %w", i+1, step.Manifest, err)
			}
		}
	}
	return nil
}

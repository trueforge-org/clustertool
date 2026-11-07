package helmhandler

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"sync"

	"github.com/rs/zerolog/log"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"gopkg.in/yaml.v3"
)

type HelmChart struct {
	ChartPath   string
	Retry       bool
	Wait        bool
	WaitForJobs bool
	Release     *HelmRelease
}

type SourceRef struct {
	Kind      string `yaml:"kind,omitempty"`
	Name      string `yaml:"name,omitempty"`
	Namespace string `yaml:"namespace,omitempty"`
}

type ChartSpec struct {
	Chart     string    `yaml:"chart,omitempty"`
	Version   string    `yaml:"version,omitempty"`
	SourceRef SourceRef `yaml:"sourceRef,omitempty"`
}

type Chart struct {
	Spec ChartSpec `yaml:"spec,omitempty"`
}

type Spec struct {
	ChartRef    *SourceRef             `yaml:"chartRef,omitempty"`
	Interval    string                 `yaml:"interval,omitempty"`
	Chart       Chart                  `yaml:"chart,omitempty"`
	ReleaseName string                 `yaml:"releaseName,omitempty"`
	Values      map[string]interface{} `yaml:"values,omitempty"`
}

type Metadata struct {
	Name      string `yaml:"name,omitempty"`
	Namespace string `yaml:"namespace,omitempty"`
}

type HelmRelease struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata,omitempty"`
	Spec       Spec     `yaml:"spec,omitempty"`
}

func LoadHelmRelease(filename string) (*HelmRelease, error) {
	// Read YAML file
	yamlFile, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Initialize HelmRelease struct
	config := &HelmRelease{}

	// Unmarshal YAML into struct
	err = yaml.Unmarshal(yamlFile, config)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal YAML: %w", err)
	}

	// Ensure Values is not nil
	if config.Spec.Values == nil {
		config.Spec.Values = make(map[string]interface{})
	}
	return config, nil
}

func (release *HelmRelease) releaseName() string {
	if release.Spec.ReleaseName != "" {
		return release.Spec.ReleaseName
	}
	return release.Metadata.Name
}

// chartSource is shared by chart installation and CRD rendering.
func chartSource(release *HelmRelease, repos map[string]*HelmRepo) (string, string, string, error) {
	repo := repos[release.Spec.Chart.Spec.SourceRef.Name]
	chartName, version := release.Spec.Chart.Spec.Chart, release.Spec.Chart.Spec.Version
	if ref := release.Spec.ChartRef; ref != nil {
		if ref.Kind != "OCIRepository" {
			return "", "", "", fmt.Errorf("unsupported chartRef kind %q", ref.Kind)
		}
		ociRepos, err := LoadAllHelmRepos(filepath.Join(helper.KubernetesPath, "repositories", "oci"))
		if err != nil {
			return "", "", "", fmt.Errorf("load OCI repositories: %w", err)
		}
		repo = ociRepos[ref.Name]
		namespace := ref.Namespace
		if namespace == "" {
			namespace = release.Metadata.Namespace
		}
		if repo == nil || repo.Kind != "OCIRepository" || repo.Metadata.Namespace != namespace {
			return "", "", "", fmt.Errorf("missing OCIRepository %s/%s", namespace, ref.Name)
		}
		if repo.Spec.Ref.Tag == "" || !strings.HasPrefix(repo.Spec.URL, "oci://") {
			return "", "", "", fmt.Errorf("OCIRepository %s requires an OCI URL and ref.tag", ref.Name)
		}
		chartName = filepath.Base(strings.TrimSuffix(repo.Spec.URL, "/"))
		version = repo.Spec.Ref.Tag
	}
	if repo == nil || repo.Spec.URL == "" {
		return "", "", "", fmt.Errorf("missing Helm repository for %s", release.Metadata.Name)
	}
	repoURL := repo.Spec.URL
	if release.Spec.ChartRef != nil {
		repoURL = strings.TrimSuffix(strings.TrimSuffix(repoURL, "/"), "/"+chartName)
	}
	return repoURL, chartName, version, nil
}

func InstallCharts(charts []HelmChart, repos map[string]*HelmRepo, async bool) error {
	install := func(chart HelmChart) error {
		release := chart.Release
		if release == nil {
			var err error
			release, err = LoadHelmRelease(filepath.Join(chart.ChartPath, "helm-release.yaml"))
			if err != nil {
				return fmt.Errorf("load chart %s: %w", chart.ChartPath, err)
			}
		}
		repoURL, chartName, version, err := chartSource(release, repos)
		if err != nil {
			return err
		}
		log.Info().Msgf("Bootstrap: Installing %s", release.Metadata.Name)
		if err := HelmInstall(repoURL, chartName, version, chart, release); err != nil {
			return fmt.Errorf("install chart %s: %w", release.releaseName(), err)
		}
		return nil
	}
	if !async {
		for _, chart := range charts {
			if err := install(chart); err != nil {
				return err
			}
		}
		return nil
	}
	failures := make(chan error, len(charts))
	var wg sync.WaitGroup
	for _, chart := range charts {
		wg.Add(1)
		go func(c HelmChart) {
			defer wg.Done()
			if err := install(c); err != nil {
				failures <- err
			}
		}(chart)
	}
	wg.Wait()
	close(failures)
	var combined error
	for err := range failures {
		combined = errors.Join(combined, err)
	}
	return combined
}

// UpgradeCharts upgrades Helm releases with provided Helm charts and repositories
func UpgradeCharts(charts []HelmChart, HelmRepos map[string]*HelmRepo, async bool) {
	var wg sync.WaitGroup
	for _, chart := range charts {
		wg.Add(1)
		go func(chart HelmChart) {
			defer wg.Done()

			// Determine paths
			valuesFile := filepath.Join(chart.ChartPath, "values.yaml")
			helmreleaseFile := filepath.Join(chart.ChartPath, "helm-release.yaml")

			// Load Helm release
			helmRelease, err := LoadHelmRelease(helmreleaseFile)
			if err != nil {
				log.Error().Err(err).Msgf("Error loading Helm release for chart at: %s\n", chart.ChartPath)

				if !async {
					os.Exit(1)
				}
				return
			}
			if helmRelease == nil {
				log.Error().Msgf("Empty Helm release for chart at %s\n", chart.ChartPath)

				if !async {
					os.Exit(1)
				}
				return
			}

			// Determine release name
			releaseName := helmRelease.Metadata.Name
			if helmRelease.Spec.ReleaseName != "" {
				releaseName = helmRelease.Spec.ReleaseName
			}

			// Determine chart name
			chartName := helmRelease.Spec.Chart.Spec.Chart

			// Validate Helm repository
			repoName := helmRelease.Spec.Chart.Spec.SourceRef.Name
			repo, ok := HelmRepos[repoName]
			if !ok || repo.Spec.URL == "" {
				log.Error().Msgf("Empty or invalid Helm repository for %s\n", repoName)

				if !async {
					os.Exit(1)
				}
				return
			}

			// Perform Helm upgrade
			log.Info().Msgf("Upgrading %s\n", helmRelease.Metadata.Name)
			err = HelmUpgrade(repo.Spec.URL, chartName, releaseName, helmRelease.Metadata.Namespace, valuesFile, helmRelease.Spec.Chart.Spec.Version, chart.Wait, true)
			if err != nil {
				log.Error().Err(err).Msgf("Error upgrading %s\n", helmRelease.Metadata.Name)

				if !async {
					os.Exit(1)
				}
				return
			}
		}(chart)

		if !async {
			wg.Wait()
		}
	}

	if async {
		wg.Wait()
	}
}

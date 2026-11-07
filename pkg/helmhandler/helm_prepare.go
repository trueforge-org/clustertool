package helmhandler

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/trueforge-org/clustertool/pkg/helper"
	fthelper "github.com/trueforge-org/forgetool/v4/pkg/helper"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/cli/values"
	"helm.sh/helm/v4/pkg/getter"
)

// prepareChart keeps CRD rendering and installation on the same chart and values.
func prepareChart(workDir, repoURL, chartName, version, chartDir string, helmRelease *HelmRelease) (*chartv2.Chart, map[string]interface{}, error) {
	releaseName := helmRelease.releaseName()
	valuesFile := filepath.Join(chartDir, "values.yaml")
	settings := cli.New()
	var chartPath string

	// Determine chart path based on chartName format
	if strings.HasPrefix(repoURL, "http://") || strings.HasPrefix(repoURL, "https://") || strings.HasPrefix(repoURL, "oci://") {
		// Handle HTTP or OCI URL
		err := HelmPull(repoURL, chartName, version, workDir, true)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to pull chart %s: %w", chartName, err)
		}
		chartPath = path.Join(workDir, fmt.Sprintf("%s-%s.tgz", chartName, version))

	} else {
		// Local chart path
		chartPath = repoURL
	}

	// Load the Helm chart using loader.Load
	chart, err := loader.Load(chartPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load chart: %w", err)
	}

	tempValuesName := releaseName + "tempvalues.yaml"
	tempValuesPath := path.Join(workDir, tempValuesName)
	// Create values.yaml from chart.Values
	err = createValuesYAML(chart.Values, tempValuesPath)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating tempvalues.yaml: %w", err)
	}
	valueFiles := []string{tempValuesPath}

	// Get the directory part of the path
	directory := filepath.Dir(valuesFile)

	tempHRValuesName := releaseName + "temphrvalues.yaml"
	tempHRValuesPath := path.Join(workDir, tempHRValuesName)
	err = createValuesYAML(helmRelease.Spec.Values, tempHRValuesPath)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating temphrvalues.yaml: %w", err)
	}
	if _, err := fthelper.EnvSubst(tempHRValuesPath, helper.TalEnv); err != nil {
		return nil, nil, fmt.Errorf("render values for %s: %w", releaseName, err)
	}
	valueFiles = append(valueFiles, tempHRValuesPath)

	if _, err := os.Stat(valuesFile); err == nil {
		valueFiles = append(valueFiles, valuesFile)

	}

	overrideValuesPath := path.Join(directory, "bootstrap-values.yaml.ct")

	if _, err := os.Stat(overrideValuesPath); err == nil {
		valueFiles = append(valueFiles, overrideValuesPath)
	}

	// Prepare values for installation
	valOpts := &values.Options{
		ValueFiles: valueFiles, // Specify value file to merge
	}

	// Create getter to fetch values from file
	valProviders := getter.All(settings)

	// Merge values with chart's default values
	vals, err := valOpts.MergeValues(valProviders)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to merge values: %w", err)
	}

	return chart, vals, nil
}

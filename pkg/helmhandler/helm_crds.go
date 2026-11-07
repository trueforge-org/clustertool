package helmhandler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/trueforge-org/clustertool/pkg/helper"
	"github.com/trueforge-org/clustertool/pkg/kubectlcmds"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/common"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	releasev1 "helm.sh/helm/v4/pkg/release/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

// ApplyChartCRDs prepares a chart and applies only its rendered CRDs server-side.
func ApplyChartCRDs(ctx context.Context, chart HelmChart, repos map[string]*HelmRepo, kubeVersion string) error {
	repoURL, chartName, version, err := chartSource(chart.Release, repos)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(helper.HelmCache, 0755); err != nil {
		return fmt.Errorf("create Helm cache: %w", err)
	}
	workDir, err := os.MkdirTemp(helper.HelmCache, chart.Release.releaseName()+"-crds-")
	if err != nil {
		return fmt.Errorf("create temporary Helm directory: %w", err)
	}
	defer os.RemoveAll(workDir)

	prepared, vals, err := prepareChart(workDir, repoURL, chartName, version, chart.ChartPath, chart.Release)
	if err != nil {
		return err
	}
	data, err := renderChartCRDs(prepared, chart.Release, vals, kubeVersion)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		log.Info().Msgf("Bootstrap: No CRDs rendered for %s", chart.Release.Metadata.Name)
		return nil
	}
	path := filepath.Join(workDir, "crds.yaml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	return kubectlcmds.KubectlApply(ctx, path)
}

// Render like helm template --include-crds --no-hooks, then retain only CRDs.
func renderChartCRDs(chart *chartv2.Chart, release *HelmRelease, vals map[string]interface{}, kubeVersion string) ([]byte, error) {
	config := new(action.Configuration)
	config.SetLogger(zerolog.NewSlogHandler(log.Logger))
	client := action.NewInstall(config)
	client.ReleaseName = release.releaseName()
	client.Namespace = release.Metadata.Namespace
	client.DryRunStrategy = action.DryRunClient
	client.IncludeCRDs = true
	client.DisableHooks = true
	version, err := common.ParseKubeVersion(kubeVersion)
	if err != nil {
		return nil, fmt.Errorf("parse Kubernetes version for CRD rendering: %w", err)
	}
	client.KubeVersion = version
	result, err := client.Run(chart, vals)
	if err != nil {
		return nil, fmt.Errorf("render CRDs for %s: %w", release.Metadata.Name, err)
	}

	// Helm returns hooks separately from the manifest, so they are never applied here.
	decoder := kubeyaml.NewYAMLOrJSONDecoder(strings.NewReader(result.(*releasev1.Release).Manifest), 4096)
	var output bytes.Buffer
	for {
		var object unstructured.Unstructured
		if err := decoder.Decode(&object); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode CRDs for %s: %w", release.Metadata.Name, err)
		}
		if object.GetKind() != "CustomResourceDefinition" {
			continue
		}
		// Templated CRDs must be adoptable by the same Helm release later.
		labels := object.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels["app.kubernetes.io/managed-by"] = "Helm"
		object.SetLabels(labels)
		annotations := object.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations["meta.helm.sh/release-name"] = release.releaseName()
		annotations["meta.helm.sh/release-namespace"] = release.Metadata.Namespace
		object.SetAnnotations(annotations)
		data, err := yaml.Marshal(object.Object)
		if err != nil {
			return nil, err
		}
		output.WriteString("---\n")
		output.Write(data)
	}
	return output.Bytes(), nil
}

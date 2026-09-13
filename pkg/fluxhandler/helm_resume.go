package fluxhandler

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
)

// Resume does not change an existing release. A deployed Helm status alone
// does not prove its workloads are ready, so retain the caller's wait policy.
func resumeHelmRelease(config *action.Configuration, name string, wait bool) (bool, error) {
	existing, err := action.NewGet(config).Run(name)
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect release %s: %w", name, err)
	}
	if existing.Info == nil || existing.Info.Status != release.StatusDeployed {
		return true, fmt.Errorf("release %s already exists but is not deployed; resolve its Helm status before resuming bootstrap", name)
	}
	if wait {
		resources, err := config.KubeClient.Build(strings.NewReader(existing.Manifest), false)
		if err != nil {
			return true, fmt.Errorf("read resources for release %s: %w", name, err)
		}
		if err := config.KubeClient.Wait(resources, 15*time.Minute); err != nil {
			return true, fmt.Errorf("wait for existing release %s: %w", name, err)
		}
	}
	log.Info().Msgf("Bootstrap: release %s is already deployed; keeping it", name)
	return true, nil
}

func installRelease(client *action.Install, chart *chart.Chart, values map[string]interface{}) (*release.Release, error) {
	result, err := client.Run(chart, values)
	if err != nil {
		// Helm can retain a failed/pending release after a timeout. Never issue
		// a second install which hides the original error behind a name conflict.
		return result, fmt.Errorf("install release %s: %w; inspect its Helm status before retrying bootstrap", client.ReleaseName, err)
	}
	return result, nil
}

package helmhandler

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"helm.sh/helm/v4/pkg/action"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/kube"
	ri "helm.sh/helm/v4/pkg/release"
	releasecommon "helm.sh/helm/v4/pkg/release/common"
	release "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage/driver"
)

// Resume does not change an existing release. A deployed Helm status alone
// does not prove its workloads are ready, so retain the caller's wait policy.
func resumeHelmRelease(config *action.Configuration, name string, wait, waitForJobs bool) (bool, error) {
	result, err := action.NewGet(config).Run(name)
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect release %s: %w", name, err)
	}
	existing := result.(*release.Release)
	if existing.Info == nil || existing.Info.Status != releasecommon.StatusDeployed {
		return true, fmt.Errorf("release %s already exists but is not deployed; resolve its Helm status before resuming bootstrap", name)
	}
	if wait {
		resources, err := config.KubeClient.Build(strings.NewReader(existing.Manifest), false)
		if err != nil {
			return true, fmt.Errorf("read resources for release %s: %w", name, err)
		}
		waiter, err := config.KubeClient.GetWaiter(kube.StatusWatcherStrategy)
		if err != nil {
			return true, fmt.Errorf("wait for existing release %s: %w", name, err)
		}
		if waitForJobs {
			err = waiter.WaitWithJobs(resources, 15*time.Minute)
		} else {
			err = waiter.Wait(resources, 15*time.Minute)
		}
		if err != nil {
			return true, fmt.Errorf("wait for existing release %s: %w", name, err)
		}
	}
	log.Info().Msgf("Bootstrap: release %s is already deployed; keeping it", name)
	return true, nil
}

func installRelease(client *action.Install, chart *chart.Chart, values map[string]interface{}) (ri.Releaser, error) {
	result, err := client.Run(chart, values)
	if err != nil {
		// Helm can retain a failed/pending release after a timeout. Never issue
		// a second install which hides the original error behind a name conflict.
		return result, fmt.Errorf("install release %s: %w; inspect its Helm status before retrying bootstrap", client.ReleaseName, err)
	}
	return result, nil
}

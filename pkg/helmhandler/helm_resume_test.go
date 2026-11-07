package helmhandler

import (
	"errors"
	"io"
	"testing"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/common"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/kube/fake"
	releasecommon "helm.sh/helm/v4/pkg/release/common"
	release "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage"
	"helm.sh/helm/v4/pkg/storage/driver"
)

type readinessClient struct {
	fake.PrintingKubeClient
	fake.PrintingKubeWaiter
	waits    int
	jobWaits int
	creates  int
	err      error
	strategy kube.WaitStrategy
}

func (c *readinessClient) Wait(_ kube.ResourceList, _ time.Duration) error { c.waits++; return c.err }
func (c *readinessClient) WaitWithJobs(_ kube.ResourceList, _ time.Duration) error {
	c.jobWaits++
	return c.err
}
func (c *readinessClient) GetWaiter(ws kube.WaitStrategy) (kube.Waiter, error) {
	return c.GetWaiterWithOptions(ws)
}
func (c *readinessClient) GetWaiterWithOptions(strategy kube.WaitStrategy, _ ...kube.WaitOption) (kube.Waiter, error) {
	c.strategy = strategy
	return c, nil
}
func (c *readinessClient) Create(r kube.ResourceList, opts ...kube.ClientCreateOption) (*kube.Result, error) {
	c.creates++
	return c.PrintingKubeClient.Create(r, opts...)
}
func helmTestConfig() (*action.Configuration, *readinessClient) {
	client := &readinessClient{PrintingKubeClient: fake.PrintingKubeClient{Out: io.Discard}, PrintingKubeWaiter: fake.PrintingKubeWaiter{Out: io.Discard}}
	return &action.Configuration{Releases: storage.Init(driver.NewMemory()), KubeClient: client, Capabilities: common.DefaultCapabilities}, client
}

func TestResumeChecksReadinessWithoutInstalling(t *testing.T) {
	cfg, client := helmTestConfig()
	rel := &release.Release{Name: "storage", Namespace: "test", Version: 1, Info: &release.Info{Status: releasecommon.StatusDeployed}, Manifest: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: example\n"}
	if err := cfg.Releases.Create(rel); err != nil {
		t.Fatal(err)
	}
	for _, wait := range []bool{false, true} {
		found, err := resumeHelmRelease(cfg, "storage", wait, false)
		if !found || err != nil {
			t.Fatal(found, err)
		}
	}
	if client.waits != 1 || client.creates != 0 {
		t.Fatal("resume ignored wait policy or installed resources")
	}
	if client.strategy != kube.StatusWatcherStrategy {
		t.Fatalf("resume used wait strategy %q", client.strategy)
	}
	if found, err := resumeHelmRelease(cfg, "storage", true, true); !found || err != nil || client.jobWaits != 1 {
		t.Fatalf("resume did not wait for Jobs: found=%v waits=%d err=%v", found, client.jobWaits, err)
	}
	client.err = errors.New("readiness timed out")
	if _, err := resumeHelmRelease(cfg, "storage", true, true); !errors.Is(err, client.err) {
		t.Fatal(err)
	}
	rel.Info.Status = releasecommon.StatusFailed
	if err := cfg.Releases.Update(rel); err != nil {
		t.Fatal(err)
	}
	if _, err := resumeHelmRelease(cfg, "storage", true, true); err == nil {
		t.Fatal("accepted failed release")
	}
	if found, err := resumeHelmRelease(cfg, "absent", true, true); found || err != nil {
		t.Fatal(found, err)
	}
}

func TestInstallTimeoutPreservesOriginalErrorAndRelease(t *testing.T) {
	cfg, client := helmTestConfig()
	client.err = errors.New("timed out waiting for readiness")
	install := action.NewInstall(cfg)
	install.ReleaseName = "storage"
	install.Namespace = "test"
	install.WaitStrategy = kube.StatusWatcherStrategy
	install.ServerSideApply = true
	install.Timeout = time.Second
	ch := &chart.Chart{Metadata: &chart.Metadata{Name: "storage", Version: "1.0.0", APIVersion: "v2"}, Templates: []*common.File{{Name: "templates/cm.yaml", Data: []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: example\n")}}}
	_, err := installRelease(install, ch, nil)
	if !errors.Is(err, client.err) {
		t.Fatalf("lost original timeout: %v", err)
	}
	if client.creates > 1 || client.waits != 1 {
		t.Fatalf("install retried: creates=%d waits=%d", client.creates, client.waits)
	}
	if client.strategy != kube.StatusWatcherStrategy {
		t.Fatalf("install used wait strategy %q", client.strategy)
	}
	result, err := cfg.Releases.Get("storage", 1)
	if err != nil || result.(*release.Release).Info.Status != releasecommon.StatusFailed {
		t.Fatalf("failed release not retained: %v", err)
	}
}

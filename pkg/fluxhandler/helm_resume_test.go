package fluxhandler

import (
	"errors"
	"io"
	"testing"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
)

type readinessClient struct {
	fake.PrintingKubeClient
	waits   int
	creates int
	err     error
}

func (c *readinessClient) Wait(_ kube.ResourceList, _ time.Duration) error { c.waits++; return c.err }
func (c *readinessClient) Create(r kube.ResourceList) (*kube.Result, error) {
	c.creates++
	return c.PrintingKubeClient.Create(r)
}
func helmTestConfig() (*action.Configuration, *readinessClient) {
	client := &readinessClient{PrintingKubeClient: fake.PrintingKubeClient{Out: io.Discard}}
	return &action.Configuration{Releases: storage.Init(driver.NewMemory()), KubeClient: client, Capabilities: chartutil.DefaultCapabilities, Log: func(string, ...interface{}) {}}, client
}

func TestResumeChecksReadinessWithoutInstalling(t *testing.T) {
	cfg, client := helmTestConfig()
	rel := &release.Release{Name: "storage", Namespace: "test", Version: 1, Info: &release.Info{Status: release.StatusDeployed}, Manifest: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: example\n"}
	if err := cfg.Releases.Create(rel); err != nil {
		t.Fatal(err)
	}
	for _, wait := range []bool{false, true} {
		found, err := resumeHelmRelease(cfg, "storage", wait)
		if !found || err != nil {
			t.Fatal(found, err)
		}
	}
	if client.waits != 1 || client.creates != 0 {
		t.Fatal("resume ignored wait policy or installed resources")
	}
	client.err = errors.New("readiness timed out")
	if _, err := resumeHelmRelease(cfg, "storage", true); !errors.Is(err, client.err) {
		t.Fatal(err)
	}
	rel.Info.Status = release.StatusFailed
	if err := cfg.Releases.Update(rel); err != nil {
		t.Fatal(err)
	}
	if _, err := resumeHelmRelease(cfg, "storage", true); err == nil {
		t.Fatal("accepted failed release")
	}
	if found, err := resumeHelmRelease(cfg, "absent", true); found || err != nil {
		t.Fatal(found, err)
	}
}

func TestInstallTimeoutPreservesOriginalErrorAndRelease(t *testing.T) {
	cfg, client := helmTestConfig()
	client.err = errors.New("timed out waiting for readiness")
	install := action.NewInstall(cfg)
	install.ReleaseName = "storage"
	install.Namespace = "test"
	install.Wait = true
	install.Timeout = time.Second
	ch := &chart.Chart{Metadata: &chart.Metadata{Name: "storage", Version: "1.0.0", APIVersion: "v2"}, Templates: []*chart.File{{Name: "templates/cm.yaml", Data: []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: example\n")}}}
	_, err := installRelease(install, ch, nil)
	if !errors.Is(err, client.err) {
		t.Fatalf("lost original timeout: %v", err)
	}
	if client.creates > 1 || client.waits != 1 {
		t.Fatalf("install retried: creates=%d waits=%d", client.creates, client.waits)
	}
	rel, err := cfg.Releases.Get("storage", 1)
	if err != nil || rel.Info.Status != release.StatusFailed {
		t.Fatalf("failed release not retained: %v", err)
	}
}

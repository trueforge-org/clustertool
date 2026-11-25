package cmd

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appembed "github.com/trueforge-org/clustertool/embed"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

func TestInitSSHTrustAndRetry(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		accepted    bool
	}{
		{"accept", "yes\n", true}, {"decline", "n\n", false},
		{"empty", "\n", false}, {"EOF", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, cache := t.TempDir(), t.TempDir()
			templates, err := fs.Sub(appembed.GenericFiles, "generic")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.CopyFS(filepath.Join(cache, "clustertool"), templates); err != nil {
				t.Fatal(err)
			}
			runInit := func(input string) ([]byte, error) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				process := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCommandErrorProcess$")
				process.Dir = dir
				process.Env = append(os.Environ(), `CLUSTERTOOL_TEST_ARGS=["init"]`, "LOCALAPPDATA="+cache, "XDG_CACHE_HOME="+cache)
				process.Stdin = strings.NewReader(input)
				return process.CombinedOutput()
			}
			if out, err := runInit(""); err != nil {
				t.Fatalf("first init: %v %s", err, out)
			}
			publicPath := filepath.Join(dir, "ssh-public-key.txt")
			secretPath := filepath.Join(dir, "kubernetes", "apps", "flux-system", "flux-instance", "app", "deploy-key.sops.yaml")
			fluxPath := filepath.Join(filepath.Dir(secretPath), "helm-release.yaml")
			for _, path := range []string{publicPath, secretPath} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("first init created a deploy key")
				}
			}
			// Reuse a dummy Talos identity to keep this test independent of talosctl.
			if err := os.WriteFile(filepath.Join(dir, "talos", "secrets.sops.yaml"), []byte("fixture: true\n"), 0600); err != nil {
				t.Fatal(err)
			}
			_, private, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			signer, err := ssh.NewSignerFromKey(private)
			if err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				connection, err := listener.Accept()
				if err != nil {
					return
				}
				defer connection.Close()
				_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
				config := &ssh.ServerConfig{NoClientAuth: true}
				config.AddHostKey(signer)
				server, _, _, err := ssh.NewServerConn(connection, config)
				if err == nil {
					_ = server.Close()
				}
			}()
			t.Cleanup(func() { _ = listener.Close(); <-done })
			gitURL := "ssh://git@" + listener.Addr().String() + "/group/subgroup/repo.git"
			settingsPath := filepath.Join(dir, "kubernetes", "config", "cluster-secrets.sops.yaml")
			settings, err := os.ReadFile(settingsPath)
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]interface{}
			if err := yaml.Unmarshal(settings, &document); err != nil {
				t.Fatal(err)
			}
			values := document["stringData"].(map[string]interface{})
			for key, value := range map[string]string{
				"GIT_REPOSITORY": gitURL, "DOMAIN_0": "example.test",
				"DOMAIN_0_EMAIL": "operator@example.test", "DOMAIN_0_CLOUDFLARE_TOKEN": "dummy-token",
			} {
				values[key] = value
			}
			writeSettings := func() {
				t.Helper()
				data, err := yaml.Marshal(document)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(settingsPath, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			writeSettings()
			out, err := runInit(tc.input)
			if (err == nil) != tc.accepted {
				t.Fatalf("second init: %v %s", err, out)
			}
			if !strings.Contains(string(out), ssh.FingerprintSHA256(signer.PublicKey())) {
				t.Fatalf("missing fingerprint: %s", out)
			}
			if !tc.accepted {
				if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
					t.Fatalf("wrong error exit: %v", err)
				}
				if _, err := os.Stat(filepath.Join(dir, "RUNAGAIN")); err != nil {
					t.Fatal("retry marker removed")
				}
				for _, path := range []string{publicPath, secretPath} {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatalf("key written before trust: %s", path)
					}
				}
				flux, err := os.ReadFile(fluxPath)
				if err != nil || !strings.Contains(string(flux), "REPLACEWITHGITREPO") {
					t.Fatal("Flux URL rendered before trust")
				}
				gitURL = "ssh://git@gitlab.com/corrected/group/repo.git"
				values["GIT_REPOSITORY"] = gitURL
				writeSettings()
				if out, err := runInit(""); err != nil {
					t.Fatalf("retry with corrected repository: %v %s", err, out)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "RUNAGAIN")); !os.IsNotExist(err) {
				t.Fatal("successful init retained retry marker")
			}
			flux, err := os.ReadFile(fluxPath)
			if err != nil || !strings.Contains(string(flux), gitURL) {
				t.Fatal("Flux URL does not match the accepted repository")
			}
			for _, path := range []string{publicPath, secretPath} {
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("missing deploy key: %v", err)
				}
			}
		})
	}
}

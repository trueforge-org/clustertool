package fluxhandler

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trueforge-org/clustertool/pkg/helper"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"gopkg.in/yaml.v3"
)

func TestOfficialGitHostKeys(t *testing.T) {
	// Fingerprints published by GitHub and GitLab independently of the key data.
	for _, tc := range []struct {
		host         string
		fingerprints map[string]string
	}{
		{"github.com", map[string]string{
			"ssh-ed25519":         "SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU",
			"ecdsa-sha2-nistp256": "SHA256:p2QAMXNIC1TJYWeIOttrVc98/R1BUFWu3/LiyKgUfQM",
		}},
		{"gitlab.com", map[string]string{
			"ssh-ed25519":         "SHA256:eUXGGm1YGsMAS7vkcx6JOJdOGHPem5gQp4taiCfCLB8",
			"ecdsa-sha2-nistp256": "SHA256:HbW3g8zUjNSksFbqTiUWPWg2Bq1x8xdGUrliXFzSnUw",
		}},
	} {
		for _, port := range []string{"22", "2222"} {
			t.Run(tc.host+":"+port, func(t *testing.T) {
				address := net.JoinHostPort(tc.host, port)
				entries, err := getKnownHostsEntry("ssh://git@" + address + "/group/repo.git")
				if err != nil {
					t.Fatal(err)
				}
				lines := strings.Split(entries, "\n")
				if len(lines) != len(tc.fingerprints) {
					t.Fatalf("unexpected host keys: %d", len(lines))
				}
				for _, line := range lines {
					_, hosts, key, _, _, err := ssh.ParseKnownHosts([]byte(line))
					if err != nil {
						t.Fatal(err)
					}
					if len(hosts) != 1 || hosts[0] != knownhosts.Normalize(address) {
						t.Fatalf("wrong host/port: %v", hosts)
					}
					if got := ssh.FingerprintSHA256(key); got != tc.fingerprints[key.Type()] {
						t.Fatalf("%s fingerprint = %s", key.Type(), got)
					}
				}
			})
		}
	}
}

func newSSHTestServer(t *testing.T, signers ...ssh.Signer) (string, ssh.PublicKey) {
	t.Helper()
	if len(signers) == 0 {
		_, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		signer, err := ssh.NewSignerFromKey(private)
		if err != nil {
			t.Fatal(err)
		}
		signers = []ssh.Signer{signer}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{NoClientAuth: true}
	for _, signer := range signers {
		config.AddHostKey(signer)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
			server, _, _, err := ssh.NewServerConn(connection, config)
			if err == nil {
				_ = server.Close()
			}
			_ = connection.Close()
		}
	}()
	t.Cleanup(func() { _ = listener.Close(); <-done })
	return listener.Addr().String(), signers[0].PublicKey()
}

func TestSSHHostKeyAlgorithms(t *testing.T) {
	newSigner := func(private any, err error) ssh.Signer {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		signer, err := ssh.NewSignerFromKey(private)
		if err != nil {
			t.Fatal(err)
		}
		return signer
	}
	restrictRSA := func(signer ssh.Signer, algorithm string) ssh.Signer {
		t.Helper()
		restricted, err := ssh.NewSignerWithAlgorithms(signer.(ssh.AlgorithmSigner), []string{algorithm})
		if err != nil {
			t.Fatal(err)
		}
		return restricted
	}
	_, privateEd25519, err := ed25519.GenerateKey(rand.Reader)
	ed25519Key := newSigner(privateEd25519, err)
	ecdsa256 := newSigner(ecdsa.GenerateKey(elliptic.P256(), rand.Reader))
	ecdsa384 := newSigner(ecdsa.GenerateKey(elliptic.P384(), rand.Reader))
	ecdsa521 := newSigner(ecdsa.GenerateKey(elliptic.P521(), rand.Reader))
	rsa2048 := newSigner(rsa.GenerateKey(rand.Reader, 2048))
	rsa1024 := newSigner(rsa.GenerateKey(rand.Reader, 1024))

	for _, tc := range []struct {
		name    string
		signers []ssh.Signer
		want    ssh.Signer
		wantErr string
	}{
		{"Ed25519 preferred", []ssh.Signer{rsa2048, ecdsa256, ecdsa384, ecdsa521, ed25519Key}, ed25519Key, ""},
		{"ECDSA P256 before RSA", []ssh.Signer{rsa2048, ecdsa256}, ecdsa256, ""},
		{"ECDSA P384 before RSA", []ssh.Signer{rsa2048, ecdsa384}, ecdsa384, ""},
		{"ECDSA P521 before RSA", []ssh.Signer{rsa2048, ecdsa521}, ecdsa521, ""},
		{"RSA SHA256", []ssh.Signer{restrictRSA(rsa2048, ssh.KeyAlgoRSASHA256)}, rsa2048, ""},
		{"RSA SHA512", []ssh.Signer{restrictRSA(rsa2048, ssh.KeyAlgoRSASHA512)}, rsa2048, ""},
		{"RSA SHA1 rejected", []ssh.Signer{restrictRSA(rsa2048, ssh.KeyAlgoRSA)}, nil, "no allowed SSH host key algorithm"},
		{"RSA 1024 SHA256 rejected", []ssh.Signer{restrictRSA(rsa1024, ssh.KeyAlgoRSASHA256)}, nil, "at least 2048 bits"},
		{"RSA 1024 SHA512 rejected", []ssh.Signer{restrictRSA(rsa1024, ssh.KeyAlgoRSASHA512)}, nil, "at least 2048 bits"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			address, _ := newSSHTestServer(t, tc.signers...)
			key, err := scanSSHHostKey(address, "git", 5*time.Second)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(key.Marshal(), tc.want.PublicKey().Marshal()) {
					t.Fatalf("selected %s, want %s", key.Type(), tc.want.PublicKey().Type())
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) || key != nil {
				t.Fatalf("expected no key and error containing %q, got key %v and error %v", tc.wantErr, key, err)
			}

			// A weak server must fail before prompting, even if the user would answer yes.
			t.Chdir(t.TempDir())
			oldApps, oldIn, oldOut := helper.KubernetesAppsPath, os.Stdin, os.Stdout
			t.Cleanup(func() { helper.KubernetesAppsPath, os.Stdin, os.Stdout = oldApps, oldIn, oldOut })
			helper.KubernetesAppsPath = filepath.Join("kubernetes", "apps")
			if err := os.WriteFile("input", []byte("yes\n"), 0600); err != nil {
				t.Fatal(err)
			}
			input, err := os.Open("input")
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			output, err := os.Create("output")
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			os.Stdin, os.Stdout = input, output
			if err := CreateGitSecret("ssh://git@" + address + "/group/repo.git"); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("weak server was not rejected before secret creation: %v", err)
			}
			prompt, err := os.ReadFile("output")
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(prompt), "Do you trust this SSH host?") {
				t.Fatal("weak server requested trust")
			}
			for _, path := range []string{"ssh-public-key.txt", filepath.Join(helper.KubernetesAppsPath, "flux-system", "flux-instance", "app", "deploy-key.sops.yaml")} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("key file created for weak server: %s", path)
				}
			}
		})
	}
}

func TestCreateGitSecretSSHTrust(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		accepted    bool
	}{
		{"yes", "yes\n", true}, {"y", "Y\r\n", true},
		{"invalid then yes", "invalid\ny\n", true},
		{"no", "no\n", false}, {"n", "n\n", false},
		{"empty", "\n", false}, {"EOF", "", false}, {"unterminated", "yes", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			oldApps, oldIn, oldOut := helper.KubernetesAppsPath, os.Stdin, os.Stdout
			t.Cleanup(func() { helper.KubernetesAppsPath, os.Stdin, os.Stdout = oldApps, oldIn, oldOut })
			helper.KubernetesAppsPath = filepath.Join("kubernetes", "apps")
			if err := os.WriteFile("input", []byte(tc.input), 0600); err != nil {
				t.Fatal(err)
			}
			input, err := os.Open("input")
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			output, err := os.Create("output")
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			os.Stdin, os.Stdout = input, output
			address, key := newSSHTestServer(t)
			// A provider name in the path must never select that provider's host keys.
			err = CreateGitSecret("ssh://git@" + address + "/github.com/group/repo.git")
			if (err == nil) != tc.accepted {
				t.Fatalf("acceptance = %v, error = %v", tc.accepted, err)
			}
			prompt, err := os.ReadFile("output")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(prompt), ssh.FingerprintSHA256(key)) || !strings.Contains(string(prompt), "[y/N]") {
				t.Fatalf("missing fingerprint or prompt: %s", prompt)
			}
			secretPath := filepath.Join(helper.KubernetesAppsPath, "flux-system", "flux-instance", "app", "deploy-key.sops.yaml")
			if !tc.accepted {
				for _, path := range []string{secretPath, "ssh-public-key.txt"} {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatalf("key file created without trust: %s", path)
					}
				}
				return
			}
			data, err := os.ReadFile(secretPath)
			if err != nil {
				t.Fatal(err)
			}
			var secret Config
			if err := yaml.Unmarshal(data, &secret); err != nil {
				t.Fatal(err)
			}
			if secret.StringData["known_hosts"] != knownhosts.Line([]string{address}, key) {
				t.Fatal("wrong trusted host/port/key")
			}
			private, err := ssh.ParsePrivateKey([]byte(secret.StringData["identity"]))
			if err != nil {
				t.Fatal(err)
			}
			public, err := os.ReadFile("ssh-public-key.txt")
			if err != nil {
				t.Fatal(err)
			}
			if string(public) != secret.StringData["identity.pub"] || string(public) != string(ssh.MarshalAuthorizedKey(private.PublicKey())) {
				t.Fatal("generated deploy key pair does not match")
			}
			if err := os.WriteFile("known_hosts", []byte(secret.StringData["known_hosts"]), 0600); err != nil {
				t.Fatal(err)
			}
			callback, err := knownhosts.New("known_hosts")
			if err != nil {
				t.Fatal(err)
			}
			remote, err := net.ResolveTCPAddr("tcp", address)
			if err != nil {
				t.Fatal(err)
			}
			if err := callback(address, remote, key); err != nil {
				t.Fatalf("stored key rejected: %v", err)
			}
			// Existing secrets must be preserved without a network request or another prompt.
			if err := CreateGitSecret("ssh://git@127.0.0.1:1/group/repo.git"); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(secretPath)
			if err != nil || !bytes.Equal(after, data) {
				t.Fatal("existing deploy secret changed")
			}
			afterPrompt, err := os.ReadFile("output")
			if err != nil || !bytes.Equal(afterPrompt, prompt) {
				t.Fatal("existing deploy secret requested trust again")
			}
		})
	}
}

func TestSSHHostKeyHandshakeTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		_, _ = io.Copy(io.Discard, connection)
	}()
	_, err = scanSSHHostKey(listener.Addr().String(), "git", 100*time.Millisecond)
	var networkError net.Error
	if !errors.As(err, &networkError) || !networkError.Timeout() {
		t.Fatalf("expected SSH timeout, got %v", err)
	}
	<-done
}

func TestSSHHostKeyConnectionRefused(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	if _, err := scanSSHHostKey(address, "git", time.Second); err == nil {
		t.Fatal("unavailable SSH endpoint accepted")
	}
}

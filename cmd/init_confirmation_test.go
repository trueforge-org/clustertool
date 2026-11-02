package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitBlocksLegacyBeforeDecryption(t *testing.T) {
	for _, legacy := range []string{"talos/talconfig.yaml", "clusterenv.yaml", "clusters/main/talos/talconfig.yaml", "clusters/main/clusterenv.yaml"} {
		for _, marker := range []bool{false, true} {
			t.Run(legacy+"/"+fmt.Sprint(marker), func(t *testing.T) {
				dir := t.TempDir()
				file := filepath.Join(dir, filepath.FromSlash(legacy))
				if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte("legacy configuration"), 0600); err != nil {
					t.Fatal(err)
				}
				if marker {
					if err := os.WriteFile(filepath.Join(dir, "RUNAGAIN"), nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(filepath.Join(dir, ".sops.yaml"), []byte("creation_rules: [\n"), 0600); err != nil {
					t.Fatal(err)
				}
				process := exec.Command(os.Args[0], "-test.run=^TestCommandErrorProcess$")
				process.Dir = dir
				process.Env = append(os.Environ(), `CLUSTERTOOL_TEST_ARGS=["init"]`)
				process.Stdin = strings.NewReader("y\n")
				out, err := process.CombinedOutput()
				if err == nil || !strings.Contains(string(out), "Legacy Talhelper-configured cluster found.") {
					t.Fatalf("legacy init was not blocked: %v %s", err, out)
				}
				for _, unwanted := range []string{"parse .sops.yaml", "Continue with init?", "talconfig.yaml", "clusterenv.yaml"} {
					if strings.Contains(string(out), unwanted) {
						t.Fatalf("unexpected output %q: %s", unwanted, out)
					}
				}
				data, err := os.ReadFile(file)
				if err != nil || string(data) != "legacy configuration" {
					t.Fatalf("legacy file changed: %v", err)
				}
			})
		}
	}
}

func TestInitConfirmationBeforeDecryption(t *testing.T) {
	for _, tc := range []struct {
		name, input                string
		marker, secrets, cancelled bool
	}{
		{"decline", "n\n", false, true, true},
		{"empty then decline", "\nn\n", false, true, true},
		{"invalid then accept", "invalid\ny\n", false, true, false},
		{"accept", "y\n", false, true, false},
		{"accept yes", "yes\n", false, true, false},
		{"decline no", "no\n", false, true, true},
		{"retry", "", true, true, false},
		{"first init", "", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, "talos"), 0755); err != nil {
				t.Fatal(err)
			}
			if tc.secrets {
				if err := os.WriteFile(filepath.Join(dir, "talos", "secrets.sops.yaml"), []byte("unchanged"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.marker {
				if err := os.WriteFile(filepath.Join(dir, "RUNAGAIN"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			// Invalid SOPS config reveals whether execution reached decryption.
			if err := os.WriteFile(filepath.Join(dir, ".sops.yaml"), []byte("creation_rules: [\n"), 0600); err != nil {
				t.Fatal(err)
			}
			process := exec.Command(os.Args[0], "-test.run=^TestCommandErrorProcess$")
			process.Dir = dir
			process.Env = append(os.Environ(), `CLUSTERTOOL_TEST_ARGS=["init"]`)
			process.Stdin = strings.NewReader(tc.input)
			out, err := process.CombinedOutput()
			if err == nil {
				t.Fatalf("expected cancellation or SOPS error: %s", out)
			}
			if strings.Contains(tc.input, "\n") && (strings.HasPrefix(tc.input, "\n") || strings.HasPrefix(tc.input, "invalid")) {
				if !strings.Contains(string(out), "Invalid input. Please enter yes/no or y/n.") || strings.Count(string(out), "Continue with init? [y/n]:") != 2 {
					t.Fatalf("expected another prompt after invalid input: %s", out)
				}
			}
			if strings.Contains(string(out), "init cancelled") != tc.cancelled {
				t.Fatalf("unexpected cancellation: %s", out)
			}
			if strings.Contains(string(out), "parse .sops.yaml") == tc.cancelled {
				t.Fatalf("unexpected decryption attempt: %s", out)
			}
			if strings.Contains(string(out), "WARNING:") != (tc.secrets && !tc.marker) {
				t.Fatalf("unexpected warning: %s", out)
			}
		})
	}
}

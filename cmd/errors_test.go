package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/trueforge-org/clustertool/pkg/sops"
)

// Exercise Cobra in a separate process: an accidental os.Exit(0) inside a
// helper must fail these tests instead of terminating the test runner.
func TestCommandErrorProcess(t *testing.T) {
	input := os.Getenv("CLUSTERTOOL_TEST_ARGS")
	if input == "" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		t.Fatal(err)
	}
	os.Args = append([]string{"clustertool"}, args...)
	RootCmd.SetArgs(args)
	log.Logger = zerolog.New(os.Stdout)
	if err := Execute(); err != nil {
		log.Fatal().Err(err).Msg("Failed to execute command")
	}
	os.Exit(0)
}

func commandProcess(t *testing.T, dir string, args ...string) ([]byte, error) {
	t.Helper()
	data, _ := json.Marshal(args)
	cmd := exec.Command(os.Args[0], "-test.run=^TestCommandErrorProcess$")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CLUSTERTOOL_TEST_ARGS="+string(data))
	return cmd.CombinedOutput()
}

func TestCommandsStopOnDecryptError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	key, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("age.agekey", []byte(key.String()), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".sops.yaml", []byte("creation_rules:\n  - path_regex: secrets.yaml\n    age: "+key.Recipient().String()+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	encrypted, err := sops.EncryptWithAgeKey([]byte("secret: sample\n"), "", "yaml")
	if err != nil {
		t.Fatal(err)
	}
	start := bytes.Index(encrypted, []byte("data:")) + len("data:")
	if start < len("data:") {
		t.Fatal("missing ciphertext")
	}
	encrypted[start] = '!'
	if err := os.WriteFile("secrets.yaml", encrypted, 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"decrypt"}, {"genconfig"}, {"flux", "bootstrap"}, {"talos", "apply"}, {"talos", "upgrade"}, {"talos", "reset"}, {"talos", "health"}, {"talos", "kubeconfig"}} {
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			out, err := commandProcess(t, dir, args...)
			if err == nil {
				t.Fatalf("returned success after decrypt error: %s", out)
			}
			if !bytes.Contains(out, []byte("secrets.yaml")) || bytes.Count(out, []byte("illegal base64")) != 1 {
				t.Fatalf("expected one error with file and cause: %s", out)
			}
			after, err := os.ReadFile("secrets.yaml")
			if err != nil || !bytes.Equal(encrypted, after) {
				t.Fatal("damaged ciphertext was replaced")
			}
		})
	}
}

func TestEncryptionCommandExitCodes(t *testing.T) {
	for _, name := range []string{"encrypt", "decrypt"} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ".sops.yaml"), []byte("creation_rules: []\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if out, err := commandProcess(t, dir, name); err != nil {
			t.Fatalf("%s success case: %v %s", name, err, out)
		}
		if err := os.WriteFile(filepath.Join(dir, ".sops.yaml"), []byte("creation_rules: [\n"), 0600); err != nil {
			t.Fatal(err)
		}
		out, err := commandProcess(t, dir, name)
		if err == nil || !bytes.Contains(out, []byte("parse .sops.yaml")) {
			t.Fatalf("%s error case: %v %s", name, err, out)
		}
	}
}

//go:build windows

package sops

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWindowsStagedSopsPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is required")
	}
	t.Chdir(t.TempDir())
	if out, err := exec.Command("git", "init").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if err := os.WriteFile(".sops.yaml", []byte("creation_rules:\n  - path_regex: '^clusters/main/secret.yaml$'\n    age: unused\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join("clusters", "main", "secret.yaml")
	config, err := LoadSopsConfig()
	if err != nil {
		t.Fatal(err)
	}
	config.CreationRules[0].EncryptedRegex = "password"
	if got := mergeRegex(p, config); got != "password" {
		t.Fatalf("Windows encryption rule mismatch: %q", got)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("password: dummy\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "add", "--", p).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	files, err := ExecuteCheck(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Encrypted || filepath.ToSlash(files[0].Path) != "clusters/main/secret.yaml" {
		t.Fatalf("staged secret was not selected: %#v", files)
	}
}

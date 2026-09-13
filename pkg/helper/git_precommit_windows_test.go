//go:build windows

package helper

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsGitInvokesPreCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git for Windows is required")
	}
	t.Chdir(t.TempDir())
	runGit := func(args ...string) ([]byte, error) { return exec.Command("git", args...).CombinedOutput() }
	if out, err := runGit("init"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	old := CacheDir
	t.Cleanup(func() { CacheDir = old })
	CacheDir = filepath.Join(t.TempDir(), "cache with spaces and 'quote")
	if err := os.MkdirAll(CacheDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := CreateEncrPreCommitHook(); err != nil {
		t.Fatal(err)
	}
	args := []string{"-c", "core.hooksPath=.git/hooks", "-c", "user.name=Hook Test", "-c", "user.email=hook@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "hook check"}
	out, err := runGit(args...)
	if err == nil || !strings.Contains(string(out), "Pre-commit executable not found") {
		t.Fatalf("missing checker must block commit: %v %s", err, out)
	}
	checker := filepath.Join(CacheDir, "precommit.exe")
	// Git's shell can execute a shebang script as a stand-in for the checker.
	if err := os.WriteFile(checker, []byte("#!/bin/sh\necho checker-invoked\nexit 7\n"), 0755); err != nil {
		t.Fatal(err)
	}
	out, err = runGit(args...)
	if err == nil || !strings.Contains(string(out), "checker-invoked") {
		t.Fatalf("checker failure must block commit: %v %s", err, out)
	}
	if err := os.WriteFile(checker, []byte("#!/bin/sh\necho checker-invoked\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	out, err = runGit(args...)
	if err != nil || !strings.Contains(string(out), "checker-invoked") {
		t.Fatalf("successful checker: %v %s", err, out)
	}
}

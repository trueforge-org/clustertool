package helper

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGitPreCommitLocalHook(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is required")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	for _, tt := range []struct {
		name           string
		localHook      bool
		checkerMissing bool
		checkerExit    int
		localExit      int
		notExecutable  bool
		wantFailure    bool
		wantOrder      string
	}{
		{name: "no local hook", wantOrder: "encryption\n"},
		{name: "local hook succeeds", localHook: true, wantOrder: "encryption\nlocal\n"},
		{name: "local hook fails", localHook: true, localExit: 9, wantFailure: true, wantOrder: "encryption\nlocal\n"},
		{name: "encryption fails", localHook: true, checkerExit: 7, wantFailure: true, wantOrder: "encryption\n"},
		{name: "checker missing", localHook: true, checkerMissing: true, wantFailure: true},
		{name: "local hook not executable", localHook: true, notExecutable: true, wantFailure: true, wantOrder: "encryption\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.notExecutable && runtime.GOOS == "windows" {
				t.Skip("Windows does not use Unix executable permission bits")
			}
			repo := filepath.Join(t.TempDir(), "repo with spaces and 'quote-café")
			if err := os.MkdirAll(repo, 0755); err != nil {
				t.Fatal(err)
			}
			t.Chdir(repo)
			runGit := func(args ...string) ([]byte, error) { return exec.Command("git", args...).CombinedOutput() }
			if out, err := runGit("init", "--template="); err != nil {
				t.Fatalf("git init: %v: %s", err, out)
			}
			if err := os.MkdirAll(filepath.Join(".git", "hooks"), 0755); err != nil {
				t.Fatal(err)
			}

			old := CacheDir
			t.Cleanup(func() { CacheDir = old })
			CacheDir = filepath.Join(repo, "checker cache")
			if err := os.MkdirAll(CacheDir, 0755); err != nil {
				t.Fatal(err)
			}
			checker := filepath.Join(CacheDir, "precommit")
			if runtime.GOOS == "windows" {
				checker += ".exe"
			}
			// Use a dummy checker to test hook ordering and failure propagation.
			if !tt.checkerMissing {
				content := fmt.Sprintf("#!/bin/sh\necho encryption >> .git/hook-order\nexit %d\n", tt.checkerExit)
				if err := os.WriteFile(checker, []byte(content), 0755); err != nil {
					t.Fatal(err)
				}
			}
			localPath := filepath.Join(".git", "hooks", "pre-commit.local")
			localContent := fmt.Sprintf("#!/bin/sh\necho local >> .git/hook-order\nexit %d\n", tt.localExit)
			if tt.localHook {
				mode := os.FileMode(0755)
				if tt.notExecutable {
					mode = 0644
				}
				if err := os.WriteFile(localPath, []byte(localContent), mode); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				if err := CreateEncrPreCommitHook(); err != nil {
					t.Fatal(err)
				}
			}
			content, err := os.ReadFile(localPath)
			if tt.localHook {
				if err != nil || string(content) != localContent {
					t.Fatalf("local hook must survive regeneration unchanged: %q, %v", content, err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("generation must not create a local hook: %v", err)
			}

			out, err := runGit("-c", "core.hooksPath=.git/hooks", "-c", "user.name=Hook Test", "-c", "user.email=hook@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "hook check")
			if (err != nil) != tt.wantFailure {
				t.Fatalf("unexpected commit result: %v: %s", err, out)
			}
			if _, err := runGit("rev-parse", "--verify", "HEAD"); (err != nil) != tt.wantFailure {
				t.Fatalf("unexpected commit existence: %v", err)
			}
			order, err := os.ReadFile(filepath.Join(".git", "hook-order"))
			if err != nil && !(tt.wantOrder == "" && os.IsNotExist(err)) {
				t.Fatal(err)
			}
			if string(order) != tt.wantOrder {
				t.Fatalf("hook execution order: got %q, want %q; output: %s", order, tt.wantOrder, out)
			}
		})
	}
}

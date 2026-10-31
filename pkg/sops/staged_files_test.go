package sops

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestExecuteCheckStagedPaths(t *testing.T) {
	initStagedFilesRepo(t)
	want := []string{
		"secrets/ascii.sops.yaml",
		"secrets/with space.sops.yaml",
		"secrets/testprivé.sops.yaml",
		"secrets/geheim-秘密.sops.yaml",
	}
	for _, path := range want {
		writeStagedFilesFixture(t, path)
		runStagedFilesGit(t, "add", "--", path)
	}
	writeStagedFilesFixture(t, "secrets/unstaged.sops.yaml")
	writeStagedFilesFixture(t, "secrets/unmatched.yaml")
	runStagedFilesGit(t, "add", "--", "secrets/unmatched.yaml")

	files, err := ExecuteCheck(true)
	if err != nil {
		t.Fatal(err)
	}
	assertStagedFilesPaths(t, files, want)
}

func TestExecuteCheckStagedSubdirectory(t *testing.T) {
	initStagedFilesRepo(t)
	config, err := os.ReadFile(".sops.yaml")
	if err != nil {
		t.Fatal(err)
	}
	writeStagedFilesFixture(t, "clustertool/secrets/testprivé.sops.yaml")
	if err := os.WriteFile(filepath.Join("clustertool", ".sops.yaml"), config, 0600); err != nil {
		t.Fatal(err)
	}
	runStagedFilesGit(t, "add", "--", "clustertool/secrets/testprivé.sops.yaml")
	t.Chdir("clustertool")

	files, err := ExecuteCheck(true)
	if err != nil {
		t.Fatal(err)
	}
	assertStagedFilesPaths(t, files, []string{"secrets/testprivé.sops.yaml"})
}

func TestExecuteCheckMissingStagedFile(t *testing.T) {
	initStagedFilesRepo(t)
	available := "secrets/available.sops.yaml"
	writeStagedFilesFixture(t, available)
	runStagedFilesGit(t, "add", "--", available)
	if err := os.WriteFile(filepath.FromSlash(available), []byte("password: unstaged-dummy\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := "secrets/missing.sops.yaml"
	writeStagedFilesFixture(t, path)
	runStagedFilesGit(t, "add", "--", path)
	before := runStagedFilesGit(t, "ls-files", "--stage", "-z")
	if err := os.Remove(filepath.FromSlash(path)); err != nil {
		t.Fatal(err)
	}

	_, err := ExecuteCheck(true)
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(filepath.ToSlash(err.Error()), path) {
		t.Fatalf("expected a missing working-tree file error naming %s, got %v", path, err)
	}
	if after := runStagedFilesGit(t, "ls-files", "--stage", "-z"); !bytes.Equal(before, after) {
		t.Fatalf("missing file changed the index: before %q, after %q", before, after)
	}
}

func TestExecuteCheckStagedDeletion(t *testing.T) {
	initStagedFilesRepo(t)
	path := "secrets/deleted.sops.yaml"
	writeStagedFilesFixture(t, path)
	runStagedFilesGit(t, "add", "--", path)
	runStagedFilesGit(t, "-c", "user.name=SOPS test", "-c", "user.email=sops@example.invalid", "commit", "-qm", "fixture")
	runStagedFilesGit(t, "rm", "--", path)
	before := runStagedFilesGit(t, "diff", "--cached", "--name-status", "-z")

	files, err := ExecuteCheck(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("staged deletion was selected: %#v", files)
	}
	if after := runStagedFilesGit(t, "diff", "--cached", "--name-status", "-z"); !bytes.Equal(before, after) {
		t.Fatalf("staged deletion changed: before %q, after %q", before, after)
	}
}

func TestExecuteCheckWorkingTreePaths(t *testing.T) {
	initStagedFilesRepo(t)
	want := []string{"secrets/unstaged.sops.yaml", "secrets/nested/geheim-秘密.sops.yaml"}
	for _, path := range want {
		writeStagedFilesFixture(t, path)
	}
	writeStagedFilesFixture(t, "secrets/unmatched.yaml")

	files, err := ExecuteCheck(false)
	if err != nil {
		t.Fatal(err)
	}
	assertStagedFilesPaths(t, files, want)
	if staged := runStagedFilesGit(t, "ls-files", "--stage", "-z"); len(staged) != 0 {
		t.Fatalf("working-tree check staged files: %q", staged)
	}
}

func initStagedFilesRepo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is required")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	// Git for Windows marks its object files read-only; make fixture cleanup portable.
	t.Cleanup(func() {
		if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				return os.Chmod(path, 0600)
			}
			return nil
		}); err != nil {
			t.Errorf("make Git fixture removable: %v", err)
		}
	})
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	runStagedFilesGit(t, "init", "--quiet")
	// Keep Git's default quoting behavior even when the user's Git config differs.
	runStagedFilesGit(t, "config", "core.quotePath", "true")
	config := "creation_rules:\n  - path_regex: '^secrets/.*\\.sops\\.yaml$'\n    age: unused\n"
	if err := os.WriteFile(".sops.yaml", []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
}

func runStagedFilesGit(t *testing.T, args ...string) []byte {
	t.Helper()
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

func writeStagedFilesFixture(t *testing.T, path string) {
	t.Helper()
	path = filepath.FromSlash(path)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("password: dummy\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func assertStagedFilesPaths(t *testing.T, files []EncrFileData, want []string) {
	t.Helper()
	var got []string
	for _, file := range files {
		got = append(got, filepath.ToSlash(file.Path))
		if file.Encrypted {
			t.Errorf("plaintext fixture reported encrypted: %s", file.Path)
		}
	}
	slices.Sort(got)
	want = slices.Clone(want)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("selected paths = %q, want %q", got, want)
	}
}

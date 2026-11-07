package helper

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/rs/zerolog/log"
)

var (
	hookGetwdFn                = os.Getwd
	hookStatFn                 = os.Stat
	hookCreateFn               = os.Create
	hookWriteStringFn          = func(file *os.File, content string) (int, error) { return file.WriteString(content) }
	hookChmodFn                = os.Chmod
	hookGOOS                   = runtime.GOOS
	buildPreCommitHookScriptFn = buildPreCommitHookScript
)

// IsCurrentDirGitRepo checks if the current directory is a Git repository.
func IsCurrentDirGitRepo() (bool, error) {
	// Get the current working directory
	dir, err := hookGetwdFn()
	if err != nil {
		return false, err
	}

	// Construct the path to the .git directory
	gitDir := filepath.Join(dir, ".git")

	// Check if the .git directory exists and is a directory
	info, err := hookStatFn(gitDir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

// CreateEncrPreCommitHook creates a pre-commit hook script in the .git/hooks directory
func CreateEncrPreCommitHook() error {
	shouldContinue, err := validateCurrentRepoForHook()
	if err != nil {
		return err
	}
	if !shouldContinue {
		return nil
	}

	dir, err := hookGetwdFn()
	if err != nil {
		return fmt.Errorf("could not get current working directory: %v", err)
	}

	hookPath, hookScript, err := buildHookFileData(dir)
	if err != nil {
		return err
	}

	if err := writeHookScript(hookPath, hookScript); err != nil {
		return err
	}

	if hookGOOS != "windows" {
		err = hookChmodFn(hookPath, 0755)
		if err != nil {
			return fmt.Errorf("could not make pre-commit hook executable: %v", err)
		}
	}

	log.Info().Msg("Pre-commit hook created successfully.")
	return nil
}

func buildHookFileData(dir string) (string, string, error) {
	hookPath := getPreCommitHookPath(dir)
	hookScript, err := buildPreCommitHookScriptFn()
	if err != nil {
		return "", "", err
	}

	return hookPath, hookScript, nil
}

func writeHookScript(hookPath, hookScript string) error {
	file, err := hookCreateFn(hookPath)
	if err != nil {
		return fmt.Errorf("could not create pre-commit hook file: %v", err)
	}
	_, writeErr := hookWriteStringFn(file, hookScript)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return fmt.Errorf("could not write pre-commit hook %s: %w", hookPath, err)
	}

	return nil
}

func validateCurrentRepoForHook() (bool, error) {
	isRepo, err := IsCurrentDirGitRepo()
	if err != nil {
		return false, fmt.Errorf("error checking if current directory is a Git repository: %v", err)
	}
	if !isRepo {
		log.Warn().Msg("The current directory is not a valid GIT repository. Skipping precommit hook creation...")
		return false, nil
	}

	log.Info().Msg("Bootstrap: The current directory is a valid GIT repository, creating precommit hook...")
	return true, nil
}

func getPreCommitHookPath(dir string) string {
	hooksDir := filepath.Join(dir, ".git", "hooks")
	return filepath.Join(hooksDir, "pre-commit")
}

func buildPreCommitHookScript() (string, error) {
	scriptPath := filepath.Join(CacheDir, "precommit")
	return buildExecutableHookScript(scriptPath), nil
}

func buildExecutableHookScript(scriptPath string) string {
	switch hookGOOS {
	case "windows":
		// Git for Windows invokes extensionless hooks through its bundled shell.
		scriptPath = "'" + strings.ReplaceAll(filepath.ToSlash(scriptPath)+".exe", "'", "'\"'\"'") + "'"
		return fmt.Sprintf(`#!/bin/sh
# Pre-commit hook script
scriptPath=%s
if [ ! -f "$scriptPath" ]; then
    echo "Pre-commit executable not found: $scriptPath. Commit aborted."
    exit 1
fi
echo "Running pre-commit script..."
"$scriptPath"
status=$?
if [ "$status" -ne 0 ]; then
    echo "Pre-commit script failed. Commit aborted."
fi
exit "$status"
`, scriptPath)
	default:
		return fmt.Sprintf(`#!/bin/sh
# Pre-commit hook script

# Check if the script exists and is executable
if [ -x "%s" ]; then
    echo "Running pre-commit script..."
    "%s"
    if [ $? -ne 0 ]; then
        echo "Pre-commit script failed. Commit aborted."
        exit 1
    fi
else
    echo "Script %s not found or not executable. Commit aborted."
    exit 1
fi
`, scriptPath, scriptPath, scriptPath)
	}
}

package talosconfig

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/trueforge-org/clustertool/embed"
	"github.com/trueforge-org/clustertool/pkg/helper"
)

const (
	SecretsFilename     = "secrets.sops.yaml"
	TalosconfigFilename = "talosconfig"
)

func SecretsPath() string {
	return filepath.Join(helper.TalosPath, SecretsFilename)
}

func TalosconfigPath() string {
	return filepath.Join(helper.TalosGenerated, TalosconfigFilename)
}

// EnsureSecrets creates the Talos cluster identity once. Existing secrets are
// never replaced by init or genconfig.
func EnsureSecrets() error {
	secretPath := SecretsPath()
	if _, err := os.Stat(secretPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check Talos secrets: %w", err)
	}

	if err := checkExistingIdentity(); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(secretPath), 0o700); err != nil {
		return fmt.Errorf("create Talos directory: %w", err)
	}

	dir, err := os.MkdirTemp(helper.TalosPath, ".secrets-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	temporary := filepath.Join(dir, "secrets.yaml")
	if err := runTalosctl("gen", "secrets", "--output-file", temporary); err != nil {
		return err
	}
	data, err := os.ReadFile(temporary)
	if err != nil {
		return err
	}
	// Exclusive creation protects a concurrent init's existing identity too.
	file, err := os.OpenFile(secretPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// Generate validates every configured node before publishing the output.
func Generate() error {
	inv, err := LoadInventory()
	if err != nil {
		return err
	}
	return generateInventory(inv)
}

func renderPatchDirs(workDir string, dirs []string, env map[string]string) ([]string, error) {
	var sourceFiles []string
	for _, relative := range dirs {
		dir := filepath.Join(helper.TalosPath, "patches", relative)
		if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlink patch directory is not supported: %s", dir)
		}
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) && (relative == "worker" || relative == "control-plane") {
			continue
		}
		if os.IsNotExist(err) && strings.HasPrefix(relative, "nodes") {
			return nil, fmt.Errorf("node patch directory %s is required", dir)
		}
		if err != nil {
			return nil, fmt.Errorf("read Talos document directory %s: %w", dir, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".yaml") || strings.HasSuffix(entry.Name(), ".yml")) {
				if entry.Type()&os.ModeSymlink != 0 {
					return nil, fmt.Errorf("symlink patch is not supported: %s", filepath.Join(dir, entry.Name()))
				}
				sourceFiles = append(sourceFiles, filepath.Join(dir, entry.Name()))
			}
		}
	}

	rendered := make([]string, 0, len(sourceFiles))
	for index, source := range sourceFiles {
		raw, err := os.ReadFile(source)
		if err != nil {
			return nil, err
		}
		content, err := render(raw, env)
		if err != nil {
			return nil, fmt.Errorf("render Talos document %s: %w", source, err)
		}
		target := filepath.Join(workDir, fmt.Sprintf("patch-%03d.yaml", index))
		if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
			return nil, fmt.Errorf("write rendered Talos document: %w", err)
		}
		rendered = append(rendered, target)
	}

	return rendered, nil
}

func runTalosctl(args ...string) error {
	command := exec.Command(embed.GetTalosExec(), args...)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		return fmt.Errorf("talosctl %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(output.String()))
	}
	return nil
}

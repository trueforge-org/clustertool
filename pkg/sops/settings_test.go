package sops

import (
	"os"
	"strings"
	"testing"
)

func TestRejectInvalidSettingsBeforeStaging(t *testing.T) {
	t.Chdir(t.TempDir()) // No Git repository: validation must happen before staging.
	file := "cluster-settings.sops.yaml"
	for _, value := range []string{"443", "true"} {
		plain := "stringData:\n  VALUE: " + value + "\n"
		if err := os.WriteFile(file, []byte(plain), 0600); err != nil {
			t.Fatal(err)
		}
		err := processFileEncryption(EncrFileData{Path: file})
		if err == nil || !strings.Contains(err.Error(), "VALUE must be a string") {
			t.Fatalf("expected string validation before Git operations, got %v", err)
		}
		data, err := os.ReadFile(file)
		if err != nil || string(data) != plain {
			t.Fatalf("invalid settings modified: %v", err)
		}
	}
	if err := processFileEncryption(EncrFileData{Path: file, Encrypted: true}); err != nil {
		t.Fatalf("encrypted file was not skipped: %v", err)
	}
}

package sops

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
)

func TestBinaryFilesRoundTrip(t *testing.T) {
	allBytes := make([]byte, 256)
	for i := range allBytes {
		allBytes[i] = byte(i)
	}
	pem := "-----BEGIN RSA PRIVATE KEY-----\ndummy-key-content\n-----END RSA PRIVATE KEY-----\n"
	for _, tc := range []struct {
		name     string
		path     string
		plain    []byte
		settings string
	}{
		{"PEM text", "key.enc.txt", []byte(pem), ""},
		{"PEM with CRLF", "key.enc.pem", []byte(strings.ReplaceAll(pem, "\n", "\r\n")), ""},
		{"binary bytes", "secret.enc.bin", allBytes, ""},
		{"key without final newline", "secret.enc.key", []byte("dummy-key-content"), ""},
		{"no extension with ENC marker", "secret", []byte("plain ENC[example] content"), ""},
		{"empty file", "empty.enc.bin", []byte{}, ""},
		{"JSON text", "secret.enc.txt", []byte("{\"password\": \"dummy-secret\"}\r\n"), ""},
		{"field selector ignored", "secret.enc.bin", allBytes, "    encrypted_regex: '^password$'\n    mac_only_encrypted: true\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			writeBinaryFilesConfig(t, tc.settings)
			path := filepath.Join("secrets", tc.path)
			if err := os.WriteFile(path, tc.plain, 0600); err != nil {
				t.Fatal(err)
			}
			files, err := ExecuteCheck(false)
			if err != nil || len(files) != 1 || files[0].Encrypted {
				t.Fatalf("expected one plaintext binary file: %v, %v", files, err)
			}
			if err := EncryptAllFiles(); err != nil {
				t.Fatal(err)
			}
			encrypted, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var envelope struct {
				Data string          `json:"data"`
				Sops json.RawMessage `json:"sops"`
			}
			if err := json.Unmarshal(encrypted, &envelope); err != nil {
				t.Fatal(err)
			}
			if len(envelope.Sops) == 0 || (len(tc.plain) > 0 && !strings.HasPrefix(envelope.Data, "ENC[AES256_GCM,")) {
				t.Fatal("binary content was not encrypted in a SOPS JSON document")
			}
			files, err = ExecuteCheck(false)
			if err != nil || len(files) != 1 || !files[0].Encrypted {
				t.Fatalf("expected one encrypted binary file: %v, %v", files, err)
			}
			if err := EncryptAllFiles(); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(encrypted, after) {
				t.Fatal("repeated encryption modified an already encrypted binary file")
			}
			if err := DecryptFiles(); err != nil {
				t.Fatal(err)
			}
			decrypted, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(decrypted, tc.plain) {
				t.Fatal("binary roundtrip did not preserve the original bytes")
			}
			files, err = ExecuteCheck(false)
			if err != nil || len(files) != 1 || files[0].Encrypted {
				t.Fatalf("expected one decrypted binary file: %v, %v", files, err)
			}
		})
	}
}

func TestBinaryDecryptRejectsCorruption(t *testing.T) {
	for _, field := range []string{"data", "mac"} {
		t.Run(field, func(t *testing.T) {
			t.Chdir(t.TempDir())
			writeBinaryFilesConfig(t, "")
			path := filepath.Join("secrets", "secret.enc.bin")
			if err := os.WriteFile(path, []byte{0, 1, 2, 255}, 0600); err != nil {
				t.Fatal(err)
			}
			if err := EncryptAllFiles(); err != nil {
				t.Fatal(err)
			}
			encrypted, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]any
			if err := json.Unmarshal(encrypted, &document); err != nil {
				t.Fatal(err)
			}
			container := document
			if field == "mac" {
				container = document["sops"].(map[string]any)
			}
			value := container[field].(string)
			pos := strings.Index(value, "data:") + len("data:")
			replacement := "A"
			if value[pos:pos+1] == replacement {
				replacement = "B"
			}
			container[field] = value[:pos] + replacement + value[pos+1:]
			tampered, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, tampered, 0600); err != nil {
				t.Fatal(err)
			}
			if err := DecryptFiles(); err == nil || !strings.Contains(err.Error(), "error decrypting file") {
				t.Fatalf("expected binary decryption to reject corrupted %s, got %v", field, err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, tampered) {
				t.Fatal("failed binary decryption overwrote the encrypted file")
			}
		})
	}
}

func TestBinaryFilesStagedDetection(t *testing.T) {
	initStagedFilesRepo(t)
	writeBinaryFilesConfig(t, "")
	path := "secrets/with space-秘密.enc.bin"
	plain := []byte{0, 1, 2, 255}
	if err := os.WriteFile(filepath.FromSlash(path), plain, 0600); err != nil {
		t.Fatal(err)
	}
	runStagedFilesGit(t, "add", "--", path)
	files, err := ExecuteCheck(true)
	if err != nil {
		t.Fatal(err)
	}
	assertStagedFilesPaths(t, files, []string{path})
	if err := EncryptAllFiles(); err != nil {
		t.Fatal(err)
	}
	if staged := runStagedFilesGit(t, "show", ":"+path); !bytes.Equal(staged, plain) {
		t.Fatal("standalone binary encryption modified the Git index")
	}
	files, err = ExecuteCheck(true)
	if err != nil || len(files) != 1 || !files[0].Encrypted || filepath.ToSlash(files[0].Path) != path {
		t.Fatalf("expected the staged binary file to be recognized as encrypted: %v, %v", files, err)
	}
	encrypted, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	if staged := runStagedFilesGit(t, "show", ":"+path); !bytes.Equal(staged, encrypted) {
		t.Fatal("staged binary check did not retain the encrypted working copy")
	}
}

func writeBinaryFilesConfig(t *testing.T, settings string) {
	t.Helper()
	t.Setenv("SOPS_AGE_KEY_FILE", "age.agekey")
	key, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("age.agekey", []byte(key.String()), 0600); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf("creation_rules:\n  - path_regex: '^secrets/'\n    age: %s\n%s", key.Recipient(), settings)
	if err := os.WriteFile(".sops.yaml", []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("secrets", 0700); err != nil {
		t.Fatal(err)
	}
}

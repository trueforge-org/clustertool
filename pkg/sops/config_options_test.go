package sops

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
)

func TestConfiguredStoresAndMAC(t *testing.T) {
	for _, tc := range []struct {
		name                string
		stores              string
		mac                 string
		indent              int
		allowMetadataChange bool
	}{
		{"defaults", "", "", 4, false},
		{"explicit false", "stores:\n  yaml:\n    indent: 2\n", "    mac_only_encrypted: false\n", 2, false},
		{"encrypted only", "stores:\n  yaml:\n    indent: 2\n", "    mac_only_encrypted: true\n", 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			key, err := age.GenerateX25519Identity()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile("age.agekey", []byte(key.String()), 0600); err != nil {
				t.Fatal(err)
			}
			config := tc.stores + fmt.Sprintf("creation_rules:\n  - path_regex: ^unrelated/\n    mac_only_encrypted: true\n    age: %s\n  - path_regex: ^secrets/\n    encrypted_regex: '^(data|stringData)$'\n%s    age: %s\n  - path_regex: .*\n    encrypted_regex: '^(data|stringData)$'\n    mac_only_encrypted: true\n    age: %s\n", key.Recipient(), tc.mac, key.Recipient(), key.Recipient())
			if err := os.WriteFile(".sops.yaml", []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir("secrets", 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("secrets", "test.yaml")
			plain := []byte("metadata:\n  name: original\nstringData:\n  password: sensitive\n")
			if err := os.WriteFile(path, plain, 0600); err != nil {
				t.Fatal(err)
			}
			if err := encryptFile(path); err != nil {
				t.Fatal(err)
			}
			encrypted, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(encrypted, []byte("sensitive")) {
				t.Fatal("secret was not encrypted")
			}
			wantIndent := "\n" + strings.Repeat(" ", tc.indent) + "name: original\n"
			if !bytes.Contains(encrypted, []byte(wantIndent)) {
				t.Fatalf("encrypted YAML does not use indent %d", tc.indent)
			}
			decrypted, err := decryptData(encrypted, "yaml")
			if err != nil || !bytes.Contains(decrypted, []byte("sensitive")) || !bytes.Contains(decrypted, []byte(wantIndent)) {
				t.Fatalf("configured roundtrip failed: %v", err)
			}
			changed := bytes.Replace(encrypted, []byte("name: original"), []byte("name: changed"), 1)
			_, err = decryptData(changed, "yaml")
			if (err == nil) != tc.allowMetadataChange {
				t.Fatalf("unexpected MAC result after metadata change: %v", err)
			}
			// The encrypted payload must remain authenticated in either MAC mode.
			start := bytes.Index(encrypted, []byte("password: ENC["))
			if start < 0 {
				t.Fatal("encrypted password not found")
			}
			start += bytes.Index(encrypted[start:], []byte("data:")) + len("data:")
			tampered := bytes.Clone(encrypted)
			if tampered[start] == 'A' {
				tampered[start] = 'B'
			} else {
				tampered[start] = 'A'
			}
			if _, err := decryptData(tampered, "yaml"); err == nil {
				t.Fatal("tampered ciphertext was accepted")
			}
		})
	}
}

func TestEncryptValidatesEncryptedRegex(t *testing.T) {
	for _, tc := range []struct {
		name    string
		field   string
		invalid bool
	}{
		{"invalid", "    encrypted_regex: '['\n", true},
		{"omitted", "", false},
		{"empty", "    encrypted_regex: ''\n", false},
		{"valid", "    encrypted_regex: '^password$'\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initStagedFilesRepo(t)
			key, err := age.GenerateX25519Identity()
			if err != nil {
				t.Fatal(err)
			}
			config := fmt.Sprintf("creation_rules:\n  - path_regex: '^secrets/'\n%s    age: %s\n", tc.field, key.Recipient())
			if err := os.WriteFile(".sops.yaml", []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			path := "secrets/test.sops.yaml"
			writeStagedFilesFixture(t, path)
			runStagedFilesGit(t, "add", "--", path)
			indexBefore := runStagedFilesGit(t, "ls-files", "--stage", "-z")
			plain := []byte("password: dummy-new-value\n")
			if err := os.WriteFile(path, plain, 0600); err != nil {
				t.Fatal(err)
			}

			err = EncryptAllFiles()
			if tc.invalid {
				if err == nil || !strings.Contains(err.Error(), "invalid encrypted regex in .sops.yaml") {
					t.Fatalf("expected encrypted regex validation error, got %v", err)
				}
				if _, err := ExecuteCheck(true); err == nil || !strings.Contains(err.Error(), "invalid encrypted regex in .sops.yaml") {
					t.Fatalf("staged check did not reject invalid encrypted regex: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if tc.invalid {
				if !bytes.Equal(data, plain) {
					t.Fatal("invalid encrypted regex modified the plaintext file")
				}
			} else if !bytes.Contains(data, []byte("ENC[AES256_GCM,")) || bytes.Contains(data, []byte("dummy-new-value")) {
				t.Fatal("valid encryption configuration left the secret unencrypted")
			}
			if indexAfter := runStagedFilesGit(t, "ls-files", "--stage", "-z"); !bytes.Equal(indexBefore, indexAfter) {
				t.Fatal("encryption or invalid configuration changed the Git index")
			}
		})
	}
}

func TestEncryptSupportedFileFormats(t *testing.T) {
	for _, tc := range []struct {
		name  string
		path  string
		plain string
	}{
		{"yaml", "secret.sops.yaml", "password: point9-test-secret\n"},
		{"yml", "secret.sops.yml", "password: point9-test-secret\n"},
		{"json", "secret.sops.json", "{\"password\":\"point9-test-secret\"}\n"},
		{"dotenv", "secret.sops.env", "PASSWORD=point9-test-secret\n"},
		{"ini", "secret.sops.ini", "[secrets]\npassword = point9-test-secret\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			key, err := age.GenerateX25519Identity()
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("SOPS_AGE_KEY", key.String())
			config := fmt.Sprintf("creation_rules:\n  - path_regex: '^secret[.]'\n    age: %s\n", key.Recipient())
			if err := os.WriteFile(".sops.yaml", []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(tc.path, []byte(tc.plain), 0600); err != nil {
				t.Fatal(err)
			}
			if err := EncryptAllFiles(); err != nil {
				t.Fatal(err)
			}
			encrypted, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(encrypted, []byte("ENC[AES256_GCM,")) || bytes.Contains(encrypted, []byte("point9-test-secret")) {
				t.Fatal("secret was not encrypted")
			}
			decrypted, err := NewCypher().Decrypt(encrypted, GetFormat(tc.path))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(decrypted, []byte("point9-test-secret")) {
				t.Fatal("roundtrip did not recover the original secret")
			}
			if err := EncryptAllFiles(); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(encrypted, after) {
				t.Fatal("repeated encryption modified an already encrypted file")
			}
		})
	}
}

func TestEncryptRejectsUnsupportedFileFormats(t *testing.T) {
	for _, tc := range []struct {
		name  string
		path  string
		plain []byte
	}{
		{"binary", "secret.bin", []byte{0, 1, 2, 255}},
		{"unknown extension with JSON", "secret.txt", []byte("{\"password\":\"point9-test-secret\"}\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			key, err := age.GenerateX25519Identity()
			if err != nil {
				t.Fatal(err)
			}
			config := fmt.Sprintf("creation_rules:\n  - path_regex: '^secret[.]'\n    age: %s\n", key.Recipient())
			if err := os.WriteFile(".sops.yaml", []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(tc.path, tc.plain, 0600); err != nil {
				t.Fatal(err)
			}
			err = EncryptAllFiles()
			if err == nil || !strings.Contains(err.Error(), "unsupported encryption format") {
				t.Fatalf("expected an unsupported encryption format error, got %v", err)
			}
			after, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(tc.plain, after) {
				t.Fatal("unsupported format modified the source file")
			}
		})
	}
}

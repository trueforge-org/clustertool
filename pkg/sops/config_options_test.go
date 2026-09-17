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

package sops

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"filippo.io/age"
	"gopkg.in/yaml.v3"
)

func TestDecryptRejectsCorruptMAC(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("SOPS_AGE_KEY_FILE", "age.agekey")
	key, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("age.agekey", []byte("# public key: "+key.Recipient().String()+"\n"+key.String()+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".sops.yaml", []byte("creation_rules:\n  - path_regex: .*\\.yaml$\n    age: "+key.Recipient().String()+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	plain := []byte("secret: persistent-identity\n")
	encrypted, err := EncryptWithAgeKey(plain, "", "yaml")
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := decryptData(encrypted, "yaml")
	if err != nil || !bytes.Contains(decrypted, []byte("persistent-identity")) {
		t.Fatalf("roundtrip failed: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(encrypted, &doc); err != nil {
		t.Fatal(err)
	}
	metadata := doc["sops"].(map[string]any)
	mac := metadata["mac"].(string)
	pos := strings.Index(mac, "data:") + len("data:")
	replacement := "A"
	if mac[pos:pos+1] == replacement {
		replacement = "B"
	}
	metadata["mac"] = mac[:pos] + replacement + mac[pos+1:]
	tampered, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decryptData(tampered, "yaml"); err == nil {
		t.Fatal("corrupted MAC was accepted")
	}
}

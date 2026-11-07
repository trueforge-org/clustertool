package helper

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestYAMLSubstitution(t *testing.T) {
	secret := "quote\": value\npassword: surprise"
	data, err := RenderYAML([]byte("password: \"${PASSWORD}\"\n"), map[string]string{"PASSWORD": secret})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]string
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc) != 1 || doc["password"] != secret {
		t.Fatal("credential changed YAML structure")
	}
	if _, err := RenderYAML([]byte("value: ${MISSING}\n"), nil); err == nil {
		t.Fatal("unresolved variable accepted")
	}
}

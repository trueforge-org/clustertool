package talosconfig

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/trueforge-org/clustertool/pkg/helper"
	"gopkg.in/yaml.v3"
)

func checkExistingIdentity() error {
	for _, path := range []string{TalosconfigPath()} {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("existing Talos cluster detected at %s: refusing to generate new CAs; restore secrets.sops.yaml before continuing", path)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func publish(staged string) error {
	// Both validated files are published together. Never replace an older backup.
	backup := helper.TalosGenerated + ".previous"
	if _, err := os.Stat(backup); err == nil {
		return fmt.Errorf("generation backup exists at %s; inspect it before retrying", backup)
	} else if !os.IsNotExist(err) {
		return err
	}
	hadPrevious := false
	if _, err := os.Stat(helper.TalosGenerated); err == nil {
		if err = os.Rename(helper.TalosGenerated, backup); err != nil {
			return err
		}
		hadPrevious = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(staged, helper.TalosGenerated); err != nil {
		if hadPrevious {
			if restoreErr := os.Rename(backup, helper.TalosGenerated); restoreErr != nil {
				return fmt.Errorf("publish: %v; restore %s: %w", err, backup, restoreErr)
			}
		}
		return err
	}
	if hadPrevious {
		return os.RemoveAll(backup)
	}
	return nil
}

// Read only the official fields needed for CLI orchestration from validated
// output. This does not define or validate another Talos schema.

func GeneratedNodeValue(path, kind string, fields ...string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var doc map[string]interface{}
		if err := dec.Decode(&doc); err == io.EOF {
			break
		} else if err != nil {
			return "", err
		}
		if kind != "" && doc["kind"] != kind {
			continue
		}
		if kind == "" && doc["kind"] != nil {
			continue
		}
		var value interface{} = doc
		for _, key := range fields {
			m, ok := value.(map[string]interface{})
			if !ok {
				value = nil
				break
			}
			value = m[key]
		}
		if s, ok := value.(string); ok && s != "" {
			return s, nil
		}
	}
	return "", fmt.Errorf("generated Talos configuration has no %s %s", kind, strings.Join(fields, "."))
}

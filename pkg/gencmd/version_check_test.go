package gencmd

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/trueforge-org/clustertool/pkg/helper"
)

func TestConfirmTalosVersion(t *testing.T) {
	for _, tc := range []struct {
		name, client      string
		accept, fail, ask bool
	}{
		{"same", "v1.14.0", false, false, false},
		{"patch", "v1.14.9", false, false, false},
		{"older-minor-decline", "v1.13.9", false, true, true},
		{"newer-minor-accept", "v1.15.0", true, false, true},
		{"major-decline", "v2.14.0", false, true, true},
		{"major-accept", "v2.14.0", true, false, true},
		{"malformed", "invalid", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withSingleNodeFixture(t)
			mockExecution(t)
			calls := 0
			runCommand = func(args []string, silent bool) ([]byte, error) {
				if !silent || !reflect.DeepEqual(args[1:], []string{"version", "--client", "--short"}) {
					t.Fatal(args, silent)
				}
				return []byte("Client:\r\nTalos " + tc.client + "\r\n"), nil
			}
			err := confirmTalosVersion(func(prompt string, defaultYes bool) bool {
				calls++
				if prompt != "Continue anyway? [y/n]: " || defaultYes {
					t.Fatal(prompt, defaultYes)
				}
				return tc.accept
			})
			if (err != nil) != tc.fail || (calls == 1) != tc.ask {
				t.Fatal(calls, err)
			}
		})
	}
}

func TestDeclinedVersionKeepsGeneratedFiles(t *testing.T) {
	// The check is read-only: declining cannot overwrite existing generated output.
	withSingleNodeFixture(t)
	mockExecution(t)
	marker := filepath.Join(helper.TalosPath, "generated", "control-1.yaml")
	if err := os.MkdirAll(filepath.Dir(marker), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("existing configuration"), 0600); err != nil {
		t.Fatal(err)
	}
	runCommand = func([]string, bool) ([]byte, error) { return []byte("Client:\nTalos v1.15.0\n"), nil }
	if err := confirmTalosVersion(func(string, bool) bool { return false }); err == nil {
		t.Fatal("decline accepted")
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "existing configuration" {
		t.Fatal(string(data), err)
	}
	cause := errors.New("cannot execute")
	runCommand = func([]string, bool) ([]byte, error) { return nil, cause }
	err = confirmTalosVersion(func(string, bool) bool { t.Fatal("prompt after version failure"); return true })
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "read talosctl version") {
		t.Fatal(err)
	}
}

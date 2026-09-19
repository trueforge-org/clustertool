package nodestatus

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func TestStatusWarningAndInsecureGuard(t *testing.T) {
	for _, stage := range []string{"maintenance", "running", "", "not maintenance"} {
		t.Run(stage, func(t *testing.T) {
			previous, logger := runCommand, log.Logger
			t.Cleanup(func() { runCommand, log.Logger = previous, logger })
			var messages bytes.Buffer
			log.Logger = zerolog.New(&messages)
			calls := 0
			runCommand = func(args []string, silent bool) (string, string, error) {
				calls++
				if !silent {
					t.Fatal("status command must be silent")
				}
				if calls == 1 {
					return "", "certificate signed by unknown authority", errors.New("exit status 1")
				}
				if !strings.Contains(strings.Join(args, " "), "--insecure") {
					t.Fatal("missing maintenance retry")
				}
				return stage + "\n", "WARNING: server version 1.14.0 is older than client version 1.14.1", nil
			}
			got, err := CheckStatus("192.168.1.151")
			if (err == nil) != (stage == "maintenance") {
				t.Fatalf("status=%q err=%v", got, err)
			}
			if stage == "maintenance" && got != stage {
				t.Fatal(got)
			}
			if calls != 2 || !strings.Contains(messages.String(), "server version 1.14.0") {
				t.Fatalf("calls=%d logs=%s", calls, messages.String())
			}
		})
	}
}

func TestReadyWarningAndCommandFailure(t *testing.T) {
	previous := runCommand
	t.Cleanup(func() { runCommand = previous })
	runCommand = func([]string, bool) (string, string, error) { return "true\n", "WARNING: version mismatch", nil }
	if got, err := CheckReadyStatus("node", true); err != nil || got != "true" {
		t.Fatal(got, err)
	}
	runCommand = func([]string, bool) (string, string, error) { return "false", "WARNING: true is not the status", nil }
	if _, err := CheckReadyStatus("node", true); err == nil {
		t.Fatal("stderr influenced readiness")
	}
	failure := errors.New("exit status 1")
	runCommand = func([]string, bool) (string, string, error) { return "maintenance", "connection refused", failure }
	if _, err := CheckStatus("node"); !errors.Is(err, failure) || !strings.Contains(err.Error(), "connection refused") {
		t.Fatal(err)
	}
	if _, err := CheckReadyStatus("node", true); !errors.Is(err, failure) || !strings.Contains(err.Error(), "connection refused") {
		t.Fatal(err)
	}
}

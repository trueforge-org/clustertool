package gencmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/trueforge-org/clustertool/pkg/talosconfig"
)

func certificateDelayMessage(seconds int) string {
	return fmt.Sprintf("rpc error: code = Unavailable desc = x509: certificate has expired or is not yet valid: current time 2026-09-12T17:10:24+02:00 is before %s", time.Date(2026, 9, 12, 15, 10, 24, 0, time.UTC).Add(time.Duration(seconds)*time.Second).Format(time.RFC3339))
}

func TestBootstrapCertificateClassification(t *testing.T) {
	for _, seconds := range []int{-1, 0, 1, 30, 31, 3600} {
		message := certificateDelayMessage(seconds)
		want := seconds > 0 && seconds <= 30
		if got := temporaryBootstrapError([]byte(message), errors.New("exit status 1")); got != want {
			t.Fatalf("gap %d: retry=%v", seconds, got)
		}
	}
	for _, message := range []string{
		"x509: certificate signed by unknown authority",
		"x509: certificate has expired or is not yet valid: current time 2026-09-12T15:10:24Z is after 2026-09-12T15:10:23Z",
		"x509: certificate has expired or is not yet valid: current time invalid is before invalid",
		certificateDelayMessage(1) + "; x509: certificate signed by unknown authority",
	} {
		if temporaryBootstrapError([]byte(message), errors.New("exit status 1")) {
			t.Fatal(message)
		}
	}
}

func TestBootstrapCertificateRecovery(t *testing.T) {
	for _, operation := range []string{"etcd", "bootstrap"} {
		for _, seconds := range []int{1, 31} {
			t.Run(fmt.Sprintf("%s-%d", operation, seconds), func(t *testing.T) {
				old := runBootstrapCommand
				t.Cleanup(func() { runBootstrapCommand = old })
				inv := &talosconfig.Inventory{BootstrapNode: "cp", Nodes: []talosconfig.Node{{Name: "cp", Role: "control-plane", Address: "192.0.2.11"}}}
				cause := errors.New("exit status 1")
				probes, calls := 0, 0
				runBootstrapCommand = func(ctx context.Context, args []string) ([]byte, error) {
					if strings.Contains(strings.Join(args, " "), "--insecure") {
						t.Fatal("TLS disabled")
					}
					if args[1] == "etcd" {
						probes++
						if operation == "etcd" && probes == 1 {
							return []byte(certificateDelayMessage(seconds)), cause
						}
						return nil, cause
					}
					calls++
					if probes != calls {
						t.Fatal("missing membership probe")
					}
					if calls == 1 {
						if operation == "bootstrap" {
							return []byte(certificateDelayMessage(seconds)), cause
						}
						return []byte("connection refused"), cause
					}
					return nil, nil
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				err := waitForBootstrap(ctx, inv, time.Millisecond)
				if seconds == 1 && (err != nil || calls != 2) {
					t.Fatalf("calls=%d: %v", calls, err)
				}
				if seconds == 31 && !errors.Is(err, cause) {
					t.Fatalf("expected certificate failure: %v", err)
				}
			})
		}
	}
}

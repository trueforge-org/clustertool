package gencmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/trueforge-org/clustertool/pkg/talosconfig"
)

func TestBootstrapInstallationRecovery(t *testing.T) {
	for _, scenario := range []string{"recover", "response-lost", "fatal", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			old := runBootstrapCommand
			t.Cleanup(func() { runBootstrapCommand = old })
			inv := &talosconfig.Inventory{BootstrapNode: "cp", Nodes: []talosconfig.Node{{Name: "cp", Role: "control-plane", Address: "192.0.2.11"}}}
			calls, probes := 0, 0
			cause := errors.New("exit status 1")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runBootstrapCommand = func(ctx context.Context, args []string) ([]byte, error) {
				if strings.Contains(strings.Join(args, " "), "--insecure") {
					t.Fatal("TLS disabled")
				}
				if args[1] == "etcd" {
					probes++
					if scenario == "response-lost" && calls == 1 {
						return []byte("NODE ID HOSTNAME PEER URLS CLIENT URLS LEARNER\n192.0.2.11 1 cp https://192.0.2.11:2380 https://192.0.2.11:2379 false\n"), nil
					}
					return nil, cause
				}
				calls++
				if probes != calls {
					t.Fatal("bootstrap retried without membership probe")
				}
				if scenario == "fatal" {
					return []byte("x509: certificate signed by unknown authority"), cause
				}
				if scenario == "timeout" {
					cancel()
					return []byte("connection refused"), cause
				}
				if scenario == "recover" && calls == 3 {
					return nil, nil
				}
				if calls == 2 {
					return []byte("bootstrap is not available yet"), cause
				}
				return []byte("rpc error: code = Unavailable desc = connection refused"), cause
			}
			err := waitForBootstrap(ctx, inv, time.Millisecond)
			switch scenario {
			case "recover":
				if err != nil || calls != 3 {
					t.Fatal(calls, err)
				}
			case "response-lost":
				if err != nil || calls != 1 || probes != 2 {
					t.Fatal(calls, probes, err)
				}
			case "fatal":
				if !errors.Is(err, cause) || calls != 1 {
					t.Fatal(calls, err)
				}
			case "timeout":
				if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "connection refused") || calls != 1 {
					t.Fatal(calls, err)
				}
			}
		})
	}
}

func TestTemporaryBootstrapErrors(t *testing.T) {
	for _, message := range []string{"connection refused", "connection reset by peer", "bootstrap is not available yet", "i/o timeout", "rpc error: code = Unavailable desc = EOF"} {
		if !temporaryBootstrapError([]byte(message), errors.New("exit status 1")) {
			t.Fatal(message)
		}
	}
	for _, message := range []string{"invalid configuration", "x509: certificate error", "rpc error: code = PermissionDenied", "rpc error: code = Unauthenticated", "unknown flag", "rpc error: code = Unavailable desc = certificate error"} {
		if temporaryBootstrapError([]byte(message), errors.New("exit status 1")) {
			t.Fatal(message)
		}
	}
}

func TestBootstrapDeadlineStopsProbes(t *testing.T) {
	old := runBootstrapCommand
	t.Cleanup(func() { runBootstrapCommand = old })
	inv := &talosconfig.Inventory{BootstrapNode: "cp", Nodes: []talosconfig.Node{{Name: "cp", Role: "control-plane", Address: "192.0.2.11"}}}
	calls := 0
	runBootstrapCommand = func(ctx context.Context, args []string) ([]byte, error) { calls++; <-ctx.Done(); return nil, ctx.Err() }
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := waitForBootstrap(ctx, inv, time.Millisecond); !errors.Is(err, context.DeadlineExceeded) || calls != 1 {
		t.Fatal(calls, err)
	}
}

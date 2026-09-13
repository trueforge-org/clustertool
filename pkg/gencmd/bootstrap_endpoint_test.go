package gencmd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/trueforge-org/clustertool/pkg/talosconfig"
)

func TestBootstrapEndpointsAndResume(t *testing.T) {
	for _, resumed := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "resumed"}[resumed], func(t *testing.T) {
			mockExecution(t)
			oldBootstrapRun := runBootstrapCommand
			t.Cleanup(func() { runBootstrapCommand = oldBootstrapRun })
			runBootstrapCommand = func(_ context.Context, args []string) ([]byte, error) { return runCommand(args, true) }
			inv := &talosconfig.Inventory{APIVersion: "clustertool/v1", Kind: "ClusterConfig", BootstrapNode: "cp2", Nodes: []talosconfig.Node{
				{Name: "cp1", Role: "control-plane", Address: "192.0.2.11"},
				{Name: "cp2", Role: "control-plane", Address: "192.0.2.12"},
				{Name: "cp3", Role: "control-plane", Address: "192.0.2.13"},
			}}
			bootstraps, kubeconfigs := 0, 0
			runCommand = func(args []string, _ bool) ([]byte, error) {
				argv := strings.Join(args, " ")
				if strings.Contains(argv, "--insecure") {
					t.Fatalf("TLS verification disabled: %s", argv)
				}
				if args[1] == "etcd" {
					if resumed && strings.Contains(argv, " -e 192.0.2.12") {
						return []byte("NODE ID HOSTNAME PEER URLS CLIENT URLS LEARNER\n192.0.2.12 1 cp2 https://192.0.2.12:2380 https://192.0.2.12:2379 false\n"), nil
					}
					if !strings.Contains(argv, " -e 192.0.2.12") {
						return []byte("x509: certificate signed by unknown authority"), errors.New("exit status 1")
					}
					return nil, errors.New("etcd unavailable")
				}
				if !strings.Contains(argv, " -n 192.0.2.12") || !strings.Contains(argv, " -e 192.0.2.12") {
					t.Fatalf("initial setup must use the inventory bootstrap node directly: %s", argv)
				}
				switch args[1] {
				case "bootstrap":
					bootstraps++
				case "kubeconfig":
					kubeconfigs++
				default:
					t.Fatalf("unexpected operation: %s", argv)
				}
				return nil, nil
			}
			if err := bootstrapIfNeeded(inv); err != nil {
				t.Fatal(err)
			}
			cmd := bootstrapCommand(inv, "kubeconfig", "-f")
			if cmd.Failover {
				t.Fatal("initial kubeconfig must not fail over to maintenance nodes")
			}
			if err := ExecCmd(cmd); err != nil {
				t.Fatal(err)
			}
			want := 1
			if resumed {
				want = 0
			}
			if bootstraps != want || kubeconfigs != 1 {
				t.Fatalf("bootstrap calls=%d (want %d), kubeconfig calls=%d", bootstraps, want, kubeconfigs)
			}
		})
	}
}

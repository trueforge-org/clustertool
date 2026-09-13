package gencmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/trueforge-org/clustertool/pkg/talosconfig"
)

func bootstrapCommand(inv *talosconfig.Inventory, operation string, extra ...string) Command {
	node := inv.Bootstrap()
	return nodeCommand(operation, node, append([]string{"-e", node.Address}, extra...)...)
}

// Bound both membership probes and bootstrap RPCs by the overall installation deadline.
var runBootstrapCommand = func(ctx context.Context, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	if ctx.Err() != nil {
		return output.Bytes(), ctx.Err()
	}
	return output.Bytes(), err
}

var certificateNotYetValid = regexp.MustCompile(`x509: certificate has expired or is not yet valid: current time (\S+) is before ([0-9T:.+Z-]+)`)

// Talosctl reports the verifier's time and certificate NotBefore in RFC3339.
// Retry only a small positive gap; every attempt still verifies TLS normally.
func shortBootstrapCertificateDelay(out []byte, err error) bool {
	message := string(out) + " " + err.Error()
	matches := certificateNotYetValid.FindStringSubmatch(message)
	if len(matches) != 3 {
		return false
	}
	current, currentErr := time.Parse(time.RFC3339Nano, matches[1])
	notBefore, notBeforeErr := time.Parse(time.RFC3339Nano, matches[2])
	if currentErr != nil || notBeforeErr != nil {
		return false
	}
	gap := notBefore.Sub(current)
	return gap > 0 && gap <= 30*time.Second
}

func permanentBootstrapError(out []byte, err error) bool {
	message := strings.ToLower(string(out) + " " + err.Error())
	for _, fatal := range []string{"unknown authority", "is after", "unauthenticated", "permissiondenied", "permission denied", "invalidargument", "invalid argument", "unknown flag"} {
		if strings.Contains(message, fatal) {
			return true
		}
	}
	return (strings.Contains(message, "x509") || strings.Contains(message, "certificate")) &&
		!shortBootstrapCertificateDelay(out, err)
}

func temporaryBootstrapError(out []byte, err error) bool {
	if permanentBootstrapError(out, err) {
		return false
	}
	if shortBootstrapCertificateDelay(out, err) {
		return true
	}
	message := strings.ToLower(string(out) + " " + err.Error())
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	for _, temporary := range []string{"bootstrap is not available yet", "connection refused", "connection reset", "no route to host", "network is unreachable", "i/o timeout", "transport is closing", "unexpected eof"} {
		if strings.Contains(message, temporary) {
			return true
		}
	}
	return strings.Contains(message, "code = unavailable") && strings.Contains(message, "eof")
}

func bootstrapIfNeeded(inv *talosconfig.Inventory) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	return waitForBootstrap(ctx, inv, 5*time.Second)
}

func waitForBootstrap(ctx context.Context, inv *talosconfig.Inventory, interval time.Duration) error {
	node := inv.Bootstrap().Address
	var lastErr error
	waiting := false
	certificateWaiting := false
	reportCertificateWait := func(out []byte, err error) {
		if !certificateWaiting && shortBootstrapCertificateDelay(out, err) {
			log.Info().Msgf("Waiting for the Talos certificate to become valid on %s; retrying with TLS verification.", node)
			certificateWaiting = true
		}
	}
	for {
		// A previous RPC may have succeeded even if its response was lost.
		for _, n := range inv.Nodes {
			if n.Role != "control-plane" {
				continue
			}
			if ctx.Err() != nil {
				break
			}
			cmd := nodeCommand("etcd", n, "members", "-e", n.Address)
			if cmd.Err != nil {
				return cmd.Err
			}
			out, err := runBootstrapCommand(ctx, cmd.Args)
			if err != nil {
				probeErr := fmt.Errorf("read etcd membership on %s: %w: %s", n.Address, err, strings.TrimSpace(string(out)))
				if n.Address == node && permanentBootstrapError(out, err) {
					return probeErr
				}
				if n.Address == node {
					reportCertificateWait(out, err)
				}
				lastErr = probeErr
				log.Debug().Err(probeErr).Msg("Bootstrap membership not available")
			}
			if err == nil {
				if _, parseErr := parseEtcdMembers(string(out), true); parseErr == nil {
					return nil
				}
			}
		}
		if ctx.Err() != nil {
			return fmt.Errorf("waiting for bootstrap on %s: %w; last error: %v", node, ctx.Err(), lastErr)
		}
		cmd := bootstrapCommand(inv, "bootstrap")
		if cmd.Err != nil {
			return cmd.Err
		}
		out, err := runBootstrapCommand(ctx, cmd.Args)
		if err == nil {
			return nil
		}
		lastErr = fmt.Errorf("node %s, bootstrap: %w: %s", node, err, strings.TrimSpace(string(out)))
		if !temporaryBootstrapError(out, err) {
			return lastErr
		}
		reportCertificateWait(out, err)
		if !waiting {
			log.Info().Msgf("Waiting for the Talos API and bootstrap availability on %s during installation.", node)
			waiting = true
		}
		log.Debug().Err(lastErr).Msg("Bootstrap not ready; retrying")
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("waiting for bootstrap on %s: %w; last error: %v", node, ctx.Err(), lastErr)
		case <-timer.C:
		}
	}
}

package gencmd

import (
	"fmt"
	"strings"
)

// Do not let forwarded flags change the validated node, input, identity, image,
// or endpoint. Those values have one source in the cluster configuration.
func ValidateExtraArgs(args []string) error {
	for _, arg := range args {
		flag, _, _ := strings.Cut(arg, "=")
		switch flag {
		case "-n", "--nodes", "-e", "--endpoints", "-f", "--file", "--talosconfig", "--context", "--image", "--to", "--insecure", "--mode", "--dry-run", "-i", "-m", "-p", "--config-patch", "--cluster", "-c", "--endpoint", "--wait", "--preserve", "--stage", "--force", "--no-reboot", "--legacy", "--siderov1-keys-dir", "--cert-fingerprint":
			return fmt.Errorf("%s must be configured in the cluster files, not forwarded as a flag", flag)
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && len(arg) > 2 {
			return fmt.Errorf("combined short flags are not supported: %s", arg)
		}
	}
	return nil
}

package helper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// RunBoundedCommand preserves argv and puts a real deadline on subprocesses,
// including connection attempts which otherwise outlive health-check timers.
func RunBoundedCommand(args []string, silent bool) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	timeout := 30 * time.Second
	if !silent {
		timeout = 35 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if !silent {
		cmd.Stdout = io.MultiWriter(&output, os.Stdout)
		cmd.Stderr = cmd.Stdout
	}
	err := cmd.Run()
	if ctx.Err() != nil {
		return output.Bytes(), fmt.Errorf("command timed out after %s: %w", timeout, ctx.Err())
	}
	return output.Bytes(), err
}

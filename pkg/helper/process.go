package helper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// filteredWriter hides expected connection messages from the terminal only.
type filteredWriter struct {
	writer  io.Writer
	filters []string
}

func (fw *filteredWriter) Write(p []byte) (int, error) {
	var lines []string
	for _, line := range strings.Split(string(p), "\n") {
		skip := false
		for _, filter := range fw.filters {
			if strings.Contains(line, filter) {
				skip = true
				break
			}
		}
		if !skip {
			lines = append(lines, line)
		}
	}
	_, err := io.WriteString(fw.writer, strings.Join(lines, "\n"))
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

// RunCommandWithTimeout follows Forgetool's RunCommand, with a deadline and
// separate, unfiltered stdout and stderr results for callers to interpret.
func RunCommandWithTimeout(args []string, silent bool) (stdout string, stderr string, err error) {
	if len(args) == 0 {
		return "", "", fmt.Errorf("empty command")
	}
	log.Trace().Msg("Command slice structure:")
	for i, arg := range args {
		log.Trace().Msgf("Index: %d, Value: %s\n", i, arg)
	}
	timeout := 30 * time.Second
	if !silent {
		timeout = 35 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdoutBuf, &stderrBuf
	if !silent {
		filters := []string{"certificate signed by unknown authority", "bootstrap is not available yet"}
		cmd.Stdout = io.MultiWriter(&stdoutBuf, &filteredWriter{writer: os.Stdout, filters: filters})
		cmd.Stderr = io.MultiWriter(&stderrBuf, &filteredWriter{writer: os.Stderr, filters: filters})
	}
	err = cmd.Run()
	if ctx.Err() != nil {
		err = fmt.Errorf("command timed out after %s: %w", timeout, ctx.Err())
	}
	return stdoutBuf.String(), stderrBuf.String(), err
}

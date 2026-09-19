package helper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCommandChild(t *testing.T) {
	mode := os.Getenv("CLUSTERTOOL_COMMAND_TEST")
	if mode == "" {
		return
	}
	fmt.Fprint(os.Stdout, "maintenance")
	fmt.Fprint(os.Stderr, "WARNING: server version is older than client version\n")
	switch mode {
	case "fail":
		os.Exit(7)
	case "timeout":
		time.Sleep(time.Minute)
	}
	os.Exit(0)
}

func TestRunCommandWithTimeout(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"ok", "fail", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("CLUSTERTOOL_COMMAND_TEST", mode)
			out, stderr, err := RunCommandWithTimeout([]string{exe, "-test.run=^TestCommandChild$"}, true)
			if out != "maintenance" || !strings.Contains(stderr, "WARNING:") {
				t.Fatalf("stdout=%q stderr=%q", out, stderr)
			}
			if (err != nil) != (mode != "ok") {
				t.Fatalf("unexpected error: %v", err)
			}
			if mode == "timeout" && (!errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "30s")) {
				t.Fatalf("missing timeout context: %v", err)
			}
		})
	}
	if _, _, err := RunCommandWithTimeout(nil, true); err == nil {
		t.Fatal("empty command accepted")
	}
}

func TestFilteredWriterConsumesHiddenBytes(t *testing.T) {
	var output bytes.Buffer
	writer := filteredWriter{writer: &output, filters: []string{"certificate signed by unknown authority"}}
	input := []byte("certificate signed by unknown authority\nWARNING: version mismatch\n")
	n, err := writer.Write(input)
	if err != nil || n != len(input) || output.String() != "WARNING: version mismatch\n" {
		t.Fatalf("n=%d err=%v output=%q", n, err, output.String())
	}
}

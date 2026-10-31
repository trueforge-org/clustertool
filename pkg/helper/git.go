package helper

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// GetStagedFiles lists staged files, excluding deletions.
func GetStagedFiles() ([]string, error) {
	// Return paths verbatim and relative to the current directory.
	cmd := exec.Command("git", "diff", "--cached", "--name-only", "--diff-filter=d", "--relative", "-z")

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to run git command: %v", err)
	}

	// Split the NUL-separated file names without trimming path whitespace.
	output := strings.TrimSuffix(out.String(), "\x00")
	if output == "" {
		return nil, nil
	}

	files := strings.Split(output, "\x00")
	return files, nil
}

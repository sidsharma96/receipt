// Package toolutil contains shared setup for repository maintenance commands.
package toolutil

import (
	"fmt"
	"os/exec"
	"strings"
)

// Root returns the enclosing Git checkout, allowing commands from subdirectories.
func Root() (string, error) {
	output, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("find checkout: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

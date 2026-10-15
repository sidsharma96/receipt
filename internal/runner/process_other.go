//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly

package runner

import (
	"os"
	"os/exec"
)

func configureProcess(c *exec.Cmd) {}

func waitSignal(*os.ProcessState) (int, bool) { return 0, false }

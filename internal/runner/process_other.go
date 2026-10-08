//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly

package runner

import "os/exec"

func configureProcess(c *exec.Cmd) {}

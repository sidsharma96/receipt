//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package runner

import (
	"os"
	"os/exec"
	"syscall"
)

// Give the command its own process group so cancellation kills subprocesses too.
func configureProcess(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return err
	}
}

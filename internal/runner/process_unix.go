//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package runner

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// terminationGrace is how long a cancelled command's process group has to exit
// after SIGTERM before it receives SIGKILL.
const terminationGrace = 5 * time.Second

// Give the command its own process group so cancellation stops subprocesses
// too. Cancellation sends SIGTERM, so tests can clean up, and SIGKILL to
// whatever is left in the group after terminationGrace.
func configureProcess(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process == nil {
			return os.ErrProcessDone
		}
		group := -c.Process.Pid
		err := syscall.Kill(group, syscall.SIGTERM)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		if err != nil {
			return err
		}
		// Wait cannot return until Cancel does, so polling here delays nothing
		// beyond the group's own exit. Signal 0 only tests whether it exists.
		for deadline := time.Now().Add(terminationGrace); time.Now().Before(deadline); {
			time.Sleep(10 * time.Millisecond)
			if syscall.Kill(group, 0) == syscall.ESRCH {
				return nil
			}
		}
		if err = syscall.Kill(group, syscall.SIGKILL); err == syscall.ESRCH {
			return nil
		}
		return err
	}
}

// waitSignal follows the shell convention: 128 plus the signal number.
func waitSignal(state *os.ProcessState) (int, bool) {
	status, ok := state.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return 0, false
	}
	return 128 + int(status.Signal()), true
}

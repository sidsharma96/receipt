//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestSignalDeathReportsSignalNumber(t *testing.T) {
	for _, signal := range []syscall.Signal{syscall.SIGKILL, syscall.SIGUSR1} {
		var out bytes.Buffer
		r, err := Run(context.Background(), t.TempDir(), []string{"/bin/sh", "-c", "kill -" + signalName(signal) + " $$"}, &out, &out)
		if err != nil || r.ExitCode != 128+int(signal) {
			t.Fatalf("%v: exit code %d, want %d (%v)", signal, r.ExitCode, 128+int(signal), err)
		}
	}
}

func signalName(s syscall.Signal) string {
	if s == syscall.SIGKILL {
		return "KILL"
	}
	return "USR1"
}

func TestCancellationReportsInterruptAndAllowsCleanup(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "cleaned")
	ctx, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer
	done := make(chan Output, 1)
	go func() {
		r, _ := Run(ctx, dir, []string{"/bin/sh", "-c", `trap 'echo done > cleaned; exit 1' TERM; echo ready; sleep 30 & wait`}, &out, &out)
		done <- r
	}()
	time.Sleep(300 * time.Millisecond)
	cancel()
	r := <-done
	if r.ExitCode != 130 {
		t.Fatalf("interrupted run reported %d, want 130", r.ExitCode)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("command got no chance to clean up before being killed", err)
	}
}

func TestCancellationKillsCommandsThatIgnoreTermination(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var out bytes.Buffer
	start := time.Now()
	if _, err := Run(ctx, t.TempDir(), []string{"/bin/sh", "-c", `trap '' TERM; sleep 30`}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > terminationGrace+4*time.Second {
		t.Fatalf("command ignoring SIGTERM survived %v", elapsed)
	}
}

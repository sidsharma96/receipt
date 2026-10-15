// Package runner executes commands directly, without shell interpolation.
package runner

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"receipt/internal/fingerprint"
	"receipt/internal/observe"
	"slices"
	"strings"
	"time"
)

type limitedBuffer struct {
	data     []byte
	limit    int
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	space := b.limit - len(b.data)
	if len(p) > space {
		b.overflow = true
		p = p[:space]
	}
	b.data = append(b.data, p...)
	return n, nil
}

// Output contains the process outcome, bounded captured streams and elapsed duration.
type Output struct {
	Stdout, Stderr []byte
	ExitCode       int
	Duration       time.Duration
	TooLarge       bool
}

// withoutToken removes Receipt's own bearer token. The manifest does not record
// it, so the command must not be able to read it either.
func withoutToken(env []string) []string {
	return slices.DeleteFunc(env, func(entry string) bool {
		return strings.HasPrefix(entry, fingerprint.TokenEnvironment+"=")
	})
}

// Run executes a command directly and streams its output while capturing bounded copies.
func Run(ctx context.Context, dir string, args []string, out, errOut io.Writer) (Output, error) {
	defer observe.Measure(ctx, observe.Execution)()
	if len(args) == 0 {
		return Output{}, errors.New("missing command")
	}
	stdout, stderr := &limitedBuffer{limit: 8 << 20}, &limitedBuffer{limit: 8 << 20}
	c := exec.CommandContext(ctx, args[0], args[1:]...)
	c.Dir = dir
	c.Env = withoutToken(c.Environ())
	configureProcess(c)
	// Stdin is closed: a cached run cannot replay interactive input.
	c.Stdout = io.MultiWriter(out, stdout)
	c.Stderr = io.MultiWriter(errOut, stderr)
	c.WaitDelay = 2 * time.Second
	start := time.Now()
	err := c.Run()
	r := Output{stdout.data, stderr.data, 0, time.Since(start), stdout.overflow || stderr.overflow}
	var exit *exec.ExitError
	if ctx.Err() != nil && (err == nil || errors.Is(err, ctx.Err()) || errors.As(err, &exit)) {
		// Receipt cancelled the command, after Ctrl-C or SIGTERM. Report an
		// interrupt, as a shell would, however the command itself ended.
		r.ExitCode = 130
		return r, nil
	}
	if errors.As(err, &exit) {
		r.ExitCode = exit.ExitCode()
		if r.ExitCode < 0 {
			// A signal killed it: 128 plus the signal number, such as 139 for SIGSEGV.
			r.ExitCode = 128
			if code, ok := waitSignal(exit.ProcessState); ok {
				r.ExitCode = code
			}
		}
		return r, nil
	}
	return r, err
}

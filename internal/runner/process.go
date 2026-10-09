// Package runner executes commands directly, without shell interpolation.
package runner

import (
	"context"
	"errors"
	"io"
	"os/exec"
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

// Run executes a command directly and streams its output while capturing bounded copies.
func Run(ctx context.Context, dir string, args []string, out, errOut io.Writer) (Output, error) {
	if len(args) == 0 {
		return Output{}, errors.New("missing command")
	}
	stdout, stderr := &limitedBuffer{limit: 8 << 20}, &limitedBuffer{limit: 8 << 20}
	c := exec.CommandContext(ctx, args[0], args[1:]...)
	c.Dir = dir
	configureProcess(c)
	// Stdin is closed: a cached run cannot replay interactive input.
	c.Stdout = io.MultiWriter(out, stdout)
	c.Stderr = io.MultiWriter(errOut, stderr)
	c.WaitDelay = 2 * time.Second
	start := time.Now()
	err := c.Run()
	r := Output{stdout.data, stderr.data, 0, time.Since(start), stdout.overflow || stderr.overflow}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			r.ExitCode = exit.ExitCode()
			if r.ExitCode < 0 {
				r.ExitCode = 130
			}
			return r, nil
		}
		return r, err
	}
	return r, nil
}

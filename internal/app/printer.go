package app

import (
	"fmt"
	"io"
)

// printer remembers the first output error. Later printing is a no-op.
// It is scoped to one command, is not concurrency-safe, and never owns cleanup.
type printer struct {
	out io.Writer
	err error
}

func (p *printer) println(args ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintln(p.out, args...)
	}
}

func (p *printer) printf(format string, args ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.out, format, args...)
	}
}

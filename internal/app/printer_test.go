package app

import (
	"errors"
	"io"
	"testing"
)

type countedFailure struct{ calls int }

func (w *countedFailure) Write(b []byte) (int, error) {
	w.calls++
	if w.calls == 2 {
		return 0, io.ErrClosedPipe
	}
	return len(b), nil
}

func TestPrinterStopsAtFirstFailure(t *testing.T) {
	w := &countedFailure{}
	p := printer{out: w}
	p.println("first")
	p.printf("%s", "second")
	p.println("must not write")
	p.printf("%s", "also skipped")
	if w.calls != 2 || !errors.Is(p.err, io.ErrClosedPipe) {
		t.Fatalf("calls=%d error=%v", w.calls, p.err)
	}
}

func TestCommandParser(t *testing.T) {
	for _, mode := range []commandName{commandRun, commandExplain, commandCache, commandServe, commandShared} {
		got, err := parseCommand(string(mode))
		if err != nil || got != mode {
			t.Fatalf("parse %q: %q, %v", mode, got, err)
		}
	}
	if _, err := parseCommand("Run"); err == nil {
		t.Fatal("accepted unknown command")
	}
}

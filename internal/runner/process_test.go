package runner

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestStreamsAndExit(t *testing.T) {
	var out, errOut bytes.Buffer
	r, e := Run(context.Background(), t.TempDir(), []string{"/bin/sh", "-c", "printf stdout; printf stderr >&2; exit 7"}, &out, &errOut)
	if e != nil || r.ExitCode != 7 || out.String() != "stdout" || errOut.String() != "stderr" || string(r.Stdout) != "stdout" {
		t.Fatalf("%+v %v %q %q", r, e, out.String(), errOut.String())
	}
}

func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	var out bytes.Buffer
	start := time.Now()
	_, _ = Run(ctx, t.TempDir(), []string{"/bin/sh", "-c", "sleep 30"}, &out, &out)
	if time.Since(start) > 4*time.Second {
		t.Fatal("cancellation stalled")
	}
}

func TestCaptureLimit(t *testing.T) {
	b := limitedBuffer{limit: 3}
	n, e := b.Write([]byte("abcdef"))
	if n != 6 || e != nil || !b.overflow || string(b.data) != "abc" {
		t.Fatalf("%+v", b)
	}
}

func TestChildDoesNotInheritClientToken(t *testing.T) {
	t.Setenv("RECEIPT_TOKEN", "rcpt_secret")
	t.Setenv("RECEIPT_OTHER", "kept")
	var out, errOut bytes.Buffer
	r, e := Run(context.Background(), t.TempDir(), []string{"/bin/sh", "-c", `printf '%s %s' "${RECEIPT_TOKEN-unset}" "$RECEIPT_OTHER"`}, &out, &errOut)
	if e != nil || r.ExitCode != 0 || out.String() != "unset kept" {
		t.Fatalf("%+v %v %q", r, e, out.String())
	}
}

package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sidsharma96/receipt/internal/cache"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestCommandOutputFailureIsReturned(t *testing.T) {
	var diagnostics bytes.Buffer
	_, err := execute(context.Background(), []string{"help"}, failingWriter{}, &diagnostics)
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("output failure was lost: %v", err)
	}
}

func TestDiagnosticFailurePreservesExitStatus(t *testing.T) {
	var output bytes.Buffer
	if code := Main([]string{"unknown"}, &output, failingWriter{}); code != 2 {
		t.Fatalf("diagnostic failure changed error exit status: %d", code)
	}
}

func setup(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	if e := exec.Command("git", "init", "-q", dir).Run(); e != nil {
		t.Fatal(e)
	}
	old, e := os.Getwd()
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chdir(dir); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.Chdir(old) })
	return dir, t.TempDir()
}

func invoke(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code, e := execute(context.Background(), args, &out, &errOut)
	if e != nil {
		t.Fatal(e)
	}
	return code, out.String(), errOut.String()
}

func TestHitInvalidateFailureAndExplain(t *testing.T) {
	dir, external := setup(t)
	counter := filepath.Join(external, "calls")
	script := "echo called >> '" + counter + "'; printf 'PASS\\n'"
	args := []string{"run", "--allow-generic", "--", "/bin/sh", "-c", script}
	code, out, status := invoke(t, args...)
	if code != 0 || out != "PASS\n" || !strings.Contains(status, "miss") {
		t.Fatalf("%d %q %q", code, out, status)
	}
	_, out, status = invoke(t, args...)
	if out != "PASS\n" || !strings.Contains(status, "hit") {
		t.Fatalf("%q %q", out, status)
	}
	b, _ := os.ReadFile(counter)
	if strings.Count(string(b), "called") != 1 {
		t.Fatal("hit executed tests")
	}
	os.WriteFile(filepath.Join(dir, "input.py"), []byte("changed"), 0o600)
	explain := append([]string{"explain"}, args[1:]...)
	_, explanation, _ := invoke(t, explain...)
	if !strings.Contains(explanation, "added file:") {
		t.Fatal(explanation)
	}
	_, _, status = invoke(t, args...)
	if !strings.Contains(status, "miss") {
		t.Fatal(status)
	}
	code, _, status = invoke(t, "run", "--allow-generic", "--", "/bin/sh", "-c", "exit 9")
	if code != 9 || !strings.Contains(status, "not cached") {
		t.Fatalf("%d %q", code, status)
	}
}

func TestChangedInputsNotStored(t *testing.T) {
	dir, _ := setup(t)
	args := []string{"run", "--allow-generic", "--", "/bin/sh", "-c", "echo changed > input.txt"}
	_, _, status := invoke(t, args...)
	if !strings.Contains(status, "not stored") {
		t.Fatal(status)
	}
	entries, _ := (cache.Disk{Dir: filepath.Join(dir, ".receipt")}).List()
	if len(entries) != 0 {
		t.Fatal("changing-input run cached")
	}
}

func TestVerificationQuarantines(t *testing.T) {
	dir, external := setup(t)
	file := filepath.Join(external, "state")
	os.WriteFile(file, []byte("a"), 0o600)
	script := "cat '" + file + "'"
	args := []string{"run", "--allow-generic", "--", "/bin/sh", "-c", script}
	invoke(t, args...)
	os.WriteFile(file, []byte("b"), 0o600)
	verify := []string{"run", "--verify", "--allow-generic", "--", "/bin/sh", "-c", script}
	code, _, status := invoke(t, verify...)
	if code != 1 || !strings.Contains(status, "quarantined") {
		t.Fatalf("%d %q", code, status)
	}
	_, _, status = invoke(t, args...)
	if !strings.Contains(status, "not stored") {
		t.Fatal(status)
	}
	entries, _ := (cache.Disk{Dir: filepath.Join(dir, ".receipt")}).List()
	if len(entries) != 0 {
		t.Fatal("quarantined result re-published")
	}
}

func TestPruneAndBypass(t *testing.T) {
	dir, _ := setup(t)
	invoke(t, "run", "--allow-generic", "--", "/bin/sh", "-c", "printf hello")
	invoke(t, "cache", "prune", "--max-bytes", "0")
	entries, _ := (cache.Disk{Dir: filepath.Join(dir, ".receipt")}).List()
	if len(entries) != 0 {
		t.Fatal("prune left entries")
	}
	invoke(t, "run", "--bypass", "--", "/bin/sh", "-c", "printf bypass")
	entries, _ = (cache.Disk{Dir: filepath.Join(dir, ".receipt")}).List()
	if len(entries) != 0 {
		t.Fatal("bypass stored result")
	}
}

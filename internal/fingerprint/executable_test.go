package fingerprint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/sidsharma96/receipt/internal/observe"
)

func TestExecutableIdentityMatchesLegacy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool")
	for _, size := range []int{0, 1, 32767, 32768, 32769, 12 << 20} {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i*31 + i/257)
		}
		if err := os.WriteFile(path, data, 0o700); err != nil {
			t.Fatal(err)
		}
		want := sha256.Sum256(data)
		for _, header := range []bool{false, true} {
			got, body, err := executableInput(context.Background(), path, header)
			if err != nil {
				t.Fatal(err)
			}
			if got != (Input{ExecutableInput, path, hex.EncodeToString(want[:])}) {
				t.Fatalf("size %d: %+v", size, got)
			}
			if header && !bytes.Equal(body, data) || !header && body != nil {
				t.Fatal("header policy changed")
			}
		}
	}
}

func TestExecutableSymlinkAndMutation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(path, []byte("first"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	before, err := executionInputs(context.Background(), dir, []string{link}, true)
	if err != nil {
		t.Fatal(err)
	}
	if before[0].Name != link || before[0].Hash != Hash([]byte("first")) {
		t.Fatal(before)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("other"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, err := executionInputs(context.Background(), dir, []string{link}, true)
	if err != nil {
		t.Fatal(err)
	}
	if after[0].Hash == before[0].Hash {
		t.Fatal("same-size/mtime edit was cached")
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	modeOnly, err := executionInputs(context.Background(), dir, []string{link}, true)
	if err != nil || modeOnly[0] != after[0] {
		t.Fatal("executable permission bits entered identity", err)
	}
}

func TestPytestHeaderPreservesUnboundedFirstLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pytest")
	for _, suffix := range []string{"", "\nbody\x00\xff"} {
		data := []byte("#!/" + string(bytes.Repeat([]byte("x"), 70000)) + suffix)
		if err := os.WriteFile(path, data, 0o700); err != nil {
			t.Fatal(err)
		}
		input, body, err := executableInput(context.Background(), path, true)
		if err != nil {
			t.Fatal(err)
		}
		probe, err := toolchainProbe([]string{path}, body, false)
		if err != nil || len(probe) == 0 || len(probe[0]) != 70001 || input.Hash != Hash(data) {
			t.Fatal("long shebang changed", err)
		}
	}
	for _, data := range [][]byte{[]byte("#!/usr/bin/env python\n"), []byte("no shebang")} {
		if _, err := toolchainProbe([]string{path}, data, false); err == nil {
			t.Fatal("invalid shebang accepted")
		}
	}
}

func TestProbeRechecksOutputAndRecordsFailures(t *testing.T) {
	dir := t.TempDir()
	ctx, trace := observe.Start(context.Background())
	args := []string{"python", "-m", "pytest"}
	t.Setenv("RECEIPT_PROBE_FIXTURE", "first")
	probe := []string{"/bin/sh", "-c", `printf '%s' "$RECEIPT_PROBE_FIXTURE"`}
	first, err := runtimeProbe(ctx, dir, args, probe)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RECEIPT_PROBE_FIXTURE", "second")
	second, err := runtimeProbe(ctx, dir, args, probe)
	if err != nil {
		t.Fatal(err)
	}
	if first.Hash != Hash([]byte("first")) || second.Hash != Hash([]byte("second")) {
		t.Fatal("probe output cached or changed")
	}
	if _, err := runtimeProbe(ctx, dir, args, []string{"/bin/sh", "-c", "exit 7"}); err == nil {
		t.Fatal("probe failure ignored")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := runtimeProbe(canceled, dir, args, probe); err == nil {
		t.Fatal("cancellation ignored")
	}
	if _, _, err := executableInput(ctx, filepath.Join(dir, "missing"), false); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, _, err := executableInput(ctx, dir, false); err == nil {
		t.Fatal("read failure ignored")
	}
	got := trace.Snapshot()
	if len(got) != 2 || got[0].Stage != "executable" || got[0].Calls != 2 || got[1].Stage != "toolchain" || got[1].Calls != 4 {
		t.Fatalf("%+v", got)
	}
}

func TestFingerprintSubstagesDoNotChangeKey(t *testing.T) {
	dir := repo(t)
	if err := os.WriteFile(filepath.Join(dir, "input"), []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"/bin/sh", "-c", "true"}
	before, err := Build(context.Background(), dir, args, nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	ctx, trace := observe.Start(context.Background())
	after, err := Build(ctx, dir, args, nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if before.Key() != after.Key() {
		t.Fatal("timing changed identity")
	}
	seen := map[string]int64{}
	for _, stage := range trace.Snapshot() {
		seen[stage.Stage] = stage.Calls
	}
	if seen["discovery"] != 2 || seen["input_files"] != 1 || seen["executable"] != 1 || seen["fingerprint"] != 1 {
		t.Fatal(seen)
	}
}

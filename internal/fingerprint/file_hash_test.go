package fingerprint

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The oracle preserves the pre-streaming byte construction and uses SHA-256
// directly, independently of the production hashFile and Hash helpers.
func TestStreamingFileInputMatchesLegacy(t *testing.T) {
	for _, size := range []int{0, 1, 4096, 32767, 32768, 32769, 131079, 8 << 20} {
		for _, mode := range []os.FileMode{0o400, 0o600, 0o644, 0o755} {
			t.Run(fmt.Sprintf("%d/%o", size, mode), func(t *testing.T) {
				checkLegacyFileInput(t, size, mode)
			})
		}
	}
}

// checkLegacyFileInput compares one filesystem observation with the old encoding.
func checkLegacyFileInput(t *testing.T, size int, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input")
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i*37 + i/251)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	got, err := fileInput(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(append([]byte(fmt.Sprintf("%o\x00", info.Mode().Perm())), data...))
	want := Input{FileInput, path, hex.EncodeToString(digest[:])}
	if got != want {
		t.Fatalf("input = %+v, want %+v", got, want)
	}
	old := Manifest{Version: 1, Root: "/fixture", Directory: "/fixture", Platform: "fixture", Command: []string{"go", "test"}, Inputs: []Input{want}}
	next := old
	next.Inputs = []Input{got}
	if old.Key() != next.Key() {
		t.Fatal("manifest key changed")
	}
}

func TestHashFileSizeHintDoesNotTruncate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input")
	data := bytes.Repeat([]byte{0, 255, 42}, 30000)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, hint := range []int64{-1, 0, 1, 32768, 1 << 40} {
		got, err := hashFile(path, "600\x00", hint)
		if err != nil {
			t.Fatal(err)
		}
		if got != Hash(append([]byte("600\x00"), data...)) {
			t.Fatalf("hint %d truncated data", hint)
		}
	}
}

func TestStreamingFileFailures(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	got, err := fileInput(missing)
	if err != nil || got != (Input{FileInput, missing, Hash([]byte("deleted"))}) {
		t.Fatalf("deleted marker: %+v, %v", got, err)
	}
	if _, err := hashFile(missing, "", 0); !os.IsNotExist(err) {
		t.Fatalf("open failure: %v", err)
	}
	if _, err := fileInput(dir); err == nil {
		t.Fatal("accepted directory")
	}
	// Opening a directory succeeds on Unix, but reading its contents as file
	// bytes fails. This exercises propagation of an error after a successful open.
	if _, err := hashFile(dir, "", 4096); err == nil {
		t.Fatal("ignored read failure")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(missing, link); err != nil {
		t.Fatal(err)
	}
	if _, err := fileInput(link); err == nil {
		t.Fatal("accepted symlink")
	}
}

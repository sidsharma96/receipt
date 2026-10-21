package app

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/sidsharma96/receipt/internal/cache"
)

func TestQuarantineMarkerIsSynced(t *testing.T) {
	var synced []string
	original := syncDir
	syncDir = func(path string) error { synced = append(synced, filepath.Clean(path)); return original(path) }
	t.Cleanup(func() { syncDir = original })
	dir := t.TempDir()
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := quarantine(dir, key); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(synced, filepath.Join(dir, "quarantine")) {
		t.Fatalf("quarantine marker rename not synced: %v", synced)
	}
}

func TestPruneRemovesStalePendingEntries(t *testing.T) {
	d := cache.Disk{Dir: t.TempDir()}
	entries := filepath.Join(d.Dir, "entries")
	if err := os.MkdirAll(entries, 0o700); err != nil {
		t.Fatal(err)
	}
	stale, fresh := filepath.Join(entries, ".pending-stale"), filepath.Join(entries, ".pending-fresh")
	for _, p := range []string{stale, fresh} {
		if err := os.WriteFile(p, []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if err := prune(d, 1<<30, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale pending file kept", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("pending file of a possibly running publication removed", err)
	}
}

package cache

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

func recordSyncs(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var synced []string
	original := syncDirectory
	syncDirectory = func(path string) error {
		mu.Lock()
		synced = append(synced, filepath.Clean(path))
		mu.Unlock()
		return original(path)
	}
	t.Cleanup(func() { syncDirectory = original })
	return func() []string { mu.Lock(); defer mu.Unlock(); return slices.Clone(synced) }
}

func TestNewDirectoriesAndRemovalsAreSynced(t *testing.T) {
	synced := recordSyncs(t)
	root := t.TempDir()
	d := Disk{Dir: filepath.Join(root, "cache")}
	v := example()
	if err := d.Put(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{root, d.Dir, filepath.Join(d.Dir, "entries")} {
		if !slices.Contains(synced(), dir) {
			t.Fatalf("%s not synced after creating a name in it: %v", dir, synced())
		}
	}
	blobs := FileBlobs{Dir: filepath.Join(root, "blobs")}
	ref, err := blobs.Put(context.Background(), []byte("output"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(synced(), blobs.Dir) {
		t.Fatalf("blob root not synced after creating shard %s: %v", ref.Hash[:2], synced())
	}
	before := len(synced())
	if err := d.Remove(v.Key); err != nil {
		t.Fatal(err)
	}
	if after := synced(); len(after) == before || after[len(after)-1] != filepath.Join(d.Dir, "entries") {
		t.Fatalf("entry removal not synced: %v", after[before:])
	}
}

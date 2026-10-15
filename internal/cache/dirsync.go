package cache

import (
	"io/fs"
	"os"
	"path/filepath"
)

// syncDirectory flushes a directory's own entries, so that names created or
// removed in it survive a crash. Tests replace it to see which directories
// were synced; the flush itself cannot be observed without losing power.
var syncDirectory = func(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close() //nolint:errcheck // Read/resource cleanup; the sync result is returned.
	return dir.Sync()
}

// SyncDir makes the creation or removal of names in the directory path durable.
func SyncDir(path string) error { return syncDirectory(path) }

// MakeDirs creates path and any missing parents, like os.MkdirAll, then syncs
// the parent of each directory it created, so the new names survive a crash.
func MakeDirs(path string, perm fs.FileMode) error {
	var missing []string
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		_, err := os.Lstat(p)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return err
		}
		missing = append(missing, p)
		if filepath.Dir(p) == p {
			break
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], perm); err != nil && !os.IsExist(err) {
			return err
		}
		if err := syncDirectory(filepath.Dir(missing[i])); err != nil {
			return err
		}
	}
	return nil
}

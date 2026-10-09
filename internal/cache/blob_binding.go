package cache

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Bind associates this directory with one metadata store; the caller holds the lifecycle filesystem lock. A marker prevents another
// metadata database/schema from treating this directory as its own orphans.
func (s FileBlobs) Bind(ctx context.Context, id string, create bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validKey.MatchString(id) {
		return fmt.Errorf("invalid store identity")
	}
	root, err := s.Location()
	if err != nil {
		return err
	}
	path := filepath.Join(root, ".store-id")
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Size() != 65 {
			return fmt.Errorf("unsafe store identity marker")
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if string(b) != id+"\n" {
			return fmt.Errorf("blob directory belongs to another metadata store")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	if !create {
		return fmt.Errorf("store identity marker missing; restore binding before maintenance")
	}
	// A synced create-if-absent marker is atomic. Crash leftovers are inspectable.
	f, err := os.CreateTemp(root, ".pending-bind-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) //nolint:errcheck // Best-effort temp cleanup; publication errors are checked separately.
	_, err = f.WriteString(id + "\n")
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Link(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(root)
	if err != nil {
		return err
	}
	defer dir.Close() //nolint:errcheck // Read/resource cleanup; writes and syncs are checked before return.
	return dir.Sync()
}

func bindingPending(name string) bool { return strings.HasPrefix(name, ".pending-bind-") }

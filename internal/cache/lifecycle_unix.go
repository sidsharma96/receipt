//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package cache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Keep filesystem exclusion even if the database session loses its lock.
func lockBlobDirectory(ctx context.Context, root string, exclusive bool) (func(), error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("unsafe blob root")
	}
	f, err := openLockFile(filepath.Join(root, ".lifecycle.lock"))
	if err != nil {
		return nil, err
	}
	operation := syscall.LOCK_SH | syscall.LOCK_NB
	if exclusive {
		operation = syscall.LOCK_EX | syscall.LOCK_NB
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			f.Close() //nolint:errcheck // Resource cleanup must not replace the primary operation error.
			return nil, err
		}
		err = syscall.Flock(int(f.Fd()), operation)
		if err == nil {
			return func() { f.Close() }, nil //nolint:errcheck // Resource cleanup must not replace the primary operation error.
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			f.Close() //nolint:errcheck // Resource cleanup must not replace the primary operation error.
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close() //nolint:errcheck // Resource cleanup must not replace the primary operation error.
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// openLockFile refuses a symlink in the open call itself, then checks the file
// it actually opened. A separate Lstat before the open would leave a window in
// which a symlink could be swapped in.
func openLockFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if errors.Is(err, syscall.ELOOP) {
		return nil, fmt.Errorf("unsafe lifecycle lock")
	}
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("unsafe lifecycle lock")
	}
	if err != nil {
		f.Close() //nolint:errcheck // Resource cleanup must not replace the primary operation error.
		return nil, err
	}
	return f, nil
}

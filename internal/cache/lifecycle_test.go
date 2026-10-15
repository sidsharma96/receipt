//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package cache

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLifecycleProcessHelper(t *testing.T) {
	dir := os.Getenv("RECEIPT_LOCK_WORKER_DIR")
	if dir == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	unlock, err := lockBlobDirectory(ctx, dir, true)
	if os.Getenv("RECEIPT_LOCK_EXPECT_BLOCKED") == "1" {
		if !errors.Is(err, context.DeadlineExceeded) {
			if unlock != nil {
				unlock()
			}
			t.Fatal("lock not shared between processes", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	unlock()
}

func TestLifecycleIndependentProcesses(t *testing.T) {
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := lockBlobDirectory(context.Background(), dir, true)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestLifecycleProcessHelper$")
	cmd.Env = append(os.Environ(), "RECEIPT_LOCK_WORKER_DIR="+dir, "RECEIPT_LOCK_EXPECT_BLOCKED=1")
	b, err := cmd.CombinedOutput()
	unlock()
	if err != nil {
		t.Fatalf("%v %s", err, b)
	}
	cmd = exec.Command(exe, "-test.run=^TestLifecycleProcessHelper$")
	cmd.Env = append(os.Environ(), "RECEIPT_LOCK_WORKER_DIR="+dir, "RECEIPT_LOCK_EXPECT_BLOCKED=0")
	if b, err = cmd.CombinedOutput(); err != nil {
		t.Fatalf("lock leaked: %v %s", err, b)
	}
}

func TestInventoryRejectsUnsafeDeletionAndCollectsKnownTemporaries(t *testing.T) {
	ctx := context.Background()
	s := FileBlobs{Dir: t.TempDir()}
	unlock, err := lockBlobDirectory(ctx, s.Dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err = s.Bind(ctx, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true); err != nil {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(s.Dir, ".pending-bind-")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString("partial"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	objects, err := s.Inventory(ctx)
	if err != nil || len(objects) != 1 || !objects[0].Pending {
		t.Fatal(objects, err)
	}
	if err = s.Remove(ctx, BlobObject{Relative: "../../escape", Pending: true}); err == nil {
		t.Fatal("unsafe path removed")
	}
	if err = s.Remove(ctx, objects[0]); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(s.Dir, ".store-id")); err != nil {
		t.Fatal("binding removed", err)
	}
	if _, err = os.Stat(filepath.Join(s.Dir, ".lifecycle.lock")); err != nil {
		t.Fatal("lock removed", err)
	}
}

// The Lstat check in lockBlobDirectory runs before the open, so a symlink
// swapped in between would be followed. Opening must refuse it by itself.
func TestLockFileOpenRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere")
	if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".lifecycle.lock")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	f, err := openLockFile(link)
	if err == nil {
		f.Close()
		t.Fatal("lock file opened through a symlink")
	}
}

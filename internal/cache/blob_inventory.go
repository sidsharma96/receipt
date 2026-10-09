package cache

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"receipt/internal/observe"
	"regexp"
	"strings"
)

// BlobObject describes an inventory item relative to the managed store root.
type BlobObject struct {
	Hash     string
	Relative string
	Size     int64
	Pending  bool
}

// ManagedBlobs adds filesystem lifecycle capabilities required by local maintenance.
type ManagedBlobs interface {
	BlobStore
	Location() (string, error)
	Inventory(context.Context) ([]BlobObject, error)
	Remove(context.Context, BlobObject) error
	Bind(context.Context, string, bool) error
}

var shardName = regexp.MustCompile(`^[a-f0-9]{2}$`)

// Location resolves the canonical local blob directory.
func (s FileBlobs) Location() (string, error) {
	if s.Dir == "" {
		return "", fmt.Errorf("explicit blob directory required")
	}
	p, err := filepath.Abs(s.Dir)
	if err != nil {
		return "", err
	}
	if p == string(os.PathSeparator) {
		return "", fmt.Errorf("unsafe blob root")
	}
	return filepath.Clean(p), nil
}

// Inventory lists recognized regular objects and rejects unsafe directory layouts.
func (s FileBlobs) Inventory(ctx context.Context) ([]BlobObject, error) {
	defer observe.Measure(ctx, observe.Files)()
	root, err := s.Location()
	if err != nil {
		return nil, err
	}
	var objects []BlobObject
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == root && os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in blob directory")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		object, err := inventoryItem(rel, d)
		if err != nil {
			return err
		}
		if object != nil {
			objects = append(objects, *object)
		}
		return nil
	})
	return objects, err
}

// inventoryItem validates layout before a file becomes a deletion candidate.
func inventoryItem(rel string, d fs.DirEntry) (*BlobObject, error) {
	if rel == "." {
		if !d.IsDir() {
			return nil, fmt.Errorf("blob root is not a directory")
		}
		return nil, nil
	}
	parts := strings.Split(rel, string(os.PathSeparator))
	if len(parts) == 1 && !d.IsDir() {
		return inventoryRootFile(rel, d)
	}
	if d.IsDir() {
		if len(parts) != 1 || !shardName.MatchString(parts[0]) {
			return nil, fmt.Errorf("unexpected directory in blob store")
		}
		return nil, nil
	}
	if len(parts) != 2 || !shardName.MatchString(parts[0]) {
		return nil, fmt.Errorf("unexpected file in blob store")
	}
	name := parts[1]
	pending := strings.HasPrefix(name, ".pending-")
	if !pending && (!validKey.MatchString(name) || !strings.HasPrefix(name, parts[0])) {
		return nil, fmt.Errorf("unexpected object name")
	}
	info, err := d.Info()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("nonregular blob object")
	}
	hash := name
	if pending {
		hash = ""
	}
	return &BlobObject{hash, rel, info.Size(), pending}, nil
}

func inventoryRootFile(rel string, d fs.DirEntry) (*BlobObject, error) {
	if !bindingPending(rel) && rel != ".store-id" && rel != ".lifecycle.lock" {
		return nil, fmt.Errorf("unexpected file in blob store")
	}
	info, err := d.Info()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("unsafe store root file")
	}
	if bindingPending(rel) {
		return &BlobObject{Relative: rel, Size: info.Size(), Pending: true}, nil
	}
	if rel == ".store-id" && info.Size() != 65 {
		return nil, fmt.Errorf("invalid store identity marker")
	}
	return nil, nil
}

// Remove deletes a validated inventory object; the caller must hold the lifecycle lock.
func (s FileBlobs) Remove(ctx context.Context, o BlobObject) error {
	defer observe.Measure(ctx, observe.Files)()
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := s.Location()
	if err != nil {
		return err
	}
	parts := strings.Split(o.Relative, string(os.PathSeparator))
	rootPending := len(parts) == 1 && o.Pending && bindingPending(parts[0])
	if !rootPending && (len(parts) != 2 || !shardName.MatchString(parts[0])) {
		return fmt.Errorf("invalid object path")
	}
	if !rootPending {
		if o.Pending {
			if !strings.HasPrefix(parts[1], ".pending-") {
				return fmt.Errorf("invalid pending object")
			}
		} else if !validKey.MatchString(parts[1]) || !strings.HasPrefix(parts[1], parts[0]) || parts[1] != o.Hash {
			return fmt.Errorf("invalid object hash")
		}
	}
	directories := []string{root}
	if !rootPending {
		directories = append(directories, filepath.Join(root, parts[0]))
	}
	for _, p := range directories {
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe blob directory")
		}
	}
	path := filepath.Join(root, o.Relative)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != o.Size {
		return fmt.Errorf("blob changed during collection")
	}
	if err = os.Remove(path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close() //nolint:errcheck // Read/resource cleanup; writes and syncs are checked before return.
	return dir.Sync()
}

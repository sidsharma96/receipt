// Package cache implements local and shared result storage and its trusted-team HTTP API.
package cache

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"receipt/internal/fingerprint"
	"receipt/internal/observe"
)

// BlobStore stores immutable bytes by SHA-256. Put must not acknowledge an
// existing object without checking its bytes. Metadata is published afterward.
type BlobStore interface {
	Get(context.Context, BlobRef) ([]byte, error)
	Put(context.Context, []byte) (BlobRef, error)
}

// BlobRef identifies immutable bytes by their SHA-256 hash and expected length.
type BlobRef struct {
	Hash string `json:"hash"`
	Size int    `json:"size"`
}

// MaxBlobBytes bounds a single captured output stream.
const MaxBlobBytes = 8 << 20

// FileBlobs stores content-addressed objects in a local directory.
type FileBlobs struct{ Dir string }

func (s FileBlobs) path(hash string) (string, error) {
	if !validKey.MatchString(hash) {
		return "", fmt.Errorf("invalid blob hash")
	}
	return filepath.Join(s.Dir, hash[:2], hash), nil
}

// Get verifies the length and hash before returning an object's bytes.
func (s FileBlobs) Get(ctx context.Context, ref BlobRef) ([]byte, error) {
	defer observe.Measure(ctx, observe.Files)()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ref.Size < 0 || ref.Size > MaxBlobBytes {
		return nil, ErrCorrupt
	}
	p, err := s.path(ref.Hash)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if os.IsNotExist(err) {
		return nil, ErrMiss
	}
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // Read/resource cleanup; writes and syncs are checked before return.
	b, err := io.ReadAll(io.LimitReader(f, MaxBlobBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) != ref.Size || fingerprint.Hash(b) != ref.Hash {
		return nil, ErrCorrupt
	}
	return b, nil
}

// Put durably publishes immutable bytes without overwriting an existing object.
func (s FileBlobs) Put(ctx context.Context, b []byte) (BlobRef, error) {
	defer observe.Measure(ctx, observe.Files)()
	ref := BlobRef{Hash: fingerprint.Hash(b), Size: len(b)}
	if len(b) > MaxBlobBytes {
		return ref, fmt.Errorf("blob exceeds limit")
	}
	if err := ctx.Err(); err != nil {
		return ref, err
	}
	p, err := s.path(ref.Hash)
	if err != nil {
		return ref, err
	}
	if err := MakeDirs(filepath.Dir(p), 0o700); err != nil {
		return ref, err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".pending-")
	if err != nil {
		return ref, err
	}
	defer os.Remove(f.Name()) //nolint:errcheck // Best-effort temp cleanup; publication errors are checked separately.
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return ref, err
	}
	if closeErr != nil {
		return ref, closeErr
	}
	if err = os.Link(f.Name(), p); os.IsExist(err) {
		// Corrupt immutable objects are not silently repaired or overwritten.
		_, err = s.Get(ctx, ref)
	}
	if err != nil {
		return ref, err
	}
	return ref, syncDirectory(filepath.Dir(p))
}

package fingerprint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"receipt/internal/observe"
)

// Bound extra Cargo reads independently of the ordinary Git input scan.
const (
	cargoMaxFiles = 20000
	cargoMaxBytes = 256 << 20
)

type cargoSourceScan struct {
	bytes int64
	files []Input
}

func (s *cargoSourceScan) add(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("cargo source must be a regular file")
	}
	if len(s.files) >= cargoMaxFiles || info.Size() > cargoMaxBytes-s.bytes {
		return fmt.Errorf("cargo dependency inventory exceeds scan budget")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck // Read-only cleanup; read failures are returned.
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, cargoMaxBytes-s.bytes+1))
	if err != nil {
		return err
	}
	s.bytes += n
	if s.bytes > cargoMaxBytes {
		return fmt.Errorf("cargo dependency inventory exceeds byte budget")
	}
	s.files = append(s.files, Input{FileInput, path, hex.EncodeToString(h.Sum(nil))})
	return nil
}

func cargoSources(ctx context.Context, m cargoMetadata, packages []cargoPackage, root, home string) ([]Input, error) {
	defer observe.Measure(ctx, observe.CargoSources)()
	var scan cargoSourceScan
	// Cargo.lock is often gitignored for libraries. It must still be recorded.
	if err := scan.add(ctx, filepath.Join(m.Workspace, "Cargo.lock")); err != nil {
		return nil, fmt.Errorf("cargo requires a readable existing lockfile: %w", err)
	}
	for _, p := range packages {
		if p.Source == nil {
			if err := scan.local(ctx, p, root); err != nil {
				return nil, err
			}
			continue
		}
		dir := filepath.Dir(p.Manifest)
		if !within(filepath.Join(home, "registry", "src"), dir) {
			return nil, fmt.Errorf("cargo registry source outside supported cache layout")
		}
		if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			return scan.add(ctx, path)
		}); err != nil {
			return nil, err
		}
	}
	b, err := json.Marshal(scan.files)
	if err != nil {
		return nil, err
	}
	return []Input{{ToolchainInput, "cargo-sources-v1", Hash(b)}}, nil
}

func (s *cargoSourceScan) local(ctx context.Context, p cargoPackage, root string) error {
	// Local sources retain Receipt's declared Git/extra-input contract.
	// Explicitly capture ignored manifests and target entry points as well.
	if !within(root, p.Manifest) {
		return fmt.Errorf("external cargo path dependency")
	}
	if err := s.add(ctx, p.Manifest); err != nil {
		return err
	}
	for _, target := range p.Targets {
		if err := s.add(ctx, target.Source); err != nil {
			return err
		}
	}
	return nil
}

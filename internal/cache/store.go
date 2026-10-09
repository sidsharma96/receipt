package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"receipt/internal/fingerprint"
	"regexp"
	"sort"
	"time"
)

// MaxEntryBytes bounds a serialized local entry or HTTP payload.
const MaxEntryBytes = 32 << 20

var (
	// ErrMiss indicates that no reusable entry was found.
	ErrMiss = errors.New("cache miss")
	// ErrCorrupt indicates an invalid or damaged persisted result.
	ErrCorrupt = errors.New("corrupt cache entry")
	validKey   = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// Result records an observation and the declared inputs used to identify it.
type Result struct {
	Version    int                  `json:"version"`
	Key        string               `json:"key"`
	Manifest   fingerprint.Manifest `json:"manifest"`
	Stdout     []byte               `json:"stdout"`
	Stderr     []byte               `json:"stderr"`
	ExitCode   int                  `json:"exit_code"`
	DurationNS int64                `json:"duration_ns"`
	CreatedAt  time.Time            `json:"created_at"`
}

// Store provides lookup and publication of validated successful results.
type Store interface {
	Get(context.Context, string) (Result, error)
	Put(context.Context, Result) error
}

type envelope struct {
	Digest  string          `json:"digest"`
	Payload json.RawMessage `json:"payload"`
}

// Validate checks the reusable-result contract and manifest/key agreement.
func Validate(r Result) error {
	var errs []error
	if r.Version != 1 {
		errs = append(errs, fmt.Errorf("result.version: expected 1"))
	}
	if err := r.Manifest.Validate(); err != nil {
		errs = append(errs, err)
	}
	if !validKey.MatchString(r.Key) || r.Key != r.Manifest.Key() {
		errs = append(errs, fmt.Errorf("result.key: does not match manifest"))
	}
	if r.ExitCode != 0 {
		errs = append(errs, fmt.Errorf("result.exit_code: reusable results require success"))
	}
	if r.DurationNS < 0 {
		errs = append(errs, fmt.Errorf("result.duration_ns: must be nonnegative"))
	}
	if len(r.Stdout) > MaxBlobBytes || len(r.Stderr) > MaxBlobBytes {
		errs = append(errs, fmt.Errorf("result output exceeds stream limit"))
	}
	return errors.Join(errs...)
}

// Encode validates a result and wraps its JSON in a checksummed envelope.
func Encode(r Result) ([]byte, error) {
	if e := Validate(r); e != nil {
		return nil, e
	}
	b, e := json.Marshal(r)
	if e != nil {
		return nil, e
	}
	out, e := json.Marshal(envelope{fingerprint.Hash(b), b})
	if len(out) > MaxEntryBytes {
		return nil, fmt.Errorf("result exceeds %d bytes", MaxEntryBytes)
	}
	return out, e
}

// Decode verifies the envelope checksum and reusable-result contract.
func Decode(b []byte) (Result, error) {
	var env envelope
	var r Result
	if len(b) > MaxEntryBytes {
		return Result{}, fmt.Errorf("%w: entry exceeds byte limit", ErrCorrupt)
	}
	if err := json.Unmarshal(b, &env); err != nil {
		return Result{}, fmt.Errorf("%w: envelope: %w", ErrCorrupt, err)
	}
	if fingerprint.Hash(env.Payload) != env.Digest {
		return Result{}, fmt.Errorf("%w: payload checksum mismatch", ErrCorrupt)
	}
	if err := json.Unmarshal(env.Payload, &r); err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	return r, nil
}

// Disk stores version-one result envelopes in local files.
type Disk struct{ Dir string }

func (d Disk) path(key string) (string, error) {
	if !validKey.MatchString(key) {
		return "", fmt.Errorf("invalid cache key")
	}
	return filepath.Join(d.Dir, "entries", key+".json"), nil
}

// Get reads a bounded entry and validates its checksum and requested key.
func (d Disk) Get(ctx context.Context, key string) (Result, error) {
	var r Result
	if e := ctx.Err(); e != nil {
		return r, e
	}
	p, e := d.path(key)
	if e != nil {
		return r, e
	}
	f, e := os.Open(p)
	if os.IsNotExist(e) {
		return r, ErrMiss
	}
	if e != nil {
		return r, e
	}
	defer f.Close() //nolint:errcheck // Read/resource cleanup; writes and syncs are checked before return.
	b, e := io.ReadAll(io.LimitReader(f, MaxEntryBytes+1))
	if e != nil {
		return r, e
	}
	r, e = Decode(b)
	if e == nil && r.Key != key {
		return r, ErrCorrupt
	}
	return r, e
}

// Put publishes complete entries using a hard link (atomic create-if-absent).
// The first successful writer wins; competing observations never overwrite it.
func (d Disk) Put(ctx context.Context, r Result) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	p, e := d.path(r.Key)
	if e != nil {
		return e
	}
	b, e := Encode(r)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(p), 0o700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(p), ".pending-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp) //nolint:errcheck // Best-effort temp cleanup; publication errors are checked separately.
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	e = os.Link(tmp, p)
	if os.IsExist(e) {
		// First publication wins only if the existing entry is actually reusable.
		// A directory or corrupt file at the destination is not a successful Put.
		_, e = d.Get(ctx, r.Key)
		return e
	}
	if e != nil {
		return e
	}
	dir, e := os.Open(filepath.Dir(p))
	if e != nil {
		return e
	}
	defer dir.Close() //nolint:errcheck // Read/resource cleanup; writes and syncs are checked before return.
	return dir.Sync()
}

// Remove deletes an entry if it exists.
func (d Disk) Remove(key string) error {
	p, e := d.path(key)
	if e != nil {
		return e
	}
	e = os.Remove(p)
	if os.IsNotExist(e) {
		return nil
	}
	return e
}

// List returns valid local entries sorted by creation time.
func (d Disk) List() ([]Result, error) {
	paths, e := filepath.Glob(filepath.Join(d.Dir, "entries", "*.json"))
	if e != nil {
		return nil, e
	}
	var out []Result
	for _, p := range paths {
		k := filepath.Base(p)
		r, e := d.Get(context.Background(), k[:len(k)-5])
		if e != nil {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

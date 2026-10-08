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

const MaxEntryBytes = 32 << 20

var ErrMiss = errors.New("cache miss")
var ErrCorrupt = errors.New("corrupt cache entry")
var validKey = regexp.MustCompile(`^[a-f0-9]{64}$`)

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

type Store interface {
	Get(context.Context, string) (Result, error)
	Put(context.Context, Result) error
}

type envelope struct {
	Digest  string          `json:"digest"`
	Payload json.RawMessage `json:"payload"`
}

func Validate(r Result) error {
	if r.Version != 1 || r.Manifest.Version != 1 || !validKey.MatchString(r.Key) || r.Key != r.Manifest.Key() || r.ExitCode != 0 {
		return fmt.Errorf("invalid result contract")
	}
	return nil
}
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
func Decode(b []byte) (Result, error) {
	var env envelope
	var r Result
	if len(b) > MaxEntryBytes || json.Unmarshal(b, &env) != nil || fingerprint.Hash(env.Payload) != env.Digest || json.Unmarshal(env.Payload, &r) != nil || Validate(r) != nil {
		return r, ErrCorrupt
	}
	return r, nil
}

type Disk struct{ Dir string }

func (d Disk) path(key string) (string, error) {
	if !validKey.MatchString(key) {
		return "", fmt.Errorf("invalid cache key")
	}
	return filepath.Join(d.Dir, "entries", key+".json"), nil
}
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
	defer f.Close()
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
	if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(p), ".pending-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
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
		return nil
	}
	if e != nil {
		return e
	}
	dir, e := os.Open(filepath.Dir(p))
	if e != nil {
		return e
	}
	defer dir.Close()
	return dir.Sync()
}
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

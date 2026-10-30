package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/sidsharma96/receipt/internal/cache"
	"github.com/sidsharma96/receipt/internal/fingerprint"
	"github.com/sidsharma96/receipt/internal/observe"
	"github.com/sidsharma96/receipt/internal/runner"
)

// runSession groups the immutable inputs and dependencies of one invocation.
// Cancellation stays explicit in method arguments; this is not shared process state.
type runSession struct {
	options                options
	config                 Config
	cacheDir, key, history string
	disk                   cache.Disk
	remote                 cache.Remote
	manifest               fingerprint.Manifest
	out, errOut            io.Writer
}

func (s runSession) explain(ctx context.Context) error {
	p := printer{out: s.out}
	p.println("key:", s.key)
	if quarantined(s.cacheDir, s.key) {
		p.println("quarantined: conflicting observations")
	}
	if _, err := s.disk.Get(ctx, s.key); err == nil {
		p.println("local cache: hit")
	} else {
		p.println("local cache: miss")
	}
	var old fingerprint.Manifest
	b, err := os.ReadFile(s.history)
	if err != nil || json.Unmarshal(b, &old) != nil {
		p.println("no previous manifest")
		return p.err
	}
	changes := fingerprint.Diff(old, s.manifest)
	if len(changes) == 0 {
		p.println("declared inputs unchanged")
	}
	for _, change := range changes {
		p.println(change)
	}
	return p.err
}

func (s runSession) recordHistory() error {
	b, err := json.Marshal(s.manifest)
	if err != nil {
		return err
	}
	if err := atomically(s.history, b); err != nil {
		diagnostic(s.errOut, "receipt: optional local cache bookkeeping failed:", err)
	}
	return nil
}

func (s runSession) lookup(ctx context.Context) (cache.Result, bool, error) {
	defer observe.Measure(ctx, observe.Lookup)()
	if s.options.refresh {
		prior, err := s.disk.Get(ctx, s.key)
		if err != nil && !errors.Is(err, cache.ErrMiss) {
			diagnostic(s.errOut, "receipt: refresh lookup unavailable:", err)
		}
		return prior, false, nil
	}
	if quarantined(s.cacheDir, s.key) {
		return cache.Result{}, false, nil
	}
	prior, err := s.disk.Get(ctx, s.key)
	if err != nil {
		if errors.Is(err, cache.ErrCorrupt) {
			diagnostic(s.errOut, "receipt: corrupt local entry; running uncached or fetching remote")
			s.removeLocal()
		}
		return s.lookupRemote(ctx)
	}
	if s.remote.URL == "" {
		return prior, true, nil
	}
	// A local copy cannot authorize replay when shared status is unavailable.
	if err := s.remote.Check(ctx, s.key); err != nil {
		if errors.Is(err, cache.ErrConflict) {
			if quarantineErr := s.quarantineLocal(); quarantineErr != nil {
				return prior, false, quarantineErr
			}
		}
		diagnostic(s.errOut, "receipt: shared status unavailable or quarantined; executing instead of local replay:", err)
		return prior, false, nil
	}
	return prior, true, nil
}

func (s runSession) lookupRemote(ctx context.Context) (cache.Result, bool, error) {
	if s.remote.URL == "" {
		return cache.Result{}, false, nil
	}
	prior, err := s.remote.Get(ctx, s.key)
	if err == nil {
		if err := s.disk.Put(ctx, prior); err != nil {
			diagnostic(s.errOut, "receipt: optional local cache bookkeeping failed:", err)
		}
		return prior, true, nil
	}
	if errors.Is(err, cache.ErrMiss) {
		return prior, false, nil
	}
	if errors.Is(err, cache.ErrConflict) {
		if quarantineErr := s.quarantineLocal(); quarantineErr != nil {
			return prior, false, quarantineErr
		}
	}
	diagnostic(s.errOut, "receipt: remote unavailable; falling back to execution:", err)
	return prior, false, nil
}

func (s runSession) removeLocal() {
	if err := s.disk.Remove(s.key); err != nil {
		diagnostic(s.errOut, "receipt: optional local cache bookkeeping failed:", err)
	}
}

func (s runSession) quarantineLocal() error {
	if err := quarantine(s.cacheDir, s.key); err != nil {
		return err
	}
	s.removeLocal()
	return nil
}

func (s runSession) replay(prior cache.Result) (int, error) {
	diagnostic(s.errOut, fmt.Sprintf("receipt: hit %s (saved test duration %s)", s.key[:12], time.Duration(prior.DurationNS)))
	if _, e := s.out.Write(prior.Stdout); e != nil {
		return 0, e
	}
	if _, e := s.errOut.Write(prior.Stderr); e != nil {
		return 0, e
	}
	entryPath := filepath.Join(s.cacheDir, "entries", s.key+".json")
	if bookkeepingErr := os.Chtimes(entryPath, time.Now(), time.Now()); bookkeepingErr != nil {
		diagnostic(s.errOut, "receipt: optional local cache bookkeeping failed:", bookkeepingErr)
	}
	if err := s.recordHistory(); err != nil {
		return 0, err
	}
	return prior.ExitCode, nil
}

func (s runSession) verify(ctx context.Context, prior cache.Result, r runner.Output, stable bool) (int, error) {
	if !stable {
		diagnostic(s.errOut, "receipt: verification inconclusive: inputs changed during execution")
		return 1, nil
	}
	matched := !r.TooLarge && r.ExitCode == prior.ExitCode &&
		bytes.Equal(r.Stdout, prior.Stdout) && bytes.Equal(r.Stderr, prior.Stderr)
	if !matched {
		if e := s.quarantineLocal(); e != nil {
			return 0, e
		}
		observation := cache.Result{
			Version:  1,
			Key:      s.key,
			Manifest: s.manifest,
			Stdout:   r.Stdout,
			Stderr:   r.Stderr,
			ExitCode: r.ExitCode,
		}
		reportConflict(ctx, s.remote, observation, s.errOut)
		diagnostic(s.errOut, "receipt: verification mismatch; key quarantined locally (output differences may be timing text)")
		if r.ExitCode != 0 {
			return r.ExitCode, nil
		}
		return 1, nil
	}
	diagnostic(s.errOut, "receipt: verification matched byte-for-byte")
	return 0, nil
}

func (s runSession) failed(ctx context.Context, prior cache.Result, r runner.Output, stable bool) (int, error) {
	if s.options.refresh && prior.Key == s.key && stable {
		if e := s.quarantineLocal(); e != nil {
			return 0, e
		}
		observation := cache.Result{
			Version:  1,
			Key:      s.key,
			Manifest: s.manifest,
			Stdout:   r.Stdout,
			Stderr:   r.Stderr,
			ExitCode: r.ExitCode,
		}
		diagnostic(s.errOut, "receipt: refresh contradicted cached success; key quarantined locally")
		reportConflict(ctx, s.remote, observation, s.errOut)
	}
	diagnostic(s.errOut, "receipt: failed results are not cached")
	return r.ExitCode, nil
}

func (s runSession) publish(ctx context.Context, r runner.Output, stable bool) (int, error) {
	if !stable || r.TooLarge || quarantined(s.cacheDir, s.key) {
		diagnostic(s.errOut, "receipt: result not stored (inputs changed, output exceeded limit, or key quarantined)")
		return 0, nil
	}
	result := cache.Result{
		Version:    1,
		Key:        s.key,
		Manifest:   s.manifest,
		Stdout:     r.Stdout,
		Stderr:     r.Stderr,
		ExitCode:   r.ExitCode,
		DurationNS: int64(r.Duration),
		CreatedAt:  time.Now().UTC(),
	}
	if s.options.refresh {
		if e := s.disk.Remove(s.key); e != nil {
			diagnostic(s.errOut, "receipt: cache refresh failed:", e)
			return 0, nil
		}
	}
	if e := s.disk.Put(ctx, result); e != nil {
		diagnostic(s.errOut, "receipt: could not store result:", e)
	}
	if e := s.upload(ctx, result); e != nil {
		return 0, e
	}
	if e := prune(s.disk, s.config.MaxBytes, outDiscard{}); e != nil {
		diagnostic(s.errOut, "receipt: pruning failed:", e)
	}
	return 0, nil
}

func (s runSession) upload(ctx context.Context, result cache.Result) error {
	if s.remote.URL == "" {
		return nil
	}
	err := s.remote.Put(ctx, result)
	if err == nil {
		return nil
	}
	if errors.Is(err, cache.ErrConflict) {
		if quarantineErr := s.quarantineLocal(); quarantineErr != nil {
			return quarantineErr
		}
		diagnostic(s.errOut, "receipt: shared conflict; key quarantined locally")
	}
	diagnostic(s.errOut, "receipt: remote upload skipped:", err)
	return nil
}

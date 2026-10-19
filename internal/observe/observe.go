// Package observe records bounded, request-local timing summaries without payloads.
package observe

import (
	"context"
	"sync"
	"time"
)

// Stage identifies a fixed measurement boundary. Durations may be nested.
type Stage uint8

const (
	// Authentication includes credential database lookup and comparison.
	Authentication Stage = iota
	// Pool measures acquiring a shared-storage database connection.
	Pool
	// DatabaseLock includes the advisory-lock query and its wait.
	DatabaseLock
	// FilesystemLock includes opening and acquiring the directory lock.
	FilesystemLock
	// SQL includes measured metadata operations, not server-only execution time.
	SQL
	// Files includes blob I/O and content checks.
	Files
	// Inventory includes metadata accounting and file enumeration.
	Inventory
	// Fingerprint includes discovery, hashing and runtime probes.
	Fingerprint
	// Execution includes the child process and output handling.
	Execution
	// Lookup includes local lookup and optional remote status/fetch work.
	Lookup
	// CargoTools includes resolving, reading and querying installed Rust tools.
	CargoTools
	// CargoMetadata includes the frozen Cargo dependency query.
	CargoMetadata
	// CargoSources includes reading the lockfile and selected dependency files.
	CargoSources
	// Discovery includes Git root/file discovery and extra-input enumeration.
	Discovery
	// InputFiles includes reading and hashing declared working-tree/extra files.
	InputFiles
	// Executable includes reading and hashing the command executable.
	Executable
	// Toolchain includes the non-Cargo runtime probe and output normalization.
	Toolchain
	// GitSelection includes invocation-local lookup and optional Apple resolution.
	GitSelection
	stageCount
)

var names = [stageCount]string{"authentication", "db_pool", "db_lock", "fs_lock", "sql", "files", "inventory", "fingerprint", "execution", "lookup", "cargo_tools", "cargo_metadata", "cargo_sources", "discovery", "input_files", "executable", "toolchain", "git_selection"}

// Timing aggregates repeated calls to one boundary, in nanoseconds.
type Timing struct {
	Stage string `json:"stage"`
	Calls int64  `json:"calls"`
	NS    int64  `json:"ns"`
}

// Trace owns fixed-size timing storage for one operation.
type Trace struct {
	mu    sync.Mutex
	times [stageCount]Timing
}

type traceKey struct{}

// Start attaches an independent trace without changing cancellation behavior.
func Start(ctx context.Context) (context.Context, *Trace) {
	t := &Trace{}
	return context.WithValue(ctx, traceKey{}, t), t
}

// Measure returns a completion function; without a trace it is a no-op.
// Call the returned function once, including on error paths.
func Measure(ctx context.Context, stage Stage) func() {
	t, ok := ctx.Value(traceKey{}).(*Trace)
	if !ok || stage >= stageCount {
		return func() {}
	}
	start := time.Now()
	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.times[stage].Calls++
		t.times[stage].NS += time.Since(start).Nanoseconds()
	}
}

// Snapshot returns measured stages in a stable order. Nested times must not be summed.
func (t *Trace) Snapshot() []Timing {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Timing, 0, stageCount)
	for i, value := range t.times {
		if value.Calls != 0 {
			value.Stage = names[i]
			out = append(out, value)
		}
	}
	return out
}

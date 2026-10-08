// Package app coordinates the CLI, fingerprint engine, runner and cache stores.
package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"receipt/internal/cache"
	"receipt/internal/fingerprint"
	"receipt/internal/runner"
	"sort"
	"strings"
	"syscall"
	"time"
)

const help = `Receipt — reuse successful test results for identical declared inputs.

  receipt run [options] -- uv run pytest [pytest options]
  receipt explain [options] -- uv run pytest [pytest options]
  receipt cache list [--cache-dir DIR]
  receipt cache prune [--cache-dir DIR] [--max-bytes N]
  receipt serve [--cache-dir DIR] [--listen 127.0.0.1:7777]
                [--postgres] [--max-inflight 16]

Run options:
  --bypass          Run directly; do not read or write the cache.
  --refresh         Skip lookup; run tests and replace the local observation.
  --verify          Rerun a hit; quarantine the key if outputs or exit code differ.
  --cache-dir DIR   Cache location (default: .receipt in the Git repository).
  --remote URL      Shared cache URL; RECEIPT_TOKEN supplies the bearer token.
  --allow-generic   Allow other commands under the declared-input contract.

Configuration: .receipt.json at the repository root.
Ignored files, external services, random input and editable dependencies are
not automatically fingerprinted. Declare extra_inputs or use --bypass.
`

type Config struct {
	ExtraInputs       []string `json:"extra_inputs"`
	IgnoreEnvironment []string `json:"ignore_environment"`
	Remote            string   `json:"remote"`
	MaxBytes          int64    `json:"max_bytes"`
}
type options struct {
	cacheDir, remote                 string
	bypass, refresh, verify, generic bool
}

func Main(args []string, out, errOut io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	code, err := execute(ctx, args, out, errOut)
	if err != nil {
		fmt.Fprintln(errOut, "receipt:", err)
		return 2
	}
	return code
}
func execute(ctx context.Context, args []string, out, errOut io.Writer) (int, error) {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprint(out, help)
		return 0, nil
	}
	switch args[0] {
	case "run", "explain":
		return run(ctx, args[0], args[1:], out, errOut)
	case "cache":
		return 0, cacheCommand(ctx, args[1:], out, errOut)
	case "serve":
		return 0, serve(ctx, args[1:], errOut)
	default:
		return 0, fmt.Errorf("unknown command %q; use receipt help", args[0])
	}
}

func settings(ctx context.Context, dir string) (string, Config, error) {
	root, e := fingerprint.Root(ctx, dir)
	if e != nil {
		return "", Config{}, e
	}
	cfg := Config{MaxBytes: 1 << 30}
	b, e := os.ReadFile(filepath.Join(root, ".receipt.json"))
	if os.IsNotExist(e) {
		return root, cfg, nil
	}
	if e != nil {
		return root, cfg, e
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if e = d.Decode(&cfg); e != nil {
		return root, cfg, fmt.Errorf(".receipt.json: %w", e)
	}
	if e = d.Decode(new(any)); e != io.EOF {
		return root, cfg, fmt.Errorf(".receipt.json must contain one JSON object")
	}
	if cfg.MaxBytes < 0 {
		return root, cfg, fmt.Errorf("max_bytes must be nonnegative")
	}
	return root, cfg, nil
}
func cacheDirectory(root, override string) (string, error) {
	p := override
	if p == "" {
		p = filepath.Join(root, ".receipt")
	}
	p, e := filepath.Abs(p)
	if e != nil {
		return "", e
	}
	// In-repository caches must use the excluded namespace to avoid self-inputs.
	rel, e := filepath.Rel(root, p)
	if e != nil {
		return "", e
	}
	if rel == "." || (!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".." && rel != ".receipt" && !strings.HasPrefix(rel, ".receipt"+string(os.PathSeparator))) {
		return "", fmt.Errorf("cache inside repository must be under .receipt")
	}
	return p, nil
}
func atomically(path string, b []byte) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
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
	return os.Rename(f.Name(), path)
}
func quarantined(dir, key string) bool {
	_, e := os.Stat(filepath.Join(dir, "quarantine", key))
	return e == nil
}
func quarantine(dir, key string) error {
	return atomically(filepath.Join(dir, "quarantine", key), []byte("conflicting observation; do not reuse\n"))
}

func run(ctx context.Context, mode string, args []string, out, errOut io.Writer) (int, error) {
	f := flag.NewFlagSet(mode, flag.ContinueOnError)
	f.SetOutput(errOut)
	var o options
	f.StringVar(&o.cacheDir, "cache-dir", "", "cache directory")
	f.StringVar(&o.remote, "remote", "", "shared cache URL")
	f.BoolVar(&o.bypass, "bypass", false, "run directly")
	f.BoolVar(&o.refresh, "refresh", false, "skip cache lookup")
	f.BoolVar(&o.verify, "verify", false, "verify a hit by running again")
	f.BoolVar(&o.generic, "allow-generic", false, "allow arbitrary commands")
	if e := f.Parse(args); e != nil {
		if e == flag.ErrHelp {
			return 0, nil
		}
		return 0, e
	}
	command := f.Args()
	if len(command) == 0 {
		return 0, fmt.Errorf("supply a command after --")
	}
	modes := 0
	for _, enabled := range []bool{o.bypass, o.refresh, o.verify} {
		if enabled {
			modes++
		}
	}
	if modes > 1 {
		return 0, fmt.Errorf("choose only one of --bypass, --refresh, --verify")
	}
	dir, e := os.Getwd()
	if e != nil {
		return 0, e
	}
	if o.bypass {
		r, e := runner.Run(ctx, dir, command, out, errOut)
		return r.ExitCode, e
	}
	root, cfg, e := settings(ctx, dir)
	if e != nil {
		return 0, e
	}
	cacheDir, e := cacheDirectory(root, o.cacheDir)
	if e != nil {
		return 0, e
	}
	if o.remote == "" {
		o.remote = cfg.Remote
	}
	disk := cache.Disk{Dir: cacheDir}
	remote := cache.Remote{URL: o.remote, Token: os.Getenv("RECEIPT_TOKEN")}
	m, e := fingerprint.Build(ctx, dir, command, cfg.ExtraInputs, cfg.IgnoreEnvironment, o.generic)
	if e != nil {
		if mode == "explain" {
			return 0, e
		}
		fmt.Fprintln(errOut, "receipt: inputs unavailable; running uncached:", e)
		r, e := runner.Run(ctx, dir, command, out, errOut)
		return r.ExitCode, e
	}
	key := m.Key()
	history := filepath.Join(cacheDir, "history", fingerprint.Hash([]byte(dir))+".json")
	if mode == "explain" {
		fmt.Fprintln(out, "key:", key)
		if quarantined(cacheDir, key) {
			fmt.Fprintln(out, "quarantined: conflicting observations")
		}
		if _, e := disk.Get(ctx, key); e == nil {
			fmt.Fprintln(out, "local cache: hit")
		} else {
			fmt.Fprintln(out, "local cache: miss")
		}
		var old fingerprint.Manifest
		b, e := os.ReadFile(history)
		if e != nil || json.Unmarshal(b, &old) != nil {
			fmt.Fprintln(out, "no previous manifest")
			return 0, nil
		}
		changes := fingerprint.Diff(old, m)
		if len(changes) == 0 {
			fmt.Fprintln(out, "declared inputs unchanged")
		} else {
			for _, c := range changes {
				fmt.Fprintln(out, c)
			}
		}
		return 0, nil
	}
	var prior cache.Result
	hit := false
	if o.refresh {
		// Refresh still observes the old local result so a failure cannot leave
		// a known-conflicting success eligible for the next lookup.
		prior, e = disk.Get(ctx, key)
	}
	if !o.refresh && !quarantined(cacheDir, key) {
		prior, e = disk.Get(ctx, key)
		if errors.Is(e, cache.ErrCorrupt) {
			fmt.Fprintln(errOut, "receipt: corrupt local entry; running uncached or fetching remote")
			_ = disk.Remove(key)
		}
		if e == nil {
			hit = true
			if o.remote != "" {
				// A cached local copy is not authoritative for shared quarantine.
				if statusErr := remote.Check(ctx, key); statusErr != nil {
					hit = false
					if errors.Is(statusErr, cache.ErrConflict) {
						if e = quarantine(cacheDir, key); e != nil {
							return 0, e
						}
						_ = disk.Remove(key)
					}
					fmt.Fprintln(errOut, "receipt: shared status unavailable or quarantined; executing instead of local replay:", statusErr)
				}
			}
		} else if o.remote != "" {
			prior, e = remote.Get(ctx, key)
			if e == nil {
				hit = true
				_ = disk.Put(ctx, prior)
			} else if !errors.Is(e, cache.ErrMiss) {
				remoteErr := e
				if errors.Is(e, cache.ErrConflict) {
					if e = quarantine(cacheDir, key); e != nil {
						return 0, e
					}
					_ = disk.Remove(key)
				}
				fmt.Fprintln(errOut, "receipt: remote unavailable; falling back to execution:", remoteErr)
			}
		}
	}
	if hit && !o.verify {
		fmt.Fprintf(errOut, "receipt: hit %s (saved test duration %s)\n", key[:12], time.Duration(prior.DurationNS))
		if _, e = out.Write(prior.Stdout); e != nil {
			return 0, e
		}
		if _, e = errOut.Write(prior.Stderr); e != nil {
			return 0, e
		}
		_ = os.Chtimes(filepath.Join(cacheDir, "entries", key+".json"), time.Now(), time.Now())
		b, _ := json.Marshal(m)
		_ = atomically(history, b)
		return prior.ExitCode, nil
	}
	if hit {
		fmt.Fprintln(errOut, "receipt: verifying", key[:12])
	} else {
		fmt.Fprintln(errOut, "receipt: miss", key[:12])
	}
	r, e := runner.Run(ctx, dir, command, out, errOut)
	if e != nil {
		return r.ExitCode, e
	}
	if ctx.Err() != nil {
		return 130, nil
	}
	after, e := fingerprint.Build(ctx, dir, command, cfg.ExtraInputs, cfg.IgnoreEnvironment, o.generic)
	stable := e == nil && after.Key() == key
	b, _ := json.Marshal(m)
	_ = atomically(history, b)
	if hit && o.verify {
		if !stable {
			fmt.Fprintln(errOut, "receipt: verification inconclusive: inputs changed during execution")
			return 1, nil
		}
		if r.TooLarge || r.ExitCode != prior.ExitCode || string(r.Stdout) != string(prior.Stdout) || string(r.Stderr) != string(prior.Stderr) {
			if e = quarantine(cacheDir, key); e != nil {
				return 0, e
			}
			_ = disk.Remove(key)
			reportConflict(ctx, remote, cache.Result{Version: 1, Key: key, Manifest: m, Stdout: r.Stdout, Stderr: r.Stderr, ExitCode: r.ExitCode}, errOut)
			fmt.Fprintln(errOut, "receipt: verification mismatch; key quarantined locally (output differences may be timing text)")
			if r.ExitCode != 0 {
				return r.ExitCode, nil
			}
			return 1, nil
		}
		fmt.Fprintln(errOut, "receipt: verification matched byte-for-byte")
		return 0, nil
	}
	if r.ExitCode != 0 {
		if o.refresh && prior.Key == key && stable {
			if e = quarantine(cacheDir, key); e != nil {
				return 0, e
			}
			_ = disk.Remove(key)
			fmt.Fprintln(errOut, "receipt: refresh contradicted cached success; key quarantined locally")
			reportConflict(ctx, remote, cache.Result{Version: 1, Key: key, Manifest: m, Stdout: r.Stdout, Stderr: r.Stderr, ExitCode: r.ExitCode}, errOut)
		}
		fmt.Fprintln(errOut, "receipt: failed results are not cached")
		return r.ExitCode, nil
	}
	if !stable || r.TooLarge || quarantined(cacheDir, key) {
		fmt.Fprintln(errOut, "receipt: result not stored (inputs changed, output exceeded limit, or key quarantined)")
		return 0, nil
	}
	result := cache.Result{Version: 1, Key: key, Manifest: m, Stdout: r.Stdout, Stderr: r.Stderr, ExitCode: r.ExitCode, DurationNS: int64(r.Duration), CreatedAt: time.Now().UTC()}
	if o.refresh {
		if e = disk.Remove(key); e != nil {
			fmt.Fprintln(errOut, "receipt: cache refresh failed:", e)
			return 0, nil
		}
	}
	if e = disk.Put(ctx, result); e != nil {
		fmt.Fprintln(errOut, "receipt: could not store result:", e)
	}
	if o.remote != "" {
		if e = remote.Put(ctx, result); e != nil {
			uploadErr := e
			if errors.Is(e, cache.ErrConflict) {
				if e = quarantine(cacheDir, key); e != nil {
					return 0, e
				}
				_ = disk.Remove(key)
				fmt.Fprintln(errOut, "receipt: shared conflict; key quarantined locally")
			}
			fmt.Fprintln(errOut, "receipt: remote upload skipped:", uploadErr)
		}
	}
	if e = prune(disk, cfg.MaxBytes, outDiscard{}); e != nil {
		fmt.Fprintln(errOut, "receipt: pruning failed:", e)
	}
	return 0, nil
}

type outDiscard struct{}

func reportConflict(ctx context.Context, remote cache.Remote, result cache.Result, errOut io.Writer) {
	if remote.URL == "" {
		return
	}
	if err := remote.Report(ctx, cache.ReportFor(result)); err != nil {
		fmt.Fprintln(errOut, "receipt: shared conflict report failed (local quarantine retained):", err)
	}
}

func (outDiscard) Write(b []byte) (int, error) { return len(b), nil }

func prune(d cache.Disk, max int64, out io.Writer) error {
	if max < 0 {
		return fmt.Errorf("negative size limit")
	}
	entries, e := os.ReadDir(filepath.Join(d.Dir, "entries"))
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	type item struct {
		key  string
		size int64
		at   time.Time
	}
	var items []item
	var total int64
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		info, e := entry.Info()
		if e != nil {
			continue
		}
		key := strings.TrimSuffix(name, ".json")
		items = append(items, item{key, info.Size(), info.ModTime()})
		total += info.Size()
	}
	sort.Slice(items, func(i, j int) bool { return items[i].at.Before(items[j].at) })
	for _, i := range items {
		if total <= max {
			break
		}
		if e = d.Remove(i.key); e != nil {
			return e
		}
		total -= i.size
		fmt.Fprintln(out, "evicted", i.key)
	}
	return nil
}

func cacheCommand(ctx context.Context, args []string, out, errOut io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("cache requires list or prune")
	}
	f := flag.NewFlagSet("cache", flag.ContinueOnError)
	f.SetOutput(errOut)
	dir := f.String("cache-dir", "", "cache directory")
	max := f.Int64("max-bytes", 1<<30, "maximum retained bytes")
	if e := f.Parse(args[1:]); e != nil {
		if e == flag.ErrHelp {
			return nil
		}
		return e
	}
	wd, e := os.Getwd()
	if e != nil {
		return e
	}
	root, e := fingerprint.Root(ctx, wd)
	if e != nil {
		if *dir == "" {
			return e
		}
		root = wd
	}
	p, e := cacheDirectory(root, *dir)
	if e != nil {
		return e
	}
	disk := cache.Disk{Dir: p}
	switch args[0] {
	case "list":
		entries, e := disk.List()
		if e != nil {
			return e
		}
		for _, r := range entries {
			fmt.Fprintf(out, "%s  %s  %s  %s\n", r.Key[:12], r.CreatedAt.Format(time.RFC3339), time.Duration(r.DurationNS), strings.Join(r.Manifest.Command, " "))
		}
		fmt.Fprintf(out, "%d entries\n", len(entries))
		return nil
	case "prune":
		return prune(disk, *max, out)
	default:
		return fmt.Errorf("unknown cache command %q", args[0])
	}
}

func serve(ctx context.Context, args []string, errOut io.Writer) error {
	f := flag.NewFlagSet("serve", flag.ContinueOnError)
	f.SetOutput(errOut)
	dir := f.String("cache-dir", "", "cache directory")
	addr := f.String("listen", "127.0.0.1:7777", "listen address")
	postgres := f.Bool("postgres", false, "use RECEIPT_DATABASE_URL for shared metadata")
	maxInflight := f.Int("max-inflight", 16, "maximum active API requests")
	if e := f.Parse(args); e != nil {
		if e == flag.ErrHelp {
			return nil
		}
		return e
	}
	token := os.Getenv("RECEIPT_TOKEN")
	if *maxInflight < 1 || *maxInflight > 128 {
		return fmt.Errorf("max-inflight must be between 1 and 128")
	}
	if len(token) < 24 {
		return fmt.Errorf("serve requires RECEIPT_TOKEN of at least 24 characters")
	}
	wd, e := os.Getwd()
	if e != nil {
		return e
	}
	if *dir == "" {
		*dir = filepath.Join(wd, ".receipt", "server")
	}
	var store cache.Store = cache.Disk{Dir: *dir}
	if *postgres {
		dsn := os.Getenv("RECEIPT_DATABASE_URL")
		if dsn == "" {
			return fmt.Errorf("--postgres requires RECEIPT_DATABASE_URL")
		}
		db, e := sql.Open("postgres", dsn)
		if e != nil {
			return fmt.Errorf("open metadata database failed")
		}
		defer db.Close()
		db.SetMaxOpenConns(16)
		db.SetMaxIdleConns(4)
		db.SetConnMaxLifetime(5 * time.Minute)
		startup, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		shared := cache.Postgres{DB: db, Blobs: cache.FileBlobs{Dir: filepath.Join(*dir, "blobs")}}
		if e = shared.Migrate(startup); e != nil {
			return fmt.Errorf("metadata migration failed (check database connectivity and permissions)")
		}
		store = shared
	}
	ln, e := net.Listen("tcp", *addr)
	if e != nil {
		return e
	}
	defer ln.Close()
	srv := &http.Server{Handler: cache.HandlerLimit(store, token, *maxInflight), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdown)
		case <-finished:
		}
	}()
	fmt.Fprintln(errOut, "receipt: shared cache listening on", ln.Addr(), "(trusted-client API; use TLS reverse proxy off loopback)")
	e = srv.Serve(ln)
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}

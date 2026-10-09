// Package app coordinates the CLI, fingerprint engine, runner and cache stores.
package app

import (
	"context"
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
	"receipt/internal/auth"
	"receipt/internal/cache"
	"receipt/internal/fingerprint"
	"receipt/internal/runner"
	"sort"
	"strings"
	"syscall"
	"time"
)

type commandName string

const (
	commandRun     commandName = "run"
	commandExplain commandName = "explain"
	commandCache   commandName = "cache"
	commandServe   commandName = "serve"
	commandShared  commandName = "shared"
	commandAuth    commandName = "auth"
	commandPrune               = "prune"
)

func parseCommand(raw string) (commandName, error) {
	mode := commandName(raw)
	switch mode {
	case commandRun, commandExplain, commandCache, commandServe, commandShared, commandAuth:
		return mode, nil
	default:
		return "", fmt.Errorf("unknown command %q; use receipt help", raw)
	}
}

const help = `Receipt — reuse successful test results for identical declared inputs.

  receipt run [options] -- uv run pytest [pytest options]
  receipt explain [options] -- uv run pytest [pytest options]
  receipt cache list [--cache-dir DIR]
  receipt cache prune [--cache-dir DIR] [--max-bytes N]
  receipt serve [--cache-dir DIR] [--listen 127.0.0.1:7777]
                [--postgres] [--credentials] [--max-inflight 16] [--request-log]
  receipt auth issue --cache-dir DIR --subject LABEL --scopes read,publish,report
                     [--expires-in 24h]
  receipt auth list --cache-dir DIR
  receipt auth revoke --cache-dir DIR --id CREDENTIAL_ID
  receipt shared stats --cache-dir DIR
  receipt shared limits --cache-dir DIR [--max-blob-bytes N] [--max-metadata-bytes N]
                        [--max-entries N] [--max-evidence N] [--apply]
  receipt shared gc --cache-dir DIR [--apply]
  receipt shared prune --cache-dir DIR [--older-than 720h] [--apply]

Shared maintenance requires RECEIPT_DATABASE_URL. Changes default to dry-run.
Auth administration requires RECEIPT_DATABASE_URL; issue reveals the secret once.
Credential mode requires --postgres and never accepts the legacy shared token.

Run options:
  --timings         Write a redacted timing summary to stderr.
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

// Config describes repository-local CLI settings.
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

// Main runs the CLI and returns the process exit status.
func Main(args []string, out, errOut io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	code, err := execute(ctx, args, out, errOut)
	if err != nil {
		diagnostic(errOut, "receipt:", err)
		return 2
	}
	return code
}

// diagnostic is best-effort: a broken diagnostic stream must not change a test outcome.
func diagnostic(out io.Writer, args ...any) {
	fmt.Fprintln(out, args...) //nolint:errcheck // No fallback output channel is available for diagnostics.
}

func execute(ctx context.Context, args []string, out, errOut io.Writer) (int, error) {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		if _, err := fmt.Fprint(out, help); err != nil {
			return 0, err
		}
		return 0, nil
	}
	mode, err := parseCommand(args[0])
	if err != nil {
		return 0, err
	}
	switch mode {
	case commandRun, commandExplain:
		return run(ctx, mode, args[1:], out, errOut)
	case commandCache:
		return 0, cacheCommand(ctx, args[1:], out, errOut)
	case commandServe:
		return 0, serve(ctx, args[1:], errOut)
	case commandShared:
		return 0, sharedCommand(ctx, args[1:], out, errOut)
	case commandAuth:
		return 0, authCommand(ctx, args[1:], out, errOut)
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
	f, e := os.Open(filepath.Join(root, ".receipt.json"))
	if os.IsNotExist(e) {
		return root, cfg, nil
	}
	if e != nil {
		return root, cfg, e
	}
	defer f.Close() //nolint:errcheck // Read-only resource cleanup; read failures are returned below.
	const maxConfigBytes = 1 << 20
	b, e := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if e != nil {
		return root, cfg, e
	}
	if len(b) > maxConfigBytes {
		return root, cfg, fmt.Errorf(".receipt.json exceeds byte limit")
	}
	if e = json.Unmarshal(b, &cfg); e != nil {
		return root, cfg, fmt.Errorf(".receipt.json: %w", e)
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
	if e := os.MkdirAll(filepath.Dir(path), 0o700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name()) //nolint:errcheck // Best-effort temp cleanup; publication errors are checked separately.
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

func run(ctx context.Context, mode commandName, args []string, out, errOut io.Writer) (code int, runErr error) {
	f := flag.NewFlagSet(string(mode), flag.ContinueOnError)
	f.SetOutput(errOut)
	var o options
	f.StringVar(&o.cacheDir, "cache-dir", "", "cache directory")
	f.StringVar(&o.remote, "remote", "", "shared cache URL")
	f.BoolVar(&o.bypass, "bypass", false, "run directly")
	f.BoolVar(&o.refresh, "refresh", false, "skip cache lookup")
	f.BoolVar(&o.verify, "verify", false, "verify a hit by running again")
	f.BoolVar(&o.generic, "allow-generic", false, "allow arbitrary commands")
	timings := f.Bool("timings", false, "write an opt-in timing summary to stderr")
	if e := f.Parse(args); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return 0, nil
		}
		return 0, e
	}
	command := f.Args()
	if *timings {
		var finish func(int, error)
		ctx, finish = startRunTrace(ctx, errOut)
		defer func() { finish(code, runErr) }()
	}
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
		if mode == commandExplain {
			return 0, e
		}
		diagnostic(errOut, "receipt: inputs unavailable; running uncached:", e)
		r, e := runner.Run(ctx, dir, command, out, errOut)
		return r.ExitCode, e
	}
	key := m.Key()
	history := filepath.Join(cacheDir, "history", fingerprint.Hash([]byte(dir))+".json")
	s := runSession{options: o, config: cfg, cacheDir: cacheDir, key: key, history: history, disk: disk, remote: remote, manifest: m, out: out, errOut: errOut}
	if mode == commandExplain {
		return 0, s.explain(ctx)
	}
	prior, hit, e := s.lookup(ctx)
	if e != nil {
		return 0, e
	}
	if hit && !o.verify {
		return s.replay(prior)
	}
	if hit {
		diagnostic(errOut, "receipt: verifying", key[:12])
	} else {
		diagnostic(errOut, "receipt: miss", key[:12])
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
	if e = s.recordHistory(); e != nil {
		return 0, e
	}
	if hit && o.verify {
		return s.verify(ctx, prior, r, stable)
	}
	if r.ExitCode != 0 {
		return s.failed(ctx, prior, r, stable)
	}
	return s.publish(ctx, r, stable)
}

type outDiscard struct{}

func reportConflict(ctx context.Context, remote cache.Remote, result cache.Result, errOut io.Writer) {
	if remote.URL == "" {
		return
	}
	if err := remote.Report(ctx, cache.ReportFor(result)); err != nil {
		diagnostic(errOut, "receipt: shared conflict report failed (local quarantine retained):", err)
	}
}

func (outDiscard) Write(b []byte) (int, error) { return len(b), nil }

func prune(d cache.Disk, maxBytes int64, out io.Writer) error {
	if maxBytes < 0 {
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
		if total <= maxBytes {
			break
		}
		if e = d.Remove(i.key); e != nil {
			return e
		}
		total -= i.size
		if _, err := fmt.Fprintln(out, "evicted", i.key); err != nil {
			return err
		}
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
	maxBytes := f.Int64("max-bytes", 1<<30, "maximum retained bytes")
	if e := f.Parse(args[1:]); e != nil {
		if errors.Is(e, flag.ErrHelp) {
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
	mode, err := parseCacheMode(args[0])
	if err != nil {
		return err
	}
	switch mode {
	case cacheList:
		entries, e := disk.List()
		if e != nil {
			return e
		}
		p := printer{out: out}
		for _, r := range entries {
			p.printf("%s  %s  %s  %s\n", r.Key[:12], r.CreatedAt.Format(time.RFC3339), time.Duration(r.DurationNS), strings.Join(r.Manifest.Command, " "))
		}
		p.printf("%d entries\n", len(entries))
		return p.err
	case cachePrune:
		return prune(disk, *maxBytes, out)
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
	credentials := f.Bool("credentials", false, "use database-backed scoped credentials (requires --postgres)")
	requestLog := f.Bool("request-log", false, "write redacted request timing summaries to stderr")
	maxInflight := f.Int("max-inflight", 16, "maximum active API requests")
	if e := f.Parse(args); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return nil
		}
		return e
	}
	token := os.Getenv("RECEIPT_TOKEN")
	if *maxInflight < 1 || *maxInflight > 128 {
		return fmt.Errorf("max-inflight must be between 1 and 128")
	}
	if *credentials && !*postgres {
		return fmt.Errorf("credentials requires --postgres")
	}
	if !*credentials && len(token) < 24 {
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
	var authenticator auth.Authenticator
	if *postgres {
		startup, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		shared, e := openShared(startup, *dir)
		if e != nil {
			return e
		}
		defer shared.DB.Close() //nolint:errcheck // Read/resource cleanup; writes and syncs are checked before return.
		store = shared
		if *credentials {
			credentialsDB := auth.Postgres{DB: shared.DB}
			if e := credentialsDB.Migrate(startup); e != nil {
				return fmt.Errorf("credential initialization failed: %w", e)
			}
			authenticator = credentialsDB
		}
	}
	ln, e := net.Listen("tcp", *addr)
	if e != nil {
		return e
	}
	defer ln.Close() //nolint:errcheck // Read/resource cleanup; writes and syncs are checked before return.
	handler := cache.HandlerLimit(store, token, *maxInflight)
	authDescription := "legacy shared token"
	if *credentials {
		handler = cache.HandlerAuthorized(store, authenticator, *maxInflight)
		authDescription = "scoped credentials"
	}
	handler, finishLog, e := requestLogging(handler, *requestLog, errOut)
	if e != nil {
		return e
	}
	defer finishLog()
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if shutdownErr := srv.Shutdown(shutdown); shutdownErr != nil {
				diagnostic(errOut, "receipt: graceful shutdown failed:", shutdownErr)
			}
		case <-finished:
		}
	}()
	diagnostic(errOut, "receipt: shared cache listening on", ln.Addr(), "with", authDescription, "(use TLS reverse proxy off loopback)")
	e = srv.Serve(ln)
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}

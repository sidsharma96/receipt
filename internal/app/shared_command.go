package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"receipt/internal/cache"
	"time"
)

func openShared(ctx context.Context, dir string) (cache.Postgres, error) {
	var empty cache.Postgres
	if dir == "" {
		return empty, fmt.Errorf("explicit --cache-dir required")
	}
	dsn := os.Getenv("RECEIPT_DATABASE_URL")
	if dsn == "" {
		return empty, fmt.Errorf("RECEIPT_DATABASE_URL required")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return empty, fmt.Errorf("open metadata database failed")
	}
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(5 * time.Minute)
	shared := cache.Postgres{DB: db, Blobs: cache.FileBlobs{Dir: filepath.Join(dir, "blobs")}}
	if err = shared.Migrate(ctx); err != nil {
		db.Close() //nolint:errcheck // Resource cleanup must not replace the primary operation error.
		return empty, fmt.Errorf("shared initialization failed (check connectivity, permissions, and blob-directory binding): %w", err)
	}
	return shared, nil
}

type sharedMode string

const (
	sharedStats  sharedMode = "stats"
	sharedLimits sharedMode = "limits"
	sharedGC     sharedMode = "gc"
	sharedPrune  sharedMode = commandPrune
)

func parseSharedMode(raw string) (sharedMode, error) {
	mode := sharedMode(raw)
	switch mode {
	case sharedStats, sharedLimits, sharedGC, sharedPrune:
		return mode, nil
	default:
		return "", fmt.Errorf("unknown shared command %q", raw)
	}
}

func sharedCommand(ctx context.Context, args []string, out, errOut io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("shared requires stats, limits, gc or prune")
	}
	mode, err := parseSharedMode(args[0])
	if err != nil {
		return err
	}
	f := flag.NewFlagSet("shared "+string(mode), flag.ContinueOnError)
	f.SetOutput(errOut)
	dir := f.String("cache-dir", "", "shared server cache directory (required)")
	apply := f.Bool("apply", false, "apply changes; default is dry-run")
	older := f.Duration("older-than", 30*24*time.Hour, "expire nonquarantined metadata older than duration")
	timeout := f.Duration("timeout", 5*time.Minute, "maintenance timeout")
	blob := f.Int64("max-blob-bytes", -1, "new stored blob byte limit")
	metadata := f.Int64("max-metadata-bytes", -1, "new logical JSON byte limit")
	entries := f.Int64("max-entries", -1, "new action row limit")
	evidence := f.Int64("max-evidence", -1, "new distinct evidence row limit")
	if err := f.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(f.Args()) != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	if *timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	visited := map[string]bool{}
	f.Visit(func(v *flag.Flag) { visited[v.Name] = true })
	if mode == sharedStats && *apply {
		return fmt.Errorf("stats is read-only")
	}
	if mode != sharedPrune && visited["older-than"] {
		return fmt.Errorf("older-than is only for prune")
	}
	for _, name := range []string{"max-blob-bytes", "max-metadata-bytes", "max-entries", "max-evidence"} {
		if mode != sharedLimits && visited[name] {
			return fmt.Errorf("limit flags are only for limits")
		}
	}
	if mode == sharedPrune && *older <= 0 {
		return fmt.Errorf("older-than must be positive")
	}
	if *dir == "" {
		return fmt.Errorf("shared maintenance requires explicit --cache-dir")
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	shared, err := openShared(ctx, *dir)
	if err != nil {
		return err
	}
	defer shared.DB.Close() //nolint:errcheck // Read/resource cleanup; writes and syncs are checked before return.
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	switch mode {
	case sharedStats:
		l, u, err := shared.Stats(ctx)
		if err != nil {
			return err
		}
		return encoder.Encode(struct {
			Limits cache.Limits `json:"limits"`
			Usage  cache.Usage  `json:"usage"`
		}{l, u})
	case sharedLimits:
		return changeSharedLimits(ctx, shared, cache.Limits{MaxBlobBytes: *blob, MaxMetadataBytes: *metadata, MaxEntries: *entries, MaxEvidence: *evidence}, visited, *apply, encoder)
	case sharedGC:
		result, err := shared.Collect(ctx, *apply)
		if err != nil {
			return err
		}
		return encoder.Encode(result)
	case sharedPrune:
		result, err := shared.Expire(ctx, time.Now().Add(-*older), *apply)
		if err != nil {
			return err
		}
		return encoder.Encode(result)
	}
	return nil
}

func changeSharedLimits(ctx context.Context, shared cache.Postgres, requested cache.Limits, visited map[string]bool, apply bool, encoder *json.Encoder) error {
	l, _, err := shared.Stats(ctx)
	if err != nil {
		return err
	}
	if visited["max-blob-bytes"] {
		l.MaxBlobBytes = requested.MaxBlobBytes
	}
	if visited["max-metadata-bytes"] {
		l.MaxMetadataBytes = requested.MaxMetadataBytes
	}
	if visited["max-entries"] {
		l.MaxEntries = requested.MaxEntries
	}
	if visited["max-evidence"] {
		l.MaxEvidence = requested.MaxEvidence
	}
	if err := shared.SetLimits(ctx, l, apply); err != nil {
		return err
	}
	return encoder.Encode(struct {
		Applied bool         `json:"applied"`
		Limits  cache.Limits `json:"limits"`
	}{apply, l})
}

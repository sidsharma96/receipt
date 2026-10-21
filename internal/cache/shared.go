package cache

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sidsharma96/receipt/internal/fingerprint"
	"github.com/sidsharma96/receipt/internal/observe"

	// Register the PostgreSQL driver used by the persistence boundary.
	_ "github.com/lib/pq"
)

// ErrConflict marks a key with contradictory observations that must not be reused.
var ErrConflict = errors.New("shared key quarantined")

// ConflictReport stores output identities, not another complete output copy.
// It is evidence from a trusted client, not cryptographic proof of execution.
type ConflictReport struct {
	Key        string               `json:"key"`
	Manifest   fingerprint.Manifest `json:"manifest"`
	StdoutHash string               `json:"stdout_hash"`
	StderrHash string               `json:"stderr_hash"`
	ExitCode   int                  `json:"exit_code"`
}

// ReportFor summarizes an observation without retaining its output bytes.
func ReportFor(r Result) ConflictReport {
	return ConflictReport{r.Key, r.Manifest, fingerprint.Hash(r.Stdout), fingerprint.Hash(r.Stderr), r.ExitCode}
}

func (r ConflictReport) validate() error {
	var errs []error
	if err := r.Manifest.Validate(); err != nil {
		errs = append(errs, err)
	}
	if !validKey.MatchString(r.Key) || r.Key != r.Manifest.Key() {
		errs = append(errs, fmt.Errorf("report.key: does not match manifest"))
	}
	if !validKey.MatchString(r.StdoutHash) || !validKey.MatchString(r.StderrHash) {
		errs = append(errs, fmt.Errorf("report output hashes: expected lowercase SHA-256"))
	}
	if r.ExitCode < 0 || r.ExitCode > 255 {
		errs = append(errs, fmt.Errorf("report.exit_code: expected 0..255"))
	}
	return errors.Join(errs...)
}

func (r ConflictReport) digest() string {
	// Execution time and creation time do not define output equality.
	b, _ := json.Marshal(struct { //nolint:errcheck // This concrete type contains only JSON-supported values and no marshal hooks.
		Stdout, Stderr string
		ExitCode       int
	}{r.StdoutHash, r.StderrHash, r.ExitCode})
	return fingerprint.Hash(b)
}

// ConflictStore is the optional shared quarantine and evidence capability.
type ConflictStore interface {
	Quarantined(context.Context, string) (bool, error)
	Report(context.Context, ConflictReport) error
}

type blobRecord struct {
	Version int     `json:"version"`
	Head    Result  `json:"head"`
	Stdout  BlobRef `json:"stdout"`
	Stderr  BlobRef `json:"stderr"`
}

// Postgres owns visibility/quarantine; Blobs owns immutable output bytes.
// Uploads happen under lifecycle locks before metadata publication; failed
// metadata commits can leave orphan blobs.
type Postgres struct {
	DB    *sql.DB
	Blobs BlobStore
}

// Migrate creates missing tables and initializes the filesystem/store binding.
func (s Postgres) Migrate(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // Preserve the primary error; rollback also releases lifecycle resources.
	// Serialize startup migrations across servers. This is intentionally a lock.
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(72656365697074)"); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS receipt_entries_v2 (
 key TEXT PRIMARY KEY CHECK (key ~ '^[a-f0-9]{64}$'),
 digest TEXT NOT NULL,
 record JSONB,
 quarantined BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
 );
 CREATE TABLE IF NOT EXISTS receipt_evidence_v2 (
 key TEXT NOT NULL REFERENCES receipt_entries_v2(key),
 digest TEXT NOT NULL,
 report JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(key, digest)
 );
 ALTER TABLE receipt_entries_v2 ADD COLUMN IF NOT EXISTS evidence_capped BOOLEAN NOT NULL DEFAULT FALSE;
 CREATE TABLE IF NOT EXISTS receipt_storage_policy_v3 (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 blob_root TEXT NOT NULL,
 store_id TEXT NOT NULL,
 max_blob_bytes BIGINT NOT NULL CHECK(max_blob_bytes >= 0),
 max_metadata_bytes BIGINT NOT NULL CHECK(max_metadata_bytes >= 0),
 max_entries BIGINT NOT NULL CHECK(max_entries >= 0),
 max_evidence BIGINT NOT NULL CHECK(max_evidence >= 0)
 );
 CREATE TABLE IF NOT EXISTS receipt_storage_usage_v6 (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 blob_bytes BIGINT NOT NULL,
 metadata_bytes BIGINT NOT NULL,
 entries BIGINT NOT NULL,
 evidence BIGINT NOT NULL
 );`)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return s.initializePolicy(ctx)
}

// Quarantined reports whether a shared key has been marked unsafe for reuse.
func (s Postgres) Quarantined(ctx context.Context, key string) (bool, error) {
	defer observe.Measure(ctx, observe.SQL)()
	if !validKey.MatchString(key) {
		return false, fmt.Errorf("invalid key")
	}
	var q bool
	err := s.DB.QueryRowContext(ctx, "SELECT quarantined FROM receipt_entries_v2 WHERE key=$1", key).Scan(&q)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return q, err
}

// Get reads validated metadata and blobs under lifecycle read locks.
func (s Postgres) Get(ctx context.Context, key string) (Result, error) {
	var empty Result
	if !validKey.MatchString(key) {
		return empty, fmt.Errorf("invalid key")
	}
	tx, err := s.lifecycle(ctx, false)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback() //nolint:errcheck // Preserve the primary error; rollback also releases lifecycle resources.
	var raw []byte
	var digest string
	var q bool
	sqlDone := observe.Measure(ctx, observe.SQL)
	err = tx.QueryRowContext(ctx, "SELECT record, digest, quarantined FROM receipt_entries_v2 WHERE key=$1", key).Scan(&raw, &digest, &q)
	sqlDone()
	if errors.Is(err, sql.ErrNoRows) {
		return empty, ErrMiss
	}
	if err != nil {
		return empty, err
	}
	if q {
		return empty, ErrConflict
	}
	var record blobRecord
	if json.Unmarshal(raw, &record) != nil || record.Version != 2 || record.Head.Key != key || len(record.Head.Stdout) != 0 || len(record.Head.Stderr) != 0 || Validate(record.Head) != nil {
		return empty, ErrCorrupt
	}
	r := record.Head
	r.Stdout, err = s.Blobs.Get(ctx, record.Stdout)
	if err != nil {
		return empty, fmt.Errorf("stdout blob: %w", err)
	}
	r.Stderr, err = s.Blobs.Get(ctx, record.Stderr)
	if err != nil {
		return empty, fmt.Errorf("stderr blob: %w", err)
	}
	if ReportFor(r).digest() != digest {
		return empty, ErrCorrupt
	}
	return r, nil
}

// Put publishes a success or quarantines contradictory observations under exclusive locks.
func (s Postgres) Put(ctx context.Context, r Result) error {
	if err := Validate(r); err != nil {
		return err
	}
	// Retain the v1 wire size contract; storage representation alone changes.
	if _, err := Encode(r); err != nil {
		return err
	}
	tx, err := s.lifecycle(ctx, true)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // Preserve the primary error; rollback also releases lifecycle resources.
	additional, err := s.checkBlobQuota(ctx, tx, r)
	if err != nil {
		return err
	}
	head := r
	head.Stdout = nil
	head.Stderr = nil
	refs := [2]BlobRef{{fingerprint.Hash(r.Stdout), len(r.Stdout)}, {fingerprint.Hash(r.Stderr), len(r.Stderr)}}
	raw, err := json.Marshal(blobRecord{2, head, refs[0], refs[1]})
	if err != nil {
		return err
	}
	// Admit the metadata first, so a quota refusal writes no files. Nothing is
	// visible to readers until Commit, and the files are written before it.
	conflict, err := s.publishTx(ctx, tx, ReportFor(r), raw, false)
	if err != nil {
		return err
	}
	for i, b := range [][]byte{r.Stdout, r.Stderr} {
		ref, err := s.Blobs.Put(ctx, b)
		if err != nil {
			return err
		}
		if ref != refs[i] {
			return fmt.Errorf("blob store returned an unexpected reference")
		}
	}
	if err = addUsage(ctx, tx, Usage{BlobBytes: additional}); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if conflict {
		return ErrConflict
	}
	return nil
}

// Report records conflict evidence and marks its key unsafe for reuse.
func (s Postgres) Report(ctx context.Context, report ConflictReport) error {
	if err := report.validate(); err != nil {
		return err
	}
	tx, err := s.lifecycle(ctx, true)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // Preserve the primary error; rollback also releases lifecycle resources.
	conflict, err := s.publishTx(ctx, tx, report, nil, true)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if conflict {
		return ErrConflict
	}
	return nil
}

// publishTx inserts the record and its evidence and updates the running totals.
// It reports whether the key is now quarantined; the caller commits.
func (s Postgres) publishTx(ctx context.Context, tx *storageTx, report ConflictReport, raw []byte, force bool) (bool, error) {
	defer observe.Measure(ctx, observe.SQL)()
	if err := report.validate(); err != nil {
		return false, err
	}
	digest := report.digest()
	limits, err := s.policy(ctx, tx)
	if err != nil {
		return false, err
	}
	usage, err := readUsage(ctx, tx)
	if err != nil {
		return false, err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM receipt_entries_v2 WHERE key=$1)", report.Key).Scan(&exists); err != nil {
		return false, err
	}
	var added Usage
	var recordBytes int64
	if !exists {
		recordBytes, err = jsonBytes(ctx, tx, raw)
		if err != nil {
			return false, err
		}
		if usage.Entries >= limits.MaxEntries || recordBytes > limits.MaxMetadataBytes-usage.MetadataBytes {
			return false, ErrQuota
		}
	}
	var record any
	if raw != nil {
		record = string(raw)
	}
	inserted, err := execInserted(ctx, tx, `INSERT INTO receipt_entries_v2(key,digest,record,quarantined)
 VALUES($1,$2,$3,$4) ON CONFLICT(key) DO NOTHING`, report.Key, digest, record, force)
	if err != nil {
		return false, err
	}
	if inserted {
		added.Entries, added.MetadataBytes = 1, recordBytes
	}
	var existing string
	var q bool
	err = tx.QueryRowContext(ctx, "SELECT digest,quarantined FROM receipt_entries_v2 WHERE key=$1 FOR UPDATE", report.Key).Scan(&existing, &q)
	if err != nil {
		return false, err
	}
	evidence, err := json.Marshal(report)
	if err != nil {
		return false, err
	}
	var known bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM receipt_evidence_v2 WHERE key=$1 AND digest=$2)", report.Key, digest).Scan(&known); err != nil {
		return false, err
	}
	usage.Entries += added.Entries
	usage.MetadataBytes += added.MetadataBytes
	bytes, err := jsonBytes(ctx, tx, evidence)
	if err != nil {
		return false, err
	}
	full := !known && (usage.Evidence >= limits.MaxEvidence || bytes > limits.MaxMetadataBytes-usage.MetadataBytes)
	if !full {
		stored, err := execInserted(ctx, tx, `INSERT INTO receipt_evidence_v2(key,digest,report) VALUES($1,$2,$3)
 ON CONFLICT(key,digest) DO NOTHING`, report.Key, digest, string(evidence))
		if err != nil {
			return false, err
		}
		if stored {
			added.Evidence, added.MetadataBytes = 1, added.MetadataBytes+bytes
		}
	}
	conflict := q || force || existing != digest
	if full && !conflict {
		return false, ErrQuota
	}
	if conflict {
		if _, err = tx.ExecContext(ctx, "UPDATE receipt_entries_v2 SET quarantined=TRUE,evidence_capped=evidence_capped OR $2 WHERE key=$1", report.Key, full); err != nil {
			return false, err
		}
	}
	return conflict, addUsage(ctx, tx, added)
}

// execInserted runs an INSERT … ON CONFLICT DO NOTHING and reports whether it added a row.
func execInserted(ctx context.Context, tx *storageTx, query string, args ...any) (bool, error) {
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

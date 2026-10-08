package cache

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"receipt/internal/fingerprint"

	_ "github.com/lib/pq"
)

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

func ReportFor(r Result) ConflictReport {
	return ConflictReport{r.Key, r.Manifest, fingerprint.Hash(r.Stdout), fingerprint.Hash(r.Stderr), r.ExitCode}
}
func (r ConflictReport) validate() error {
	if !validKey.MatchString(r.Key) || r.Manifest.Version != 1 || r.Key != r.Manifest.Key() || !validKey.MatchString(r.StdoutHash) || !validKey.MatchString(r.StderrHash) || r.ExitCode < 0 || r.ExitCode > 255 {
		return fmt.Errorf("invalid conflict report")
	}
	return nil
}
func (r ConflictReport) digest() string {
	// Execution time and creation time do not define output equality.
	b, _ := json.Marshal(struct {
		Stdout, Stderr string
		ExitCode       int
	}{r.StdoutHash, r.StderrHash, r.ExitCode})
	return fingerprint.Hash(b)
}

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
// Uploads precede transactions: failed metadata commits can leave orphan blobs.
type Postgres struct {
	DB    *sql.DB
	Blobs BlobStore
}

func (s Postgres) Migrate(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
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
 );`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s Postgres) Quarantined(ctx context.Context, key string) (bool, error) {
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

func (s Postgres) Get(ctx context.Context, key string) (Result, error) {
	var empty Result
	if !validKey.MatchString(key) {
		return empty, fmt.Errorf("invalid key")
	}
	var raw []byte
	var digest string
	var q bool
	err := s.DB.QueryRowContext(ctx, "SELECT record, digest, quarantined FROM receipt_entries_v2 WHERE key=$1", key).Scan(&raw, &digest, &q)
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

func (s Postgres) Put(ctx context.Context, r Result) error {
	if err := Validate(r); err != nil {
		return err
	}
	// Retain the v1 wire size contract; storage representation alone changes.
	if _, err := Encode(r); err != nil {
		return err
	}
	stdout, err := s.Blobs.Put(ctx, r.Stdout)
	if err != nil {
		return err
	}
	stderr, err := s.Blobs.Put(ctx, r.Stderr)
	if err != nil {
		return err
	}
	head := r
	head.Stdout = nil
	head.Stderr = nil
	raw, err := json.Marshal(blobRecord{2, head, stdout, stderr})
	if err != nil {
		return err
	}
	return s.publish(ctx, ReportFor(r), raw, false)
}

func (s Postgres) Report(ctx context.Context, report ConflictReport) error {
	return s.publish(ctx, report, nil, true)
}

func (s Postgres) publish(ctx context.Context, report ConflictReport, raw []byte, force bool) error {
	if err := report.validate(); err != nil {
		return err
	}
	digest := report.digest()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var record any
	if raw != nil {
		record = string(raw)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO receipt_entries_v2(key,digest,record,quarantined)
 VALUES($1,$2,$3,$4) ON CONFLICT(key) DO NOTHING`, report.Key, digest, record, force)
	if err != nil {
		return err
	}
	var existing string
	var q bool
	err = tx.QueryRowContext(ctx, "SELECT digest,quarantined FROM receipt_entries_v2 WHERE key=$1 FOR UPDATE", report.Key).Scan(&existing, &q)
	if err != nil {
		return err
	}
	evidence, _ := json.Marshal(report)
	_, err = tx.ExecContext(ctx, `INSERT INTO receipt_evidence_v2(key,digest,report) VALUES($1,$2,$3)
 ON CONFLICT(key,digest) DO NOTHING`, report.Key, digest, string(evidence))
	if err != nil {
		return err
	}
	conflict := q || force || existing != digest
	if conflict {
		if _, err = tx.ExecContext(ctx, "UPDATE receipt_entries_v2 SET quarantined=TRUE WHERE key=$1", report.Key); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if conflict {
		return ErrConflict
	}
	return nil
}

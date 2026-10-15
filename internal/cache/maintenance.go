package cache

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"receipt/internal/fingerprint"
	"receipt/internal/observe"
	"sync"
	"time"
)

// ErrQuota indicates that a shared storage budget would be exceeded.
var ErrQuota = errors.New("shared storage quota exceeded")

// All shared reads/writes and maintenance must participate. This deliberately
// serializes writers for a small deployment; it is not a high-throughput design.
const lifecycleLock int64 = 72656365697075

// Limits bounds stored blob bytes, logical JSON bytes, action rows and evidence rows.
type Limits struct {
	MaxBlobBytes     int64 `json:"max_blob_bytes"`
	MaxMetadataBytes int64 `json:"max_metadata_bytes"`
	MaxEntries       int64 `json:"max_entries"`
	MaxEvidence      int64 `json:"max_evidence"`
}

// DefaultLimits supplies the initial budgets for a newly initialized shared store.
var DefaultLimits = Limits{1 << 30, 64 << 20, 10000, 100000}

// Usage reports logical metadata size and inventoried filesystem usage, not database disk size.
type Usage struct {
	BlobBytes      int64 `json:"blob_bytes"`
	BlobFiles      int64 `json:"blob_files"`
	PendingFiles   int64 `json:"pending_files"`
	MetadataBytes  int64 `json:"metadata_bytes"`
	Entries        int64 `json:"entries"`
	Evidence       int64 `json:"evidence"`
	EvidenceCapped int64 `json:"evidence_capped_entries"`
}

// MaintenanceResult reports a dry-run plan or completed maintenance changes.
type MaintenanceResult struct {
	Applied bool  `json:"applied"`
	Entries int64 `json:"entries"`
	Files   int64 `json:"files"`
	Bytes   int64 `json:"bytes"`
}

type storageTx struct {
	*sql.Tx
	conn      *sql.Conn
	unlock    func()
	closeOnce sync.Once
}

func (tx *storageTx) close() {
	tx.closeOnce.Do(func() {
		tx.conn.Close() //nolint:errcheck // Resource cleanup must not replace the primary operation error.
		if tx.unlock != nil {
			tx.unlock()
		}
	})
}
func (tx *storageTx) Rollback() error { err := tx.Tx.Rollback(); tx.close(); return err }
func (tx *storageTx) Commit() error   { err := tx.Tx.Commit(); tx.close(); return err }

func (s Postgres) lifecycle(ctx context.Context, exclusive bool) (*storageTx, error) {
	// Do not let database/sql release the lock automatically on cancellation
	// while a filesystem operation is still in progress. Release on return.
	poolDone := observe.Measure(ctx, observe.Pool)
	conn, err := s.DB.Conn(ctx)
	poolDone()
	if err != nil {
		return nil, err
	}
	sqltx, err := conn.BeginTx(context.WithoutCancel(ctx), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		conn.Close() //nolint:errcheck // Resource cleanup must not replace the primary operation error.
		return nil, err
	}
	tx := &storageTx{Tx: sqltx, conn: conn}
	function := "pg_advisory_xact_lock_shared"
	if exclusive {
		function = "pg_advisory_xact_lock"
	}
	lockDone := observe.Measure(ctx, observe.DatabaseLock)
	_, err = tx.ExecContext(ctx, "SELECT "+function+"($1)", lifecycleLock)
	lockDone()
	if err != nil {
		tx.Rollback() //nolint:errcheck // Preserve the primary error; rollback also releases lifecycle resources.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	blobs, err := s.managed()
	if err != nil {
		tx.Rollback() //nolint:errcheck // Preserve the primary error; rollback also releases lifecycle resources.
		return nil, err
	}
	root, err := blobs.Location()
	if err != nil {
		tx.Rollback() //nolint:errcheck // Preserve the primary error; rollback also releases lifecycle resources.
		return nil, err
	}
	fileLockDone := observe.Measure(ctx, observe.FilesystemLock)
	tx.unlock, err = lockBlobDirectory(ctx, root, exclusive)
	fileLockDone()
	if err != nil {
		tx.Rollback() //nolint:errcheck // Preserve the primary error; rollback also releases lifecycle resources.
		return nil, err
	}
	return tx, nil
}

func (s Postgres) managed() (ManagedBlobs, error) {
	blobs, ok := s.Blobs.(ManagedBlobs)
	if !ok {
		return nil, fmt.Errorf("blob backend does not support safe maintenance")
	}
	return blobs, nil
}

func (s Postgres) initializePolicy(ctx context.Context) error {
	blobs, err := s.managed()
	if err != nil {
		return err
	}
	root, err := blobs.Location()
	if err != nil {
		return err
	}
	tx, err := s.lifecycle(ctx, true)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // Preserve the primary error; rollback also releases lifecycle resources.
	idBytes := make([]byte, 32)
	if _, err = rand.Read(idBytes); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO receipt_storage_policy_v3(singleton,blob_root,store_id,max_blob_bytes,max_metadata_bytes,max_entries,max_evidence)
 VALUES(TRUE,$1,$2,$3,$4,$5,$6) ON CONFLICT(singleton) DO NOTHING`, root, hex.EncodeToString(idBytes), DefaultLimits.MaxBlobBytes, DefaultLimits.MaxMetadataBytes, DefaultLimits.MaxEntries, DefaultLimits.MaxEvidence)
	if err != nil {
		return err
	}
	var boundRoot, id string
	if err = tx.QueryRowContext(ctx, "SELECT blob_root,store_id FROM receipt_storage_policy_v3 WHERE singleton=TRUE").Scan(&boundRoot, &id); err != nil {
		return err
	}
	if boundRoot != root {
		return fmt.Errorf("blob directory does not match database storage policy")
	}
	if err = blobs.Bind(ctx, id, true); err != nil {
		return err
	}
	if _, err = s.policy(ctx, tx); err != nil {
		return err
	}
	if err = s.reconcileUsage(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s Postgres) policy(ctx context.Context, tx *storageTx) (Limits, error) {
	var l Limits
	var root, id string
	err := tx.QueryRowContext(ctx, `SELECT blob_root,store_id,max_blob_bytes,max_metadata_bytes,max_entries,max_evidence FROM receipt_storage_policy_v3 WHERE singleton=TRUE`).Scan(&root, &id, &l.MaxBlobBytes, &l.MaxMetadataBytes, &l.MaxEntries, &l.MaxEvidence)
	if err != nil {
		return l, err
	}
	blobs, err := s.managed()
	if err != nil {
		return l, err
	}
	location, err := blobs.Location()
	if err != nil {
		return l, err
	}
	if location != root {
		return l, fmt.Errorf("blob directory does not match database storage policy")
	}
	if err = blobs.Bind(ctx, id, false); err != nil {
		return l, err
	}
	return l, nil
}

func metadataUsage(ctx context.Context, tx *storageTx) (Usage, error) {
	defer observe.Measure(ctx, observe.SQL)()
	var u Usage
	err := tx.QueryRowContext(ctx, `SELECT count(*), COALESCE(sum(octet_length(record::text)),0), count(*) FILTER(WHERE evidence_capped) FROM receipt_entries_v2`).Scan(&u.Entries, &u.MetadataBytes, &u.EvidenceCapped)
	if err != nil {
		return u, err
	}
	var evidenceBytes int64
	err = tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(octet_length(report::text)),0) FROM receipt_evidence_v2`).Scan(&u.Evidence, &evidenceBytes)
	u.MetadataBytes += evidenceBytes
	return u, err
}

// readUsage returns the running totals that uploads check their quotas against.
// Every writer updates them in the transaction that changes what they count, so
// an upload never has to scan the tables or walk the output directory.
func readUsage(ctx context.Context, tx *storageTx) (Usage, error) {
	var u Usage
	err := tx.QueryRowContext(ctx, "SELECT blob_bytes,metadata_bytes,entries,evidence FROM receipt_storage_usage_v6 WHERE singleton=TRUE").Scan(&u.BlobBytes, &u.MetadataBytes, &u.Entries, &u.Evidence)
	return u, err
}

// addUsage applies a change to the running totals inside the caller's transaction.
func addUsage(ctx context.Context, tx *storageTx, d Usage) error {
	if d == (Usage{}) {
		return nil
	}
	_, err := tx.ExecContext(ctx, `UPDATE receipt_storage_usage_v6 SET blob_bytes=blob_bytes+$1,metadata_bytes=metadata_bytes+$2,entries=entries+$3,evidence=evidence+$4 WHERE singleton=TRUE`, d.BlobBytes, d.MetadataBytes, d.Entries, d.Evidence)
	return err
}

// reconcileUsage recounts everything and replaces the running totals. It runs
// at every startup, so totals left wrong by a crash after file writes, an older
// build or a restored backup are corrected the next time a process opens the store.
func (s Postgres) reconcileUsage(ctx context.Context, tx *storageTx) error {
	u, _, err := s.inventory(ctx, tx)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO receipt_storage_usage_v6(singleton,blob_bytes,metadata_bytes,entries,evidence) VALUES(TRUE,$1,$2,$3,$4)
 ON CONFLICT(singleton) DO UPDATE SET blob_bytes=EXCLUDED.blob_bytes,metadata_bytes=EXCLUDED.metadata_bytes,entries=EXCLUDED.entries,evidence=EXCLUDED.evidence`, u.BlobBytes, u.MetadataBytes, u.Entries, u.Evidence)
	return err
}

func jsonBytes(ctx context.Context, tx *storageTx, b []byte) (int64, error) {
	if b == nil {
		return 0, nil
	}
	var size int64
	err := tx.QueryRowContext(ctx, "SELECT octet_length($1::jsonb::text)", string(b)).Scan(&size)
	return size, err
}

func (s Postgres) inventory(ctx context.Context, tx *storageTx) (Usage, []BlobObject, error) {
	defer observe.Measure(ctx, observe.Inventory)()
	u, err := metadataUsage(ctx, tx)
	if err != nil {
		return u, nil, err
	}
	blobs, err := s.managed()
	if err != nil {
		return u, nil, err
	}
	objects, err := blobs.Inventory(ctx)
	if err != nil {
		return u, nil, err
	}
	for _, o := range objects {
		u.BlobBytes += o.Size
		if o.Pending {
			u.PendingFiles++
		} else {
			u.BlobFiles++
		}
	}
	return u, objects, nil
}

// Stats reads current budgets and usage while holding the lifecycle read locks.
func (s Postgres) Stats(ctx context.Context) (Limits, Usage, error) {
	tx, err := s.lifecycle(ctx, false)
	if err != nil {
		return Limits{}, Usage{}, err
	}
	defer tx.Rollback() //nolint:errcheck // Preserve the primary error; rollback also releases lifecycle resources.
	l, err := s.policy(ctx, tx)
	if err != nil {
		return l, Usage{}, err
	}
	u, _, err := s.inventory(ctx, tx)
	return l, u, err
}

// SetLimits validates proposed budgets and optionally persists them.
func (s Postgres) SetLimits(ctx context.Context, l Limits, apply bool) error {
	if err := l.validate(); err != nil {
		return err
	}
	tx, err := s.lifecycle(ctx, true)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // Preserve the primary error; rollback also releases lifecycle resources.
	if _, err = s.policy(ctx, tx); err != nil {
		return err
	}
	u, _, err := s.inventory(ctx, tx)
	if err != nil {
		return err
	}
	if u.BlobBytes > l.MaxBlobBytes || u.MetadataBytes > l.MaxMetadataBytes || u.Entries > l.MaxEntries || u.Evidence > l.MaxEvidence {
		return fmt.Errorf("limits are below current usage; prune/collect first")
	}
	if !apply {
		return nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE receipt_storage_policy_v3 SET max_blob_bytes=$1,max_metadata_bytes=$2,max_entries=$3,max_evidence=$4 WHERE singleton=TRUE`, l.MaxBlobBytes, l.MaxMetadataBytes, l.MaxEntries, l.MaxEvidence)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// validate aggregates independent field errors before any database work.
func (l Limits) validate() error {
	var errs []error
	for _, field := range []struct {
		name  string
		value int64
	}{
		{"max_blob_bytes", l.MaxBlobBytes},
		{"max_metadata_bytes", l.MaxMetadataBytes},
		{"max_entries", l.MaxEntries},
		{"max_evidence", l.MaxEvidence},
	} {
		if field.value < 0 {
			errs = append(errs, fmt.Errorf("%s: limits must be nonnegative", field.name))
		}
	}
	return errors.Join(errs...)
}

// checkBlobQuota returns the bytes this upload adds to the output directory.
// It checks only this result's two files, not the whole store.
func (s Postgres) checkBlobQuota(ctx context.Context, tx *storageTx, r Result) (int64, error) {
	l, err := s.policy(ctx, tx)
	if err != nil {
		return 0, err
	}
	var q bool
	err = tx.QueryRowContext(ctx, "SELECT quarantined FROM receipt_entries_v2 WHERE key=$1", r.Key).Scan(&q)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if q {
		return 0, ErrConflict
	}
	u, err := readUsage(ctx, tx)
	if err != nil {
		return 0, err
	}
	blobs, err := s.managed()
	if err != nil {
		return 0, err
	}
	present := map[string]bool{}
	additional := int64(0)
	for _, b := range [][]byte{r.Stdout, r.Stderr} {
		ref := fingerprint.Hash(b)
		if present[ref] {
			continue
		}
		present[ref] = true
		stored, err := blobs.Contains(ctx, ref)
		if err != nil {
			return 0, err
		}
		if !stored {
			additional += int64(len(b))
		}
	}
	if additional > l.MaxBlobBytes-u.BlobBytes {
		return 0, ErrQuota
	}
	return additional, nil
}

// Collect only removes unreferenced objects. It does not mutate metadata;
// cancellation/partial deletion is retryable without rollback restoring refs.
func (s Postgres) Collect(ctx context.Context, apply bool) (MaintenanceResult, error) {
	result := MaintenanceResult{Applied: apply}
	tx, err := s.lifecycle(ctx, true)
	if err != nil {
		return result, err
	}
	defer tx.Rollback() //nolint:errcheck // Preserve the primary error; rollback also releases lifecycle resources.
	if _, err = s.policy(ctx, tx); err != nil {
		return result, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT key,digest,record FROM receipt_entries_v2 WHERE record IS NOT NULL")
	if err != nil {
		return result, err
	}
	live := map[string]bool{}
	for rows.Next() {
		var raw []byte
		var key, digest string
		if err = rows.Scan(&key, &digest, &raw); err != nil {
			rows.Close() //nolint:errcheck // Preserve the scan/validation error; successful iteration checks rows.Err.
			return result, err
		}
		var rec blobRecord
		if json.Unmarshal(raw, &rec) != nil || rec.Version != 2 || Validate(rec.Head) != nil || rec.Head.Key != key || len(rec.Head.Stdout) != 0 || len(rec.Head.Stderr) != 0 || (ConflictReport{Key: key, Manifest: rec.Head.Manifest, StdoutHash: rec.Stdout.Hash, StderrHash: rec.Stderr.Hash, ExitCode: rec.Head.ExitCode}).digest() != digest {
			rows.Close() //nolint:errcheck // Preserve the scan/validation error; successful iteration checks rows.Err.
			return result, ErrCorrupt
		}
		for _, ref := range []BlobRef{rec.Stdout, rec.Stderr} {
			if !validKey.MatchString(ref.Hash) || ref.Size < 0 || ref.Size > MaxBlobBytes {
				rows.Close() //nolint:errcheck // Preserve the scan/validation error; successful iteration checks rows.Err.
				return result, ErrCorrupt
			}
			live[ref.Hash] = true
		}
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return result, err
	}
	if closeErr != nil {
		return result, closeErr
	}
	blobs, err := s.managed()
	if err != nil {
		return result, err
	}
	objects, err := blobs.Inventory(ctx)
	if err != nil {
		return result, err
	}
	for _, o := range objects {
		if !o.Pending && live[o.Hash] {
			continue
		}
		if err = ctx.Err(); err != nil {
			return result, err
		}
		if apply {
			if err = blobs.Remove(ctx, o); err != nil {
				return result, err
			}
		}
		result.Files++
		result.Bytes += o.Size
	}
	return result, resetBlobUsage(ctx, tx, apply, objects, result.Bytes)
}

// resetBlobUsage sets the output total to what a completed collection left and
// commits; a dry run changes nothing. The walk saw every file, so the total is
// exact, including files that a failed upload wrote but never counted.
func resetBlobUsage(ctx context.Context, tx *storageTx, apply bool, objects []BlobObject, removed int64) error {
	if !apply {
		return nil
	}
	remaining := -removed
	for _, o := range objects {
		remaining += o.Size
	}
	if _, err := tx.ExecContext(ctx, "UPDATE receipt_storage_usage_v6 SET blob_bytes=$1 WHERE singleton=TRUE", remaining); err != nil {
		return err
	}
	return tx.Commit()
}

// Expire is storage retention, not test-result freshness. It preserves all
// quarantined keys/evidence; clients may still hold standalone/local copies.
func (s Postgres) Expire(ctx context.Context, before time.Time, apply bool) (MaintenanceResult, error) {
	result := MaintenanceResult{Applied: apply}
	if before.IsZero() || before.After(time.Now()) {
		return result, fmt.Errorf("retention cutoff must be in the past")
	}
	tx, err := s.lifecycle(ctx, true)
	if err != nil {
		return result, err
	}
	defer tx.Rollback() //nolint:errcheck // Preserve the primary error; rollback also releases lifecycle resources.
	if _, err = s.policy(ctx, tx); err != nil {
		return result, err
	}
	err = tx.QueryRowContext(ctx, "SELECT count(*) FROM receipt_entries_v2 WHERE NOT quarantined AND created_at<$1", before).Scan(&result.Entries)
	if err != nil {
		return result, err
	}
	if !apply {
		return result, nil
	}
	var removed Usage
	var evidenceBytes int64
	err = tx.QueryRowContext(ctx, `WITH d AS (DELETE FROM receipt_evidence_v2 WHERE key IN (SELECT key FROM receipt_entries_v2 WHERE NOT quarantined AND created_at<$1) RETURNING octet_length(report::text) AS n)
 SELECT count(*),COALESCE(sum(n),0) FROM d`, before).Scan(&removed.Evidence, &evidenceBytes)
	if err != nil {
		return result, err
	}
	err = tx.QueryRowContext(ctx, `WITH d AS (DELETE FROM receipt_entries_v2 WHERE NOT quarantined AND created_at<$1 RETURNING octet_length(record::text) AS n)
 SELECT count(*),COALESCE(sum(n),0) FROM d`, before).Scan(&removed.Entries, &removed.MetadataBytes)
	if err != nil {
		return result, err
	}
	removed.MetadataBytes += evidenceBytes
	if err = addUsage(ctx, tx, Usage{MetadataBytes: -removed.MetadataBytes, Entries: -removed.Entries, Evidence: -removed.Evidence}); err != nil {
		return result, err
	}
	return result, tx.Commit()
}

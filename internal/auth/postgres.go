package auth

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/lib/pq"
)

// credentialLock serializes credential-table migration and issuance. It must
// differ from the shared-storage keys (72656365697074 and 72656365697075) so
// that issuing a credential never waits for, or pauses, cache traffic.
const credentialLock int64 = 72656365697076

// Postgres stores credential hashes and authoritative expiry/revocation state.
// It borrows DB; the caller owns closing the connection pool.
type Postgres struct{ DB *sql.DB }

// Migrate creates the credential table under a migration lock. Existing cache
// tables and wire/key formats are unchanged. No default credential is issued.
func (s Postgres) Migrate(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // Preserve the primary operation error; successful work commits explicitly.
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", credentialLock); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS receipt_credentials_v5 (
 id TEXT PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
 token_hash TEXT NOT NULL CHECK (token_hash ~ '^[a-f0-9]{64}$'),
 subject TEXT NOT NULL CHECK (length(subject) BETWEEN 1 AND 128),
 scopes TEXT[] NOT NULL CHECK (cardinality(scopes) BETWEEN 1 AND 4)
 CHECK (scopes <@ ARRAY['read','publish','report','metrics']::text[]),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 expires_at TIMESTAMPTZ NOT NULL,
 revoked_at TIMESTAMPTZ,
 CHECK (expires_at > created_at)
 )`); err != nil {
		return err
	}
	return tx.Commit()
}

// Issue creates a random credential with explicit scopes and database-clock expiry.
func (s Postgres) Issue(ctx context.Context, subject string, scopes []Scope, lifetime time.Duration) (Issued, error) {
	if err := ValidateIssue(subject, scopes, lifetime); err != nil {
		return Issued{}, err
	}
	id, token, err := generate()
	if err != nil {
		return Issued{}, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Issued{}, err
	}
	defer tx.Rollback() //nolint:errcheck // Preserve the primary operation error; successful work commits explicitly.
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", credentialLock); err != nil {
		return Issued{}, err
	}
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM receipt_credentials_v5").Scan(&count); err != nil {
		return Issued{}, err
	}
	if count >= MaxCredentials {
		return Issued{}, ErrCapacity
	}
	scopes = slices.Clone(scopes)
	slices.Sort(scopes)
	credential := Credential{Principal: Principal{ID: id, Subject: subject, Scopes: scopes}}
	err = tx.QueryRowContext(ctx, `INSERT INTO receipt_credentials_v5(id,token_hash,subject,scopes,expires_at)
 VALUES($1,$2,$3,$4,clock_timestamp()+($5::bigint * interval '1 microsecond'))
 RETURNING created_at, expires_at`, id, digest(token), subject, pq.Array(scopes), lifetime.Microseconds()).Scan(&credential.CreatedAt, &credential.ExpiresAt)
	if err != nil {
		return Issued{}, err
	}
	if err := tx.Commit(); err != nil {
		return Issued{}, err
	}
	return Issued{Credential: credential, Token: token}, nil
}

// Authenticate rechecks the row every time; there is no authorization cache.
// The check is a request-time snapshot, not cancellation of already admitted work.
func (s Postgres) Authenticate(ctx context.Context, token string) (Principal, error) {
	id, err := tokenID(token)
	if err != nil {
		return Principal{}, ErrInvalid
	}
	var principal Principal
	var stored string
	var scopes []string
	var live bool
	err = s.DB.QueryRowContext(ctx, `SELECT id,subject,scopes,expires_at,token_hash,
 revoked_at IS NULL AND expires_at > clock_timestamp()
 FROM receipt_credentials_v5 WHERE id=$1`, id).Scan(&principal.ID, &principal.Subject, pq.Array(&scopes), &principal.ExpiresAt, &stored, &live)
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, ErrInvalid
	}
	if err != nil {
		return Principal{}, fmt.Errorf("credential lookup failed: %w", err)
	}
	if !live || subtle.ConstantTimeCompare([]byte(stored), []byte(digest(token))) != 1 || !subjectPattern.MatchString(principal.Subject) {
		return Principal{}, ErrInvalid
	}
	principal.Scopes = make([]Scope, len(scopes))
	for i, scope := range scopes {
		principal.Scopes[i] = Scope(scope)
	}
	if err := validateScopes(principal.Scopes); err != nil {
		return Principal{}, ErrInvalid
	}
	return principal, nil
}

// Revoke disables future checks. Repeated revocation succeeds without changing its time.
func (s Postgres) Revoke(ctx context.Context, id string) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	result, err := s.DB.ExecContext(ctx, "UPDATE receipt_credentials_v5 SET revoked_at=COALESCE(revoked_at,clock_timestamp()) WHERE id=$1", id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrInvalid
	}
	return nil
}

// List returns bounded public metadata including expired and revoked rows.
func (s Postgres) List(ctx context.Context) ([]Credential, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,subject,scopes,expires_at,created_at,revoked_at
 FROM receipt_credentials_v5 ORDER BY created_at,id LIMIT $1`, MaxCredentials+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // Read-only cleanup; successful iteration checks rows.Err below.
	out := []Credential{}
	for rows.Next() {
		var credential Credential
		var scopes []string
		if err := rows.Scan(&credential.ID, &credential.Subject, pq.Array(&scopes), &credential.ExpiresAt, &credential.CreatedAt, &credential.RevokedAt); err != nil {
			return nil, err
		}
		for _, scope := range scopes {
			credential.Scopes = append(credential.Scopes, Scope(scope))
		}
		out = append(out, credential)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > MaxCredentials {
		return nil, ErrCapacity
	}
	return out, nil
}

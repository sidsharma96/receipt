package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"receipt/internal/auth"
	"strings"
	"testing"
	"time"
)

func authCommandFixture(t *testing.T) (auth.Postgres, string) {
	t.Helper()
	dsn := os.Getenv("RECEIPT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set RECEIPT_TEST_DATABASE_URL to a disposable PostgreSQL database")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("receipt_auth_cli_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		dsn = u.String()
	} else {
		dsn += " search_path=" + schema
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	t.Setenv("RECEIPT_DATABASE_URL", dsn)
	return auth.Postgres{DB: db}, t.TempDir()
}

func TestAuthCLILifecycle(t *testing.T) {
	credentials, dir := authCommandFixture(t)
	ctx := context.Background()
	var out, diagnostics bytes.Buffer
	if err := authCommand(ctx, []string{"issue", "--cache-dir", dir, "--subject", "alice", "--scopes", "read,publish", "--expires-in", "1h"}, &out, &diagnostics); err != nil {
		t.Fatal(err)
	}
	var issued auth.Issued
	if err := json.Unmarshal(out.Bytes(), &issued); err != nil || issued.Token == "" {
		t.Fatal("issue did not return credential", err)
	}
	if _, err := credentials.Authenticate(ctx, issued.Token); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := authCommand(ctx, []string{"list", "--cache-dir", dir}, &out, &diagnostics); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), issued.Token) || strings.Contains(out.String(), "token_hash") {
		t.Fatal("list revealed token")
	}
	out.Reset()
	if err := authCommand(ctx, []string{"revoke", "--cache-dir", dir, "--id", issued.ID}, &out, &diagnostics); err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.Authenticate(ctx, issued.Token); !errors.Is(err, auth.ErrInvalid) {
		t.Fatal("CLI revocation ineffective", err)
	}
	if err := authCommand(ctx, []string{"issue", "--cache-dir", dir, "--subject", "lost-output", "--scopes", "read"}, failingWriter{}, &diagnostics); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal("issue output failure hidden", err)
	}
	list, err := credentials.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatal("broken output pretended to roll back issuance", len(list), err)
	}
	for _, credential := range list {
		if credential.Subject == "lost-output" {
			if err := credentials.Revoke(ctx, credential.ID); err != nil {
				t.Fatal("lost-output credential could not be revoked", err)
			}
		}
	}
}

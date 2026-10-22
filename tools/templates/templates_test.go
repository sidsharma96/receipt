// Package templates checks selected contracts in the deployment examples.
// These are source checks, not a TOML parser or live deployment validation.
package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/sidsharma96/receipt/tools/internal/toolutil"
)

func read(t *testing.T, path string) string {
	t.Helper()
	root, err := toolutil.Root()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	// The templates only use full-line comments; don't mistake policy comments for settings.
	return regexp.MustCompile(`(?m)^\s*#.*$`).ReplaceAllString(string(data), "")
}

func require(t *testing.T, text string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(text, fragment) {
			t.Errorf("missing %s", fragment)
		}
	}
}

func TestFlyTemplatesKeepAppsSeparate(t *testing.T) {
	api := read(t, "deploy/fly/api.toml")
	db := read(t, "deploy/fly/postgres.toml")
	require(t, api, `app = "replace-with-new-receipt-api-name"`, `source = "receipt_blobs"`, `internal_port = 7777`, `port = 443`, `handlers = ["tls", "http"]`, `auto_stop_machines = "off"`)
	require(t, db, `app = "replace-with-new-receipt-db-name"`, `source = "receipt_pg"`, `POSTGRES_INITDB_ARGS = "--auth-host=scram-sha-256"`)
	for _, text := range []string{api, db} {
		require(t, text, `destination = "/data"`, `initial_size = "3gb"`)
		for _, forbidden := range []string{"release_command", "RECEIPT_DATABASE_URL", "POSTGRES_PASSWORD"} {
			if strings.Contains(text, forbidden) {
				t.Error("unexpected setting", forbidden)
			}
		}
	}
	if strings.Contains(db, "services") || strings.Contains(db, "http_service") {
		t.Fatal("database publicly exposed")
	}
}

func TestFlyStartupContract(t *testing.T) {
	require(t, read(t, "deploy/fly/Dockerfile"), `"--postgres"`, `"--credentials"`, `"/data/cache"`, "util-linux")
	require(t, read(t, "deploy/fly/entrypoint.sh"), "mountpoint -q /data", "exec gosu receipt:receipt")
}

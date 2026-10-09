package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestAuthCommandRejectsInvalidInputBeforeDatabase(t *testing.T) {
	t.Setenv("RECEIPT_DATABASE_URL", "")
	for _, args := range [][]string{
		{},
		{"unknown"},
		{"issue"},
		{"list", "--cache-dir", "x", "--subject", "alice"},
		{"issue", "--cache-dir", "x", "--subject", "alice", "--scopes", "unknown"},
		{"issue", "--cache-dir", "x", "--subject", "alice", "--scopes", "read", "--expires-in", "0"},
		{"issue", "--cache-dir", "x", "--subject", "", "--scopes", "read"},
		{"revoke", "--cache-dir", "x"},
		{"revoke", "--cache-dir", "x", "--id", "not-an-id"},
		{"list", "--cache-dir", "x", "extra"},
		{"list", "--cache-dir", "x", "--timeout", "0"},
	} {
		var out, diagnostics bytes.Buffer
		if err := authCommand(context.Background(), args, &out, &diagnostics); err == nil || strings.Contains(err.Error(), "DATABASE_URL") {
			t.Fatal("invalid input reached database", args, err)
		}
	}
	var out, diagnostics bytes.Buffer
	if err := authCommand(context.Background(), []string{"issue", "--help"}, &out, &diagnostics); err != nil || !strings.Contains(diagnostics.String(), "scopes") {
		t.Fatal(err, diagnostics.String())
	}
}

func TestCredentialServerModeValidation(t *testing.T) {
	t.Setenv("RECEIPT_TOKEN", "")
	var diagnostics bytes.Buffer
	if err := serve(context.Background(), []string{"--credentials"}, &diagnostics); err == nil || !strings.Contains(err.Error(), "--postgres") {
		t.Fatal(err)
	}
	if err := serve(context.Background(), nil, &diagnostics); err == nil || !strings.Contains(err.Error(), "RECEIPT_TOKEN") {
		t.Fatal("legacy startup changed", err)
	}
}

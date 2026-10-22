package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sidsharma96/receipt/tools/internal/toolutil"
)

func TestEnvironmentExcludesRunnerSettings(t *testing.T) {
	t.Setenv("RECEIPT_TOKEN", "private")
	t.Setenv("RECEIPT_TEST_DATABASE_URL", "production")
	t.Setenv("RECEIPT_TEST_CARGO", "1")
	env, err := testEnvironment("/go/bin", "test-image")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env, "\n")
	if len(env) != 9 || strings.Contains(joined, "private") || strings.Contains(joined, "RECEIPT_TEST_") || !strings.Contains(joined, "RECEIPT_RUNNER_IMAGE=test-image") {
		t.Fatal(env)
	}
	if _, err := testEnvironment("/go/bin", ""); err == nil {
		t.Fatal("accepted missing image")
	}
}

func TestIdentityReadiness(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
		want   bool
	}{
		{`{"subject":"receipt-self-ci"}`, 200, true},
		{`{"subject":"other"}`, 200, false},
		{`[]`, 200, false},
		{`null`, 200, false},
		{`{}`, 200, false},
		{`invalid`, 200, false},
		{`{"subject":"receipt-self-ci"}`, 503, false},
		{strings.Repeat(" ", 4097) + `{"subject":"receipt-self-ci"}`, 200, false},
	} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/identity" || r.Header.Get("Authorization") != "Bearer private" {
				t.Error("incorrect identity request")
			}
			w.WriteHeader(tc.status)
			w.Write([]byte(tc.body))
		}))
		got := remoteReady(context.Background(), server.URL, "private", server.Client().Transport)
		server.Close()
		if got != tc.want {
			t.Fatalf("%s status %d: %t", tc.body, tc.status, got)
		}
	}
}

func TestNoRedirectOrCredentiallessRequest(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Redirect(w, r, "/destination", http.StatusFound)
	}))
	defer server.Close()
	if remoteReady(context.Background(), server.URL, "private", server.Client().Transport) || calls != 1 {
		t.Fatal("followed redirect")
	}
	if remoteReady(context.Background(), server.URL, "", server.Client().Transport) || calls != 1 {
		t.Fatal("requested without credentials")
	}
	if remoteReady(context.Background(), "http://example.invalid", "private", nil) {
		t.Fatal("allowed HTTP")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if remoteReady(ctx, server.URL, "private", server.Client().Transport) {
		t.Fatal("allowed canceled request")
	}
}

func TestOnlyMainReceivesPrivateCacheCredentials(t *testing.T) {
	root, err := toolutil.Root()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".github/workflows/receipt-ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	uncached, cached, ok := strings.Cut(text, "  cached:\n")
	if !ok || strings.Contains(uncached, "secrets.") || !strings.Contains(cached, "if: github.event_name == 'push' && github.ref == 'refs/heads/main'") || strings.Count(text, "secrets.RECEIPT_SELF_CI_TOKEN") != 1 {
		t.Fatal("private cache job no longer isolated to trusted main pushes")
	}
}

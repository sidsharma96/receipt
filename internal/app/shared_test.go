package app

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"receipt/internal/cache"
	"strings"
	"sync/atomic"
	"testing"
)

type fakeShared struct {
	cache.Disk
	q atomic.Bool
}

func (s *fakeShared) Quarantined(context.Context, string) (bool, error) { return s.q.Load(), nil }
func (s *fakeShared) Report(context.Context, cache.ConflictReport) error {
	s.q.Store(true)
	return cache.ErrConflict
}

func (s *fakeShared) Get(ctx context.Context, key string) (cache.Result, error) {
	if s.q.Load() {
		return cache.Result{}, cache.ErrConflict
	}
	return s.Disk.Get(ctx, key)
}

func (s *fakeShared) Put(ctx context.Context, r cache.Result) error {
	if s.q.Load() {
		return cache.ErrConflict
	}
	return s.Disk.Put(ctx, r)
}

func TestSharedQuarantineBlocksLocalReplay(t *testing.T) {
	_, external := setup(t)
	token := "a-test-token-of-at-least-24-characters"
	t.Setenv("RECEIPT_TOKEN", token)
	store := &fakeShared{Disk: cache.Disk{Dir: filepath.Join(external, "server")}}
	server := httptest.NewServer(cache.Handler(store, token))
	defer server.Close()
	args := []string{"run", "--remote", server.URL, "--allow-generic", "--", "/bin/sh", "-c", "printf pass"}
	invoke(t, args...)
	_, _, status := invoke(t, args...)
	if !strings.Contains(status, "hit") {
		t.Fatal(status)
	}
	store.q.Store(true)
	_, out, status := invoke(t, args...)
	if out != "pass" || strings.Contains(status, "receipt: hit") || !strings.Contains(status, "not stored") {
		t.Fatalf("%q %q", out, status)
	}
}

func TestVerificationReportsSharedFailure(t *testing.T) {
	_, external := setup(t)
	token := "a-test-token-of-at-least-24-characters"
	t.Setenv("RECEIPT_TOKEN", token)
	state := filepath.Join(external, "state")
	if err := os.WriteFile(state, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &fakeShared{Disk: cache.Disk{Dir: filepath.Join(external, "server")}}
	server := httptest.NewServer(cache.Handler(store, token))
	defer server.Close()
	command := "exit $(cat '" + state + "')"
	args := []string{"run", "--remote", server.URL, "--allow-generic", "--", "/bin/sh", "-c", command}
	invoke(t, args...)
	if err := os.WriteFile(state, []byte("7"), 0o600); err != nil {
		t.Fatal(err)
	}
	verify := append([]string{"run", "--verify"}, args[1:]...)
	code, _, status := invoke(t, verify...)
	if code != 7 || !store.q.Load() || !strings.Contains(status, "mismatch") {
		t.Fatalf("%d %q", code, status)
	}
}

func TestSharedOutageDoesNotReplayLocalCopy(t *testing.T) {
	_, external := setup(t)
	token := "a-test-token-of-at-least-24-characters"
	t.Setenv("RECEIPT_TOKEN", token)
	store := &fakeShared{Disk: cache.Disk{Dir: filepath.Join(external, "server")}}
	server := httptest.NewServer(cache.Handler(store, token))
	defer server.Close()
	args := []string{"run", "--remote", server.URL, "--allow-generic", "--", "/bin/sh", "-c", "printf pass"}
	invoke(t, args...)
	server.Close()
	_, out, status := invoke(t, args...)
	if out != "pass" || strings.Contains(status, "receipt: hit") || !strings.Contains(status, "executing instead") {
		t.Fatalf("%q %q", out, status)
	}
}

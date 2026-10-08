package app

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"receipt/internal/cache"
	"strings"
	"testing"
)

func TestLayeredRemoteHitAndFallback(t *testing.T) {
	_, external := setup(t)
	token := "a-test-token-of-at-least-24-characters"
	t.Setenv("RECEIPT_TOKEN", token)
	serverDisk := cache.Disk{Dir: filepath.Join(external, "server")}
	s := httptest.NewServer(cache.Handler(serverDisk, token))
	defer s.Close()
	command := []string{"/bin/sh", "-c", "printf pass"}
	args := append([]string{"run", "--cache-dir", filepath.Join(external, "client-one"), "--remote", s.URL, "--allow-generic", "--"}, command...)
	_, _, status := invoke(t, args...)
	if !strings.Contains(status, "miss") {
		t.Fatal(status)
	}
	args[2] = filepath.Join(external, "client-two")
	_, out, status := invoke(t, args...)
	if out != "pass" || !strings.Contains(status, "hit") {
		t.Fatalf("%q %q", out, status)
	}
	serverEntries, e := serverDisk.List()
	if e != nil || len(serverEntries) != 1 {
		t.Fatalf("%v %v", serverEntries, e)
	}
	args[2] = filepath.Join(external, "client-three")
	s.Close()
	_, out, status = invoke(t, args...)
	if out != "pass" || !strings.Contains(status, "falling back") {
		t.Fatalf("%q %q", out, status)
	}
}

func TestCorruptEntryFallsBack(t *testing.T) {
	dir, _ := setup(t)
	args := []string{"run", "--allow-generic", "--", "/bin/sh", "-c", "printf pass"}
	invoke(t, args...)
	d := cache.Disk{Dir: filepath.Join(dir, ".receipt")}
	entries, _ := d.List()
	if len(entries) != 1 {
		t.Fatal(entries)
	}
	os.WriteFile(filepath.Join(d.Dir, "entries", entries[0].Key+".json"), []byte("partial"), 0600)
	_, out, status := invoke(t, args...)
	if out != "pass" || !strings.Contains(status, "corrupt") {
		t.Fatalf("%q %q", out, status)
	}
	if _, e := d.Get(context.Background(), entries[0].Key); e != nil {
		t.Fatal(e)
	}
}

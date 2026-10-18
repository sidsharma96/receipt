package fingerprint

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"receipt/internal/observe"
	"testing"
)

func discoveryFixture(t *testing.T) string {
	t.Helper()
	root := repo(t)
	for _, name := range []string{"nested", "extra"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range map[string]string{".gitignore": "ignored\n.receipt/\n", ".receipt.json": "{}", "tracked": "old", "space name": "space", "line\nbreak": "newline", "ignored": "ignored", "extra/value": "extra", "nested/input": "nested"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if b, err := exec.Command("git", "-C", root, "add", "tracked").CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", b, err)
	}
	if err := os.Remove(filepath.Join(root, "tracked")); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestBuildAtRootCompatibility(t *testing.T) {
	root := discoveryFixture(t)
	for _, dir := range []string{root, filepath.Join(root, "nested")} {
		resolved, err := Root(context.Background(), dir)
		if err != nil {
			t.Fatal(err)
		}
		args := []string{"/bin/sh", "-c", "true"}
		before, err := Build(context.Background(), dir, args, []string{"extra", "ignored"}, nil, true)
		if err != nil {
			t.Fatal(err)
		}
		ctx, trace := observe.Start(context.Background())
		after, err := BuildAtRoot(ctx, resolved, dir, args, []string{"extra", "ignored"}, nil, true)
		if err != nil {
			t.Fatal(err)
		}
		if before.Key() != after.Key() {
			t.Fatal("root reuse changed key")
		}
		for _, s := range trace.Snapshot() {
			if s.Stage == "discovery" && s.Calls != 1 {
				t.Fatal(s)
			}
		}
		if err := os.WriteFile(filepath.Join(root, "new"), []byte("fresh"), 0o600); err != nil {
			t.Fatal(err)
		}
		fresh, err := BuildAtRoot(context.Background(), resolved, dir, args, []string{"extra", "ignored"}, nil, true)
		if err != nil {
			t.Fatal(err)
		}
		if fresh.Key() == after.Key() {
			t.Fatal("file listing reused")
		}
		if err := os.Remove(filepath.Join(root, "new")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBuildAtRootFailures(t *testing.T) {
	root := repo(t)
	args := []string{"/bin/sh", "-c", "true"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := BuildAtRoot(ctx, root, root, args, nil, nil, true); err == nil {
		t.Fatal("cancellation ignored")
	}
	if _, err := BuildAtRoot(context.Background(), root, root, nil, nil, nil, true); err == nil {
		t.Fatal("empty command accepted")
	}
	if _, err := BuildAtRoot(context.Background(), root, root, args, []string{"missing"}, nil, true); err == nil {
		t.Fatal("missing extra accepted")
	}
	if err := os.Symlink("missing", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildAtRoot(context.Background(), root, root, args, nil, nil, true); err == nil {
		t.Fatal("symlink accepted")
	}
}

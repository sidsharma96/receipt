package fingerprint

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func repo(t *testing.T) string {
	t.Helper()
	p := t.TempDir()
	if b, e := exec.Command("git", "init", "-q", p).CombinedOutput(); e != nil {
		t.Fatalf("git init: %s %v", b, e)
	}
	return p
}

func TestCanonicalKey(t *testing.T) {
	a := Manifest{Version: 1, Command: []string{"pytest"}, Inputs: []Input{{"file", "b", "2"}, {"file", "a", "1"}}}
	b := a
	b.Inputs = []Input{a.Inputs[1], a.Inputs[0]}
	if a.Key() != b.Key() {
		t.Fatal("input order changed key")
	}
	b.Command = []string{"pytest", "-q"}
	if a.Key() == b.Key() {
		t.Fatal("command not fingerprinted")
	}
}

func TestWorkingTreeAndEnvironment(t *testing.T) {
	dir := repo(t)
	path := filepath.Join(dir, "test.py")
	os.WriteFile(path, []byte("one"), 0o600)
	args := []string{"/bin/sh", "-c", "true"}
	ctx := context.Background()
	a, e := Build(ctx, dir, args, nil, nil, true)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(path, []byte("two"), 0o600)
	b, e := Build(ctx, dir, args, nil, nil, true)
	if e != nil {
		t.Fatal(e)
	}
	if a.Key() == b.Key() {
		t.Fatal("untracked file change did not invalidate")
	}
	t.Setenv("RECEIPT_TEST_SECRET", "a secret value")
	c, e := Build(ctx, dir, args, nil, nil, true)
	if e != nil {
		t.Fatal(e)
	}
	if c.Key() == b.Key() {
		t.Fatal("environment change did not invalidate")
	}
	for _, in := range c.Inputs {
		if in.Kind == "environment" && in.Name == "RECEIPT_TEST_SECRET" && in.Hash != "" && in.Hash == "a secret value" {
			t.Fatal("plaintext secret persisted")
		}
	}
}

func TestIgnoredExtraAndSymlink(t *testing.T) {
	dir := repo(t)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("secret.txt\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("one"), 0o600)
	args := []string{"/bin/sh", "-c", "true"}
	a, e := Build(context.Background(), dir, args, []string{"secret.txt"}, nil, true)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("two"), 0o600)
	b, e := Build(context.Background(), dir, args, []string{"secret.txt"}, nil, true)
	if e != nil {
		t.Fatal(e)
	}
	if a.Key() == b.Key() {
		t.Fatal("explicit ignored input missing")
	}
	os.Symlink("secret.txt", filepath.Join(dir, "link"))
	if _, e = Build(context.Background(), dir, args, nil, nil, true); e == nil {
		t.Fatal("symlink was accepted")
	}
}

func TestDiff(t *testing.T) {
	a := Manifest{Inputs: []Input{{"file", "a", "1"}}}
	b := Manifest{Inputs: []Input{{"file", "a", "2"}, {"file", "b", "3"}}}
	if len(Diff(a, b)) != 2 {
		t.Fatal(Diff(a, b))
	}
}

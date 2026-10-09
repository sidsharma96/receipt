package app

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// This extra opt-in copies prepared registry data. It never mutates the supplied
// Cargo home and never downloads anything. Pinning makes the fixture repeatable.
func TestCargoRegistryMutation(t *testing.T) {
	prepared := os.Getenv("RECEIPT_TEST_CARGO_REGISTRY")
	if os.Getenv("RECEIPT_TEST_CARGO") != "1" || prepared == "" {
		t.Skip("set RECEIPT_TEST_CARGO=1 and RECEIPT_TEST_CARGO_REGISTRY to a prepared Cargo home containing memchr 2.8.3")
	}
	dir, external := setup(t)
	home := t.TempDir()
	const registry = "index.crates.io-1949cf8c6b5b557f"
	const crate = "memchr-2.8.3"
	for _, rel := range []string{"registry/index", "registry/src/" + registry + "/" + crate, "registry/cache/" + registry + "/" + crate + ".crate"} {
		copyCargoFixture(t, filepath.Join(prepared, rel), filepath.Join(home, rel))
	}
	t.Setenv("CARGO_HOME", home)
	t.Setenv("CARGO_TARGET_DIR", filepath.Join(external, "target"))
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "Cargo.toml"), "[package]\nname='registry_fixture'\nversion='0.1.0'\nedition='2021'\n[lib]\npath='lib.rs'\n[dependencies]\nmemchr='=2.8.3'\n")
	write(filepath.Join(dir, "lib.rs"), "#[test] fn search() { assert_eq!(memchr::memchr(b'b', b\"abc\"), Some(1)); }\n")
	if out, err := exec.Command("cargo", "generate-lockfile", "--offline").CombinedOutput(); err != nil {
		t.Fatalf("prepare offline fixture: %s %v", out, err)
	}
	lock, err := os.ReadFile(filepath.Join(dir, "Cargo.lock"))
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"run", "--cache-dir", filepath.Join(external, "receipt"), "--", "cargo", "test", "--frozen", "--lib", "--quiet"}
	check := func(want int, diagnostic string) {
		t.Helper()
		code, _, stderr := invoke(t, args...)
		if code != want || !strings.Contains(stderr, diagnostic) {
			t.Fatalf("want %d %s; got %d %s", want, diagnostic, code, stderr)
		}
	}
	check(0, "receipt: miss")
	check(0, "receipt: hit")
	source := filepath.Join(home, "registry", "src", registry, crate, "src", "lib.rs")
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	write(source, string(original)+"\ncompile_error!(\"receipt dependency mutation\");\n")
	// Cargo assumes registry sources are immutable; remove only this fixture's
	// generated dependency artifacts so a real execution must see the edit.
	if out, err := exec.Command("cargo", "clean", "--offline", "-p", "memchr").CombinedOutput(); err != nil {
		t.Fatalf("clean isolated dependency artifacts: %s %v", out, err)
	}
	check(101, "failed results are not cached")
	after, err := os.ReadFile(filepath.Join(dir, "Cargo.lock"))
	if err != nil || string(lock) != string(after) {
		t.Fatal("lockfile changed during dependency mutation")
	}
	write(source, string(original))
	check(0, "receipt: hit")
	// Removing prepared data must fail the probe, not reinterpret it as no deps.
	t.Run("missing offline dependencies", func(t *testing.T) {
		t.Setenv("CARGO_HOME", t.TempDir())
		check(101, "cargo frozen metadata unavailable")
	})
}

func copyCargoFixture(t *testing.T, source, destination string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !entry.Type().IsRegular() {
			t.Fatalf("fixture source is not regular: %s", path)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

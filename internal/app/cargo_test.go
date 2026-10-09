package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Opt-in: tests use installed Rust tools but never download a toolchain/dependency.
func TestCargoReuseAndRefusal(t *testing.T) {
	if os.Getenv("RECEIPT_TEST_CARGO") != "1" {
		t.Skip("set RECEIPT_TEST_CARGO=1 with an installed rustup toolchain")
	}
	for _, name := range []string{"cargo", "rustup", "rustc", "rustdoc"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Fatal(err)
		}
	}
	dir, external := setup(t)
	t.Setenv("CARGO_HOME", t.TempDir())
	t.Setenv("CARGO_TARGET_DIR", filepath.Join(external, "target"))
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("Cargo.toml", "[package]\nname='receipt_fixture'\nversion='0.1.0'\nedition='2021'\n[lib]\npath='lib.rs'\n[features]\nalternate=[]\n[dependencies]\nhelper={path='helper'}\n")
	if err := os.Mkdir(filepath.Join(dir, "helper"), 0o700); err != nil {
		t.Fatal(err)
	}
	write("helper/Cargo.toml", "[package]\nname='helper'\nversion='0.1.0'\nedition='2021'\n[lib]\npath='lib.rs'\n")
	write("helper/lib.rs", "pub fn value() -> i32 { 4 }\n")
	write(".gitignore", "Cargo.lock\n.cargo/\nbuild.rs\n")
	write(".receipt.json", `{"ignore_environment":["RUSTFLAGS"]}`)
	good := "#[test] fn passes() { assert_eq!(helper::value(), 4); }\n"
	write("lib.rs", good)
	if out, err := exec.Command("cargo", "generate-lockfile", "--offline").CombinedOutput(); err != nil {
		t.Fatalf("lockfile: %s %v", out, err)
	}
	args := []string{"run", "--timings", "--cache-dir", filepath.Join(external, "receipt"), "--", "cargo", "test", "--frozen", "--lib", "--quiet"}
	check := func(want int, status string, command []string) {
		t.Helper()
		code, _, errOut := invoke(t, command...)
		if code != want || !strings.Contains(errOut, status) {
			t.Fatalf("code=%d diagnostics=%s", code, errOut)
		}
		if strings.Contains(errOut, "inputs unavailable") && strings.Contains(errOut, `"stage":"lookup"`) {
			t.Fatal("refusal reached cache lookup", errOut)
		}
		if strings.Contains(status, "receipt: hit") {
			for _, stage := range []string{"cargo_tools", "cargo_metadata", "cargo_sources"} {
				if !strings.Contains(errOut, `"stage":"`+stage+`"`) {
					t.Fatalf("hit missing %s timing: %s", stage, errOut)
				}
			}
		}
	}
	check(0, "receipt: miss", args)
	check(0, "receipt: hit", args)
	// These changes are deliberately excluded from the general input scan.
	// The Cargo guard must still refuse lookup of the already saved success.
	t.Run("ignored compiler override", func(t *testing.T) {
		t.Setenv("RUSTFLAGS", "--cfg receipt_override")
		code, _, diagnostics := invoke(t, args...)
		if code != 0 || !strings.Contains(diagnostics, "compiler override RUSTFLAGS") || strings.Contains(diagnostics, `"stage":"lookup"`) {
			t.Fatalf("override reused result: %d %s", code, diagnostics)
		}
	})
	check(0, "receipt: hit", args)
	if err := os.Mkdir(filepath.Join(dir, ".cargo"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(".cargo/config.toml", "# deliberately ignored but still unsupported\n")
	check(0, "cargo config files require uncached execution", args)
	if err := os.Remove(filepath.Join(dir, ".cargo", "config.toml")); err != nil {
		t.Fatal(err)
	}
	check(0, "receipt: hit", args)
	check(0, "receipt: miss", append(append([]string{}, args...), "--features", "alternate"))
	write("lib.rs", "#[test] fn fails() { assert!(false); }\n")
	check(101, "failed results are not cached", args)
	write("lib.rs", good)
	check(0, "receipt: hit", args)
	write("helper/lib.rs", "pub fn value() -> i32 { 3 }\n")
	check(101, "failed results are not cached", args)
	write("helper/lib.rs", "pub fn value() -> i32 { 4 }\n")
	check(0, "receipt: hit", args)
	lock, err := os.ReadFile(filepath.Join(dir, "Cargo.lock"))
	if err != nil {
		t.Fatal(err)
	}
	write("Cargo.lock", string(lock)+"\n# receipt lock change\n")
	check(0, "receipt: miss", args)
	write("build.rs", "fn main() {}\n")
	check(0, "build scripts and procedural macros", args)
	// Generic opt-in is not a way around a recognized adapter's policy.
	generic := append([]string{"run", "--allow-generic"}, args[1:]...)
	check(0, "build scripts and procedural macros", generic)
	if err := os.Remove(filepath.Join(dir, "build.rs")); err != nil {
		t.Fatal(err)
	}
	check(0, "receipt: hit", args)
	write("helper/Cargo.toml", "[package]\nname='helper'\nversion='0.1.0'\nedition='2021'\n[lib]\npath='lib.rs'\nproc-macro=true\n")
	// This dependency cannot export an ordinary function as a procedural macro.
	// The original compiler failure must win over the earlier saved success.
	check(101, `package "helper" uses proc-macro`, args)
}

package fingerprint

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sidsharma96/receipt/internal/observe"
)

func TestCargoPrivateDiagnosticAndFailedTiming(t *testing.T) {
	for _, name := range []string{"https://secret@private.invalid/crate", "line\nbreak", strings.Repeat("a", 65), ""} {
		if cargoDiagnosticName(name) != "<unnamed>" {
			t.Fatal("untrusted package name escaped diagnostic boundary")
		}
	}
	if cargoDiagnosticName("my-crate_2") != "my-crate_2" {
		t.Fatal("valid crate name lost")
	}
	ctx, trace := observe.Start(context.Background())
	_, err := cargoMetadataOutput(ctx, t.TempDir(), "sh", []string{"-c", "echo private-registry-secret >&2; exit 1"})
	if err == nil || !strings.Contains(err.Error(), "frozen metadata unavailable") || strings.Contains(err.Error(), "private-registry-secret") {
		t.Fatalf("unsafe or unhelpful error: %v", err)
	}
	timings := trace.Snapshot()
	if len(timings) != 1 || timings[0].Stage != "cargo_metadata" || timings[0].Calls != 1 {
		t.Fatalf("failed probe timing lost: %+v", timings)
	}
}

func TestCargoCommandBoundary(t *testing.T) {
	for _, args := range [][]string{
		{"cargo", "test", "--frozen"},
		{"cargo", "test", "--frozen", "--manifest-path", "member/Cargo.toml", "--features", "a,b", "--no-default-features", "--lib", "--quiet"},
		{"cargo", "test", "--frozen", "--all-features", "--release"},
	} {
		if _, err := parseCargo(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	for _, args := range [][]string{
		{"cargo"},
		{"cargo", "+stable", "test", "--frozen"},
		{"cargo", "test"},
		{"cargo", "test", "--locked"},
		{"cargo", "test", "--frozen", "--manifest-path"},
		{"cargo", "test", "--frozen", "--features", "--lib"},
		{"cargo", "test", "--frozen", "--", "--nocapture"},
		{"cargo", "test", "--frozen", "--frozen"},
		{"cargo", "test", "--frozen", "--config", "build.rustc='other'"},
		{"cargo", "test", "--frozen", "--target", "other"},
		{"cargo", "test", "--frozen", "-p", "other"},
	} {
		if _, err := parseCargo(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	o, err := parseCargo([]string{"cargo", "test", "--frozen", "--features", "a", "--lib"})
	if err != nil || !reflect.DeepEqual(o.metadata, []string{"metadata", "--format-version", "1", "--frozen", "--features", "a"}) {
		t.Fatalf("feature selection not forwarded: %v %v", o, err)
	}
}

func cargoFixture(root string) cargoMetadata {
	return cargoMetadata{
		Version: 1, Workspace: root,
		Packages: []cargoPackage{{ID: "root", Manifest: filepath.Join(root, "Cargo.toml"), Targets: []cargoTarget{{Kind: []cargoTargetKind{cargoLib}, Source: filepath.Join(root, "lib.rs")}}}},
		Resolve:  &cargoResolution{Root: "root", Nodes: []cargoNode{{ID: "root", Deps: []cargoDependency{}}}},
	}
}

func edge(id, kind string) cargoDependency {
	return cargoDependency{Package: id, Kinds: []cargoDependencyKind{{Kind: cargoDependencyType(kind)}}}
}

func TestCargoDependencyClosure(t *testing.T) {
	root := t.TempDir()
	m := cargoFixture(root)
	p := m.Packages[0]
	p.ID = "dependency"
	m.Packages = append(m.Packages, p)
	m.Resolve.Nodes[0].Deps = []cargoDependency{edge("dependency", "dev")}
	// A dependency's own tests are not run. Its dev dependency is not needed.
	m.Resolve.Nodes = append(m.Resolve.Nodes, cargoNode{ID: "dependency", Deps: []cargoDependency{edge("absent-dev", "dev"), edge("root", "")}})
	got, err := cargoClosure(m, root)
	if err != nil || len(got) != 2 {
		t.Fatalf("closure/cycle handling: %v %v", got, err)
	}
	m.Resolve.Nodes[1].Deps[0] = edge("absent-dev", "")
	if _, err := cargoClosure(m, root); err == nil {
		t.Fatal("missing normal dependency accepted")
	}
}

func TestCargoPolicyRefusals(t *testing.T) {
	root := t.TempDir()
	for _, kind := range []string{"custom-build", "proc-macro", "future-kind"} {
		m := cargoFixture(root)
		m.Packages[0].Targets[0].Kind = []cargoTargetKind{cargoTargetKind(kind)}
		if _, err := cargoClosure(m, root); err == nil {
			t.Fatalf("accepted %s", kind)
		}
	}
	for _, modify := range []func(*cargoMetadata){
		func(m *cargoMetadata) { m.Resolve = nil },
		func(m *cargoMetadata) { m.Resolve.Root = "" },
		func(m *cargoMetadata) { m.Version = 2 },
		func(m *cargoMetadata) { m.Resolve.Nodes[0].Deps = nil },
		func(m *cargoMetadata) {
			m.Packages[0].Manifest = filepath.Join(filepath.Dir(root), "outside", "Cargo.toml")
		},
		func(m *cargoMetadata) {
			m.Packages[0].Targets[0].Source = filepath.Join(filepath.Dir(root), "outside.rs")
		},
		func(m *cargoMetadata) { s := "git+https://example.invalid/private"; m.Packages[0].Source = &s },
		func(m *cargoMetadata) { m.Resolve.Nodes[0].Deps = []cargoDependency{edge("root", "future")} },
		func(m *cargoMetadata) { m.Resolve.Nodes = append(m.Resolve.Nodes, m.Resolve.Nodes[0]) },
	} {
		m := cargoFixture(root)
		modify(&m)
		if _, err := cargoClosure(m, root); err == nil {
			t.Fatalf("unsafe metadata accepted: %+v", m)
		}
	}
}

func TestCargoConfigurationAndOverrides(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "child")
	os.Mkdir(dir, 0o700)
	t.Setenv("CARGO_HOME", t.TempDir())
	if _, err := cargoPolicy(dir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUSTC_WRAPPER", "wrapper")
	if _, err := cargoPolicy(dir); err == nil {
		t.Fatal("wrapper accepted")
	}
	t.Setenv("RUSTC_WRAPPER", "")
	t.Setenv("CARGO_BUILD_TARGET", "other")
	if _, err := cargoPolicy(dir); err == nil {
		t.Fatal("target override accepted")
	}
	t.Setenv("CARGO_BUILD_TARGET", "")
	os.Mkdir(filepath.Join(root, ".cargo"), 0o700)
	os.WriteFile(filepath.Join(root, ".cargo", "config.toml"), []byte(""), 0o600)
	if _, err := cargoPolicy(dir); err == nil {
		t.Fatal("ancestor configuration accepted")
	}
}

func TestCargoSourcesCaptureLockAndRegistryBytes(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	m := cargoFixture(root)
	for _, name := range []string{"Cargo.toml", "Cargo.lock", "lib.rs"} {
		os.WriteFile(filepath.Join(root, name), []byte(name), 0o600)
	}
	dir := filepath.Join(home, "registry", "src", "registry", "dep")
	os.MkdirAll(dir, 0o700)
	path := filepath.Join(dir, "lib.rs")
	os.WriteFile(path, []byte("one"), 0o600)
	source := "registry+https://example.invalid/index"
	packages := append(m.Packages, cargoPackage{ID: "dep", Manifest: filepath.Join(dir, "Cargo.toml"), Source: &source})
	scan := func() string {
		inputs, err := cargoSources(context.Background(), m, packages, root, home)
		if err != nil {
			t.Fatal(err)
		}
		return inputs[0].Hash
	}
	a := scan()
	os.WriteFile(path, []byte("two"), 0o600)
	b := scan()
	if a == b {
		t.Fatal("registry bytes not captured")
	}
	os.WriteFile(filepath.Join(root, "Cargo.lock"), []byte("changed"), 0o600)
	if b == scan() {
		t.Fatal("lockfile not captured")
	}
	os.Symlink(path, filepath.Join(dir, "link"))
	if _, err := cargoSources(context.Background(), m, packages, root, home); err == nil {
		t.Fatal("registry symlink accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cargoSources(ctx, m, packages, root, home); err == nil {
		t.Fatal("cancellation lost")
	}
}

func TestCargoScanBudgets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source")
	os.WriteFile(path, []byte("content"), 0o600)
	for _, scan := range []cargoSourceScan{{bytes: cargoMaxBytes}, {files: make([]Input, cargoMaxFiles)}} {
		if err := scan.add(context.Background(), path); err == nil {
			t.Fatal("scan budget ignored")
		}
	}
	var out cargoBuffer
	if _, err := out.Write(make([]byte, cargoProbeLimit+1)); err == nil {
		t.Fatal("probe budget ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cargoOutput(ctx, t.TempDir(), "git", "--version"); err == nil {
		t.Fatal("probe cancellation ignored")
	}
}

func TestCargoToolSelection(t *testing.T) {
	dir := t.TempDir()
	shimPath, resolved, different := filepath.Join(dir, "rustup"), filepath.Join(dir, "rustc"), filepath.Join(dir, "other")
	for _, path := range []string{shimPath, resolved, different} {
		if err := os.WriteFile(path, []byte("tool"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	shim, err := os.Stat(shimPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, selected := range []string{shimPath, resolved} {
		if err := checkCargoTool(selected, resolved, shim); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkCargoTool(different, resolved, shim); err == nil {
		t.Fatal("unresolved tool substitution accepted")
	}
	a, err := fileInput(resolved)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(resolved, []byte("changed compiler"), 0o700)
	b, err := fileInput(resolved)
	if err != nil || a.Hash == b.Hash {
		t.Fatal("tool bytes not distinguished", err)
	}
}

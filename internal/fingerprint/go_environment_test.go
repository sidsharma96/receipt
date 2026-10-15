package fingerprint

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStableGoEnvironment(t *testing.T) {
	root := filepath.Join(t.TempDir(), "space and [brackets]")
	flags := func(suffix string) string {
		return `-fPIC "-ffile-prefix-map=` + filepath.Join(root, "go-build"+suffix) + `=/tmp/go-build" -O2`
	}
	encode := func(flag, tmp, version string) []byte {
		b, err := json.Marshal(map[string]string{"GOGCCFLAGS": flag, "GOTMPDIR": tmp, "GOVERSION": version, "CGO_CFLAGS": "-O2"})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	base, err := stableGoEnvironment(encode(flags("123"), root, "go1.27.0"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(base), "scratch") {
		t.Fatal("generated path was not normalized")
	}
	same, err := stableGoEnvironment(encode(flags("456"), root, "go1.27.0"))
	if err != nil || string(base) != string(same) {
		t.Fatalf("scratch suffix changed identity: %s %v", same, err)
	}
	for _, changed := range [][]byte{
		encode(flags("123")+" -O3", root, "go1.27.0"),
		encode(flags("123"), root, "go1.28.0"),
		encode(flags("123"), root+"other", "go1.27.0"),
		encode(strings.ReplaceAll(flags("123"), "=/tmp/go-build", "=/other"), root, "go1.27.0"),
		encode(strings.ReplaceAll(flags("123"), "ffile", "fdebug"), root, "go1.27.0"),
		encode(flags("123abc"), root, "go1.27.0"),
	} {
		got, err := stableGoEnvironment(changed)
		if err != nil || string(base) == string(got) {
			t.Fatalf("meaningful change lost: %s %v", changed, err)
		}
	}
	for _, data := range []string{`null`, `{}`, `[]`, `{"GOGCCFLAGS":3}`, `{"GOGCCFLAGS":""}`, `{} {}`} {
		if _, err := stableGoEnvironment([]byte(data)); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}

func TestGoEnvironmentPreservesOtherSettings(t *testing.T) {
	flag := "-fdebug-prefix-map=" + filepath.Join(os.TempDir(), "go-build123") + "=/tmp/go-build"
	env := map[string]string{"GOGCCFLAGS": flag, "GOTMPDIR": "", "CGO_CFLAGS": "-O2", "CC": "clang", "FUTURE_SETTING": "one"}
	normalize := func() string {
		b, err := json.Marshal(env)
		if err != nil {
			t.Fatal(err)
		}
		got, err := stableGoEnvironment(b)
		if err != nil {
			t.Fatal(err)
		}
		return string(got)
	}
	base := normalize()
	if !strings.Contains(base, "scratch") {
		t.Fatal("default root/debug flag not normalized")
	}
	for _, field := range []string{"CGO_CFLAGS", "CC", "FUTURE_SETTING"} {
		old := env[field]
		env[field] = "changed"
		if base == normalize() {
			t.Fatalf("lost %s", field)
		}
		env[field] = old
	}
	for _, flags := range []string{"prefix" + flag, flag + "suffix", "-O2", strings.ReplaceAll(flag, "=/tmp/go-build", "=/elsewhere")} {
		env["GOGCCFLAGS"] = flags
		var got map[string]string
		if err := json.Unmarshal([]byte(normalize()), &got); err != nil {
			t.Fatal(err)
		}
		if got["GOGCCFLAGS"] != flags {
			t.Fatalf("rewrote unrecognized flags %q", flags)
		}
	}
}

func TestGoBuildStableAndInvalidates(t *testing.T) {
	dir := repo(t)
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"go", "test", "-count=1", "./..."}
	build := func() Manifest {
		m, err := Build(context.Background(), dir, args, nil, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	a, b := build(), build()
	if a.Key() != b.Key() {
		t.Fatalf("unchanged Go probe differs: %v", Diff(a, b))
	}
	if err := os.WriteFile(path, []byte("package main\n// changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := build()
	if a.Key() == c.Key() {
		t.Fatal("source change lost")
	}
	t.Setenv("CGO_CFLAGS", "-O1")
	d := build()
	if c.Key() == d.Key() {
		t.Fatal("compiler setting change lost")
	}
}

func TestAdjacentScratchFlagsAreBothNormalized(t *testing.T) {
	root := t.TempDir()
	flags := func(a, b string) string {
		return "-ffile-prefix-map=" + filepath.Join(root, "go-build"+a) + "=/tmp/go-build -fdebug-prefix-map=" + filepath.Join(root, "go-build"+b) + "=/tmp/go-build"
	}
	normalize := func(flag string) string {
		b, err := json.Marshal(map[string]string{"GOGCCFLAGS": flag, "GOTMPDIR": root})
		if err != nil {
			t.Fatal(err)
		}
		got, err := stableGoEnvironment(b)
		if err != nil {
			t.Fatal(err)
		}
		return string(got)
	}
	first, second := normalize(flags("1", "2")), normalize(flags("3", "4"))
	if first != second || strings.Count(first, "scratch") != 2 {
		t.Fatalf("second adjacent flag kept its random suffix: %s %s", first, second)
	}
}

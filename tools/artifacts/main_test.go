package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if _, err := git(root, nil, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	return root
}

func write(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func stage(t *testing.T, root, path string) {
	t.Helper()
	// Forced additions occur only in disposable fixtures to test policy bypasses.
	if _, err := git(root, nil, "add", "-f", "--", path); err != nil {
		t.Fatal(err)
	}
}

func TestIndexUsesBlobNotWorkingCopy(t *testing.T) {
	for _, path := range []string{"record.JSON", "tool.py", "tool.pyc", "scripts/utils/probe.go", "large.txt"} {
		t.Run(path, func(t *testing.T) {
			root := fixture(t)
			content := "example"
			if path == "large.txt" {
				content = strings.Repeat("x", maxSourceBytes+1)
			}
			write(t, root, path, content)
			stage(t, root, path)
			if err := os.Remove(filepath.Join(root, path)); err != nil {
				t.Fatal(err)
			}
			if err := check(root, true); err == nil {
				t.Fatal("accepted forbidden staged blob after working deletion")
			}
			if err := check(root, false); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestIgnoredUtilitiesAndDataStayLocal(t *testing.T) {
	root := fixture(t)
	write(t, root, ".gitignore", "scripts/utils/\n.tools/\n")
	write(t, root, "scripts/utils/probe.py", "local")
	write(t, root, ".tools/raw.json", "{}")
	if err := check(root, false); err != nil {
		t.Fatal(err)
	}
	stage(t, root, "scripts/utils/probe.py")
	if err := check(root, true); err == nil {
		t.Fatal("accepted forced utility")
	}
}

func TestSharedGoAndSizeBoundary(t *testing.T) {
	root := fixture(t)
	write(t, root, "tools/helper.go", "package main")
	write(t, root, "exact.txt", strings.Repeat("x", maxSourceBytes))
	stage(t, root, ".")
	if err := check(root, true); err != nil {
		t.Fatal(err)
	}
	write(t, root, "exact.txt", strings.Repeat("x", maxSourceBytes+1))
	if err := check(root, false); err == nil {
		t.Fatal("accepted oversized working file")
	}
	stage(t, root, "exact.txt")
	write(t, root, "exact.txt", "small")
	if err := check(root, true); err == nil {
		t.Fatal("used working size instead of index size")
	}
}

package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunReusesOnlyInitialRoot(t *testing.T) {
	_, external := setup(t)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(external, "git-calls")
	wrapper := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$RECEIPT_GIT_LOG\"\nexec \"$RECEIPT_REAL_GIT\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(external, "git"), []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RECEIPT_REAL_GIT", git)
	t.Setenv("RECEIPT_GIT_LOG", log)
	t.Setenv("PATH", external+string(os.PathListSeparator)+os.Getenv("PATH"))
	args := []string{"run", "--allow-generic", "--", "/bin/sh", "-c", "true"}
	for _, want := range []int{2, 1} {
		if err := os.WriteFile(log, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		invoke(t, args...)
		b, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(b), "rev-parse --show-toplevel") != want || strings.Count(string(b), "ls-files -z") != want {
			t.Fatalf("want %d root and list calls: %s", want, b)
		}
	}
}

func TestPostRunRootIsRediscovered(t *testing.T) {
	dir, _ := setup(t)
	// A nested repository created by the command must change the post-run root.
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(nested); err != nil {
		t.Fatal(err)
	}
	_, _, status := invoke(t, "run", "--allow-generic", "--", "git", "init", "-q")
	if !strings.Contains(status, "not stored") {
		t.Fatal(status)
	}
}

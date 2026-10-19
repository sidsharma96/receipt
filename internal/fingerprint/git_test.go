package fingerprint

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"receipt/internal/observe"
	"sort"
	"strings"
	"testing"
)

func TestSelectedGitCompatibility(t *testing.T) {
	root := discoveryFixture(t)
	for _, dir := range []string{root, filepath.Join(root, "nested")} {
		args := []string{"/bin/sh", "-c", "true"}
		before, err := Build(context.Background(), dir, args, []string{"extra", "ignored"}, nil, true)
		if err != nil {
			t.Fatal(err)
		}
		ctx, trace := observe.Start(context.Background())
		ctx = WithGit(ctx)
		ctx = WithGit(ctx)
		after, err := Build(ctx, dir, args, []string{"extra", "ignored"}, nil, true)
		if err != nil {
			t.Fatal(err)
		}
		canonical := func(m Manifest) []byte {
			sort.Slice(m.Inputs, func(i, j int) bool {
				if m.Inputs[i].Kind != m.Inputs[j].Kind {
					return m.Inputs[i].Kind < m.Inputs[j].Kind
				}
				return m.Inputs[i].Name < m.Inputs[j].Name
			})
			b, _ := json.Marshal(m)
			return b
		}
		if before.Key() != after.Key() || string(canonical(before)) != string(canonical(after)) {
			t.Fatal("selection changed the full manifest or key")
		}
		calls := int64(0)
		for _, timing := range trace.Snapshot() {
			if timing.Stage == "git_selection" {
				calls += timing.Calls
			}
		}
		if calls != 1 {
			t.Fatalf("want one selection, got %d", calls)
		}
	}
}

func TestGitSelectionLifetimeAndWrapper(t *testing.T) {
	root := repo(t)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 2)
	for i := range paths {
		paths[i] = t.TempDir()
		// A custom Git wrapper controls output; it must never be bypassed.
		body := "#!/bin/sh\nif [ \"$1\" = marker ]; then printf '%s' \"$0\"; else exec \"" + realGit + "\" \"$@\"; fi\n"
		if err := os.WriteFile(filepath.Join(paths[i], "git"), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", paths[0])
	ctx := WithGit(context.Background())
	t.Setenv("PATH", paths[1])
	for _, test := range []struct {
		ctx  context.Context
		want string
	}{{ctx, paths[0]}, {WithGit(context.Background()), paths[1]}} {
		b, err := commandOutput(test.ctx, root, "git", "marker")
		if err != nil || string(b) != filepath.Join(test.want, "git") {
			t.Fatalf("wrapper not preserved: %q %v", b, err)
		}
		args := []string{"/bin/sh", "-c", "true"}
		before, err := Build(context.Background(), root, args, nil, nil, true)
		if err != nil {
			t.Fatal(err)
		}
		after, err := Build(test.ctx, root, args, nil, nil, true)
		if err != nil || before.Key() != after.Key() {
			t.Fatalf("wrapper changed key: %v", err)
		}
	}
	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "git")
	if err := os.Symlink(realGit, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", linkDir)
	if selected := WithGit(context.Background()).Value(gitKey{}).(gitSelection); selected.path != link {
		t.Fatalf("custom symlink rewritten: %+v", selected)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Root(canceled, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := Root(WithGit(context.Background()), root); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("missing Git: %v", err)
	}
}

func TestDeveloperGitSelection(t *testing.T) {
	direct := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(direct, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, selected, platform, output, want string
		err                                    error
		calls                                  int
	}{
		{"developer", "/usr/bin/git", "darwin", direct + "\n", direct, nil, 1},
		{"custom", direct, "darwin", "", direct, nil, 0},
		{"symlink", "/custom/bin/git", "darwin", "", "/custom/bin/git", nil, 0},
		{"linux", "/usr/bin/git", "linux", "", "/usr/bin/git", nil, 0},
		{"failed", "/usr/bin/git", "darwin", "", "/usr/bin/git", errors.New("unavailable"), 1},
		{"relative", "/usr/bin/git", "darwin", "relative/git", "/usr/bin/git", nil, 1},
		{"multiline", "/usr/bin/git", "darwin", direct + "\nextra", "/usr/bin/git", nil, 1},
		{"missing", "/usr/bin/git", "darwin", direct + "-missing", "/usr/bin/git", nil, 1},
		{"launcher", "/usr/bin/git", "darwin", "/usr/bin/git", "/usr/bin/git", nil, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			got := developerGit(context.Background(), test.selected, test.platform, func(context.Context) ([]byte, error) {
				calls++
				return []byte(test.output), test.err
			})
			if got != test.want || calls != test.calls {
				t.Fatalf("got %s, %d calls", got, calls)
			}
		})
	}
}

func TestDeveloperGitUsesActiveSelection(t *testing.T) {
	if _, err := os.Stat("/usr/bin/xcrun"); err != nil {
		t.Skip("Apple developer tools unavailable")
	}
	// Verify real developer-directory selection, not a PATH-provided xcrun.
	b, err := exec.Command("/usr/bin/xcode-select", "-p").Output()
	if err != nil {
		t.Skip("no selected developer directory")
	}
	t.Setenv("DEVELOPER_DIR", strings.TrimSpace(string(b)))
	want, err := exec.Command("/usr/bin/xcrun", "--find", "git").Output()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	ctx := WithGit(context.Background())
	selected := ctx.Value(gitKey{}).(gitSelection)
	if selected.path != strings.TrimSpace(string(want)) || selected.err != nil {
		t.Fatalf("selection: %+v", selected)
	}
	t.Setenv("DEVELOPER_DIR", filepath.Join(t.TempDir(), "missing"))
	// A new invocation resolves again and falls back to the selected launcher.
	next := WithGit(context.Background()).Value(gitKey{}).(gitSelection)
	if next.path != "/usr/bin/git" {
		t.Fatalf("stale developer selection: %+v", next)
	}
}

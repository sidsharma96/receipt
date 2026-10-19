package fingerprint

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// legacyScratchFlags is the exact pre-optimization regexp/repeated-pass policy.
func legacyScratchFlags(flags, prefix string) string {
	pattern := regexp.MustCompile(`(^|[ \t"'])(-f(?:file|debug)-prefix-map=` + regexp.QuoteMeta(prefix) + `)[0-9]+(=/tmp/go-build)([ \t"']|$)`)
	for {
		next := pattern.ReplaceAllString(flags, `${1}${2}<scratch>${3}${4}`)
		if next == flags {
			return flags
		}
		flags = next
	}
}

func TestScratchScannerMatchesLegacy(t *testing.T) {
	for _, root := range []string{"/tmp", "/space and [brackets]", "/quote'\"tab\t", "/line\nbreak", "/日本語", ".", "/tmp/go-build123"} {
		prefix := filepath.Join(root, "go-build")
		for _, kind := range []string{"file", "debug"} {
			flag := "-f" + kind + "-prefix-map=" + prefix + "123=/tmp/go-build"
			for _, before := range []string{"", " ", "\t", "'", "\"", "\n", "x"} {
				for _, after := range []string{"", " ", "\t", "'", "\"", "\n", "x"} {
					flags := before + flag + after + flag + " " + flag
					if got, want := normalizeScratchFlags(flags, prefix), legacyScratchFlags(flags, prefix); got != want {
						t.Fatalf("%q: got %q want %q", flags, got, want)
					}
				}
			}
		}
	}
}

func FuzzScratchScannerMatchesLegacy(f *testing.F) {
	for _, root := range []string{"/tmp", "/a b/[c]", "/line\nbreak", "."} {
		prefix := filepath.Join(root, "go-build")
		f.Add(root, "-ffile-prefix-map="+prefix+"123=/tmp/go-build -fdebug-prefix-map="+prefix+"456=/tmp/go-build")
	}
	f.Add("/tmp", "-ffile-prefix-map=/tmp/go-build12a=/tmp/go-build")
	f.Fuzz(func(t *testing.T, root, flags string) {
		// JSON decoding produces valid UTF-8; raw TMPDIR refusal is tested separately.
		root = strings.ToValidUTF8(root, "\uFFFD")
		prefix := filepath.Join(root, "go-build")
		got, want := normalizeScratchFlags(flags, prefix), legacyScratchFlags(flags, prefix)
		if got != want {
			t.Fatalf("root %q flags %q: got %q want %q", root, flags, got, want)
		}
		// Embed arbitrary path/surroundings around a valid candidate as well.
		candidate := flags + " -ffile-prefix-map=" + prefix + "123=/tmp/go-build " + strings.ReplaceAll(flags, "123", "456")
		if got, want := normalizeScratchFlags(candidate, prefix), legacyScratchFlags(candidate, prefix); got != want {
			t.Fatalf("embedded mismatch: %q != %q", got, want)
		}
	})
}

func TestInvalidUTF8TemporaryRootIsRefused(t *testing.T) {
	t.Setenv("TMPDIR", "/tmp/invalid-\xff")
	if os.TempDir() != "/tmp/invalid-\xff" {
		t.Skip("platform does not use TMPDIR")
	}
	if _, err := stableGoEnvironment([]byte(`{"GOGCCFLAGS":"-O2","GOTMPDIR":""}`)); err == nil {
		t.Fatal("invalid UTF-8 root accepted")
	}
}

func TestScratchFallbackPreservesLegacy(t *testing.T) {
	for _, size := range []int{4095, 4096, 4097, 16384} {
		prefix := strings.Repeat("x", size)
		flags := "-ffile-prefix-map=" + prefix + "123=/tmp/go-build"
		got, err := scratchFlags(flags, prefix)
		if err != nil || got != legacyScratchFlags(flags, prefix) {
			t.Fatal("long path policy changed", err)
		}
	}
	if _, err := scratchFlags("-O2", "/invalid-\xff/go-build"); err == nil {
		t.Fatal("old compiler refusal lost")
	}
}

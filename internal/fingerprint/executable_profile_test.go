package fingerprint

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkExecutableInput isolates executable byte hashing from subprocess probes.
func BenchmarkExecutableInput(b *testing.B) {
	path := filepath.Join(b.TempDir(), "tool")
	data := bytes.Repeat([]byte{0, 127, 255, 1}, 3<<20)
	if err := os.WriteFile(path, data, 0o700); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := executableInput(context.Background(), path, false); err != nil {
			b.Fatal(err)
		}
	}
}

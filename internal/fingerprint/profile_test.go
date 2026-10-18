package fingerprint

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkProfileFileInput measures the existing complete-file fingerprint path.
// Fixture creation is excluded; repeated reads normally hit the OS page cache.
func BenchmarkProfileFileInput(b *testing.B) {
	for _, size := range []int{4096, 8 << 20} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "input")
			if err := os.WriteFile(path, bytes.Repeat([]byte("x"), size), 0o600); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := fileInput(path); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

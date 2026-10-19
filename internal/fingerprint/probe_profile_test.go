package fingerprint

import (
	"context"
	"os/exec"
	"testing"
)

// BenchmarkProbeBreakdown separates a fresh Go process from local normalization.
func BenchmarkProbeBreakdown(b *testing.B) {
	data, err := exec.Command("go", "env", "-json").Output()
	if err != nil {
		b.Fatal(err)
	}
	b.Run("GoProcess", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := commandOutput(context.Background(), "", "go", "env", "-json"); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("GoNormalize", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := stableGoEnvironment(data); err != nil {
				b.Fatal(err)
			}
		}
	})
}

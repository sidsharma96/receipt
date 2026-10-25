package cache

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/sidsharma96/receipt/internal/fingerprint"
)

// BenchmarkProfileCodec isolates wire encoding/decoding from network and storage.
func BenchmarkProfileCodec(b *testing.B) {
	for _, size := range []int{1024, 1 << 20} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			r := example()
			r.Stdout = bytes.Repeat([]byte("x"), size)
			encoded, err := Encode(r)
			if err != nil {
				b.Fatal(err)
			}
			b.Run("encode", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(size))
				for i := 0; i < b.N; i++ {
					if _, err := Encode(r); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("decode", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(size))
				for i := 0; i < b.N; i++ {
					if _, err := Decode(encoded); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

// BenchmarkManifestDecode varies input count while keeping captured output small.
// It measures validation and decoding of resident bytes, excluding disk and scans.
func BenchmarkManifestDecode(b *testing.B) {
	for _, count := range []int{1, 1024, 12886} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			r := example()
			for i := 0; i < count; i++ {
				r.Manifest.Inputs = append(r.Manifest.Inputs, fingerprint.Input{
					Kind: fingerprint.FileInput,
					Name: fmt.Sprintf("/fixture/source/package-%05d/input.go", i),
					Hash: fingerprint.Hash([]byte("fixture")),
				})
			}
			r.Key = r.Manifest.Key()
			encoded, err := Encode(r)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(encoded)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := Decode(encoded); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkProfileDuplicateBlob includes filesystem sync calls but no SQL/HTTP.
func BenchmarkProfileDuplicateBlob(b *testing.B) {
	for _, size := range []int{1 << 20, MaxBlobBytes} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			s := FileBlobs{Dir: b.TempDir()}
			data := bytes.Repeat([]byte("x"), size)
			ctx := context.Background()
			if _, err := s.Put(ctx, data); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := s.Put(ctx, data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

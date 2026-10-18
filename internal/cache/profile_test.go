package cache

import (
	"bytes"
	"context"
	"fmt"
	"testing"
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

// BenchmarkProfileDuplicateBlob includes filesystem sync calls but no SQL/HTTP.
func BenchmarkProfileDuplicateBlob(b *testing.B) {
	s := FileBlobs{Dir: b.TempDir()}
	data := bytes.Repeat([]byte("x"), 1<<20)
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
}

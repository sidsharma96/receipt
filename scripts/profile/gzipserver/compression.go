package main

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash"
	"io"
	"net/http"
	"strings"
	"time"
)

const gzipLevel = 5

type bodyCount struct {
	writer io.Writer
	bytes  int64
}

func (w *bodyCount) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	w.bytes += int64(n)
	return n, err
}

type response struct {
	http.ResponseWriter
	encoded    bodyCount
	hash       hash.Hash
	decoded    int64
	compressor *gzip.Writer
	enabled    bool
	status     int
	failed     bool
}

func (w *response) WriteHeader(code int) {
	if w.status != 0 {
		return
	}
	w.status = code
	if code == http.StatusOK && w.enabled {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		w.Header().Del("Content-Length")
		// gzipLevel is a programmer-owned valid level, checked by fixture tests.
		compressor, err := gzip.NewWriterLevel(&w.encoded, gzipLevel)
		if err != nil {
			w.failed = true
			w.ResponseWriter.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.compressor = compressor
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *response) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	writer := io.Writer(&w.encoded)
	if w.compressor != nil {
		writer = w.compressor
	}
	n, err := writer.Write(p)
	w.decoded += int64(n)
	_, _ = w.hash.Write(p[:n]) // hash.Hash.Write never returns an error.
	if err != nil {
		w.failed = true
	}
	return n, err
}

// compression is deliberately a fixture: one fresh level-5 encoder per response,
// no pooling, only successful GET entry bodies, not Caddy's implementation.
func compression(next http.Handler, enabled bool, events io.Writer) http.Handler {
	return http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		started := time.Now()
		w := &response{
			ResponseWriter: out, encoded: bodyCount{writer: out}, hash: sha256.New(),
			enabled: enabled && r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/entries/") && r.Header.Get("Accept-Encoding") == "gzip",
		}
		next.ServeHTTP(w, r)
		if w.compressor != nil {
			if err := w.compressor.Close(); err != nil {
				w.failed = true
			}
		}
		event := struct {
			Type          string `json:"type"`
			Status        int    `json:"status"`
			Gzip          bool   `json:"gzip"`
			EncodedBytes  int64  `json:"encoded_bytes"`
			DecodedBytes  int64  `json:"decoded_bytes"`
			DecodedSHA256 string `json:"decoded_sha256"`
			HandlerNS     int64  `json:"handler_ns"`
			Failed        bool   `json:"failed"`
		}{"gzip_server", w.status, w.compressor != nil, w.encoded.bytes, w.decoded, hex.EncodeToString(w.hash.Sum(nil)), time.Since(started).Nanoseconds(), w.failed}
		_ = json.NewEncoder(events).Encode(event) //nolint:errcheck // Best-effort diagnostic; the harness rejects missing records.
	})
}

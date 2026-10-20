package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"receipt/internal/cache"
	"receipt/internal/fingerprint"
	"strings"
	"testing"
)

func TestCompressionPreservesRealRemoteResult(t *testing.T) {
	manifest := fingerprint.Manifest{Version: 1, Command: []string{"pytest"}, Inputs: []fingerprint.Input{}}
	saved := cache.Result{Version: 1, Key: manifest.Key(), Manifest: manifest, Stdout: bytes.Repeat([]byte("success\n"), 1024)}
	store := cache.Disk{Dir: t.TempDir()}
	if err := store.Put(context.Background(), saved); err != nil {
		t.Fatal(err)
	}
	expected, err := cache.Encode(saved)
	if err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "identity", true: "gzip"}[enabled], func(t *testing.T) {
			var events bytes.Buffer
			server := httptest.NewServer(compression(cache.Handler(store, "private-fixture-token"), enabled, &events))
			defer server.Close()
			remote := cache.Remote{URL: server.URL, Token: "private-fixture-token", Client: server.Client()}
			actual, err := remote.Get(context.Background(), saved.Key)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := cache.Encode(actual)
			if err != nil || !bytes.Equal(encoded, expected) {
				t.Fatal("decoded result changed")
			}
			var event struct {
				Gzip    bool `json:"gzip"`
				Encoded int  `json:"encoded_bytes"`
				Decoded int  `json:"decoded_bytes"`
			}
			if err := json.Unmarshal(events.Bytes(), &event); err != nil {
				t.Fatal(err)
			}
			if event.Gzip != enabled || event.Decoded != len(expected) {
				t.Fatal("wrong byte accounting")
			}
			if enabled && event.Encoded >= event.Decoded {
				t.Fatal("fixture did not compress")
			}
		})
	}
}

func TestErrorsAndUnrequestedCompressionStayPlain(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusConflict, http.StatusNotFound, http.StatusNoContent, http.StatusOK} {
		var events bytes.Buffer
		handler := compression(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }), true, &events)
		request := httptest.NewRequest(http.MethodGet, "http://localhost/v1/entries/key", nil)
		request.Header.Set("Accept-Encoding", "identity")
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, request)
		if out.Code != status || out.Header().Get("Content-Encoding") != "" {
			t.Fatal("status or identity response changed")
		}
	}
	for _, payload := range []string{"not a gzip stream", "\x1f\x8b\x08"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = io.Copy(w, strings.NewReader(payload))
		}))
		remote := cache.Remote{URL: server.URL, Token: "private-fixture-token", Client: server.Client()}
		_, err := remote.Get(context.Background(), strings.Repeat("a", 64))
		server.Close()
		if err == nil {
			t.Fatal("invalid or truncated gzip accepted")
		}
	}
}

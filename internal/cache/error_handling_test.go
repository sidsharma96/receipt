package cache

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type wrappedFailureStore struct{ err error }

func (s wrappedFailureStore) Get(context.Context, string) (Result, error) { return Result{}, s.err }
func (s wrappedFailureStore) Put(context.Context, Result) error           { return s.err }

func TestHTTPRecognizesWrappedStorageErrors(t *testing.T) {
	v := example()
	payload, err := Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method string
		err    error
		status int
	}{
		{http.MethodGet, ErrMiss, http.StatusNotFound},
		{http.MethodGet, ErrConflict, http.StatusConflict},
		{http.MethodPut, ErrConflict, http.StatusConflict},
		{http.MethodPut, ErrQuota, http.StatusInsufficientStorage},
	} {
		req := httptest.NewRequest(tc.method, "/v1/entries/"+v.Key, bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer test-token")
		out := httptest.NewRecorder()
		Handler(wrappedFailureStore{fmt.Errorf("backend: %w", tc.err)}, "test-token").ServeHTTP(out, req)
		if out.Code != tc.status {
			t.Fatalf("%s %v: status=%d", tc.method, tc.err, out.Code)
		}
	}
}

func TestLimitsAggregateBeforeDatabaseAccess(t *testing.T) {
	err := (Postgres{}).SetLimits(context.Background(), Limits{-1, -2, -3, -4}, true)
	if err == nil {
		t.Fatal("negative limits accepted")
	}
	for _, field := range []string{"max_blob_bytes", "max_metadata_bytes", "max_entries", "max_evidence"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("missing %s: %v", field, err)
		}
	}
	if err := (Limits{}).validate(); err != nil {
		t.Fatal(err)
	}
}

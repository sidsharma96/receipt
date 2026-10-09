package cache

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func postgresFixture(t *testing.T) Postgres {
	t.Helper()
	dsn := os.Getenv("RECEIPT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set RECEIPT_TEST_DATABASE_URL to a disposable PostgreSQL database")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("receipt_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	scoped := dsn
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		scoped = u.String()
	} else {
		scoped += " search_path=" + schema
	}
	db, err := sql.Open("postgres", scoped)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(12)
	t.Cleanup(func() {
		db.Close()
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	s := Postgres{DB: db, Blobs: FileBlobs{Dir: t.TempDir()}}
	if err = s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPostgresConformance(t *testing.T) {
	s := postgresFixture(t)
	ctx := context.Background()
	v := example()
	if _, err := s.Get(ctx, v.Key); !errors.Is(err, ErrMiss) {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.Put(ctx, v) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Get(ctx, v.Key)
	if err != nil || string(got.Stdout) != "pass\n" {
		t.Fatalf("%+v %v", got, err)
	}
	v.DurationNS = 123
	v.CreatedAt = time.Now()
	if err = s.Put(ctx, v); err != nil {
		t.Fatal("timing caused conflict", err)
	}
	var count int
	if err = s.DB.QueryRow("SELECT count(*) FROM receipt_evidence_v2").Scan(&count); err != nil || count != 1 {
		t.Fatalf("evidence %d %v", count, err)
	}
	var raw []byte
	if err = s.DB.QueryRow("SELECT record FROM receipt_entries_v2 WHERE key=$1", v.Key).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var record blobRecord
	if json.Unmarshal(raw, &record) != nil || len(record.Head.Stdout) != 0 || record.Version != 2 {
		t.Fatal("inline output in metadata")
	}
	v.Stdout = []byte("different")
	if err = s.Put(ctx, v); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, v.Key); !errors.Is(err, ErrConflict) {
		t.Fatal("conflicted result replayable", err)
	}
	q, err := s.Quarantined(ctx, v.Key)
	if err != nil || !q {
		t.Fatal(q, err)
	}
	if err = s.DB.QueryRow("SELECT count(*) FROM receipt_evidence_v2").Scan(&count); err != nil || count != 2 {
		t.Fatalf("conflict evidence %d %v", count, err)
	}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal("migration not repeatable", err)
	}
	reopened := Postgres{DB: s.DB, Blobs: s.Blobs}
	if _, err = reopened.Get(ctx, v.Key); !errors.Is(err, ErrConflict) {
		t.Fatal("quarantine not persistent", err)
	}
}

func TestPostgresConcurrentConflictAndReport(t *testing.T) {
	s := postgresFixture(t)
	ctx := context.Background()
	v := example()
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		r := v
		r.Stdout = []byte(fmt.Sprintf("output-%d", i))
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.Put(ctx, r) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if _, err := s.Get(ctx, v.Key); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	failure := example()
	failure.Manifest.Command = []string{"another-command"}
	failure.Key = failure.Manifest.Key()
	failure.ExitCode = 7
	if err := s.Report(ctx, ReportFor(failure)); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, failure.Key); !errors.Is(err, ErrConflict) {
		t.Fatal("report-before-upload failed", err)
	}
	failure.ExitCode = 0
	if err := s.Put(ctx, failure); !errors.Is(err, ErrConflict) {
		t.Fatal("tombstone overwritten", err)
	}
}

func TestPostgresMissingBlobAndHTTP(t *testing.T) {
	s := postgresFixture(t)
	ctx := context.Background()
	v := example()
	token := "test-token-at-least-24-characters"
	server := httptest.NewServer(Handler(s, token))
	defer server.Close()
	remote := Remote{URL: server.URL, Token: token}
	if err := remote.Put(ctx, v); err != nil {
		t.Fatal(err)
	}
	if err := remote.Check(ctx, v.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Get(ctx, v.Key); err != nil {
		t.Fatal(err)
	}
	ref := BlobRef{Hash: ReportFor(v).StdoutHash, Size: len(v.Stdout)}
	files := s.Blobs.(FileBlobs)
	p, _ := files.path(ref.Hash)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, v.Key); !errors.Is(err, ErrMiss) {
		t.Fatal("missing blob not rejected", err)
	}
	if err := remote.Put(ctx, v); err != nil {
		t.Fatal("missing blob not restored", err)
	}
	if err := os.WriteFile(p, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Get(ctx, v.Key); err == nil {
		t.Fatal("corrupt blob replayed")
	}
	if _, err := files.Put(ctx, v.Stdout); !errors.Is(err, ErrCorrupt) {
		t.Fatal(err)
	}
	if err := remote.Report(ctx, ReportFor(v)); err != nil {
		t.Fatal(err)
	}
	if err := remote.Check(ctx, v.Key); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err := remote.Get(ctx, v.Key); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", server.URL+"/v1/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var metrics map[string]int64
	if json.NewDecoder(response.Body).Decode(&metrics) != nil || metrics["conflicts"] < 2 {
		t.Fatal(metrics)
	}
}

type blockedStore struct {
	entered chan struct{}
	release chan struct{}
}

func (s blockedStore) Get(context.Context, string) (Result, error) {
	close(s.entered)
	<-s.release
	return Result{}, ErrMiss
}
func (s blockedStore) Put(context.Context, Result) error { return nil }
func TestHTTPAdmissionAndMetricsAuth(t *testing.T) {
	s := blockedStore{make(chan struct{}), make(chan struct{})}
	token := "test-token-at-least-24-characters"
	h := HandlerLimit(s, token, 1)
	done := make(chan struct{})
	request := func(path string) *http.Request {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		return r
	}
	go func() { defer close(done); h.ServeHTTP(httptest.NewRecorder(), request("/v1/entries/"+example().Key)) }()
	<-s.entered
	out := httptest.NewRecorder()
	h.ServeHTTP(out, request("/v1/entries/"+example().Key))
	if out.Code != 503 {
		t.Fatal(out.Code)
	}
	out = httptest.NewRecorder()
	h.ServeHTTP(out, httptest.NewRequest("GET", "/v1/metrics", nil))
	if out.Code != 401 {
		t.Fatal(out.Code)
	}
	out = httptest.NewRecorder()
	h.ServeHTTP(out, request("/v1/metrics"))
	var metrics map[string]int64
	if json.Unmarshal(out.Body.Bytes(), &metrics) != nil || metrics["active"] != 1 || metrics["rejected"] != 1 {
		t.Fatal(metrics)
	}
	close(s.release)
	<-done
}

package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sidsharma96/receipt/internal/auth"
	"github.com/sidsharma96/receipt/internal/observe"
)

func TestRequestDiagnosticsRedactionAndConcurrentIDs(t *testing.T) {
	var out bytes.Buffer
	log, err := NewRequestLog(&out)
	if err != nil {
		t.Fatal(err)
	}
	spy := &permissionSpy{}
	identity := &authenticationStub{principal: auth.Principal{ID: "private-id", Subject: "private-subject", Scopes: []auth.Scope{auth.Read}}}
	h := log.Handler(HandlerAuthorized(spy, identity, 32))
	var wg sync.WaitGroup
	ids := make(chan string, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest(http.MethodPut, "/v1/entries/"+example().Key+"?secret=private-query", strings.NewReader("private-body"))
			r.Header.Set("Authorization", "Bearer "+scopedTestToken)
			r.Header.Set("X-Receipt-Request-ID", "attacker-id")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden {
				t.Errorf("status %d", w.Code)
			}
			ids <- w.Header().Get("X-Receipt-Request-ID")
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		if id == "" || seen[id] {
			t.Fatal("missing/duplicate ID")
		}
		seen[id] = true
	}
	for _, secret := range []string{scopedTestToken, "private-id", "private-subject", "private-query", "private-body", "attacker-id", example().Key} {
		if strings.Contains(out.String(), secret) {
			t.Fatal("secret in log")
		}
	}
	d := json.NewDecoder(&out)
	for i := 0; i < 32; i++ {
		var e RequestEvent
		if err := d.Decode(&e); err != nil {
			t.Fatal(err)
		}
		if !seen[e.ID] || e.Status != 403 || e.Operation != "publish" || len(e.Stages) != 1 || e.Stages[0].Stage != "authentication" {
			t.Fatalf("%+v", e)
		}
		delete(seen, e.ID)
	}
	if len(seen) != 0 || spy.calls.Load() != 0 {
		t.Fatal("lost IDs or denied storage work")
	}
}

type failedLog struct{}

func (failedLog) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestDiagnosticsPreserveResponseAndLogFailure(t *testing.T) {
	log, err := NewRequestLog(failedLog{})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	log.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204); w.WriteHeader(500) })).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 204 || log.Failures() != 1 {
		t.Fatal(w.Code, log.Failures())
	}
	var out bytes.Buffer
	log, err = NewRequestLog(&out)
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic swallowed")
			}
		}()
		log.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("private-panic") })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/private-path", nil))
	}()
	var e RequestEvent
	if err = json.Unmarshal(out.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	if e.Status != 0 || e.Operation != "unknown" || strings.Contains(out.String(), "private") {
		t.Fatalf("%+v", e)
	}
}

func TestDiagnosticsPostgresLockWaitAndCancellation(t *testing.T) {
	s := postgresFixture(t)
	if err := s.Put(context.Background(), example()); err != nil {
		t.Fatal(err)
	}
	held, err := s.lifecycle(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	ctx, trace := observe.Start(ctx)
	if _, err = s.Get(ctx, example().Key); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	var waited bool
	for _, timing := range trace.Snapshot() {
		if timing.Stage == "db_lock" && timing.NS >= int64(20*time.Millisecond) {
			waited = true
		}
		if timing.Stage == "files" {
			t.Fatal("files touched before lock")
		}
	}
	if !waited {
		t.Fatalf("missing wait: %+v", trace.Snapshot())
	}
	if err = held.Rollback(); err != nil {
		t.Fatal(err)
	}
	ctx, trace = observe.Start(context.Background())
	if _, err = s.Get(ctx, example().Key); err != nil {
		t.Fatal(err)
	}
	stages := map[string]bool{}
	for _, value := range trace.Snapshot() {
		stages[value.Stage] = true
	}
	for _, name := range []string{"db_pool", "db_lock", "fs_lock", "sql", "files"} {
		if !stages[name] {
			t.Fatal("missing " + name)
		}
	}
}

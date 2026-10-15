package cache

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"receipt/internal/auth"
	"strings"
	"sync"
	"testing"
	"time"
)

func credentialFixture(t *testing.T) (Postgres, auth.Postgres) {
	t.Helper()
	shared := postgresFixture(t)
	credentials := auth.Postgres{DB: shared.DB}
	if err := credentials.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return shared, credentials
}

func issueForTest(t *testing.T, credentials auth.Postgres, scopes ...auth.Scope) auth.Issued {
	t.Helper()
	issued, err := credentials.Issue(context.Background(), "alice", scopes, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return issued
}

func TestCredentialIssueAuthenticateListAndRevoke(t *testing.T) {
	shared, credentials := credentialFixture(t)
	ctx := context.Background()
	if err := credentials.Migrate(ctx); err != nil {
		t.Fatal("migration not repeatable", err)
	}
	issued := issueForTest(t, credentials, auth.Publish, auth.Read)
	principal, err := credentials.Authenticate(ctx, issued.Token)
	if err != nil || principal.ID != issued.ID || principal.Subject != "alice" || !principal.Allows(auth.Read) || principal.Allows(auth.Report) {
		t.Fatal("issued credential not authenticated correctly", err)
	}
	var hash string
	if err := shared.DB.QueryRow("SELECT token_hash FROM receipt_credentials_v5 WHERE id=$1", issued.ID).Scan(&hash); err != nil || len(hash) != 64 || hash == issued.Token {
		t.Fatal("raw bearer persisted", err)
	}
	list, err := credentials.List(ctx)
	if err != nil || len(list) != 1 || list[0].ID != issued.ID {
		t.Fatal("credential listing failed", err)
	}
	b := jsonBytesForTest(t, list)
	if strings.Contains(string(b), issued.Token) || strings.Contains(string(b), hash) || strings.Contains(string(b), "token") {
		t.Fatal("list leaked credential material")
	}
	wrong := issued.Token[:len(issued.Token)-1] + "0"
	if wrong == issued.Token {
		wrong = issued.Token[:len(issued.Token)-1] + "1"
	}
	for _, token := range []string{wrong, hash, "rcpt_" + strings.Repeat("f", 32) + "_" + strings.Repeat("f", 64)} {
		if _, err := credentials.Authenticate(ctx, token); !errors.Is(err, auth.ErrInvalid) {
			t.Fatal("forged token accepted", err)
		}
	}
	if err := credentials.Revoke(ctx, issued.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.Authenticate(ctx, issued.Token); !errors.Is(err, auth.ErrInvalid) {
		t.Fatal("revoked token accepted", err)
	}
	list, _ = credentials.List(ctx)
	first := *list[0].RevokedAt
	if err := credentials.Revoke(ctx, issued.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = credentials.List(ctx)
	if !list[0].RevokedAt.Equal(first) {
		t.Fatal("repeat revoke changed timestamp")
	}
	if err := credentials.Revoke(ctx, strings.Repeat("f", 32)); !errors.Is(err, auth.ErrInvalid) {
		t.Fatal("unknown id silently acknowledged", err)
	}
}

func TestDatabaseExpiryAndInvalidStoredScopesFailClosed(t *testing.T) {
	shared, credentials := credentialFixture(t)
	ctx := context.Background()
	issued := issueForTest(t, credentials, auth.Read)
	if _, err := shared.DB.Exec(`UPDATE receipt_credentials_v5 SET scopes=ARRAY['read','read'] WHERE id=$1`, issued.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.Authenticate(ctx, issued.Token); !errors.Is(err, auth.ErrInvalid) {
		t.Fatal("invalid stored scopes authorized", err)
	}
	if _, err := shared.DB.Exec(`UPDATE receipt_credentials_v5 SET scopes=ARRAY['read'],created_at=clock_timestamp()-interval '2 seconds',expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, issued.ID); err != nil {
		t.Fatal(err)
	}
	h := HandlerAuthorized(&permissionSpy{}, credentials, 2)
	if w := scopedRequest(t, h, http.MethodGet, identityPath, issued.Token, nil); w.Code != 401 {
		t.Fatal("expired credential accepted", w.Code)
	}
	if _, err := credentials.Authenticate(ctx, issued.Token); !errors.Is(err, auth.ErrInvalid) {
		t.Fatal(err)
	}
}

func TestCredentialCapacityUnderConcurrentIssuance(t *testing.T) {
	shared, credentials := credentialFixture(t)
	ctx := context.Background()
	// Fill all but one retained row using public, deliberately nonusable fixtures.
	_, err := shared.DB.Exec(`INSERT INTO receipt_credentials_v5(id,token_hash,subject,scopes,expires_at)
 SELECT lpad(to_hex(n),32,'0'),repeat('a',64),'capacity',ARRAY['read'],clock_timestamp()+interval '1 hour' FROM generate_series(1,1023) n`)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := credentials.Issue(ctx, "alice", []auth.Scope{auth.Read}, time.Hour)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	success, full := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			success++
		case errors.Is(err, auth.ErrCapacity):
			full++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || full != 7 {
		t.Fatal("credential capacity raced", success, full)
	}
	list, err := credentials.List(ctx)
	if err != nil || len(list) != auth.MaxCredentials {
		t.Fatal("row budget exceeded", len(list), err)
	}
	if err := credentials.Revoke(ctx, list[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.Issue(ctx, "alice", []auth.Scope{auth.Read}, time.Hour); !errors.Is(err, auth.ErrCapacity) {
		t.Fatal("revoked row incorrectly freed budget", err)
	}
}

func TestScopedRemoteConformanceAndRevocation(t *testing.T) {
	shared, credentials := credentialFixture(t)
	issued := issueForTest(t, credentials, auth.Read, auth.Publish, auth.Report)
	server := httptest.NewServer(HandlerAuthorized(shared, credentials, 2))
	defer server.Close()
	remote := Remote{URL: server.URL, Token: issued.Token}
	ctx := context.Background()
	v := example()
	if err := remote.Put(ctx, v); err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Get(ctx, v.Key); err != nil {
		t.Fatal(err)
	}
	if err := remote.Check(ctx, v.Key); err != nil {
		t.Fatal(err)
	}
	conflict := v
	conflict.Stdout = []byte("different")
	if err := remote.Report(ctx, ReportFor(conflict)); err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Get(ctx, v.Key); !errors.Is(err, ErrConflict) {
		t.Fatal("scoped conflict not quarantined", err)
	}
	if err := credentials.Revoke(ctx, issued.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Get(ctx, v.Key); err == nil {
		t.Fatal("revoked remote authorized")
	}
}

func TestCredentialDatabaseOutageDoesNotFallBack(t *testing.T) {
	shared, credentials := credentialFixture(t)
	issued := issueForTest(t, credentials, auth.Read)
	if err := shared.DB.Close(); err != nil {
		t.Fatal(err)
	}
	store := &permissionSpy{}
	w := scopedRequest(t, HandlerAuthorized(store, credentials, 1), http.MethodGet, "/v1/entries/"+example().Key, issued.Token, nil)
	if w.Code != 503 || store.calls.Load() != 0 {
		t.Fatal("DB outage did not fail closed", w.Code)
	}
}

type pausedPublication struct {
	Postgres
	entered, release chan struct{}
}

func (s pausedPublication) Put(ctx context.Context, r Result) error {
	close(s.entered)
	select {
	case <-s.release:
		return s.Postgres.Put(ctx, r)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestRevocationDoesNotCancelAnAlreadyAuthenticatedUpload(t *testing.T) {
	shared, credentials := credentialFixture(t)
	issued := issueForTest(t, credentials, auth.Publish)
	store := pausedPublication{shared, make(chan struct{}), make(chan struct{})}
	h := HandlerAuthorized(store, credentials, 2)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(store.release) }) }
	defer release()
	v := example()
	body, err := Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- scopedRequest(t, h, http.MethodPut, "/v1/entries/"+v.Key, issued.Token, body) }()
	select {
	case <-store.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("publication did not start")
	}
	if err := credentials.Revoke(context.Background(), issued.ID); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case w := <-done:
		if w.Code != 204 {
			t.Fatal("already authenticated upload unexpectedly revoked", w.Code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("upload stuck")
	}
	if _, err := shared.Get(context.Background(), v.Key); err != nil {
		t.Fatal("admitted upload did not finish", err)
	}
	if w := scopedRequest(t, h, http.MethodPut, "/v1/entries/"+v.Key, issued.Token, body); w.Code != 401 {
		t.Fatal("new request ignored revocation", w.Code)
	}
}

func TestCredentialIssuanceDoesNotWaitForStorageLock(t *testing.T) {
	shared, credentials := credentialFixture(t)
	ctx := context.Background()
	reader, err := shared.lifecycle(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Rollback()
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := credentials.Migrate(bounded); err != nil {
		t.Fatal("credential migration waited for an in-flight storage read", err)
	}
	if _, err := credentials.Issue(bounded, "alice", []auth.Scope{auth.Read}, time.Hour); err != nil {
		t.Fatal("credential issuance waited for an in-flight storage read", err)
	}
}

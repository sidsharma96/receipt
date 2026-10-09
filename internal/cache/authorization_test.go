package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"receipt/internal/auth"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const scopedTestToken = "rcpt_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

type authenticationStub struct {
	principal auth.Principal
	err       error
	calls     atomic.Int64
}

func (s *authenticationStub) Authenticate(context.Context, string) (auth.Principal, error) {
	s.calls.Add(1)
	return s.principal, s.err
}

type permissionSpy struct{ calls atomic.Int64 }

func (s *permissionSpy) Get(context.Context, string) (Result, error) {
	s.calls.Add(1)
	return example(), nil
}
func (s *permissionSpy) Put(context.Context, Result) error { s.calls.Add(1); return nil }
func (s *permissionSpy) Quarantined(context.Context, string) (bool, error) {
	s.calls.Add(1)
	return false, nil
}
func (s *permissionSpy) Report(context.Context, ConflictReport) error { s.calls.Add(1); return nil }

func scopedRequest(t *testing.T, h http.Handler, method, path, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestEveryScopeAgainstEveryOperation(t *testing.T) {
	v := example()
	entry, err := Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	report := jsonBytesForTest(t, ReportFor(v))
	for _, scope := range []auth.Scope{auth.Read, auth.Publish, auth.Report, auth.Metrics} {
		for _, endpoint := range []struct {
			method, path string
			required     auth.Scope
			body         []byte
			status       int
		}{
			{http.MethodGet, "/v1/entries/" + v.Key, auth.Read, nil, 200},
			{http.MethodGet, "/v1/status/" + v.Key, auth.Read, nil, 204},
			{http.MethodPut, "/v1/entries/" + v.Key, auth.Publish, entry, 204},
			{http.MethodPost, "/v1/conflicts/" + v.Key, auth.Report, report, 204},
			{http.MethodGet, "/v1/metrics", auth.Metrics, nil, 200},
		} {
			t.Run(string(scope)+"/"+endpoint.method+endpoint.path, func(t *testing.T) {
				store := &permissionSpy{}
				identity := &authenticationStub{principal: auth.Principal{ID: strings.Repeat("a", 32), Subject: "alice", Scopes: []auth.Scope{scope}}}
				h := HandlerAuthorized(store, identity, 2)
				w := scopedRequest(t, h, endpoint.method, endpoint.path, scopedTestToken, endpoint.body)
				want := http.StatusForbidden
				if scope == endpoint.required {
					want = endpoint.status
				}
				if w.Code != want {
					t.Fatal(w.Code, w.Body.String())
				}
				if want == http.StatusForbidden && store.calls.Load() != 0 {
					t.Fatal("forbidden request touched storage")
				}
				if w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("missing no-store")
				}
			})
		}
	}
}

func TestCredentialFailuresNeverReachStorage(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"invalid", auth.ErrInvalid, 401},
		{"wrapped invalid", errors.Join(errors.New("backend"), auth.ErrInvalid), 401},
		{"unavailable", errors.New("sensitive database detail"), 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &permissionSpy{}
			identity := &authenticationStub{err: tc.err, principal: auth.Principal{Scopes: []auth.Scope{auth.Publish}}}
			w := scopedRequest(t, HandlerAuthorized(store, identity, 1), http.MethodPut, "/v1/entries/"+example().Key, scopedTestToken, []byte("not JSON"))
			if w.Code != tc.want || store.calls.Load() != 0 || strings.Contains(w.Body.String(), "sensitive") {
				t.Fatal(w.Code, w.Body.String(), store.calls.Load())
			}
		})
	}
	identity := &authenticationStub{}
	h := HandlerAuthorized(&permissionSpy{}, identity, 1)
	for _, token := range []string{"", "legacy-token-at-least-24-characters", scopedTestToken + " "} {
		if w := scopedRequest(t, h, http.MethodGet, "/v1/identity", token, nil); w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/v1/identity", nil)
	r.Header.Add("Authorization", "Bearer "+scopedTestToken)
	r.Header.Add("Authorization", "Bearer "+scopedTestToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 || identity.calls.Load() != 0 {
		t.Fatal("ambiguous auth headers reached provider")
	}
	if w := scopedRequest(t, HandlerAuthorized(&permissionSpy{}, nil, 1), http.MethodGet, "/v1/identity", scopedTestToken, nil); w.Code != 503 {
		t.Fatal("nil provider did not fail closed", w.Code)
	}
}

func TestIdentityAndUnmappedRoutes(t *testing.T) {
	identity := &authenticationStub{principal: auth.Principal{ID: strings.Repeat("a", 32), Subject: "alice", Scopes: []auth.Scope{auth.Read}}}
	store := &permissionSpy{}
	h := HandlerAuthorized(store, identity, 1)
	w := scopedRequest(t, h, http.MethodGet, "/v1/identity", scopedTestToken, nil)
	var got auth.Principal
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Subject != "alice" || strings.Contains(w.Body.String(), scopedTestToken) {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodDelete, "/v1/entries/" + example().Key, 405},
		{http.MethodGet, "/v1/entries/bad", 404},
		{http.MethodGet, "/v1/future-admin", 404},
		{http.MethodPost, "/v1/metrics", 405},
		{http.MethodPost, "/v1/identity", 405},
		{http.MethodPut, "/v1/status/" + example().Key, 405},
	} {
		if w := scopedRequest(t, h, tc.method, tc.path, scopedTestToken, nil); w.Code != tc.want {
			t.Fatal(w.Code, tc)
		}
	}
	if store.calls.Load() != 0 {
		t.Fatal("unmapped request reached storage")
	}
}

type blockingAuthentication struct {
	entered, release chan struct{}
	once             sync.Once
	calls            atomic.Int64
}

func (s *blockingAuthentication) Authenticate(ctx context.Context, _ string) (auth.Principal, error) {
	s.calls.Add(1)
	s.once.Do(func() { close(s.entered) })
	select {
	case <-s.release:
		return auth.Principal{Scopes: []auth.Scope{auth.Read}}, nil
	case <-ctx.Done():
		return auth.Principal{}, ctx.Err()
	}
}

func TestAuthenticationAdmissionAndCancellation(t *testing.T) {
	provider := &blockingAuthentication{entered: make(chan struct{}), release: make(chan struct{})}
	h := HandlerAuthorized(&permissionSpy{}, provider, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest(http.MethodGet, "/v1/identity", nil).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+scopedTestToken)
	done := make(chan struct{})
	go func() { defer close(done); h.ServeHTTP(httptest.NewRecorder(), r) }()
	select {
	case <-provider.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("auth did not start")
	}
	if w := scopedRequest(t, h, http.MethodGet, "/v1/identity", scopedTestToken, nil); w.Code != 503 {
		t.Fatal("auth bound bypassed", w.Code)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled authentication stuck")
	}
	// The same cancelled request must enter the provider again, proving that
	// cancellation released the authentication admission slot.
	h.ServeHTTP(httptest.NewRecorder(), r)
	if provider.calls.Load() != 2 {
		t.Fatal("authentication slot leaked after cancellation")
	}
}

func TestScopedMetricsCountDenialsWithoutCallerLabels(t *testing.T) {
	provider := &authenticationStub{principal: auth.Principal{ID: strings.Repeat("a", 32), Subject: "alice", Scopes: []auth.Scope{auth.Read}}}
	h := HandlerAuthorized(&permissionSpy{}, provider, 2)
	scopedRequest(t, h, http.MethodPut, "/v1/entries/"+example().Key, scopedTestToken, nil)
	provider.err = auth.ErrInvalid
	scopedRequest(t, h, http.MethodGet, identityPath, scopedTestToken, nil)
	provider.err = errors.New("database detail")
	scopedRequest(t, h, http.MethodGet, identityPath, scopedTestToken, nil)
	provider.err = nil
	provider.principal.Scopes = []auth.Scope{auth.Metrics}
	w := scopedRequest(t, h, http.MethodGet, metricsPath, scopedTestToken, nil)
	var counters map[string]int64
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &counters) != nil || counters["unauthorized"] != 1 || counters["forbidden"] != 1 || counters["auth_failures"] != 1 {
		t.Fatal("incorrect scoped counters", w.Code, counters)
	}
	if strings.Contains(w.Body.String(), "alice") || strings.Contains(w.Body.String(), scopedTestToken) {
		t.Fatal("metrics leaked caller label or token")
	}
}

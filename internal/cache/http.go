package cache

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sidsharma96/receipt/internal/auth"
)

const (
	conflictsPath = "conflicts"
	entriesPath   = "entries"
	statusPath    = "status"
	identityPath  = "/v1/identity"
	metricsPath   = "/v1/metrics"
)

// Remote is an authenticated HTTP client for the trusted-team cache API.
type Remote struct {
	URL, Token string
	Client     *http.Client
}

func (r Remote) request(ctx context.Context, method, key string, body []byte) (*http.Response, error) {
	return r.requestPath(ctx, method, entriesPath, key, body)
}

func (r Remote) requestPath(ctx context.Context, method, kind, key string, body []byte) (*http.Response, error) {
	if !validKey.MatchString(key) {
		return nil, fmt.Errorf("invalid key")
	}
	u, e := url.Parse(r.URL)
	if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid remote URL")
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return nil, fmt.Errorf("remote cache requires HTTPS except on loopback")
	}
	req, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(r.URL, "/")+"/v1/"+kind+"/"+key, bytes.NewReader(body))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+r.Token)
	req.Header.Set("Content-Type", "application/json")
	c := r.Client
	if c == nil {
		c = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return c.Do(req)
}

// Get fetches and validates a reusable result from the shared API.
func (r Remote) Get(ctx context.Context, key string) (Result, error) {
	var v Result
	resp, e := r.request(ctx, http.MethodGet, key, nil)
	if e != nil {
		return v, e
	}
	defer resp.Body.Close() //nolint:errcheck // Read/resource cleanup; writes and syncs are checked before return.
	if resp.StatusCode == http.StatusNotFound {
		return v, ErrMiss
	}
	if resp.StatusCode == http.StatusConflict {
		return v, ErrConflict
	}
	if resp.StatusCode != http.StatusOK {
		return v, fmt.Errorf("remote lookup: HTTP %d", resp.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, MaxEntryBytes+1))
	if e != nil {
		return v, e
	}
	v, e = Decode(b)
	if e == nil && v.Key != key {
		return v, ErrCorrupt
	}
	return v, e
}

// Put uploads a validated successful result.
func (r Remote) Put(ctx context.Context, v Result) error {
	b, e := Encode(v)
	if e != nil {
		return e
	}
	resp, e := r.request(ctx, http.MethodPut, v.Key, b)
	if e != nil {
		return e
	}
	defer resp.Body.Close() //nolint:errcheck // Read/resource cleanup; writes and syncs are checked before return.
	if resp.StatusCode == http.StatusConflict {
		return ErrConflict
	}
	if resp.StatusCode == http.StatusInsufficientStorage {
		return ErrQuota
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("remote store: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Check refuses reuse when the shared key is quarantined or status is unavailable.
func (r Remote) Check(ctx context.Context, key string) error {
	resp, err := r.requestPath(ctx, http.MethodGet, statusPath, key, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck // Read/resource cleanup; writes and syncs are checked before return.
	if resp.StatusCode == http.StatusConflict {
		return ErrConflict
	}
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return fmt.Errorf("shared status: HTTP %d", resp.StatusCode)
}

// Report sends conflict evidence to the shared API.
func (r Remote) Report(ctx context.Context, report ConflictReport) error {
	if err := report.validate(); err != nil {
		return err
	}
	b, err := json.Marshal(report)
	if err != nil {
		return err
	}
	resp, err := r.requestPath(ctx, http.MethodPost, conflictsPath, report.Key, b)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck // Read/resource cleanup; writes and syncs are checked before return.
	if resp.StatusCode == http.StatusInsufficientStorage {
		return ErrQuota
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("report conflict: HTTP %d", resp.StatusCode)
	}
	return nil
}

// APIMetrics uses aggregate counters only: no tokens, paths or output labels.
type APIMetrics struct {
	requests, rejected, conflicts, misses, failures, nanos atomic.Int64
	unauthorized, forbidden, authFailures, authBusy        atomic.Int64
}

// Handler serves one trusted user/team; token possession does not prove honest uploads.
func Handler(store Store, token string) http.Handler { return HandlerLimit(store, token, 16) }

// HandlerLimit adds bounded admission to the trusted-team API.
func HandlerLimit(store Store, token string, limit int) http.Handler {
	if limit < 1 {
		limit = 1
	}
	api := &cacheAPI{store: store, token: token, slots: make(chan struct{}, limit), metrics: &APIMetrics{}}
	return http.HandlerFunc(api.serveHTTP)
}

type cacheAPI struct {
	store         Store
	token         string
	slots         chan struct{}
	metrics       *APIMetrics
	authenticator auth.Authenticator
	authSlots     chan struct{}
}

func (a *cacheAPI) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if a.authSlots != nil {
		a.serveScoped(w, r)
		return
	}
	if a.token == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+a.token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	a.serveAuthenticated(w, r, nil)
}

func (a *cacheAPI) serveAuthenticated(w http.ResponseWriter, r *http.Request, principal *auth.Principal) {
	slots, metrics := a.slots, a.metrics
	if principal != nil && r.URL.Path == identityPath && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(principal); err != nil {
			metrics.failures.Add(1)
		}
		return
	}
	if r.URL.Path == metricsPath && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]int64{"requests": metrics.requests.Load(), "active": int64(len(slots)), "rejected": metrics.rejected.Load(), conflictsPath: metrics.conflicts.Load(), "misses": metrics.misses.Load(), "failures": metrics.failures.Load(), "duration_ns_total": metrics.nanos.Load(), "unauthorized": metrics.unauthorized.Load(), "forbidden": metrics.forbidden.Load(), "auth_failures": metrics.authFailures.Load(), "auth_busy": metrics.authBusy.Load()}); err != nil {
			metrics.failures.Add(1)
		}
		return
	}
	metrics.requests.Add(1)
	select {
	case slots <- struct{}{}:
	default:
		metrics.rejected.Add(1)
		w.Header().Set("Retry-After", "1")
		http.Error(w, "busy", http.StatusServiceUnavailable)
		return
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	defer func() { <-slots; metrics.nanos.Add(time.Since(started).Nanoseconds()) }()
	observed := &statusWriter{ResponseWriter: w, status: http.StatusOK}
	w = observed
	defer func() {
		switch observed.status {
		case http.StatusConflict:
			metrics.conflicts.Add(1)
		case http.StatusNotFound:
			metrics.misses.Add(1)
		default:
			if observed.status >= http.StatusInternalServerError {
				metrics.failures.Add(1)
			}
		}
	}()

	a.route(w, r)
}

func (a *cacheAPI) route(w http.ResponseWriter, r *http.Request) {
	for _, kind := range []string{statusPath, conflictsPath} {
		prefix := "/v1/" + kind + "/"
		if strings.HasPrefix(r.URL.Path, prefix) {
			a.shared(w, r, kind, strings.TrimPrefix(r.URL.Path, prefix))
			return
		}
	}
	key := strings.TrimPrefix(r.URL.Path, "/v1/entries/")
	if !strings.HasPrefix(r.URL.Path, "/v1/entries/") || !validKey.MatchString(key) {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		a.getEntry(w, r, key)
	case http.MethodPut:
		a.putEntry(w, r, key)
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *cacheAPI) shared(w http.ResponseWriter, r *http.Request, kind, key string) {
	if !validKey.MatchString(key) {
		http.NotFound(w, r)
		return
	}
	shared, ok := a.store.(ConflictStore)
	if !ok {
		http.Error(w, "shared conflict contract unavailable", http.StatusNotImplemented)
		return
	}
	if kind == statusPath && r.Method == http.MethodGet {
		a.status(w, r, key, shared)
		return
	}
	if kind == conflictsPath && r.Method == http.MethodPost {
		a.report(w, r, key, shared)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (a *cacheAPI) status(w http.ResponseWriter, r *http.Request, key string, shared ConflictStore) {
	q, err := shared.Quarantined(r.Context(), key)
	if err != nil {
		http.Error(w, "status unavailable", http.StatusInternalServerError)
	} else if q {
		http.Error(w, "quarantined", http.StatusConflict)
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *cacheAPI) report(w http.ResponseWriter, r *http.Request, key string, shared ConflictStore) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxEntryBytes)
	b, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "report too large", http.StatusRequestEntityTooLarge)
		return
	}
	var report ConflictReport
	if json.Unmarshal(b, &report) != nil || report.Key != key || report.validate() != nil {
		http.Error(w, "invalid report", http.StatusBadRequest)
		return
	}
	err = shared.Report(r.Context(), report)
	if errors.Is(err, ErrQuota) {
		http.Error(w, "storage quota exceeded", http.StatusInsufficientStorage)
		return
	}
	if err != nil && !errors.Is(err, ErrConflict) {
		http.Error(w, "report unavailable", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *cacheAPI) getEntry(w http.ResponseWriter, r *http.Request, key string) {
	store, metrics := a.store, a.metrics
	v, e := store.Get(r.Context(), key)
	if errors.Is(e, ErrMiss) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(e, ErrConflict) {
		http.Error(w, "quarantined", http.StatusConflict)
		return
	}
	if e != nil {
		http.Error(w, "cache unavailable", http.StatusInternalServerError)
		return
	}
	b, e := Encode(v)
	if e != nil {
		http.Error(w, "cache unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(b); err != nil {
		// Headers may already be sent; count failure rather than send a second response.
		metrics.failures.Add(1)
	}
}

func (a *cacheAPI) putEntry(w http.ResponseWriter, r *http.Request, key string) {
	store := a.store
	r.Body = http.MaxBytesReader(w, r.Body, MaxEntryBytes)
	b, e := io.ReadAll(r.Body)
	if e != nil {
		http.Error(w, "entry too large or unreadable", http.StatusRequestEntityTooLarge)
		return
	}
	v, e := Decode(b)
	if e != nil || v.Key != key {
		http.Error(w, "invalid entry", http.StatusBadRequest)
		return
	}
	if e = store.Put(r.Context(), v); errors.Is(e, ErrConflict) {
		http.Error(w, "quarantined", http.StatusConflict)
		return
	} else if errors.Is(e, ErrQuota) {
		http.Error(w, "storage quota exceeded", http.StatusInsufficientStorage)
		return
	} else if e != nil {
		http.Error(w, "cache unavailable", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) { w.status = code; w.ResponseWriter.WriteHeader(code) }

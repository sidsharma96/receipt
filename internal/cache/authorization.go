package cache

import (
	"context"
	"errors"
	"net/http"
	"receipt/internal/auth"
	"strings"
	"time"
)

// HandlerAuthorized uses only the supplied authenticator; it never falls back
// to a legacy token. Authentication and storage have separate bounded admission.
// A nil authenticator fails closed. The caller owns backend resources.
func HandlerAuthorized(store Store, authenticator auth.Authenticator, limit int) http.Handler {
	if limit < 1 {
		limit = 1
	}
	a := &cacheAPI{store: store, authenticator: authenticator, slots: make(chan struct{}, limit), authSlots: make(chan struct{}, limit), metrics: &APIMetrics{}}
	return http.HandlerFunc(a.serveHTTP)
}

func (a *cacheAPI) serveScoped(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.authenticate(w, r)
	if !ok {
		return
	}
	scope, known := requiredScope(r)
	if !known && (r.URL.Path != identityPath || r.Method != http.MethodGet) {
		rejectUnmapped(w, r)
		return
	}
	if known && !principal.Allows(scope) {
		a.metrics.forbidden.Add(1)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	a.serveAuthenticated(w, r, &principal)
}

func (a *cacheAPI) authenticate(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") || len(values[0]) != len("Bearer ")+auth.TokenLength {
		a.unauthorized(w)
		return auth.Principal{}, false
	}
	select {
	case a.authSlots <- struct{}{}:
		defer func() { <-a.authSlots }()
	default:
		a.metrics.authBusy.Add(1)
		w.Header().Set("Retry-After", "1")
		http.Error(w, "authentication busy", http.StatusServiceUnavailable)
		return auth.Principal{}, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if a.authenticator == nil {
		a.metrics.authFailures.Add(1)
		http.Error(w, "authentication unavailable", http.StatusServiceUnavailable)
		return auth.Principal{}, false
	}
	principal, err := a.authenticator.Authenticate(ctx, strings.TrimPrefix(values[0], "Bearer "))
	if errors.Is(err, auth.ErrInvalid) {
		a.unauthorized(w)
		return auth.Principal{}, false
	}
	if err != nil {
		a.metrics.authFailures.Add(1)
		w.Header().Set("Retry-After", "1")
		http.Error(w, "authentication unavailable", http.StatusServiceUnavailable)
		return auth.Principal{}, false
	}
	return principal, true
}

func (a *cacheAPI) unauthorized(w http.ResponseWriter) {
	a.metrics.unauthorized.Add(1)
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

// Every usable scoped route must be mapped here or explicitly handled as identity.
func requiredScope(r *http.Request) (auth.Scope, bool) {
	if r.URL.Path == metricsPath && r.Method == http.MethodGet {
		return auth.Metrics, true
	}
	for _, path := range []string{entriesPath, statusPath, conflictsPath} {
		prefix := "/v1/" + path + "/"
		if !strings.HasPrefix(r.URL.Path, prefix) || !validKey.MatchString(strings.TrimPrefix(r.URL.Path, prefix)) {
			continue
		}
		switch {
		case path == entriesPath && r.Method == http.MethodPut:
			return auth.Publish, true
		case (path == entriesPath || path == statusPath) && r.Method == http.MethodGet:
			return auth.Read, true
		case path == conflictsPath && r.Method == http.MethodPost:
			return auth.Report, true
		}
	}
	return "", false
}

// Do not dispatch unmapped routes into storage: a future endpoint must explicitly
// choose a permission before scoped callers can reach it.
func rejectUnmapped(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == identityPath || r.URL.Path == metricsPath {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	for _, path := range []string{entriesPath, statusPath, conflictsPath} {
		prefix := "/v1/" + path + "/"
		if !strings.HasPrefix(r.URL.Path, prefix) || !validKey.MatchString(strings.TrimPrefix(r.URL.Path, prefix)) {
			continue
		}
		allow := http.MethodGet
		switch path {
		case entriesPath:
			allow = "GET, PUT"
		case conflictsPath:
			allow = http.MethodPost
		}
		w.Header().Set("Allow", allow)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.NotFound(w, r)
}

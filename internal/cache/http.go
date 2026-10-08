package cache

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

type Remote struct {
	URL, Token string
	Client     *http.Client
}

func (r Remote) request(ctx context.Context, method, key string, body []byte) (*http.Response, error) {
	return r.requestPath(ctx, method, "entries", key, body)
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
func (r Remote) Get(ctx context.Context, key string) (Result, error) {
	var v Result
	resp, e := r.request(ctx, "GET", key, nil)
	if e != nil {
		return v, e
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return v, ErrMiss
	}
	if resp.StatusCode == 409 {
		return v, ErrConflict
	}
	if resp.StatusCode != 200 {
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
func (r Remote) Put(ctx context.Context, v Result) error {
	b, e := Encode(v)
	if e != nil {
		return e
	}
	resp, e := r.request(ctx, "PUT", v.Key, b)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode == 409 {
		return ErrConflict
	}
	if resp.StatusCode != 204 {
		return fmt.Errorf("remote store: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Handler is for one trusted user/team. Possession of the token grants read
// and write access; it does not establish that uploaded test results are honest.
func (r Remote) Check(ctx context.Context, key string) error {
	resp, err := r.requestPath(ctx, "GET", "status", key, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 409 {
		return ErrConflict
	}
	if resp.StatusCode == 204 {
		return nil
	}
	return fmt.Errorf("shared status: HTTP %d", resp.StatusCode)
}

func (r Remote) Report(ctx context.Context, report ConflictReport) error {
	if err := report.validate(); err != nil {
		return err
	}
	b, _ := json.Marshal(report)
	resp, err := r.requestPath(ctx, "POST", "conflicts", report.Key, b)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 204 {
		return fmt.Errorf("report conflict: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Metrics use aggregate counters only: no tokens, paths or output labels.
type APIMetrics struct{ requests, rejected, conflicts, misses, failures, nanos atomic.Int64 }

func Handler(store Store, token string) http.Handler { return HandlerLimit(store, token, 16) }
func HandlerLimit(store Store, token string, limit int) http.Handler {
	if limit < 1 {
		limit = 1
	}
	slots := make(chan struct{}, limit)
	metrics := &APIMetrics{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if token == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			http.Error(w, "unauthorized", 401)
			return
		}
		if r.URL.Path == "/v1/metrics" && r.Method == "GET" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]int64{"requests": metrics.requests.Load(), "active": int64(len(slots)), "rejected": metrics.rejected.Load(), "conflicts": metrics.conflicts.Load(), "misses": metrics.misses.Load(), "failures": metrics.failures.Load(), "duration_ns_total": metrics.nanos.Load()})
			return
		}
		metrics.requests.Add(1)
		select {
		case slots <- struct{}{}:
		default:
			metrics.rejected.Add(1)
			w.Header().Set("Retry-After", "1")
			http.Error(w, "busy", 503)
			return
		}
		started := time.Now()
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		defer func() { <-slots; metrics.nanos.Add(time.Since(started).Nanoseconds()) }()
		observed := &statusWriter{ResponseWriter: w, status: 200}
		w = observed
		defer func() {
			switch observed.status {
			case 409:
				metrics.conflicts.Add(1)
			case 404:
				metrics.misses.Add(1)
			default:
				if observed.status >= 500 {
					metrics.failures.Add(1)
				}
			}
		}()
		for _, kind := range []string{"status", "conflicts"} {
			prefix := "/v1/" + kind + "/"
			if !strings.HasPrefix(r.URL.Path, prefix) {
				continue
			}
			key := strings.TrimPrefix(r.URL.Path, prefix)
			if !validKey.MatchString(key) {
				http.NotFound(w, r)
				return
			}
			shared, ok := store.(ConflictStore)
			if !ok {
				http.Error(w, "shared conflict contract unavailable", 501)
				return
			}
			if kind == "status" && r.Method == "GET" {
				q, err := shared.Quarantined(r.Context(), key)
				if err != nil {
					http.Error(w, "status unavailable", 500)
				} else if q {
					http.Error(w, "quarantined", 409)
				} else {
					w.WriteHeader(204)
				}
				return
			}
			if kind == "conflicts" && r.Method == "POST" {
				r.Body = http.MaxBytesReader(w, r.Body, MaxEntryBytes)
				b, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, "report too large", 413)
					return
				}
				var report ConflictReport
				if json.Unmarshal(b, &report) != nil || report.Key != key || report.validate() != nil {
					http.Error(w, "invalid report", 400)
					return
				}
				err = shared.Report(r.Context(), report)
				if err != nil && err != ErrConflict {
					http.Error(w, "report unavailable", 500)
					return
				}
				w.WriteHeader(204)
				return
			}
			http.Error(w, "method not allowed", 405)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/v1/entries/")
		if !strings.HasPrefix(r.URL.Path, "/v1/entries/") || !validKey.MatchString(key) {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case "GET":
			v, e := store.Get(r.Context(), key)
			if e == ErrMiss {
				http.NotFound(w, r)
				return
			}
			if e == ErrConflict {
				http.Error(w, "quarantined", 409)
				return
			}
			if e != nil {
				http.Error(w, "cache unavailable", 500)
				return
			}
			b, e := Encode(v)
			if e != nil {
				http.Error(w, "cache unavailable", 500)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(b)
		case "PUT":
			r.Body = http.MaxBytesReader(w, r.Body, MaxEntryBytes)
			b, e := io.ReadAll(r.Body)
			if e != nil {
				http.Error(w, "entry too large or unreadable", 413)
				return
			}
			v, e := Decode(b)
			if e != nil || v.Key != key {
				http.Error(w, "invalid entry", 400)
				return
			}
			if e = store.Put(r.Context(), v); e == ErrConflict {
				http.Error(w, "quarantined", 409)
				return
			} else if e != nil {
				http.Error(w, "cache unavailable", 500)
				return
			}
			w.WriteHeader(204)
		default:
			w.Header().Set("Allow", "GET, PUT")
			http.Error(w, "method not allowed", 405)
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) { w.status = code; w.ResponseWriter.WriteHeader(code) }

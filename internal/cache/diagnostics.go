package cache

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"receipt/internal/observe"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// RequestEvent deliberately excludes paths, keys, headers, actors and error text.
type RequestEvent struct {
	Version    int              `json:"version"`
	ID         string           `json:"request_id"`
	Operation  string           `json:"operation"`
	Status     int              `json:"status"`
	DurationNS int64            `json:"duration_ns"`
	Stages     []observe.Timing `json:"stages"`
}

// RequestLog writes one summary after a handler returns and releases its locks.
// Writes are serialized and synchronous: a blocked destination delays completion.
// This is opt-in diagnostics, not a durable audit or a distributed trace system.
type RequestLog struct {
	mu       sync.Mutex
	encoder  *json.Encoder
	prefix   string
	sequence atomic.Uint64
	failures atomic.Uint64
}

// NewRequestLog creates a process-local ID namespace; caller owns the writer.
func NewRequestLog(out io.Writer) (*RequestLog, error) {
	if out == nil {
		return nil, fmt.Errorf("request log requires a writer")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("request log identity: %w", err)
	}
	return &RequestLog{encoder: json.NewEncoder(out), prefix: hex.EncodeToString(nonce[:])}, nil
}

// Failures counts failed log writes without changing HTTP outcomes.
func (l *RequestLog) Failures() uint64 { return l.failures.Load() }

// Handler adds a server-generated response ID. Incoming IDs are never trusted.
func (l *RequestLog) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, trace := observe.Start(r.Context())
		id := fmt.Sprintf("%s-%x", l.prefix, l.sequence.Add(1))
		w.Header().Set("X-Receipt-Request-ID", id)
		response := &diagnosticResponse{ResponseWriter: w}
		started := time.Now()
		defer func() {
			status := response.status
			if status == 0 {
				status = http.StatusOK
			}
			if !response.completed {
				status = 0
			} // A panic is not a successful response.
			event := RequestEvent{1, id, requestOperation(r), status, time.Since(started).Nanoseconds(), trace.Snapshot()}
			l.mu.Lock()
			defer l.mu.Unlock()
			if err := l.encoder.Encode(event); err != nil {
				l.failures.Add(1)
			}
		}()
		next.ServeHTTP(response, r.WithContext(ctx))
		response.completed = true
	})
}

type diagnosticResponse struct {
	http.ResponseWriter
	status    int
	completed bool
}

func (w *diagnosticResponse) WriteHeader(code int) {
	if code >= 100 && code < 200 {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	if w.status != 0 {
		return
	}
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *diagnosticResponse) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}
func (w *diagnosticResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func requestOperation(r *http.Request) string {
	if r.Method == http.MethodGet {
		if r.URL.Path == identityPath {
			return "identity"
		}
		if r.URL.Path == metricsPath {
			return "metrics"
		}
	}
	for _, route := range []struct{ method, prefix, name string }{
		{http.MethodGet, "/v1/entries/", "fetch"},
		{http.MethodPut, "/v1/entries/", "publish"},
		{http.MethodGet, "/v1/status/", "status"},
		{http.MethodPost, "/v1/conflicts/", "report"},
	} {
		if r.Method == route.method && strings.HasPrefix(r.URL.Path, route.prefix) && validKey.MatchString(strings.TrimPrefix(r.URL.Path, route.prefix)) {
			return route.name
		}
	}
	return "unknown"
}

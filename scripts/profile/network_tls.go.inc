// Standalone diagnostic. No credentials, payload logging, or disabled verification.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"
	"syscall"
	"time"
)

type sample struct {
	Wall          time.Time
	CPU           int64
	Bytes, Allocs uint64
}

func snap() sample {
	var r syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &r); err != nil {
		panic(err)
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return sample{time.Now(), r.Utime.Sec*1e9 + int64(r.Utime.Usec)*1000 + r.Stime.Sec*1e9 + int64(r.Stime.Usec)*1000, m.TotalAlloc, m.Mallocs}
}
func delta(a, b sample) map[string]interface{} {
	return map[string]interface{}{"wall_ms": float64(b.Wall.Sub(a.Wall)) / 1e6, "process_cpu_ms": float64(b.CPU-a.CPU) / 1e6, "allocated_bytes": b.Bytes - a.Bytes, "allocations": b.Allocs - a.Allocs}
}
func emit(v interface{}) {
	if err := json.NewEncoder(os.Stdout).Encode(v); err != nil {
		panic(err)
	}
}
func main() {
	mode := flag.String("mode", "full", "full (fresh TCP every request), resume (shared ticket cache), reuse (same HTTP transport), roots (only trust-store loading)")
	n := flag.Int("n", 1, "bounded request count, 1..100")
	preload := flag.Bool("preload", false, "load default OS trust pool before HTTP timing; still verify normally")
	h1 := flag.Bool("h1", false, "force HTTP/1.1 control")
	tls12 := flag.Bool("tls12", false, "force TLS1.2 control")
	curve := flag.String("curve", "default", "default, x25519, p256")
	profile := flag.String("cpu-profile", "", "optional local CPU profile path (separate diagnostic run)")
	flag.Parse()
	if *n < 1 || *n > 100 || (*mode != "full" && *mode != "resume" && *mode != "reuse" && *mode != "roots") {
		panic("invalid bounded mode/count")
	}
	if *profile != "" {
		f, e := os.Create(*profile)
		if e != nil {
			panic(e)
		}
		defer f.Close()
		if e = pprof.StartCPUProfile(f); e != nil {
			panic(e)
		}
		defer pprof.StopCPUProfile()
	}
	emit(map[string]interface{}{"kind": "metadata", "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "cpus": runtime.NumCPU(), "mode": *mode, "preload": *preload, "scope": "public unauthenticated /v1/identity; no Receipt payload; per-process CPU includes other runtime goroutines; memstats instrumentation perturbs timing"})
	if *preload || *mode == "roots" {
		a := snap()
		pool, e := x509.SystemCertPool()
		b := snap()
		if e != nil {
			panic("system root load failed")
		}
		emit(map[string]interface{}{"kind": "root_load", "cost": delta(a, b), "subjects": len(pool.Subjects())})
		if *mode == "roots" {
			return
		}
	}
	cfg := &tls.Config{}
	if *mode == "resume" {
		cfg.ClientSessionCache = tls.NewLRUClientSessionCache(2)
	}
	if *tls12 {
		cfg.MinVersion = tls.VersionTLS12
		cfg.MaxVersion = tls.VersionTLS12
	}
	switch *curve {
	case "default":
	case "x25519":
		cfg.CurvePreferences = []tls.CurveID{tls.X25519}
	case "p256":
		cfg.CurvePreferences = []tls.CurveID{tls.CurveP256}
	default:
		panic("invalid curve")
	}
	newTransport := func() *http.Transport {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.Proxy = nil
		t.TLSClientConfig = cfg
		t.ForceAttemptHTTP2 = !*h1
		if *h1 {
			t.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
		}
		return t
	}
	tr := newTransport()
	for i := 0; i < *n; i++ {
		if i > 0 && *mode != "reuse" {
			tr.CloseIdleConnections()
			tr = newTransport()
		}
		var mu sync.Mutex
		times := map[string]int64{}
		var ta, tb sample
		var state tls.ConnectionState
		var reused bool
		start := time.Now()
		mark := func(k string) { mu.Lock(); times[k] = time.Since(start).Nanoseconds(); mu.Unlock() }
		trace := &httptrace.ClientTrace{
			DNSStart: func(httptrace.DNSStartInfo) { mark("dns_start") }, DNSDone: func(httptrace.DNSDoneInfo) { mark("dns_done") },
			ConnectStart: func(string, string) { mark("tcp_start") }, ConnectDone: func(string, string, error) { mark("tcp_done") },
			TLSHandshakeStart: func() { a := snap(); mu.Lock(); ta = a; mu.Unlock(); mark("tls_start") },
			TLSHandshakeDone: func(s tls.ConnectionState, e error) {
				mark("tls_done")
				b := snap()
				mu.Lock()
				tb = b
				state = s
				mu.Unlock()
			},
			GotConn:      func(g httptrace.GotConnInfo) { mu.Lock(); reused = g.Reused; mu.Unlock(); mark("got_conn") },
			WroteRequest: func(httptrace.WroteRequestInfo) { mark("written") }, GotFirstResponseByte: func() { mark("first_byte") },
		}
		req, e := http.NewRequest("GET", "https://receipt.sidsharma.dev/v1/identity", nil)
		if e != nil {
			panic(e)
		}
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
		c := &http.Client{Transport: tr, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, e := c.Do(req)
		if e != nil {
			emit(map[string]interface{}{"kind": "request", "i": i, "failed": true})
			tr.CloseIdleConnections()
			continue
		}
		_, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, 65537))
		closeErr := resp.Body.Close()
		mark("closed")
		mu.Lock()
		out := map[string]interface{}{"kind": "request", "i": i, "status": resp.StatusCode, "protocol": resp.Proto, "reused": reused, "events_ns": times, "failed": readErr != nil || closeErr != nil}
		if !ta.Wall.IsZero() {
			out["tls_cost"] = delta(ta, tb)
			out["tls_version"] = state.Version
			out["cipher"] = tls.CipherSuiteName(state.CipherSuite)
			out["resumed"] = state.DidResume
			out["alpn"] = state.NegotiatedProtocol
			certs := []map[string]interface{}{}
			for _, p := range state.PeerCertificates {
				certs = append(certs, map[string]interface{}{"der_bytes": len(p.Raw), "public_key": p.PublicKeyAlgorithm.String(), "signature": p.SignatureAlgorithm.String()})
			}
			out["peer_certificates"] = certs
		}
		mu.Unlock()
		emit(out)
		// Re-check the peer chain with the same default trust store, once warmed.
		// This is a separate local operation, not the time inside Handshake.
		if i == 0 && len(state.PeerCertificates) > 0 && !state.DidResume {
			ints := x509.NewCertPool()
			for _, p := range state.PeerCertificates[1:] {
				ints.AddCert(p)
			}
			a := snap()
			chains, e := state.PeerCertificates[0].Verify(x509.VerifyOptions{DNSName: "receipt.sidsharma.dev", Intermediates: ints})
			b := snap()
			lens := []int{}
			for _, c := range chains {
				lens = append(lens, len(c))
			}
			emit(map[string]interface{}{"kind": "warm_chain_verify", "cost": delta(a, b), "failed": e != nil, "chain_lengths": lens})
		}
	}
	tr.CloseIdleConnections()
	fmt.Fprintln(os.Stderr, "probe complete; no credentials used")
}

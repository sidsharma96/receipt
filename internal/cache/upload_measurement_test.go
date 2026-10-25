package cache

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/sidsharma96/receipt/internal/fingerprint"
)

const (
	uploadSamples = 8
	uploadToken   = "synthetic-upload-measurement-only"
)

type uploadCase struct {
	Name    string
	Inputs  int
	Output  int
	Workers int
	Repeat  bool
}

func selectedUploadCase(t *testing.T) uploadCase {
	t.Helper()
	c := uploadCase{Name: os.Getenv("RECEIPT_UPLOAD_CASE"), Inputs: 1, Output: 1024}
	switch c.Name {
	case "small":
	case "output":
		c.Output = MaxBlobBytes
	case "manifest":
		c.Inputs = 10000
	case "combined":
		c.Inputs, c.Output = 10000, MaxBlobBytes
	default:
		t.Fatal("RECEIPT_UPLOAD_CASE must be small, output, manifest or combined")
	}
	var err error
	c.Workers, err = strconv.Atoi(os.Getenv("RECEIPT_UPLOAD_WORKERS"))
	if err != nil || (c.Workers != 1 && c.Workers != 2) {
		t.Fatal("RECEIPT_UPLOAD_WORKERS must be 1 or 2")
	}
	switch os.Getenv("RECEIPT_UPLOAD_MODE") {
	case "new":
	case "repeat":
		c.Repeat = true
	default:
		t.Fatal("RECEIPT_UPLOAD_MODE must be new or repeat")
	}
	return c
}

func uploadResult(c uploadCase, index int) Result {
	r := example()
	r.Manifest.Root, r.Manifest.Directory = "/fixture", "/fixture"
	r.Manifest.Platform = "linux/amd64"
	r.Manifest.Command = []string{"go", "test", fmt.Sprintf("./package-%02d", index)}
	r.Manifest.Inputs = make([]fingerprint.Input, c.Inputs)
	for i := range r.Manifest.Inputs {
		r.Manifest.Inputs[i] = fingerprint.Input{
			Kind: fingerprint.FileInput,
			Name: fmt.Sprintf("/fixture/source/package-%05d/input.go", i),
			Hash: fingerprint.Hash([]byte(fmt.Sprint(i))),
		}
	}
	r.Key = r.Manifest.Key()
	r.Stdout = bytes.Repeat([]byte("x"), c.Output)
	copy(r.Stdout, fmt.Sprintf("%08d", index))
	return r
}

// TestUploadMeasurement is an opt-in Linux experiment, never a timing assertion.
// Run each case in a fresh test process; setup contributes to whole-process HWM.
func TestUploadMeasurement(t *testing.T) {
	if os.Getenv("RECEIPT_UPLOAD_MEASURE") != "1" {
		t.Skip("set RECEIPT_UPLOAD_MEASURE=1 and a disposable RECEIPT_TEST_DATABASE_URL")
	}
	if runtime.GOOS != "linux" {
		t.Skip("RSS measurement requires Linux /proc")
	}
	c := selectedUploadCase(t)
	s := postgresFixture(t)
	stats := &uploadSQL{}
	// Replace only the fixture's scoped connection pool; never the admin pool.
	var schema string
	if err := s.DB.QueryRow("SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	connector, err := pq.NewConnector(os.Getenv("RECEIPT_TEST_DATABASE_URL") + " search_path=" + schema)
	if err != nil {
		t.Fatal("measurement requires a keyword-form PostgreSQL DSN:", err)
	}
	db := sql.OpenDB(uploadConnector{connector, stats})
	db.SetMaxOpenConns(2)
	t.Cleanup(func() { db.Close() })
	s.DB = db
	var logs bytes.Buffer
	logger, err := NewRequestLog(&logs)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(logger.Handler(HandlerLimit(s, uploadToken, 2)))
	defer server.Close()
	runUploadClient(t, server.URL, "warm", nil)
	if c.Repeat {
		runUploadClient(t, server.URL, "seed", nil)
	}
	logs.Reset()
	var before, after runtime.MemStats
	var baselineRSS, peakRSS, highWater int64
	var stop chan struct{}
	var sampled chan int64
	client := runUploadClient(t, server.URL, "measure", func(start bool) {
		if start {
			runtime.GC()
			baselineRSS, _ = uploadMemory(t)
			stop, sampled = sampleUploadRSS()
			stats.mu.Lock()
			stats.active = true
			stats.mu.Unlock()
			runtime.ReadMemStats(&before)
		} else {
			runtime.ReadMemStats(&after)
			stats.mu.Lock()
			stats.active = false
			stats.mu.Unlock()
			close(stop)
			peakRSS = <-sampled
			_, highWater = uploadMemory(t)
		}
	})
	if peakRSS < 1 {
		t.Fatal("Linux RSS sampling failed")
	}
	server.Close() // Join handlers before decoding the synchronous request log.
	events := make([]RequestEvent, 0, uploadSamples)
	decoder := json.NewDecoder(&logs)
	for decoder.More() {
		var event RequestEvent
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		if event.Status != http.StatusNoContent {
			t.Fatalf("unsuccessful server event: %+v", event)
		}
		events = append(events, event)
	}
	if len(events) != uploadSamples || logger.Failures() != 0 {
		t.Fatal("missing measurement events")
	}
	var entries, evidence int
	var recordBytes, evidenceBytes int64
	err = db.QueryRow(`SELECT count(*),COALESCE(sum(octet_length(record::text)),0) FROM receipt_entries_v2`).Scan(&entries, &recordBytes)
	if err != nil {
		t.Fatal(err)
	}
	err = db.QueryRow(`SELECT count(*),COALESCE(sum(octet_length(report::text)),0) FROM receipt_evidence_v2`).Scan(&evidence, &evidenceBytes)
	if err != nil || entries != uploadSamples+1 || evidence != entries {
		t.Fatalf("rows: %d evidence: %d error: %v", entries, evidence, err)
	}
	for i := 0; i < uploadSamples; i++ {
		want := uploadResult(c, i)
		got, err := s.Get(context.Background(), want.Key)
		if err != nil || !bytes.Equal(got.Stdout, want.Stdout) || len(got.Stderr) != 0 {
			t.Fatal("output readback mismatch", err)
		}
	}
	assertCountersMatch(t, s, "upload measurement")
	record := struct {
		Case                                             uploadCase
		Go                                               string
		AllocBytes                                       uint64
		BaselineRSS, SampledPeakRSS, ProcessHighWaterRSS int64
		RecordBytes, EvidenceBytes                       int64
		Client                                           uploadClientResult
		SQL                                              uploadSQLReport
		Events                                           []RequestEvent
	}{
		c, runtime.Version(), after.TotalAlloc - before.TotalAlloc, baselineRSS, peakRSS, highWater,
		recordBytes, evidenceBytes, client, stats.snapshot(), events,
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("UPLOAD_MEASURE " + string(raw))
}

type uploadClientResult struct {
	WireBytes int
	BatchNS   int64
	LatencyNS []int64
}

// TestUploadMeasurementClient runs in a separate process: client preparation and
// HTTP allocations must not be counted as server allocations or resident memory.
func TestUploadMeasurementClient(t *testing.T) {
	url := os.Getenv("RECEIPT_UPLOAD_URL")
	if url == "" {
		t.Skip("measurement subprocess only")
	}
	c := selectedUploadCase(t)
	count := uploadSamples
	if os.Getenv("RECEIPT_UPLOAD_PHASE") == "warm" {
		c.Inputs, c.Output, c.Workers, count = 1, 1024, 1, 1
	}
	values := make([]Result, count)
	payloads := make([][]byte, count)
	for i := range values {
		index := i
		if count == 1 {
			index = -1
		}
		values[i] = uploadResult(c, index)
		var err error
		payloads[i], err = Encode(values[i])
		if err != nil {
			t.Fatal(err)
		}
	}
	fmt.Println("UPLOAD_READY")
	var signal [1]byte
	if _, err := io.ReadFull(os.Stdin, signal[:]); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	defer client.CloseIdleConnections()
	result := uploadClientResult{WireBytes: len(payloads[0]), LatencyNS: make([]int64, count)}
	errs := make(chan error, c.Workers)
	var wg sync.WaitGroup
	started := time.Now()
	for worker := 0; worker < c.Workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := worker; i < count; i += c.Workers {
				request, err := http.NewRequest(http.MethodPut, url+"/v1/entries/"+values[i].Key, bytes.NewReader(payloads[i]))
				if err != nil {
					errs <- err
					return
				}
				request.Header.Set("Authorization", "Bearer "+uploadToken)
				start := time.Now()
				response, err := client.Do(request)
				if err != nil {
					errs <- err
					return
				}
				_, readErr := io.Copy(io.Discard, response.Body)
				closeErr := response.Body.Close()
				if err := errors.Join(readErr, closeErr); err != nil {
					errs <- err
					return
				}
				if response.StatusCode != http.StatusNoContent {
					errs <- fmt.Errorf("upload status %d", response.StatusCode)
					return
				}
				result.LatencyNS[i] = time.Since(start).Nanoseconds()
			}
		}(worker)
	}
	wg.Wait()
	result.BatchNS = time.Since(started).Nanoseconds()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(string(raw))
}

func runUploadClient(t *testing.T, url, phase string, boundary func(bool)) uploadClientResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestUploadMeasurementClient$", "-test.timeout=90s")
	cmd.Env = append(os.Environ(), "RECEIPT_UPLOAD_URL="+url, "RECEIPT_UPLOAD_PHASE="+phase)
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(out)
	ready, err := reader.ReadString('\n')
	if err != nil || ready != "UPLOAD_READY\n" {
		cancel()
		cmd.Wait()
		t.Fatalf("client preparation: %q %v %s", ready, err, diagnostics.String())
	}
	if boundary != nil {
		boundary(true)
	}
	_, writeErr := in.Write([]byte{1})
	closeErr := in.Close()
	line, readErr := reader.ReadString('\n')
	_, drainErr := io.Copy(io.Discard, reader)
	waitErr := cmd.Wait()
	if boundary != nil {
		boundary(false)
	}
	if writeErr != nil || closeErr != nil || readErr != nil || drainErr != nil || waitErr != nil {
		t.Fatalf("client failed: write %v close %v read %v drain %v wait %v output %q %s", writeErr, closeErr, readErr, drainErr, waitErr, line, diagnostics.String())
	}
	var result uploadClientResult
	if err := json.Unmarshal([]byte(line), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func readUploadMemory() (rss, highWater int64, err error) {
	raw, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || (fields[0] != "VmRSS:" && fields[0] != "VmHWM:") {
			continue
		}
		value, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, 0, err
		}
		if fields[0] == "VmRSS:" {
			rss = value * 1024
		} else {
			highWater = value * 1024
		}
	}
	if rss == 0 || highWater == 0 {
		return 0, 0, fmt.Errorf("missing Linux memory counters")
	}
	return rss, highWater, nil
}

func uploadMemory(t *testing.T) (int64, int64) {
	t.Helper()
	rss, highWater, err := readUploadMemory()
	if err != nil {
		t.Fatal(err)
	}
	return rss, highWater
}

// Sampling may miss short peaks; /proc's whole-process high water is separate.
// The sampler's own small allocations remain part of server TotalAlloc.
func sampleUploadRSS() (chan struct{}, chan int64) {
	stop, result := make(chan struct{}), make(chan int64, 1)
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		var peak int64
		for {
			rss, _, err := readUploadMemory()
			if err != nil {
				result <- -1
				return
			}
			peak = max(peak, rss)
			select {
			case <-stop:
				result <- peak
				return
			case <-ticker.C:
			}
		}
	}()
	return stop, result
}

type (
	uploadStatement struct{ Calls, NS int64 }
	uploadSQLReport struct {
		Statements             map[string]uploadStatement
		LockWaitNS, LockHeldNS []int64
	}
)

type uploadSQL struct {
	mu     sync.Mutex
	active bool
	report uploadSQLReport
}

func (s *uploadSQL) record(query string, elapsed time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return
	}
	if s.report.Statements == nil {
		s.report.Statements = make(map[string]uploadStatement)
	}
	value := s.report.Statements[query]
	value.Calls++
	value.NS += elapsed.Nanoseconds()
	s.report.Statements[query] = value
}

func (s *uploadSQL) snapshot() uploadSQLReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.report
}

type uploadConnector struct {
	driver.Connector
	stats *uploadSQL
}

type uploadPQConn interface {
	driver.Conn
	driver.ConnBeginTx
	driver.ExecerContext
	driver.QueryerContext
	driver.Pinger
	driver.SessionResetter
	driver.Validator
}

func (c uploadConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	pqConn, ok := conn.(uploadPQConn)
	if !ok {
		conn.Close()
		return nil, fmt.Errorf("pq lacks required measurement interfaces")
	}
	return &uploadConn{uploadPQConn: pqConn, stats: c.stats}, nil
}

type uploadConn struct {
	uploadPQConn
	stats  *uploadSQL
	locked time.Time
}

func (c *uploadConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	start := time.Now()
	result, err := c.uploadPQConn.ExecContext(ctx, query, args)
	elapsed := time.Since(start)
	c.stats.record(query, elapsed)
	if err == nil && query == "SELECT pg_advisory_xact_lock($1)" {
		c.stats.mu.Lock()
		if c.stats.active {
			c.locked = time.Now()
			c.stats.report.LockWaitNS = append(c.stats.report.LockWaitNS, elapsed.Nanoseconds())
		}
		c.stats.mu.Unlock()
	}
	return result, err
}

func (c *uploadConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	start := time.Now()
	rows, err := c.uploadPQConn.QueryContext(ctx, query, args)
	c.stats.record(query, time.Since(start)) // Dispatch through rows availability, not iteration.
	return rows, err
}

func (c *uploadConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	start := time.Now()
	tx, err := c.uploadPQConn.BeginTx(ctx, opts)
	c.stats.record("BEGIN", time.Since(start))
	if err != nil {
		return nil, err
	}
	return uploadTx{Tx: tx, conn: c}, nil
}

type uploadTx struct {
	driver.Tx
	conn *uploadConn
}

func (tx uploadTx) finish(name string, finish func() error) error {
	start := time.Now()
	err := finish()
	tx.conn.stats.record(name, time.Since(start))
	if !tx.conn.locked.IsZero() {
		tx.conn.stats.mu.Lock()
		if tx.conn.stats.active {
			tx.conn.stats.report.LockHeldNS = append(tx.conn.stats.report.LockHeldNS, time.Since(tx.conn.locked).Nanoseconds())
		}
		tx.conn.locked = time.Time{}
		tx.conn.stats.mu.Unlock()
	}
	return err
}

func (tx uploadTx) Commit() error   { return tx.finish("COMMIT", tx.Tx.Commit) }
func (tx uploadTx) Rollback() error { return tx.finish("ROLLBACK", tx.Tx.Rollback) }

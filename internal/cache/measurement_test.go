package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"receipt/internal/auth"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"
)

// TestMeasurementWorkloads is an opt-in experiment, not a performance assertion.
// The fixture creates an isolated schema and temp blob directory for each size.
func TestMeasurementWorkloads(t *testing.T) {
	if os.Getenv("RECEIPT_MEASURE") != "1" {
		t.Skip("set RECEIPT_MEASURE=1 and a disposable RECEIPT_TEST_DATABASE_URL")
	}
	for _, size := range []int{16, 128} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			s, credentials := credentialFixture(t)
			ctx := context.Background()
			values := make([]Result, size)
			for i := range values {
				v := example()
				v.Manifest.Command = []string{"pytest", fmt.Sprint(i)}
				v.Key = v.Manifest.Key()
				v.Stdout = append(bytes.Repeat([]byte("x"), 1024), []byte(fmt.Sprint(i))...)
				if err := s.Put(ctx, v); err != nil {
					t.Fatal(err)
				}
				values[i] = v
			}
			issued := issueForTest(t, credentials, auth.Read, auth.Publish)
			for _, mode := range []string{"read", "write", "mixed", "read-during-collection"} {
				for _, workers := range []int{1, 4} {
					measureWorkload(t, s, credentials, issued.Token, values, mode, workers)
				}
			}
		})
	}
}

func measurementRequest(client *http.Client, url, token string, value Result, mode string) (int64, error) {
	method := http.MethodGet
	var body io.Reader
	if mode == "write" {
		method = http.MethodPut
		b, err := Encode(value)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(b)
	}
	r, err := http.NewRequest(method, url+"/v1/entries/"+value.Key, body)
	if err != nil {
		return 0, err
	}
	r.Header.Set("Authorization", "Bearer "+token)
	started := time.Now()
	response, err := client.Do(r)
	if err != nil {
		return 0, err
	}
	_, readErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return 0, err
	}
	if response.StatusCode >= 300 {
		return 0, fmt.Errorf("measurement status %d", response.StatusCode)
	}
	return time.Since(started).Nanoseconds(), nil
}

func measureWorkload(t *testing.T, s Postgres, credentials auth.Postgres, token string, values []Result, mode string, workers int) {
	t.Helper()
	var logs bytes.Buffer
	logger, err := NewRequestLog(&logs)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(logger.Handler(HandlerAuthorized(s, credentials, 16)))
	defer server.Close()
	client := server.Client()
	client.Timeout = 10 * time.Second
	var wg sync.WaitGroup
	errs := make(chan error, workers+1)
	durations := make(chan int64, workers*8)
	start := make(chan struct{})
	if mode == "read-during-collection" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < 8; i++ {
				if _, err := s.Collect(context.Background(), false); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			for i := 0; i < 8; i++ {
				v := values[(worker*8+i)%len(values)]
				requestMode := mode
				if mode == "mixed" && (worker+i)%2 == 0 {
					requestMode = "write"
				}
				ns, err := measurementRequest(client, server.URL, token, v, requestMode)
				if err != nil {
					errs <- err
					return
				}
				durations <- ns
			}
		}(worker)
	}
	close(start)
	wg.Wait()
	server.Close()
	close(errs)
	close(durations)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	reportMeasurements(t, len(values), mode, workers, durations, &logs)
}

func reportMeasurements(t *testing.T, size int, mode string, workers int, durations <-chan int64, logs *bytes.Buffer) {
	t.Helper()
	samples := []int64{}
	for ns := range durations {
		samples = append(samples, ns)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	stages := map[string]int64{}
	decoder := json.NewDecoder(logs)
	events := 0
	for {
		var e RequestEvent
		err := decoder.Decode(&e)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		events++
		for _, timing := range e.Stages {
			stages[timing.Stage] += timing.NS
		}
	}
	if events != len(samples) {
		t.Fatalf("events %d, samples %d", events, len(samples))
	}
	record := struct {
		StoreSize   int              `json:"store_entries"`
		Mode        string           `json:"mode"`
		Workers     int              `json:"workers"`
		SamplesNS   []int64          `json:"sorted_client_ns"`
		StageTotals map[string]int64 `json:"server_stage_totals_ns"`
		Go          string           `json:"go"`
		Platform    string           `json:"platform"`
	}{size, mode, workers, samples, stages, runtime.Version(), runtime.GOOS + "/" + runtime.GOARCH}
	b, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("MEASURE " + string(b))
}

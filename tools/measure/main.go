// Command measure compares direct execution, a local miss and local hits.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type stage struct {
	Stage string `json:"stage"`
	NS    int64  `json:"ns"`
	Calls int64  `json:"calls"`
}
type timing struct {
	Type       string  `json:"type"`
	DurationNS int64   `json:"duration_ns"`
	Stages     []stage `json:"stages"`
}
type sample struct {
	Kind      string  `json:"kind"`
	ClientNS  int64   `json:"client_ns"`
	ExitCode  int     `json:"exit_code"`
	Stages    []stage `json:"stages,omitempty"`
	ReceiptNS int64   `json:"receipt_ns,omitempty"`
	Executed  *bool   `json:"executed,omitempty"`
}

// Retain only the final stderr tail; a noisy workload must not fill memory.
type tail struct{ data []byte }

func (t *tail) Write(p []byte) (int, error) {
	const limit = 64 << 10
	n := len(p)
	if n >= limit {
		t.data = append(t.data[:0], p[n-limit:]...)
		return n, nil
	}
	if len(t.data)+n > limit {
		t.data = t.data[len(t.data)+n-limit:]
	}
	t.data = append(t.data, p...)
	return n, nil
}

func parseTiming(data []byte, kind string) (timing, bool, error) {
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	var summary timing
	if err := json.Unmarshal(lines[len(lines)-1], &summary); err != nil {
		return summary, false, fmt.Errorf("missing final Receipt timing summary")
	}
	if summary.Type != "receipt_timing" || summary.Stages == nil || summary.DurationNS < 0 {
		return summary, false, fmt.Errorf("invalid Receipt timing summary")
	}
	executed := false
	for _, s := range summary.Stages {
		if s.Stage == "execution" {
			executed = true
		}
	}
	if kind == "hit" && executed {
		return summary, executed, fmt.Errorf("expected a hit but Receipt executed; inspect inputs separately")
	}
	if kind == "miss" && !executed {
		return summary, executed, fmt.Errorf("expected initial execution in a new cache")
	}
	return summary, executed, nil
}

func measure(command []string, cwd, kind string) (sample, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = cwd
	cmd.Stdout = io.Discard
	stderr := &tail{}
	cmd.Stderr = stderr
	started := time.Now()
	err := cmd.Run()
	s := sample{Kind: kind, ClientNS: time.Since(started).Nanoseconds()}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return s, fmt.Errorf("%s exited %d; no performance claim recorded", kind, exit.ExitCode())
		}
		return s, fmt.Errorf("%s could not complete; no performance claim recorded", kind)
	}
	if kind == "direct" {
		return s, nil
	}
	summary, executed, err := parseTiming(stderr.data, kind)
	if err != nil {
		return s, err
	}
	s.Stages, s.ReceiptNS, s.Executed = summary.Stages, summary.DurationNS, &executed
	return s, nil
}

func collect(repo, receipt string, repeats int, command []string) (_ []sample, err error) {
	cache, err := os.MkdirTemp("", "receipt-measure-")
	if err != nil {
		return nil, fmt.Errorf("create measurement cache: %w", err)
	}
	defer func() { err = errors.Join(err, os.RemoveAll(cache)) }()
	wrapped := append([]string{receipt, "run", "--timings", "--cache-dir", cache, "--"}, command...)
	var samples []sample
	for i := 0; i < 2*repeats+1; i++ {
		args, kind := command, "direct"
		if i == repeats {
			args, kind = wrapped, "miss"
		} else if i > repeats {
			args, kind = wrapped, "hit"
		}
		s, err := measure(args, repo, kind)
		if err != nil {
			return nil, err
		}
		samples = append(samples, s)
	}
	return samples, nil
}

func run() error {
	repo := flag.String("repo", "", "checkout in which to run the selected command")
	receipt := flag.String("receipt", "", "Receipt executable")
	repeats := flag.Int("repeats", 5, "direct and hit samples (1–100)")
	flag.Parse()
	if *repo == "" || *receipt == "" || len(flag.Args()) == 0 || *repeats < 1 || *repeats > 100 {
		return fmt.Errorf("provide --repo, --receipt, 1–100 repeats, and -- followed by a command")
	}
	binary, err := filepath.Abs(*receipt)
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	samples, err := collect(*repo, binary, *repeats, flag.Args())
	if err != nil {
		return err
	}
	output := struct {
		Samples []sample `json:"samples"`
		Scope   string   `json:"scope"`
	}{samples, "one selected command; warm OS caches; no edits between runs"}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		return fmt.Errorf("write measurements: %w", err)
	}
	return nil
}

func main() {
	log.SetFlags(0)
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

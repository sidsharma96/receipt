package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"receipt/internal/observe"
	"testing"
)

type timingBrokenWriter struct{}

func (timingBrokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestRunTimingSummaryIsSeparateAndRedacted(t *testing.T) {
	var out bytes.Buffer
	ctx, finish := startRunTrace(context.Background(), &out)
	observe.Measure(ctx, observe.Execution)()
	finish(7, errors.New("private command or output"))
	var got struct {
		Type     string
		ExitCode int `json:"exit_code"`
		Failed   bool
		Stages   []observe.Timing
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != "receipt_timing" || got.ExitCode != 7 || !got.Failed || len(got.Stages) != 1 || bytes.Contains(out.Bytes(), []byte("private")) {
		t.Fatalf("%s", out.Bytes())
	}
	_, finish = startRunTrace(context.Background(), timingBrokenWriter{})
	finish(0, nil)
}

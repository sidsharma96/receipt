package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"receipt/internal/cache"
	"receipt/internal/observe"
	"time"
)

func requestLogging(handler http.Handler, enabled bool, out io.Writer) (http.Handler, func(), error) {
	if !enabled {
		return handler, func() {}, nil
	}
	logger, err := cache.NewRequestLog(out)
	if err != nil {
		return nil, nil, err
	}
	return logger.Handler(handler), func() {
		if logger.Failures() > 0 {
			diagnostic(out, "receipt: request log writes failed:", logger.Failures())
		}
	}, nil
}

func startRunTrace(ctx context.Context, out io.Writer) (context.Context, func(int, error)) {
	ctx, trace := observe.Start(ctx)
	started := time.Now()
	return ctx, func(code int, runErr error) {
		summary := struct {
			Type       string           `json:"type"`
			DurationNS int64            `json:"duration_ns"`
			ExitCode   int              `json:"exit_code"`
			Failed     bool             `json:"failed"`
			Stages     []observe.Timing `json:"stages"`
		}{"receipt_timing", time.Since(started).Nanoseconds(), code, runErr != nil, trace.Snapshot()}
		if err := json.NewEncoder(out).Encode(summary); err != nil {
			// Optional diagnostics do not change the command's result.
			return
		}
	}
}

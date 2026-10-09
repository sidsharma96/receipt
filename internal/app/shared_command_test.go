package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestSharedCommandValidation(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"unknown"},
		{"stats", "--apply"},
		{"gc", "--older-than", "24h"},
		{"prune", "--older-than", "0"},
		{"gc", "--timeout", "0"},
		{"gc", "--max-entries", "1"},
		{"gc"},
	} {
		var out, errOut bytes.Buffer
		if err := sharedCommand(context.Background(), args, &out, &errOut); err == nil {
			t.Fatal("invalid command accepted", args)
		}
	}
	var out, errOut bytes.Buffer
	if err := sharedCommand(context.Background(), []string{"gc", "--help"}, &out, &errOut); err != nil || !strings.Contains(errOut.String(), "apply") {
		t.Fatal(err, errOut.String())
	}
}

func TestParseSharedMode(t *testing.T) {
	for _, mode := range []sharedMode{sharedStats, sharedLimits, sharedGC, sharedPrune} {
		got, err := parseSharedMode(string(mode))
		if err != nil || got != mode {
			t.Fatalf("parse %q: got %q, %v", mode, got, err)
		}
	}
	for _, raw := range []string{"", "Stats", " stats", "unknown"} {
		if _, err := parseSharedMode(raw); err == nil {
			t.Fatalf("accepted invalid mode %q", raw)
		}
	}
}

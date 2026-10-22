package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestTimingRequiresExpectedExecution(t *testing.T) {
	hit := []byte(`{"type":"receipt_timing","duration_ns":10,"stages":[]}`)
	miss := []byte(`{"type":"receipt_timing","duration_ns":10,"stages":[{"stage":"execution","ns":9,"calls":1}]}`)
	for _, tc := range []struct {
		data []byte
		kind string
		fail bool
	}{
		{hit, "hit", false},
		{miss, "miss", false},
		{hit, "miss", true},
		{miss, "hit", true},
		{[]byte("private child output"), "hit", true},
		{[]byte(`{"type":"receipt_timing"}`), "hit", true},
	} {
		_, _, err := parseTiming(tc.data, tc.kind)
		if (err != nil) != tc.fail {
			t.Fatalf("%s: error=%v", tc.kind, err)
		}
		if err != nil && strings.Contains(err.Error(), "private child output") {
			t.Fatal("leaked output")
		}
	}
}

func TestTailRetainsFinalSummary(t *testing.T) {
	var buffer tail
	buffer.Write(bytes.Repeat([]byte("x"), 100000))
	buffer.Write([]byte("\nsummary\n"))
	if len(buffer.data) != 64<<10 || !bytes.HasSuffix(buffer.data, []byte("\nsummary\n")) {
		t.Fatal("incorrect bounded tail")
	}
}

func TestFailedCommandDoesNotProduceSampleClaim(t *testing.T) {
	_, err := measure([]string{"sh", "-c", "echo private-output >&2; exit 3"}, t.TempDir(), "direct")
	if err == nil || !strings.Contains(err.Error(), "exited 3") || strings.Contains(err.Error(), "private-output") {
		t.Fatal(err)
	}
}

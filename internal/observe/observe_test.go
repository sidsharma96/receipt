package observe

import (
	"context"
	"sync"
	"testing"
)

func TestTraceIsolationAndConcurrentAggregation(t *testing.T) {
	ctx, a := Start(context.Background())
	_, b := Start(ctx)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); defer Measure(ctx, Files)() }()
	}
	wg.Wait()
	got := a.Snapshot()
	if len(got) != 1 || got[0].Calls != 32 || got[0].NS < 0 || len(b.Snapshot()) != 0 {
		t.Fatalf("%+v", got)
	}
	Measure(context.Background(), Files)()
	Measure(ctx, Stage(255))()
	got[0].Calls = 0
	if a.Snapshot()[0].Calls != 32 {
		t.Fatal("snapshot aliases recorder")
	}
}

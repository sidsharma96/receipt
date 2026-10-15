package cache

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// countingBlobs records directory walks so a test can prove that an upload
// does not inventory the whole store.
type countingBlobs struct {
	FileBlobs
	walks *atomic.Int64
}

func (b countingBlobs) Inventory(ctx context.Context) ([]BlobObject, error) {
	b.walks.Add(1)
	return b.FileBlobs.Inventory(ctx)
}

func TestUploadDoesNotWalkTheStore(t *testing.T) {
	s := postgresFixture(t)
	ctx := context.Background()
	for _, name := range []string{"a", "b", "c"} {
		if err := s.Put(ctx, namedResult(name, name)); err != nil {
			t.Fatal(err)
		}
	}
	walks := &atomic.Int64{}
	s.Blobs = countingBlobs{s.Blobs.(FileBlobs), walks}
	if err := s.Put(ctx, namedResult("d", "d")); err != nil {
		t.Fatal(err)
	}
	if n := walks.Load(); n != 0 {
		t.Fatalf("upload walked the output directory %d times", n)
	}
}

// assertCountersMatch compares the running totals with a full recount.
func assertCountersMatch(t *testing.T, s Postgres, step string) {
	t.Helper()
	ctx := context.Background()
	tx, err := s.lifecycle(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	counted, err := readUsage(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	actual, _, err := s.inventory(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	if counted.BlobBytes != actual.BlobBytes || counted.MetadataBytes != actual.MetadataBytes || counted.Entries != actual.Entries || counted.Evidence != actual.Evidence {
		t.Fatalf("%s: counters %+v, recount %+v", step, counted, actual)
	}
}

func TestUsageCountersFollowEveryChange(t *testing.T) {
	s := postgresFixture(t)
	ctx := context.Background()
	assertCountersMatch(t, s, "empty")
	a := namedResult("a", "shared output")
	b := namedResult("b", "shared output")
	if err := s.Put(ctx, a); err != nil {
		t.Fatal(err)
	}
	assertCountersMatch(t, s, "first upload")
	if err := s.Put(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, b); err != nil {
		t.Fatal(err)
	}
	assertCountersMatch(t, s, "duplicate and shared output")
	b.Stdout = []byte("different output")
	if err := s.Put(ctx, b); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	assertCountersMatch(t, s, "conflict")
	never := namedResult("never-stored", "x")
	if err := s.Report(ctx, ReportFor(never)); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	assertCountersMatch(t, s, "report for a new key")
	if _, err := s.DB.Exec("UPDATE receipt_entries_v2 SET created_at=now()-interval '48 hours'"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Expire(ctx, time.Now().Add(-24*time.Hour), true); err != nil {
		t.Fatal(err)
	}
	assertCountersMatch(t, s, "expiry")
	if _, err := s.Collect(ctx, true); err != nil {
		t.Fatal(err)
	}
	assertCountersMatch(t, s, "collection")
	if _, err := s.DB.Exec("UPDATE receipt_storage_usage_v6 SET blob_bytes=0, metadata_bytes=0, entries=0, evidence=0"); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	assertCountersMatch(t, s, "startup reconciliation")
	// A store created by an earlier build has no totals table at all.
	if _, err := s.DB.Exec("DROP TABLE receipt_storage_usage_v6"); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	assertCountersMatch(t, s, "store from an earlier build")
	if err := s.Put(ctx, namedResult("after upgrade", "new")); err != nil {
		t.Fatal(err)
	}
	assertCountersMatch(t, s, "upload after upgrade")
}

func TestRejectedUploadWritesNoFiles(t *testing.T) {
	s := postgresFixture(t)
	ctx := context.Background()
	l := DefaultLimits
	l.MaxEntries = 1
	if err := s.SetLimits(ctx, l, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, namedResult("a", "a")); err != nil {
		t.Fatal(err)
	}
	_, before, err := s.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, namedResult("b", "b")); !errors.Is(err, ErrQuota) {
		t.Fatal(err)
	}
	_, after, err := s.Stats(ctx)
	if err != nil || after.BlobFiles != before.BlobFiles || after.BlobBytes != before.BlobBytes {
		t.Fatalf("a rejected upload left files: before %+v, after %+v, %v", before, after, err)
	}
}

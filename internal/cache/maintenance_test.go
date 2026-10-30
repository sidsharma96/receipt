package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func namedResult(name, output string) Result {
	v := example()
	v.Manifest.Command = []string{name}
	v.Key = v.Manifest.Key()
	v.Stdout = []byte(output)
	return v
}

func TestSharedQuotaCountsIdenticalStreamsOnce(t *testing.T) {
	for _, output := range []string{"12345", ""} {
		t.Run(fmt.Sprintf("bytes-%d", len(output)), func(t *testing.T) {
			s := postgresFixture(t)
			ctx := context.Background()
			limits := DefaultLimits
			limits.MaxBlobBytes = int64(len(output))
			if err := s.SetLimits(ctx, limits, true); err != nil {
				t.Fatal(err)
			}
			v := namedResult("identical-streams", output)
			v.Stderr = bytes.Clone(v.Stdout)
			if err := s.Put(ctx, v); err != nil {
				t.Fatal("identical stdout and stderr must fit one copy's byte budget", err)
			}
			got, err := s.Get(ctx, v.Key)
			if err != nil || !bytes.Equal(got.Stdout, v.Stdout) || !bytes.Equal(got.Stderr, v.Stderr) {
				t.Fatal("shared streams did not round-trip", err)
			}
			_, usage, err := s.Stats(ctx)
			if err != nil || usage.BlobBytes != int64(len(output)) || usage.BlobFiles != 1 {
				t.Fatalf("identical streams counted more than once: %+v, %v", usage, err)
			}
			assertCountersMatch(t, s, "identical stdout and stderr")
		})
	}
}

func TestSharedQuotaAndDedup(t *testing.T) {
	s := postgresFixture(t)
	ctx := context.Background()
	limits := DefaultLimits
	limits.MaxBlobBytes = 5
	limits.MaxEntries = 2
	if err := s.SetLimits(ctx, limits, true); err != nil {
		t.Fatal(err)
	}
	a := namedResult("a", "12345")
	b := namedResult("b", "12345")
	if err := s.Put(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, b); err != nil {
		t.Fatal("shared bytes charged twice", err)
	}
	if err := s.Put(ctx, namedResult("c", "12345")); !errors.Is(err, ErrQuota) {
		t.Fatal("entry quota not enforced", err)
	}
	if err := s.Put(ctx, namedResult("d", "other")); !errors.Is(err, ErrQuota) {
		t.Fatal("blob quota not enforced", err)
	}
	l, u, err := s.Stats(ctx)
	if err != nil || u.BlobBytes != 5 || u.Entries != 2 || u.BlobFiles != 2 || l != limits {
		t.Fatalf("%+v %+v %v", l, u, err)
	}
	lower := limits
	lower.MaxBlobBytes = 4
	if err = s.SetLimits(ctx, lower, true); err == nil {
		t.Fatal("lowered below existing usage")
	}
	higher := limits
	higher.MaxBlobBytes = 100
	if err = s.SetLimits(ctx, higher, false); err != nil {
		t.Fatal(err)
	}
	l, _, err = s.Stats(ctx)
	if err != nil || l != limits {
		t.Fatal("dry-run changed limits")
	}
	server := httptest.NewServer(Handler(s, "a-token-long-enough-for-tests"))
	defer server.Close()
	remote := Remote{URL: server.URL, Token: "a-token-long-enough-for-tests"}
	if err = remote.Put(ctx, namedResult("e", "more")); !errors.Is(err, ErrQuota) {
		t.Fatal("HTTP quota mapping", err)
	}
}

func TestEvidenceLimitStillQuarantines(t *testing.T) {
	s := postgresFixture(t)
	ctx := context.Background()
	l := DefaultLimits
	l.MaxEvidence = 1
	if err := s.SetLimits(ctx, l, true); err != nil {
		t.Fatal(err)
	}
	v := example()
	if err := s.Put(ctx, v); err != nil {
		t.Fatal(err)
	}
	v.Stdout = []byte("different")
	if err := s.Put(ctx, v); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	v.ExitCode = 7
	if err := s.Report(ctx, ReportFor(v)); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	_, u, err := s.Stats(ctx)
	if err != nil || u.Evidence != 1 || u.EvidenceCapped != 1 {
		t.Fatalf("%+v %v", u, err)
	}
	if _, err = s.Get(ctx, v.Key); !errors.Is(err, ErrConflict) {
		t.Fatal("evidence quota erased safety decision", err)
	}
}

func TestMetadataQuotaAndOrphanCollection(t *testing.T) {
	s := postgresFixture(t)
	ctx := context.Background()
	l := DefaultLimits
	l.MaxMetadataBytes = 1
	if err := s.SetLimits(ctx, l, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, example()); !errors.Is(err, ErrQuota) {
		t.Fatal(err)
	}
	_, u, err := s.Stats(ctx)
	if err != nil || u.Entries != 0 || u.Evidence != 0 || u.BlobFiles != 0 {
		t.Fatalf("a refused upload wrote files: %+v %v", u, err)
	}
	// Orphans now come only from failures after the files are written; make two directly.
	for _, b := range []string{"orphan one", "orphan two"} {
		if _, err = s.Blobs.Put(ctx, []byte(b)); err != nil {
			t.Fatal(err)
		}
	}
	preview, err := s.Collect(ctx, false)
	if err != nil || preview.Files != 2 {
		t.Fatalf("%+v %v", preview, err)
	}
	_, still, err := s.Stats(ctx)
	if err != nil || still.BlobFiles != 2 {
		t.Fatal("dry run removed blobs")
	}
	applied, err := s.Collect(ctx, true)
	if err != nil || applied.Files != 2 {
		t.Fatalf("%+v %v", applied, err)
	}
	_, after, err := s.Stats(ctx)
	if err != nil || after.BlobBytes != 0 || after.BlobFiles != 0 {
		t.Fatalf("%+v %v", after, err)
	}
	again, err := s.Collect(ctx, true)
	if err != nil || again.Files != 0 {
		t.Fatal("collection not repeatable", err)
	}
}

func TestStorageRetentionPreservesQuarantine(t *testing.T) {
	s := postgresFixture(t)
	ctx := context.Background()
	a := namedResult("a", "one")
	b := namedResult("b", "two")
	if err := s.Put(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, b); err != nil {
		t.Fatal(err)
	}
	b.Stdout = []byte("three")
	if err := s.Put(ctx, b); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("UPDATE receipt_entries_v2 SET created_at=now()-interval '48 hours'"); err != nil {
		t.Fatal(err)
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	preview, err := s.Expire(ctx, cutoff, false)
	if err != nil || preview.Entries != 1 {
		t.Fatal(preview, err)
	}
	if _, err = s.Get(ctx, a.Key); err != nil {
		t.Fatal("dry-run removed result", err)
	}
	result, err := s.Expire(ctx, cutoff, true)
	if err != nil || result.Entries != 1 {
		t.Fatal(result, err)
	}
	if _, err = s.Get(ctx, a.Key); !errors.Is(err, ErrMiss) {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, b.Key); !errors.Is(err, ErrConflict) {
		t.Fatal("quarantine lost", err)
	}
	collected, err := s.Collect(ctx, true)
	if err != nil || collected.Files != 2 {
		t.Fatal(collected, err)
	}
	_, u, err := s.Stats(ctx)
	if err != nil || u.BlobFiles != 2 || u.Entries != 1 || u.Evidence != 2 {
		t.Fatalf("%+v %v", u, err)
	}
}

func TestGCFailsClosedOnCorruptMetadataAndSymlinks(t *testing.T) {
	s := postgresFixture(t)
	ctx := context.Background()
	v := example()
	if err := s.Put(ctx, v); err != nil {
		t.Fatal(err)
	}
	orphan, err := s.Blobs.Put(ctx, []byte("orphan"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("UPDATE receipt_entries_v2 SET record='{}'::jsonb"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Collect(ctx, true); !errors.Is(err, ErrCorrupt) {
		t.Fatal(err)
	}
	if _, err = s.Blobs.Get(ctx, orphan); err != nil {
		t.Fatal("deleted before metadata validation", err)
	}
	files := s.Blobs.(FileBlobs)
	target := filepath.Join(t.TempDir(), "keep")
	if err = os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(target, filepath.Join(files.Dir, "aa")); err != nil {
		t.Fatal(err)
	}
	if _, err = files.Inventory(ctx); err == nil {
		t.Fatal("symlink accepted")
	}
	b, err := os.ReadFile(target)
	if err != nil || string(b) != "untouched" {
		t.Fatal("external target touched")
	}
}

func TestStorageBindingRejectsWrongMetadataStore(t *testing.T) {
	a := postgresFixture(t)
	b := postgresFixture(t)
	ctx := context.Background()
	v := example()
	if err := a.Put(ctx, v); err != nil {
		t.Fatal(err)
	}
	b.Blobs = a.Blobs
	root, _ := a.Blobs.(FileBlobs).Location()
	if _, err := b.DB.Exec("UPDATE receipt_storage_policy_v3 SET blob_root=$1", root); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Collect(ctx, true); err == nil {
		t.Fatal("different metadata store collected shared directory")
	}
	if _, err := a.Get(ctx, v.Key); err != nil {
		t.Fatal("original store damaged", err)
	}
}

type blockingBlobs struct {
	FileBlobs
	entered chan struct{}
	release chan struct{}
	first   atomic.Bool
	read    bool
}

func (b *blockingBlobs) wait() {
	if b.first.CompareAndSwap(false, true) {
		close(b.entered)
		<-b.release
	}
}

func (b *blockingBlobs) Put(ctx context.Context, data []byte) (BlobRef, error) {
	if !b.read {
		b.wait()
	}
	return b.FileBlobs.Put(ctx, data)
}

func (b *blockingBlobs) Get(ctx context.Context, ref BlobRef) ([]byte, error) {
	if b.read {
		b.wait()
	}
	return b.FileBlobs.Get(ctx, ref)
}

func TestLifecycleCancellationKeepsFilesystemExclusion(t *testing.T) {
	s := postgresFixture(t)
	b := &blockingBlobs{FileBlobs: s.Blobs.(FileBlobs), entered: make(chan struct{}), release: make(chan struct{})}
	s.Blobs = b
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(b.release) }) }
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Put(ctx, example()) }()
	<-b.entered
	cancel()
	gcCtx, gcCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer gcCancel()
	if _, err := s.Collect(gcCtx, true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("collection entered active publication", err)
	}
	release()
	if err := <-done; err == nil {
		t.Fatal("canceled upload succeeded")
	}
	if _, err := s.Collect(context.Background(), true); err != nil {
		t.Fatal("lock leaked", err)
	}
}

func TestDatabaseConnectionLossKeepsFilesystemExclusion(t *testing.T) {
	s := postgresFixture(t)
	b := &blockingBlobs{FileBlobs: s.Blobs.(FileBlobs), entered: make(chan struct{}), release: make(chan struct{})}
	s.Blobs = b
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(b.release) }) }
	defer release()
	ctx := context.Background()
	done := make(chan error, 1)
	go func() { done <- s.Put(ctx, example()) }()
	<-b.entered
	var pid int
	if err := s.DB.QueryRow(`SELECT pid FROM pg_locks WHERE locktype='advisory' AND objid=$1 AND classid=$2 AND mode='ExclusiveLock' AND granted`, lifecycleLock&0xffffffff, lifecycleLock>>32).Scan(&pid); err != nil {
		release()
		<-done
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("SELECT pg_terminate_backend($1)", pid); err != nil {
		release()
		<-done
		t.Fatal(err)
	}
	gcCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if _, err := s.Collect(gcCtx, true); !errors.Is(err, context.DeadlineExceeded) {
		release()
		<-done
		t.Fatal("filesystem exclusion lost with SQL session", err)
	}
	release()
	if err := <-done; err == nil {
		t.Fatal("dead connection committed upload")
	}
	if _, err := s.Collect(ctx, true); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentUploadsAndCollection(t *testing.T) {
	s := postgresFixture(t)
	ctx := context.Background()
	errs := make(chan error, 40)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		v := namedResult(fmt.Sprintf("job-%d", i), "shared")
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.Put(ctx, v) }()
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.Collect(ctx, true); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 20; i++ {
		if _, err := s.Get(ctx, namedResult(fmt.Sprintf("job-%d", i), "shared").Key); err != nil {
			t.Fatal("referenced output collected", err)
		}
	}
}

func TestRetentionWaitsForReaders(t *testing.T) {
	s := postgresFixture(t)
	ctx := context.Background()
	v := example()
	if err := s.Put(ctx, v); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("UPDATE receipt_entries_v2 SET created_at=now()-interval '48 hours'"); err != nil {
		t.Fatal(err)
	}
	b := &blockingBlobs{FileBlobs: s.Blobs.(FileBlobs), entered: make(chan struct{}), release: make(chan struct{}), read: true}
	s.Blobs = b
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(b.release) }) }
	defer release()
	done := make(chan error, 1)
	go func() { _, err := s.Get(ctx, v.Key); done <- err }()
	<-b.entered
	bounded, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if _, err := s.Expire(bounded, time.Now().Add(-24*time.Hour), true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("expired while reader active", err)
	}
	release()
	if err := <-done; err != nil {
		t.Fatal("reader output deleted", err)
	}
	if _, err := s.Expire(ctx, time.Now().Add(-24*time.Hour), true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Collect(ctx, true); err != nil {
		t.Fatal(err)
	}
}

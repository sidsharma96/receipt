package cache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDiskPublicationFailureCleansTemporaryAndAllowsRetry(t *testing.T) {
	d := Disk{Dir: t.TempDir()}
	v := example()
	path, err := d.path(v.Key)
	if err != nil {
		t.Fatal(err)
	}
	// The destination is occupied by a directory. The temp file can be written
	// and synced, but it cannot become a readable result at this path.
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := d.Put(context.Background(), v); err == nil {
		t.Fatal("publication failure acknowledged as success")
	}
	pending, err := filepath.Glob(filepath.Join(d.Dir, "entries", ".pending-*"))
	if err != nil || len(pending) != 0 {
		t.Fatal("temp files left after failed publication", pending, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := d.Put(context.Background(), v); err != nil {
		t.Fatal("retry failed", err)
	}
	if _, err := d.Get(context.Background(), v.Key); err != nil {
		t.Fatal("retry did not publish a readable result", err)
	}
}

func TestDiskDoesNotAcknowledgeCorruptExistingEntry(t *testing.T) {
	d := Disk{Dir: t.TempDir()}
	v := example()
	if err := d.Put(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	path, _ := d.path(v.Key)
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := d.Put(context.Background(), v); !errors.Is(err, ErrCorrupt) {
		t.Fatal("corrupt existing entry acknowledged", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != `{}` {
		t.Fatal("corruption silently overwritten")
	}
}

var errInjectedStorage = errors.New("injected storage failure")

// This wrapper exercises the actual FileBlobs publication and SQL workflow;
// it fails a selected operation without adding test switches to production code.
type failingBlobs struct {
	FileBlobs
	puts, removes           int
	failPutAt, failRemoveAt int
}

func (b *failingBlobs) Put(ctx context.Context, data []byte) (BlobRef, error) {
	b.puts++
	if b.puts == b.failPutAt {
		return BlobRef{}, errInjectedStorage
	}
	return b.FileBlobs.Put(ctx, data)
}

func (b *failingBlobs) Remove(ctx context.Context, object BlobObject) error {
	b.removes++
	if b.removes == b.failRemoveAt {
		return errInjectedStorage
	}
	return b.FileBlobs.Remove(ctx, object)
}

func assertNoPublishedRows(t *testing.T, s Postgres, key string) {
	t.Helper()
	for _, table := range []string{"receipt_entries_v2", "receipt_evidence_v2"} {
		var count int
		if err := s.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatal("failed upload changed metadata", table, count, err)
		}
	}
	if _, err := s.Get(context.Background(), key); !errors.Is(err, ErrMiss) {
		t.Fatal("failed upload became visible", err)
	}
}

func TestPostgresSecondBlobFailureLeavesCollectableOrphan(t *testing.T) {
	s := postgresFixture(t)
	b := &failingBlobs{FileBlobs: s.Blobs.(FileBlobs), failPutAt: 2}
	s.Blobs = b
	v := example()
	ctx := context.Background()
	if err := s.Put(ctx, v); !errors.Is(err, errInjectedStorage) {
		t.Fatal(err)
	}
	assertNoPublishedRows(t, s, v.Key)
	objects, err := b.Inventory(ctx)
	if err != nil || len(objects) != 1 || objects[0].Pending {
		t.Fatal("expected one complete orphan, not partial files", objects, err)
	}
	result, err := s.Collect(ctx, true)
	if err != nil || result.Files != 1 {
		t.Fatal("failed upload orphan not collectable", result, err)
	}
	if err := s.Put(ctx, v); err != nil {
		t.Fatal("retry failed or lock leaked", err)
	}
	if _, err := s.Get(ctx, v.Key); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresMetadataFailureLeavesCollectableBlobs(t *testing.T) {
	s := postgresFixture(t)
	v := example()
	ctx := context.Background()
	// A real PostgreSQL deferred trigger fails the commit after both files
	// were published. This is not a mock transaction or a simulated rollback.
	if _, err := s.DB.Exec(`CREATE FUNCTION phase4_injected_failure() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'injected commit failure'; END$$;
 CREATE CONSTRAINT TRIGGER phase4_injected_failure AFTER INSERT ON receipt_entries_v2 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION phase4_injected_failure()`); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, v); err == nil {
		t.Fatal("metadata failure acknowledged")
	}
	assertNoPublishedRows(t, s, v.Key)
	objects, err := s.Blobs.(ManagedBlobs).Inventory(ctx)
	if err != nil || len(objects) != 2 {
		t.Fatal("SQL failure unexpectedly removed files", objects, err)
	}
	result, err := s.Collect(ctx, true)
	if err != nil || result.Files != 2 {
		t.Fatal(result, err)
	}
	if _, err := s.DB.Exec("DROP TRIGGER phase4_injected_failure ON receipt_entries_v2"); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, v); err != nil {
		t.Fatal("retry failed or lock leaked", err)
	}
}

func TestPostgresCollectionPartialFailureIsRetryable(t *testing.T) {
	s := postgresFixture(t)
	b := &failingBlobs{FileBlobs: s.Blobs.(FileBlobs), failRemoveAt: 2}
	s.Blobs = b
	v := example()
	ctx := context.Background()
	if err := s.Put(ctx, v); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{"orphan-a", "orphan-b", "orphan-c"} {
		if _, err := b.Put(ctx, []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	result, err := s.Collect(ctx, true)
	if !errors.Is(err, errInjectedStorage) || result.Files != 1 {
		t.Fatal("expected one deletion followed by failure", result, err)
	}
	if _, err := s.Get(ctx, v.Key); err != nil {
		t.Fatal("partial collection damaged live output or leaked lock", err)
	}
	result, err = s.Collect(ctx, true)
	if err != nil || result.Files != 2 {
		t.Fatal("retry did not finish remaining orphans", result, err)
	}
}

func TestPostgresMalformedMetadataStopsCollectionBeforeDeletion(t *testing.T) {
	s := postgresFixture(t)
	v := example()
	ctx := context.Background()
	if err := s.Put(ctx, v); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Blobs.Put(ctx, []byte("orphan")); err != nil {
		t.Fatal(err)
	}
	// JSONB cannot preserve duplicate keys, but it can retain an unknown field.
	if _, err := s.DB.Exec(`UPDATE receipt_entries_v2 SET record=record || '{"unexpected":true}'::jsonb WHERE key=$1`, v.Key); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Blobs.(ManagedBlobs).Inventory(ctx)
	if _, err := s.Get(ctx, v.Key); !errors.Is(err, ErrCorrupt) {
		t.Fatal("malformed metadata was read", err)
	}
	if result, err := s.Collect(ctx, true); !errors.Is(err, ErrCorrupt) || result.Files != 0 {
		t.Fatal("collector deleted before validating metadata", result, err)
	}
	after, err := s.Blobs.(ManagedBlobs).Inventory(ctx)
	if err != nil || len(after) != len(before) {
		t.Fatal("collection changed files despite invalid metadata", after, err)
	}
}

func TestPostgresInvalidDirectInputDoesNotTouchStorage(t *testing.T) {
	s := postgresFixture(t)
	v := example()
	v.DurationNS = -1
	if err := s.Put(context.Background(), v); err == nil {
		t.Fatal("invalid direct result accepted")
	}
	report := ReportFor(v)
	report.ExitCode = 256
	if err := s.Report(context.Background(), report); err == nil {
		t.Fatal("invalid direct report accepted")
	}
	assertNoPublishedRows(t, s, v.Key)
	objects, err := s.Blobs.(ManagedBlobs).Inventory(context.Background())
	if err != nil || len(objects) != 0 {
		t.Fatal("invalid input created blobs", objects, err)
	}
}

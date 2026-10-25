package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"testing"
)

func TestDuplicateBlobRejectsChangedBytes(t *testing.T) {
	want := bytes.Repeat([]byte("original"), 8192)
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"same-length", bytes.Repeat([]byte("modified"), 8192)},
		{"truncated", want[:len(want)-1]},
		{"trailing-byte", append(append([]byte(nil), want...), 'x')},
		{"over-limit", bytes.Repeat([]byte("x"), MaxBlobBytes+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := FileBlobs{Dir: t.TempDir()}
			ref, err := s.Put(context.Background(), want)
			if err != nil {
				t.Fatal(err)
			}
			path, err := s.path(ref.Hash)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, test.data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err = s.Put(context.Background(), want); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("damaged duplicate acknowledged: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, test.data) {
				t.Fatal("duplicate publication changed the stored object", err)
			}
		})
	}
}

func TestDuplicateBlobVerification(t *testing.T) {
	s := FileBlobs{Dir: t.TempDir()}
	ctx := context.Background()
	for _, size := range []int{0, 32767, 32768, 32769, MaxBlobBytes} {
		data := bytes.Repeat([]byte("x"), size)
		ref, err := s.Put(ctx, data)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.Put(ctx, data); err != nil {
			t.Fatalf("valid duplicate of %d bytes: %v", size, err)
		}
		path, err := s.path(ref.Hash)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err = s.verifyExisting(ctx, ref); !errors.Is(err, ErrMiss) {
			t.Fatalf("missing duplicate: %v", err)
		}
		if err = os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err = s.verifyExisting(ctx, ref); err == nil {
			t.Fatal("directory read error suppressed")
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if err = s.verifyExisting(canceled, ref); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled verification: %v", err)
		}
	}
}

func TestBlobProcessHelper(t *testing.T) {
	dir := os.Getenv("RECEIPT_BLOB_WORKER_DIR")
	if dir == "" {
		return
	}
	if _, err := (FileBlobs{Dir: dir}).Put(context.Background(), []byte("process-shared")); err != nil {
		t.Fatal(err)
	}
}

func TestBlobIndependentProcesses(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(exe, "-test.run=^TestBlobProcessHelper$")
			cmd.Env = append(os.Environ(), "RECEIPT_BLOB_WORKER_DIR="+dir)
			b, err := cmd.CombinedOutput()
			if err != nil {
				err = fmt.Errorf("%w: %s", err, b)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	s := FileBlobs{Dir: dir}
	ref, err := s.Put(context.Background(), []byte("process-shared"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
}

func TestFileBlobs(t *testing.T) {
	s := FileBlobs{Dir: t.TempDir()}
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.Put(ctx, []byte("same bytes")); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	ref, err := s.Put(ctx, []byte("same bytes"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Get(ctx, ref)
	if err != nil || string(b) != "same bytes" {
		t.Fatalf("%q %v", b, err)
	}
	wrong := ref
	wrong.Size++
	if _, err = s.Get(ctx, wrong); !errors.Is(err, ErrCorrupt) {
		t.Fatal(err)
	}
	p, _ := s.path(ref.Hash)
	if err = os.WriteFile(p, []byte("damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, ref); !errors.Is(err, ErrCorrupt) {
		t.Fatal(err)
	}
	if _, err = s.Put(ctx, []byte("same bytes")); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("corrupt object acknowledged: %v", err)
	}
	if _, err = s.path("../../escape"); err == nil {
		t.Fatal("unsafe hash accepted")
	}
	if _, err = s.Put(ctx, make([]byte, MaxBlobBytes+1)); err == nil {
		t.Fatal("oversized blob accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.Get(canceled, ref); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

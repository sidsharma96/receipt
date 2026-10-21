package cache

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/sidsharma96/receipt/internal/fingerprint"
)

func example() Result {
	m := fingerprint.Manifest{Version: 1, Command: []string{"pytest"}, Inputs: []fingerprint.Input{}}
	return Result{Version: 1, Key: m.Key(), Manifest: m, Stdout: []byte("pass\n")}
}

func TestDiskAtomicConcurrent(t *testing.T) {
	d := Disk{t.TempDir()}
	v := example()
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- d.Put(ctx, v) }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	r, e := d.Get(ctx, v.Key)
	if e != nil || string(r.Stdout) != "pass\n" {
		t.Fatalf("%+v %v", r, e)
	}
	v.Stdout = []byte("different")
	if e = d.Put(ctx, v); e != nil {
		t.Fatal(e)
	}
	r, _ = d.Get(ctx, v.Key)
	if string(r.Stdout) != "pass\n" {
		t.Fatal("entry overwritten")
	}
	files, _ := filepath.Glob(filepath.Join(d.Dir, "entries", ".pending-*"))
	if len(files) > 0 {
		t.Fatal("temporary files leaked")
	}
}

func TestCorruptionAndInvalidKey(t *testing.T) {
	d := Disk{t.TempDir()}
	v := example()
	d.Put(context.Background(), v)
	p, _ := d.path(v.Key)
	os.WriteFile(p, []byte(`{"payload":`), 0o600)
	if _, e := d.Get(context.Background(), v.Key); !errors.Is(e, ErrCorrupt) {
		t.Fatal(e)
	}
	if _, e := d.Get(context.Background(), "../../escape"); e == nil {
		t.Fatal("invalid path accepted")
	}
	v.ExitCode = 1
	if e := d.Put(context.Background(), v); e == nil {
		t.Fatal("failed result stored")
	}
}

func TestEnvelopeTamper(t *testing.T) {
	b, _ := Encode(example())
	for i := range b {
		if b[i] == 'p' {
			b[i] = 'x'
			break
		}
	}
	if _, e := Decode(b); e == nil {
		t.Fatal("tamper not detected")
	}
}

func TestHTTPConformance(t *testing.T) {
	d := Disk{t.TempDir()}
	token := "test-token-at-least-24-characters"
	s := httptest.NewServer(Handler(d, token))
	defer s.Close()
	r := Remote{URL: s.URL, Token: token}
	v := example()
	ctx := context.Background()
	if _, e := r.Get(ctx, v.Key); !errors.Is(e, ErrMiss) {
		t.Fatal(e)
	}
	if e := r.Put(ctx, v); e != nil {
		t.Fatal(e)
	}
	got, e := r.Get(ctx, v.Key)
	if e != nil || string(got.Stdout) != string(v.Stdout) {
		t.Fatalf("%+v %v", got, e)
	}
	r.Token = "wrong"
	if _, e := r.Get(ctx, v.Key); e == nil {
		t.Fatal("unauthenticated request accepted")
	}
	resp, e := http.Get(s.URL + "/v1/entries/" + v.Key)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal(resp.StatusCode)
	}
}

func TestRemoteRejectsPlaintextNonLoopback(t *testing.T) {
	r := Remote{URL: "http://example.com", Token: "secret"}
	if _, e := r.Get(context.Background(), example().Key); e == nil {
		t.Fatal("unsafe transport accepted")
	}
}

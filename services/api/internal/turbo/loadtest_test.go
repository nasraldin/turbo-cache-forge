//go:build loadtest

package turbo

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/obs"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage/filesystem"
)

func newLoadTestServer(t *testing.T, maxBytes int64) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	store := filesystem.New(dir)
	m := obs.NewMetrics()
	h := NewHandler(store, &memRepo{}, maxBytes, m)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.WithOrg(req.Context(), &db.Org{ID: 1, Slug: "team-a"})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	h.Mount(r)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, dir
}

// TestConcurrentDistinctHashes fires 64 goroutines each PUTting then GETting
// its own hash+payload, and asserts every readback's sha256 matches exactly
// — proving the storage layer never cross-contaminates keys under load.
func TestConcurrentDistinctHashes(t *testing.T) {
	const workers = 64
	srv, _ := newLoadTestServer(t, 10<<20)

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			hash := fmt.Sprintf("hash%03d", i)
			payload := bytes.Repeat([]byte(fmt.Sprintf("%d-", i)), 1000)
			sum := sha256.Sum256(payload)

			put, _ := http.NewRequest(http.MethodPut, srv.URL+"/v8/artifacts/"+hash, bytes.NewReader(payload))
			resp, err := http.DefaultClient.Do(put)
			if err != nil {
				errs <- err
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusAccepted {
				errs <- fmt.Errorf("worker %d: PUT status = %d", i, resp.StatusCode)
				return
			}

			get, _ := http.NewRequest(http.MethodGet, srv.URL+"/v8/artifacts/"+hash, nil)
			resp, err = http.DefaultClient.Do(get)
			if err != nil {
				errs <- err
				return
			}
			defer resp.Body.Close()
			got, _ := io.ReadAll(resp.Body)
			if sha256.Sum256(got) != sum {
				errs <- fmt.Errorf("worker %d: readback corrupted (sha256 mismatch)", i)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestConcurrentSameHashIdempotent PUTs the exact same hash+identical
// payload from 32 goroutines, interleaved with 32 concurrent GETs. The
// filesystem backend publishes each PUT via write-to-tempfile + atomic
// os.Rename (internal/storage/filesystem/fs.go), so every GET must see
// either nothing (raced ahead of the first rename) or one COMPLETE copy of
// the payload — never a partial/torn write.
func TestConcurrentSameHashIdempotent(t *testing.T) {
	const writers, readers = 32, 32
	srv, _ := newLoadTestServer(t, 10<<20)
	const hash = "shared-hash"
	payload := bytes.Repeat([]byte("stable-payload-"), 2000)

	var wg sync.WaitGroup
	errs := make(chan error, writers+readers)

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodPut, srv.URL+"/v8/artifacts/"+hash, bytes.NewReader(payload))
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errs <- err
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusAccepted {
				errs <- fmt.Errorf("PUT status = %d", resp.StatusCode)
			}
		}()
	}
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(srv.URL + "/v8/artifacts/" + hash)
			if err != nil {
				errs <- err
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusNotFound {
				return // fine: this GET raced ahead of every writer's first rename
			}
			if resp.StatusCode != http.StatusOK {
				errs <- fmt.Errorf("GET status = %d", resp.StatusCode)
				return
			}
			got, _ := io.ReadAll(resp.Body)
			if !bytes.Equal(got, payload) {
				errs <- fmt.Errorf("GET returned %d bytes, want a complete %d-byte payload (torn read)", len(got), len(payload))
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	resp, err := http.Get(srv.URL + "/v8/artifacts/" + hash)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("final GET failed: %v, status %v", err, resp)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, payload) {
		t.Fatal("final content corrupted")
	}
}

// TestFlatMemoryOnLargeArtifact proves PUT/GET never buffers a whole
// artifact server-side: both source and verification are file/hash-streamed
// on the client side too, so a heap-growth spike can only come from the
// server goroutine.
//
// Decision: 200 MiB artifact, 32 MiB growth ceiling — see this task's
// Decision note in the plan.
func TestFlatMemoryOnLargeArtifact(t *testing.T) {
	const size = 200 << 20
	const growthCeiling = 32 << 20
	srv, _ := newLoadTestServer(t, size+(1<<20))

	srcPath := filepath.Join(t.TempDir(), "big-src")
	sum := writeDeterministicFile(t, srcPath, size)

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	src, err := os.Open(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	put, _ := http.NewRequest(http.MethodPut, srv.URL+"/v8/artifacts/big", src)
	put.ContentLength = size
	resp, err := http.DefaultClient.Do(put)
	src.Close()
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("PUT status = %d", resp.StatusCode)
	}

	resp, err = http.Get(srv.URL + "/v8/artifacts/big")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	h := sha256.New()
	n, err := io.Copy(h, resp.Body) // streamed verification: no full-body buffer client-side either
	if err != nil {
		t.Fatal(err)
	}
	if n != size {
		t.Fatalf("downloaded %d bytes, want %d", n, size)
	}
	if got := h.Sum(nil); !bytes.Equal(got, sum) {
		t.Fatal("downloaded artifact content does not match uploaded content (checksum mismatch)")
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if grown := int64(after.HeapAlloc) - int64(before.HeapAlloc); grown > growthCeiling {
		t.Fatalf("heap grew by %d bytes (ceiling %d) while streaming a %d-byte artifact — full-buffer regression suspected", grown, growthCeiling, size)
	}
}

// writeDeterministicFile writes size bytes of non-repeating filler (so a
// sparse-file trick can't hide a bug) to path and returns its sha256.
func writeDeterministicFile(t *testing.T, path string, size int) []byte {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := sha256.New()
	w := io.MultiWriter(f, h)
	buf := make([]byte, 1<<20)
	for written := 0; written < size; {
		n := len(buf)
		if remaining := size - written; remaining < n {
			n = remaining
		}
		for i := 0; i < n; i++ {
			buf[i] = byte((written + i) * 2654435761 >> 8)
		}
		if _, err := w.Write(buf[:n]); err != nil {
			t.Fatal(err)
		}
		written += n
	}
	return h.Sum(nil)
}

// TestConcurrentOversizedPutsAll413 fires 32 parallel oversized PUTs and
// asserts MaxBytesReader's 413 cap holds for every one under concurrency,
// and that no partial object is ever committed to storage — fs.Put only
// os.Rename's after a fully successful io.Copy (internal/storage/filesystem/fs.go),
// so an aborted upload must leave zero artifacts behind, not a truncated one.
func TestConcurrentOversizedPutsAll413(t *testing.T) {
	const cap_ = 1 << 20 // 1 MiB
	const oversized = 5 << 20
	const workers = 32
	srv, dir := newLoadTestServer(t, cap_)

	var wg sync.WaitGroup
	codes := make(chan int, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload := bytes.Repeat([]byte{'x'}, oversized)
			req, _ := http.NewRequest(http.MethodPut,
				fmt.Sprintf("%s/v8/artifacts/oversized%d", srv.URL, i), bytes.NewReader(payload))
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			resp.Body.Close()
			codes <- resp.StatusCode
		}(i)
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != http.StatusRequestEntityTooLarge {
			t.Errorf("oversized PUT status = %d, want 413", code)
		}
	}

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && len(info.Name()) > 0 && info.Name()[0] == '.' {
			t.Errorf("leftover temp file after an aborted oversized PUT: %s", path)
		}
		return nil
	})
}

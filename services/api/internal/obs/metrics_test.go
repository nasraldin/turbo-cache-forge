package obs

import (
	"bytes"
	"io"
	"net/http/httptest"
	"testing"
)

// fakeReaderFromWriter proves whether io.Copy actually used the ReadFrom
// fast path (as *http.response would for sendfile) instead of falling back
// to a byte-by-byte copy loop through Write.
type fakeReaderFromWriter struct {
	httptest.ResponseRecorder
	readFromCalled bool
	written        []byte
}

func (f *fakeReaderFromWriter) ReadFrom(r io.Reader) (int64, error) {
	f.readFromCalled = true
	b, err := io.ReadAll(r)
	f.written = b
	return int64(len(b)), err
}

// readerOnly strips every interface but io.Read. *bytes.Reader implements
// io.WriterTo, and io.Copy prefers src.WriteTo over dst.ReadFrom (see
// io.Copy's copyBuffer: it checks src.(WriterTo) first) — so copying a bare
// *bytes.Reader would exercise WriteTo, not the statusWriter.ReadFrom path
// this test targets. Wrapping strips WriterTo and forces io.Copy to consult
// dst's ReaderFrom, which is what's actually under test here.
type readerOnly struct{ io.Reader }

func TestStatusWriterForwardsReaderFrom(t *testing.T) {
	fw := &fakeReaderFromWriter{}
	sw := &statusWriter{ResponseWriter: fw, status: 200}

	src := readerOnly{bytes.NewReader([]byte("payload-bytes"))}
	n, err := io.Copy(sw, src) // io.Copy type-asserts sw itself for io.ReaderFrom
	if err != nil {
		t.Fatal(err)
	}
	if !fw.readFromCalled {
		t.Fatal("io.Copy did not use the ReaderFrom fast path — statusWriter must forward it")
	}
	if n != int64(len("payload-bytes")) || !bytes.Equal(fw.written, []byte("payload-bytes")) {
		t.Fatalf("copied %q (%d bytes), want %q", fw.written, n, "payload-bytes")
	}
}

func TestStatusWriterReadFromFallsBackWithoutReaderFrom(t *testing.T) {
	rec := httptest.NewRecorder() // *httptest.ResponseRecorder itself has no ReadFrom
	sw := &statusWriter{ResponseWriter: rec, status: 200}
	n, err := io.Copy(sw, bytes.NewReader([]byte("x")))
	if err != nil || n != 1 || rec.Body.String() != "x" {
		t.Fatalf("fallback copy = %d, %v, body=%q", n, err, rec.Body.String())
	}
}

package artifactview

import (
	"archive/tar"
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// makeZstdTar builds a zstd-compressed tar from the given regular files and dirs.
func makeZstdTar(t *testing.T, files map[string]string, dirs []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(zw)
	for _, d := range dirs {
		if err := tw.WriteHeader(&tar.Header{Name: d, Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func find(m Manifest, path string) (Entry, bool) {
	for _, e := range m.Entries {
		if e.Path == path {
			return e, true
		}
	}
	return Entry{}, false
}

func TestDecodeListsEntriesAndPreviewsText(t *testing.T) {
	blob := makeZstdTar(t, map[string]string{"pkg/dist/index.js": "console.log(1)"}, []string{"pkg/dist/"})
	m := Decode(bytes.NewReader(blob))
	if m.Format != "zstd-tar" {
		t.Fatalf("format = %q, want zstd-tar", m.Format)
	}
	f, ok := find(m, "pkg/dist/index.js")
	if !ok || !f.Previewable || f.Preview != "console.log(1)" {
		t.Fatalf("file entry = %+v, want previewable text", f)
	}
	d, ok := find(m, "pkg/dist/")
	if !ok || !d.IsDir || d.Previewable {
		t.Fatalf("dir entry = %+v, want is_dir & not previewable", d)
	}
}

func TestDecodeBinaryNotPreviewable(t *testing.T) {
	m := Decode(bytes.NewReader(makeZstdTar(t, map[string]string{"a.bin": "ab\x00cd"}, nil)))
	f, _ := find(m, "a.bin")
	if f.Previewable || f.Preview != "" {
		t.Fatalf("binary entry = %+v, want not previewable", f)
	}
}

func TestDecodeOversizedNotPreviewable(t *testing.T) {
	big := strings.Repeat("x", maxPreviewBytes+1)
	m := Decode(bytes.NewReader(makeZstdTar(t, map[string]string{"big.txt": big}, nil)))
	f, _ := find(m, "big.txt")
	if f.Previewable {
		t.Fatalf("oversized entry previewable, want false")
	}
}

func TestDecodeOpaqueOnNonZstd(t *testing.T) {
	if m := Decode(bytes.NewReader([]byte("definitely not a zstd stream"))); m.Format != "opaque" {
		t.Fatalf("format = %q, want opaque", m.Format)
	}
}

func TestDecodeOpaqueOnZstdNonTar(t *testing.T) {
	var buf bytes.Buffer
	zw, _ := zstd.NewWriter(&buf)
	_, _ = zw.Write([]byte("plain text, not a tar"))
	_ = zw.Close()
	if m := Decode(bytes.NewReader(buf.Bytes())); m.Format != "opaque" {
		t.Fatalf("format = %q, want opaque", m.Format)
	}
}

func TestDecodeTruncatesAtEntryCap(t *testing.T) {
	files := make(map[string]string, maxEntries+5)
	for i := 0; i < maxEntries+5; i++ {
		files["f"+strconv.Itoa(i)] = "x"
	}
	m := Decode(bytes.NewReader(makeZstdTar(t, files, nil)))
	if !m.Truncated || len(m.Entries) != maxEntries {
		t.Fatalf("entries=%d truncated=%v, want %d & true", len(m.Entries), m.Truncated, maxEntries)
	}
}

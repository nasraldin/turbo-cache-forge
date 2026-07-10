// Package artifactview decodes a Turbo cache artifact (a zstd-compressed tar of
// a task's outputs) into a file manifest with inline text previews, for the
// admin "view contents" surface. It never trusts the blob: anything that isn't a
// decodable zstd-tar (client-encrypted, unknown format) degrades to "opaque".
package artifactview

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/klauspost/compress/zstd"
)

const (
	maxEntries      = 1000      // stop listing beyond this (Truncated=true)
	maxDecompressed = 32 << 20  // 32 MiB total decompressed budget (zip-bomb guard)
	maxPreviewBytes = 64 << 10  // per-file preview cap
	maxTotalPreview = 512 << 10 // total inlined preview budget across all files
)

type Entry struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	IsDir       bool   `json:"is_dir"`
	Preview     string `json:"preview,omitempty"`
	Previewable bool   `json:"previewable"`
}

type Manifest struct {
	Format       string  `json:"format"` // "zstd-tar" | "opaque"
	TotalEntries int     `json:"total_entries"`
	Truncated    bool    `json:"truncated"`
	Entries      []Entry `json:"entries"`
}

func opaque() Manifest { return Manifest{Format: "opaque", Entries: []Entry{}} }

// Decode returns a manifest of the artifact's tar entries. Text files up to
// maxPreviewBytes (within a total maxTotalPreview budget) get an inline UTF-8
// preview; binaries and oversized files are listed without one. Undecodable
// blobs return Format:"opaque" (no error — blobs are stored verbatim).
func Decode(r io.Reader) Manifest {
	zr, err := zstd.NewReader(r)
	if err != nil {
		return opaque()
	}
	defer zr.Close()

	tr := tar.NewReader(io.LimitReader(zr, maxDecompressed+1))
	hdr, err := tr.Next()
	if err != nil {
		return opaque() // not a (zstd) tar
	}

	m := Manifest{Format: "zstd-tar", Entries: []Entry{}}
	totalPreview := 0
	for err == nil {
		if len(m.Entries) >= maxEntries {
			m.Truncated = true
			break
		}
		name := hdr.Name
		e := Entry{
			Path:  name,
			Size:  hdr.Size,
			IsDir: hdr.Typeflag == tar.TypeDir || (len(name) > 0 && name[len(name)-1] == '/'),
		}
		if !e.IsDir && hdr.Typeflag == tar.TypeReg && hdr.Size <= maxPreviewBytes && totalPreview+int(hdr.Size) <= maxTotalPreview {
			buf := make([]byte, hdr.Size)
			n, rerr := io.ReadFull(tr, buf)
			if rerr == nil || errors.Is(rerr, io.ErrUnexpectedEOF) {
				if b := buf[:n]; isText(b) {
					e.Preview = string(b)
					e.Previewable = true
					totalPreview += n
				}
			}
		}
		m.Entries = append(m.Entries, e)
		m.TotalEntries++
		hdr, err = tr.Next()
	}
	if err != nil && !errors.Is(err, io.EOF) {
		m.Truncated = true // valid start, broke or hit the decompress cap mid-stream
	}
	return m
}

// isText reports whether b is UTF-8 with no NUL byte.
func isText(b []byte) bool {
	if bytes.IndexByte(b, 0) >= 0 {
		return false
	}
	return utf8.Valid(b)
}

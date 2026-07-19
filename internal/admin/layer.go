package admin

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/klauspost/compress/zstd"
)

type layerEntry struct {
	Path      string `json:"path"`
	Type      string `json:"type"`
	Size      int64  `json:"size,omitempty"`
	SizeHuman string `json:"size_human,omitempty"`
	Mode      string `json:"mode,omitempty"`
	LinkName  string `json:"link_name,omitempty"`
}

var (
	gzipMagic = []byte{0x1f, 0x8b}
	zstdMagic = []byte{0x28, 0xb5, 0x2f, 0xfd}
)

// layerEntriesCache holds parsed layer listings keyed by blob digest. A digest
// is content-addressed — the same digest can never point at different bytes —
// so cached entries never go stale and need no TTL or invalidation. Bounded to
// 128 layers so browsing doesn't grow memory unbounded on a long-running server.
var layerEntriesCache, _ = lru.New[string, []layerEntry](128)

// parseLayerEntries lists the files inside a layer tarball without buffering
// its contents in memory — layers can be hundreds of MB, so entries are read
// directly off the stream via tar.Reader.Next(), which discards each file's
// body for us instead of requiring it to be read.
func parseLayerEntries(r io.Reader) ([]layerEntry, error) {
	br := bufio.NewReader(r)

	magic, _ := br.Peek(4)

	var tr *tar.Reader
	switch {
	case len(magic) == 4 && bytes.Equal(magic, zstdMagic):
		zr, err := zstd.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("zstd: %w", err)
		}
		defer zr.Close()
		tr = tar.NewReader(zr)
	case len(magic) >= 2 && bytes.Equal(magic[:2], gzipMagic):
		gzr, err := gzip.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("gzip: %w", err)
		}
		defer func() { _ = gzr.Close() }()
		tr = tar.NewReader(gzr)
	default:
		tr = tar.NewReader(br)
	}

	entries := make([]layerEntry, 0, 128)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}

		entry := layerEntry{
			Path:     hdr.Name,
			Type:     tarTypeLabel(hdr.Typeflag),
			Mode:     fmt.Sprintf("%04o", hdr.Mode&0o7777),
			LinkName: hdr.Linkname,
		}
		if hdr.Typeflag == tar.TypeReg {
			entry.Size = hdr.Size
			entry.SizeHuman = humanSize(hdr.Size)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func tarTypeLabel(t byte) string {
	switch t {
	case tar.TypeDir:
		return "dir"
	case tar.TypeSymlink:
		return "symlink"
	case tar.TypeLink:
		return "hardlink"
	case tar.TypeReg:
		return "file"
	default:
		return "other"
	}
}

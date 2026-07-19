package admin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func tarWithOneFile(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	content := []byte("hello from the layer")
	if err := tw.WriteHeader(&tar.Header{Name: "etc/motd", Mode: 0644, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestParseLayerEntriesFormats(t *testing.T) {
	plain := tarWithOneFile(t)

	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	_, _ = gw.Write(plain)
	_ = gw.Close()

	var zsBuf bytes.Buffer
	zw, err := zstd.NewWriter(&zsBuf)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = zw.Write(plain)
	_ = zw.Close()

	for name, payload := range map[string][]byte{
		"plain tar": plain,
		"gzip":      gzBuf.Bytes(),
		"zstd":      zsBuf.Bytes(),
	} {
		entries, err := parseLayerEntries(bytes.NewReader(payload))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(entries) != 1 || entries[0].Path != "etc/motd" {
			t.Errorf("%s: entries = %+v", name, entries)
		}
	}
}

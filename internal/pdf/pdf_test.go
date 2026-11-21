package pdf

import (
	"bytes"
	"compress/zlib"
	"io"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// parse checks the file structure a reader relies on — header, every xref
// offset landing on its object, trailer, EOF — and returns the decompressed
// content streams, one per page.
func parse(t *testing.T, raw []byte) []string {
	t.Helper()
	if !bytes.HasPrefix(raw, []byte("%PDF-1.4\n")) || !bytes.HasSuffix(raw, []byte("%%EOF\n")) {
		t.Fatalf("not a PDF file: %q…%q", raw[:min(len(raw), 16)], raw[max(0, len(raw)-16):])
	}
	m := regexp.MustCompile(`startxref\n(\d+)\n%%EOF\n$`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("no startxref")
	}
	xref, _ := strconv.Atoi(string(m[1]))
	if !bytes.HasPrefix(raw[xref:], []byte("xref\n0 ")) {
		t.Fatalf("startxref %d does not point at the xref table", xref)
	}
	lines := strings.Split(string(raw[xref:]), "\n")
	count, _ := strconv.Atoi(strings.Fields(lines[1])[1])
	for i := 1; i < count; i++ {
		entry := lines[2+i]
		if len(entry) != 19 || !strings.HasSuffix(entry, " n ") { // 20 bytes with the \n
			t.Fatalf("xref entry %d malformed: %q", i, entry)
		}
		off, _ := strconv.Atoi(entry[:10])
		if want := strconv.Itoa(i) + " 0 obj\n"; !bytes.HasPrefix(raw[off:], []byte(want)) {
			t.Fatalf("xref entry %d points at %q, want %q", i, raw[off:off+12], want)
		}
	}
	if !bytes.Contains(raw, []byte("trailer\n<< /Size "+strconv.Itoa(count)+" /Root 1 0 R")) {
		t.Fatal("trailer does not match the xref size")
	}

	var pages []string
	streams := regexp.MustCompile(`(?s)<< /Length (\d+) /Filter /FlateDecode >>\nstream\n`)
	for _, loc := range streams.FindAllSubmatchIndex(raw, -1) {
		n, _ := strconv.Atoi(string(raw[loc[2]:loc[3]]))
		body := raw[loc[1] : loc[1]+n]
		if !bytes.HasPrefix(raw[loc[1]+n:], []byte("\nendstream")) {
			t.Fatal("stream /Length does not match its data")
		}
		zr, err := zlib.NewReader(bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		plain, err := io.ReadAll(zr)
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, string(plain))
	}
	return pages
}

func TestDocumentStructure(t *testing.T) {
	d := New(842, 595, "Report (draft)", time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC))
	p1 := d.AddPage()
	p1.Text(36, 50, HelveticaBold, 16, Color{}, "Vulnerability report")
	p1.Rect(36, 60, 100, 10, Color{R: 1})
	p1.Line(36, 80, 806, 80, 0.5, Color{0.5, 0.5, 0.5})
	d.AddPage().Text(36, 50, Courier, 8, Color{}, "CVE-2024-0001 (openssl) \\ back")

	raw, err := d.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	pages := parse(t, raw)
	if len(pages) != 2 || !bytes.Contains(raw, []byte("/Count 2")) {
		t.Fatalf("want 2 pages, got %d streams", len(pages))
	}
	// Top-left coordinates become PDF's bottom-left ones.
	if !strings.Contains(pages[0], "36 545 Td (Vulnerability report) Tj") {
		t.Errorf("page 1 text: %s", pages[0])
	}
	if !strings.Contains(pages[0], "1 0 0 rg 36 525 100 10 re f") {
		t.Errorf("page 1 rectangle: %s", pages[0])
	}
	if !strings.Contains(pages[1], `(CVE-2024-0001 \(openssl\) \\ back) Tj`) {
		t.Errorf("parentheses and backslashes must be escaped: %s", pages[1])
	}
	if !bytes.Contains(raw, []byte(`/Title (Report \(draft\))`)) || !bytes.Contains(raw, []byte("/CreationDate (D:20261121100000Z)")) {
		t.Error("info dictionary")
	}
}

func TestEmptyDocumentHasOnePage(t *testing.T) {
	raw, err := New(595, 842, "", time.Now()).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if pages := parse(t, raw); len(pages) != 1 {
		t.Fatalf("pages = %d", len(pages))
	}
}

func TestWinAnsi(t *testing.T) {
	got := winAnsi("é — “ok” … 漢 ✓")
	want := []byte{0xe9, ' ', 0x97, ' ', 0x93, 'o', 'k', 0x94, ' ', 0x85, ' ', '?', ' ', '?'}
	if !bytes.Equal(got, want) {
		t.Fatalf("winAnsi = % x, want % x", got, want)
	}
}

func TestMonoFit(t *testing.T) {
	// Courier 10pt: 6pt per character, so 60pt holds 10.
	if got := MonoFit("0123456789", 10, 60); got != "0123456789" {
		t.Errorf("fits exactly: %q", got)
	}
	if got := MonoFit("0123456789AB", 10, 60); got != "012345678…" {
		t.Errorf("cut: %q", got)
	}
	if got := MonoWidth(10, 10); got != 60 {
		t.Errorf("MonoWidth = %v", got)
	}
}

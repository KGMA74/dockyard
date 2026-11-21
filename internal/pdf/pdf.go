// Package pdf writes simple PDF documents — text, filled rectangles and lines
// on fixed-size pages — with the 14 standard fonts every PDF reader has, so
// nothing is embedded and nothing is pulled in: the binary stays pure Go.
// It is meant for generated reports, not for typesetting: there is no text
// shaping, and characters outside Windows-1252 print as '?'.
package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"strings"
	"time"
)

// Font is one of the standard PDF fonts this package uses.
type Font int

const (
	Helvetica Font = iota
	HelveticaBold
	Courier
	CourierBold
)

var fontNames = []string{"Helvetica", "Helvetica-Bold", "Courier", "Courier-Bold"}

// MonoWidth is the width in points of n characters of Courier at size: every
// Courier glyph is 600/1000 em, which makes fitting text into a column exact.
func MonoWidth(size float64, n int) float64 { return 0.6 * size * float64(n) }

// MonoFit returns s cut to fit width points of Courier at size, ending with
// "…" when it had to be cut.
func MonoFit(s string, size, width float64) string {
	fits := int(width / (0.6 * size))
	r := []rune(s)
	if len(r) <= fits {
		return s
	}
	if fits <= 1 {
		return string(r[:max(fits, 0)])
	}
	return string(r[:fits-1]) + "…"
}

// Color is an RGB color, components in [0, 1].
type Color struct{ R, G, B float64 }

// Doc is a document of pages that all share one size, in points
// (1/72 inch). A4 landscape is 842 x 595.
type Doc struct {
	width, height float64
	title         string
	created       time.Time
	pages         []*Page
}

// New starts a document whose pages are width x height points.
func New(width, height float64, title string, created time.Time) *Doc {
	return &Doc{width: width, height: height, title: title, created: created}
}

// Page is one page. Coordinates are in points from the top-left corner,
// y growing downwards; text is placed by its baseline.
type Page struct {
	doc *Doc
	ops bytes.Buffer
}

// AddPage appends an empty page and returns it.
func (d *Doc) AddPage() *Page {
	p := &Page{doc: d}
	d.pages = append(d.pages, p)
	return p
}

// Text draws s with its baseline starting at (x, y).
func (p *Page) Text(x, y float64, font Font, size float64, c Color, s string) {
	fmt.Fprintf(&p.ops, "BT /F%d %s Tf %s rg %s %s Td (%s) Tj ET\n",
		int(font)+1, num(size), rgb(c), num(x), num(p.doc.height-y), escape(winAnsi(s)))
}

// Rect fills the rectangle whose top-left corner is (x, y).
func (p *Page) Rect(x, y, w, h float64, fill Color) {
	fmt.Fprintf(&p.ops, "%s rg %s %s %s %s re f\n", rgb(fill), num(x), num(p.doc.height-y-h), num(w), num(h))
}

// Line strokes a straight line.
func (p *Page) Line(x1, y1, x2, y2, width float64, c Color) {
	fmt.Fprintf(&p.ops, "%s w %s RG %s %s m %s %s l S\n",
		num(width), rgb(c), num(x1), num(p.doc.height-y1), num(x2), num(p.doc.height-y2))
}

// Bytes serializes the document.
func (d *Doc) Bytes() ([]byte, error) {
	if len(d.pages) == 0 {
		d.AddPage()
	}
	var out bytes.Buffer
	var offsets []int // offsets[i] is where object i+1 starts
	obj := func(body string) int {
		offsets = append(offsets, out.Len())
		n := len(offsets)
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", n, body)
		return n
	}

	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")

	// Fixed numbering: 1 catalog, 2 page tree, 3 info, 4.. fonts, then a
	// page and its content stream for each page.
	firstFont := 4
	firstPage := firstFont + len(fontNames)
	kids := make([]string, len(d.pages))
	for i := range d.pages {
		kids[i] = fmt.Sprintf("%d 0 R", firstPage+2*i)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d /MediaBox [0 0 %s %s] >>",
		strings.Join(kids, " "), len(d.pages), num(d.width), num(d.height)))
	obj(fmt.Sprintf("<< /Title (%s) /Producer (Dockyard) /CreationDate (D:%s) >>",
		escape(winAnsi(d.title)), d.created.UTC().Format("20060102150405Z")))
	fonts := make([]string, len(fontNames))
	for i, name := range fontNames {
		obj(fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /%s /Encoding /WinAnsiEncoding >>", name))
		fonts[i] = fmt.Sprintf("/F%d %d 0 R", i+1, firstFont+i)
	}
	resources := "<< /Font << " + strings.Join(fonts, " ") + " >> >>"

	for i, p := range d.pages {
		var z bytes.Buffer
		zw := zlib.NewWriter(&z)
		if _, err := zw.Write(p.ops.Bytes()); err != nil {
			return nil, err
		}
		if err := zw.Close(); err != nil {
			return nil, err
		}
		contents := firstPage + 2*i + 1
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /Resources %s /Contents %d 0 R >>", resources, contents))
		obj(fmt.Sprintf("<< /Length %d /Filter /FlateDecode >>\nstream\n%s\nendstream", z.Len(), z.Bytes()))
	}

	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, off := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R /Info 3 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return out.Bytes(), nil
}

// num formats a coordinate or size compactly.
func num(f float64) string {
	s := fmt.Sprintf("%.2f", f)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}

func rgb(c Color) string { return num(c.R) + " " + num(c.G) + " " + num(c.B) }

// escape protects the characters with a meaning inside a PDF string.
func escape(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		switch c {
		case '\\', '(', ')':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		case '\n', '\r', '\t':
			sb.WriteByte(' ')
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// winAnsiExtra maps the characters of Windows-1252's 0x80–0x9F range.
var winAnsiExtra = map[rune]byte{
	'€': 0x80, '‚': 0x82, 'ƒ': 0x83, '„': 0x84, '…': 0x85, '†': 0x86, '‡': 0x87,
	'ˆ': 0x88, '‰': 0x89, 'Š': 0x8a, '‹': 0x8b, 'Œ': 0x8c, 'Ž': 0x8e,
	'‘': 0x91, '’': 0x92, '“': 0x93, '”': 0x94, '•': 0x95, '–': 0x96, '—': 0x97,
	'˜': 0x98, '™': 0x99, 'š': 0x9a, '›': 0x9b, 'œ': 0x9c, 'ž': 0x9e, 'Ÿ': 0x9f,
}

// winAnsi encodes s in Windows-1252, the encoding the standard fonts are
// declared with; anything else becomes '?'.
func winAnsi(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 0x20 && r < 0x7f, r >= 0xa0 && r <= 0xff:
			out = append(out, byte(r))
		case r == '\t' || r == '\n' || r == '\r':
			out = append(out, byte(r))
		default:
			if b, ok := winAnsiExtra[r]; ok {
				out = append(out, b)
			} else {
				out = append(out, '?')
			}
		}
	}
	return out
}

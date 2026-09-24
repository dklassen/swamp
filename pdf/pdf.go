// Package pdf renders markdown source into a formatted PDF document. It
// supports the subset of markdown swamp's drafted cover letters/resumes
// actually use (see assets/*/resume.md, cover_letter.md): headings,
// paragraphs, bold/italic emphasis, bullet/ordered lists, thematic breaks,
// and links -- not a general-purpose markdown renderer. Kept as its own
// package rather than folded into documents/assets, which is deliberately
// content-unaware (see documents.go's doc comment and decisions.log,
// issue #45) -- this package is exactly the content-processing concern
// that one was built to stay separate from.
package pdf

import (
	"bytes"
	_ "embed"
	"fmt"

	"github.com/go-pdf/fpdf"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/text"
)

const (
	baseFontSize = 11.0
	// fontFamily is Adobe's Source Sans 3, embedded from fonts/ (SIL Open
	// Font License, see fonts/OFL.md) rather than a core PDF font
	// (Helvetica) translated through a WinAnsi/cp1252 codepage -- real
	// resume content uses characters cp1252 has no glyph for at all
	// (e.g. "->" arrows in a pipeline description), which the
	// translator was silently mangling into "." instead of erroring.
	// Source Sans 3 covers those characters, written as native UTF-8
	// through AddUTF8FontFromBytes. It replaced Go's own bundled gofont
	// family, which had the same coverage and needed no vendored files
	// but read as recognizably "the Go font" on a resume or cover letter
	// (see decisions.log).
	fontFamily = "SourceSans3"
)

var (
	//go:embed fonts/SourceSans3-Regular.ttf
	fontRegular []byte
	//go:embed fonts/SourceSans3-Bold.ttf
	fontBold []byte
	//go:embed fonts/SourceSans3-It.ttf
	fontItalic []byte
	//go:embed fonts/SourceSans3-BoldIt.ttf
	fontBoldItalic []byte
)

// Render converts markdown source into PDF-encoded bytes.
func Render(source []byte) ([]byte, error) {
	root := goldmark.DefaultParser().Parse(text.NewReader(source))

	doc := fpdf.New("P", "mm", "Letter", "")
	doc.SetMargins(20, 20, 20)
	doc.SetAutoPageBreak(true, 20)
	doc.AddUTF8FontFromBytes(fontFamily, "", fontRegular)
	doc.AddUTF8FontFromBytes(fontFamily, "B", fontBold)
	doc.AddUTF8FontFromBytes(fontFamily, "I", fontItalic)
	doc.AddUTF8FontFromBytes(fontFamily, "BI", fontBoldItalic)
	doc.AddPage()
	doc.SetFont(fontFamily, "", baseFontSize)
	// Keeps the content stream as literal, greppable text instead of
	// deflate-compressed -- lets tests assert on real rendered output
	// without a second, PDF-parsing dependency. The size cost is
	// negligible for a one-to-two-page text document.
	doc.SetCompression(false)

	r := &renderer{doc: doc, source: source}
	r.renderChildren(root)

	if r.err != nil {
		return nil, r.err
	}
	if err := doc.Error(); err != nil {
		return nil, fmt.Errorf("pdf: render: %w", err)
	}

	var buf bytes.Buffer
	if err := doc.Output(&buf); err != nil {
		return nil, fmt.Errorf("pdf: encode output: %w", err)
	}
	return buf.Bytes(), nil
}

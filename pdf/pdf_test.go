package pdf

import (
	"bytes"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// containsText reports whether pdfBytes' content stream contains s. The
// embedded UTF-8 font this package uses (see pdf.go) writes text as two
// bytes per character, big-endian Unicode code point -- not literal
// ASCII/UTF-8 -- so a plain bytes.Contains against s's own bytes would
// never match. Render disables PDF compression, so this is real, literal
// content-stream text, not a guess about internal structure.
func containsText(pdfBytes []byte, s string) bool {
	return bytes.Contains(pdfBytes, utf16beBytes(s))
}

func utf16beBytes(s string) []byte {
	out := make([]byte, 0, len(s)*2)
	for _, r := range s {
		out = append(out, byte(r>>8), byte(r))
	}
	return out
}

func TestRender_ProducesValidPDF(t *testing.T) {
	t.Parallel()

	got, err := Render([]byte("Hello world"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.HasPrefix(got, []byte("%PDF-1.")) {
		t.Errorf("output does not start with a PDF header, got first bytes: %q", got[:min(20, len(got))])
	}
}

func TestRender_IncludesParagraphText(t *testing.T) {
	t.Parallel()

	got, err := Render([]byte("Hello world, this is a cover letter."))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !containsText(got, "Hello") {
		t.Errorf("output does not contain paragraph text %q", "Hello")
	}
}

// fontResourceNames returns the distinct font resource identifiers (e.g.
// "F0a76...") selected by "Tf" operators in pdfBytes' content stream --
// used to check that a style change (bold, heading size, ...) actually
// selected a different registered font rather than reusing the same one.
func fontResourceNames(pdfBytes []byte) map[string]bool {
	re := regexp.MustCompile(`/(F[0-9a-zA-Z]+) [\d.]+ Tf`)
	seen := map[string]bool{}
	for _, m := range re.FindAllSubmatch(pdfBytes, -1) {
		seen[string(m[1])] = true
	}
	return seen
}

func TestRender_IncludesBoldAndPlainTextInSameParagraph(t *testing.T) {
	t.Parallel()

	got, err := Render([]byte("Regular text and **bold text** together."))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !containsText(got, "Regular") {
		t.Errorf("output does not contain plain text %q", "Regular")
	}
	if !containsText(got, "bold text") {
		t.Errorf("output does not contain emphasized text %q", "bold text")
	}
	if names := fontResourceNames(got); len(names) < 2 {
		t.Errorf("output selects only %d distinct font resource(s), want at least 2 (plain vs. bold) -- bold emphasis wasn't applied", len(names))
	}
}

func TestRender_IncludesBulletListItems(t *testing.T) {
	t.Parallel()

	got, err := Render([]byte("- First bullet point\n- Second bullet point\n"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !containsText(got, "First bullet") {
		t.Errorf("output does not contain first list item text")
	}
	if !containsText(got, "Second bullet") {
		t.Errorf("output does not contain second list item text")
	}
	if !containsText(got, bulletGlyph) {
		t.Errorf("output does not contain the bullet glyph %q", bulletGlyph)
	}
}

func TestRender_IncludesOrderedListNumbering(t *testing.T) {
	t.Parallel()

	got, err := Render([]byte("1. Alpha step\n2. Beta step\n"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !containsText(got, "1.") {
		t.Errorf("output does not contain ordinal %q", "1.")
	}
	if !containsText(got, "2.") {
		t.Errorf("output does not contain ordinal %q", "2.")
	}
}

// TestRender_OrderedListStartingAtZeroKeepsZero is a regression test for
// a bug caught in code review: renderList treated any zero list.Start as
// "unset, default to 1," but goldmark always sets Start from the actual
// literal number that opened the list (see parser/list.go) -- an
// ordered list genuinely written as "0. Zeroth" parses with Start = 0
// legitimately, and renderList's zero-check silently renumbered it from
// 1 instead.
func TestRender_OrderedListStartingAtZeroKeepsZero(t *testing.T) {
	t.Parallel()

	got, err := Render([]byte("0. Zeroth step\n1. First step\n"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !containsText(got, "0.") {
		t.Errorf("output does not contain ordinal %q", "0.")
	}
}

func TestRender_DrawsThematicBreakAsALine(t *testing.T) {
	t.Parallel()

	withRule, err := Render([]byte("Above\n\n---\n\nBelow"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	withoutRule, err := Render([]byte("Above\n\nBelow"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.Contains(withRule, []byte(" l S\n")) {
		t.Errorf("output with a thematic break does not contain a stroked line operator (%q)", " l S")
	}
	if len(withRule) <= len(withoutRule) {
		t.Errorf("thematic break did not add any drawing content to the output")
	}
}

func TestRender_LinkIncludesDisplayTextAndURL(t *testing.T) {
	t.Parallel()

	got, err := Render([]byte("See [my site](https://example.com/dana) for more."))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !containsText(got, "my site") {
		t.Errorf("output does not contain link display text %q", "my site")
	}
	// The link target (a URI action, not a text-showing operator) is a
	// plain literal string regardless of the text font, so it's checked
	// as raw ASCII rather than through containsText.
	if !bytes.Contains(got, []byte("https://example.com/dana")) {
		t.Errorf("output does not contain link URL %q", "https://example.com/dana")
	}
}

// TestRender_AutoLinkIncludesURLText is a regression test for a bug
// caught in code review: renderInline's default case assumes an unknown
// inline node exposes its text via child *ast.Text nodes reachable
// through FirstChild(), but *ast.AutoLink stores its value without
// attaching it as a child node, so a CommonMark autolink like
// "<https://example.com>" rendered nothing at all.
func TestRender_AutoLinkIncludesURLText(t *testing.T) {
	t.Parallel()

	got, err := Render([]byte("Visit <https://example.com> for more."))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !containsText(got, "Visit") {
		t.Errorf("output does not contain surrounding text %q", "Visit")
	}
	if !containsText(got, "example.com") {
		t.Errorf("output does not contain autolink text %q", "example.com")
	}
	if !bytes.Contains(got, []byte("https://example.com")) {
		t.Errorf("output does not contain autolink URL %q", "https://example.com")
	}
}

// TestRender_EmailAutoLinkTargetsMailto is a regression test caught
// during manual verification of the AutoLink fix above: goldmark's
// default parser never sets AutoLink.Protocol (see
// parser/auto_link.go), so AutoLink.URL() returns the bare address
// literal for an email autolink like "<dana@doe.com>" rather than a
// "mailto:" URI -- despite AutoLink.Protocol's own doc comment
// describing that prefixing as URL()'s job. A PDF viewer treats an
// unprefixed address as plain text, not a clickable mailto link.
func TestRender_EmailAutoLinkTargetsMailto(t *testing.T) {
	t.Parallel()

	got, err := Render([]byte("Reach me at <dana@example.com>."))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.Contains(got, []byte("mailto:dana@example.com")) {
		t.Errorf("output does not target a mailto: URI for the email autolink")
	}
}

func TestRender_EmptySourceProducesValidPDFWithoutError(t *testing.T) {
	t.Parallel()

	got, err := Render([]byte(""))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.HasPrefix(got, []byte("%PDF-1.")) {
		t.Errorf("output does not start with a PDF header")
	}
}

// TestRender_PreservesUnicodePunctuationAndSymbols pins the reason this
// package writes text through an embedded UTF-8 font (see pdf.go's
// fontFamily doc comment) rather than a core PDF font translated through
// a WinAnsi/cp1252 codepage: manual verification against a real resume
// caught cp1252 silently substituting "." for an arrow character (U+2192,
// used in a pipeline description) instead of erroring -- a correctness
// bug, not a formatting nicety. Em/en dashes (also real resume content,
// e.g. date ranges) are included too since they're outside plain ASCII.
func TestRender_PreservesUnicodePunctuationAndSymbols(t *testing.T) {
	t.Parallel()

	got, err := Render([]byte("Team Lead — Platform (2018–2020): ingest → transform → store"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"—", "–", "→"} {
		if !containsText(got, want) {
			t.Errorf("output does not contain %q", want)
		}
	}
}

// TestRender_HeadingAfterListStartsAtLeftMargin is a regression test for
// a bug caught in manual verification: a heading immediately following a
// bullet list rendered indented under the list, because renderList's
// deferred margin restore doesn't retroactively move the cursor X
// position the list's own line breaks left indented. Every block must
// start flush against the current left margin -- see renderBlock.
func TestRender_HeadingAfterListStartsAtLeftMargin(t *testing.T) {
	t.Parallel()

	withoutList, err := Render([]byte("# Heading One"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	afterList, err := Render([]byte("- one\n- two\n\n# Heading One"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	x1, ok1 := textStartX(t, withoutList, "Heading One")
	x2, ok2 := textStartX(t, afterList, "Heading One")
	if !ok1 || !ok2 {
		t.Fatalf("could not locate heading text's starting Td position in one or both outputs")
	}
	if x1 != x2 {
		t.Errorf("heading after a list starts at X=%v, want X=%v (same as a heading with no preceding list) -- it inherited the list's indent", x2, x1)
	}
}

// textStartX finds the X coordinate of the "Td" operator immediately
// preceding txt's first occurrence in pdfBytes' content stream -- e.g.
// "12.34 56.78 Td (Heading One)Tj" -> 12.34. Relies on Render disabling
// PDF compression so the content stream is literal, searchable text.
func textStartX(t *testing.T, pdfBytes []byte, txt string) (float64, bool) {
	t.Helper()
	idx := bytes.Index(pdfBytes, append([]byte("("), utf16beBytes(txt)...))
	if idx < 0 {
		return 0, false
	}
	line := pdfBytes[:idx]
	lineStart := bytes.LastIndexByte(line, '\n') + 1
	fields := strings.Fields(string(pdfBytes[lineStart:idx]))
	// Expect "... <x> <y> Td " immediately before the opening paren --
	// x is third-from-last (Td, then y, then x).
	if len(fields) < 3 {
		return 0, false
	}
	x, err := strconv.ParseFloat(fields[len(fields)-3], 64)
	if err != nil {
		return 0, false
	}
	return x, true
}

// textStartY is textStartX's counterpart for the Y coordinate of the
// same "Td" operator (second-from-last field, immediately before X).
func textStartY(t *testing.T, pdfBytes []byte, txt string) (float64, bool) {
	t.Helper()
	idx := bytes.Index(pdfBytes, append([]byte("("), utf16beBytes(txt)...))
	if idx < 0 {
		return 0, false
	}
	line := pdfBytes[:idx]
	lineStart := bytes.LastIndexByte(line, '\n') + 1
	fields := strings.Fields(string(pdfBytes[lineStart:idx]))
	if len(fields) < 2 {
		return 0, false
	}
	y, err := strconv.ParseFloat(fields[len(fields)-2], 64)
	if err != nil {
		return 0, false
	}
	return y, true
}

// TestRender_ListItemWithMultipleParagraphsOnSeparateLines is a
// regression test for a bug caught in code review: renderList's per-item
// loop called renderInlineChildren directly on each of a list item's
// child blocks, bypassing renderBlock's line-break handling, so a loose
// list item with more than one paragraph ran its paragraphs together
// onto a single line instead of separate ones.
func TestRender_ListItemWithMultipleParagraphsOnSeparateLines(t *testing.T) {
	t.Parallel()

	got, err := Render([]byte("- First paragraph.\n\n  Second paragraph.\n"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	y1, ok1 := textStartY(t, got, "First paragraph.")
	y2, ok2 := textStartY(t, got, "Second paragraph.")
	if !ok1 || !ok2 {
		t.Fatalf("could not locate one or both paragraphs' starting Td position")
	}
	if y1 == y2 {
		t.Errorf("both paragraphs render at Y=%v -- want separate lines", y1)
	}
}

// TestRender_UnsupportedBlockTypeReturnsError is a regression test for a
// bug caught in code review: renderBlock's switch had no default case,
// so a block type outside this package's documented supported subset
// (see pdf.go's doc comment) -- a blockquote, code block, fenced code
// block, or HTML block -- was silently dropped from the output with no
// error, rather than surfacing that its content didn't make it into the
// PDF.
func TestRender_UnsupportedBlockTypeReturnsError(t *testing.T) {
	t.Parallel()

	_, err := Render([]byte("> A great quote from a reference.\n"))
	if err == nil {
		t.Fatalf("Render did not return an error for an unsupported blockquote")
	}
}

func TestRender_RendersRealResumeContentWithoutError(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("testdata/resume.md")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}

	got, err := Render(source)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.HasPrefix(got, []byte("%PDF-1.")) {
		t.Errorf("output does not start with a PDF header")
	}
	if !containsText(got, "Summary") {
		t.Errorf("output does not contain expected heading text %q", "Summary")
	}
}

func TestRender_IncludesHeadingText(t *testing.T) {
	t.Parallel()

	got, err := Render([]byte("# Dana Klassen\n\nSenior Engineer"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !containsText(got, "Dana") {
		t.Errorf("output does not contain heading text %q", "Dana")
	}
	if !containsText(got, "Senior") {
		t.Errorf("output does not contain paragraph text %q", "Senior")
	}
}

// fontSizeBeforeText returns the point size operand of the "Tf" operator
// that was in effect immediately before txt's first occurrence in
// pdfBytes' content stream -- used to check that a heading's text was
// actually drawn at its level-based size (see headingFontSize), not just
// that headingFontSize was computed and then discarded.
func fontSizeBeforeText(t *testing.T, pdfBytes []byte, txt string) (float64, bool) {
	t.Helper()
	idx := bytes.Index(pdfBytes, append([]byte("("), utf16beBytes(txt)...))
	if idx < 0 {
		return 0, false
	}
	re := regexp.MustCompile(`/F[0-9a-zA-Z]+ ([\d.]+) Tf`)
	matches := re.FindAllSubmatchIndex(pdfBytes[:idx], -1)
	if len(matches) == 0 {
		return 0, false
	}
	last := matches[len(matches)-1]
	size, err := strconv.ParseFloat(string(pdfBytes[last[2]:last[3]]), 64)
	if err != nil {
		return 0, false
	}
	return size, true
}

// TestRender_HeadingTextUsesHeadingFontSize is a regression test for a
// bug caught in code review: renderInline's *ast.Text case hardcoded
// SetFont(..., baseFontSize), so a heading's larger, level-based size
// (set by renderBlock just before rendering the heading's children) was
// immediately overwritten and the glyphs were drawn at body text size.
func TestRender_HeadingTextUsesHeadingFontSize(t *testing.T) {
	t.Parallel()

	got, err := Render([]byte("# Big Title\n\nBody text."))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	size, ok := fontSizeBeforeText(t, got, "Big Title")
	if !ok {
		t.Fatalf("could not locate heading text's preceding Tf font size")
	}
	if want := headingFontSize(1); size != want {
		t.Errorf("heading text drawn at font size %v, want %v (headingFontSize(1))", size, want)
	}
}

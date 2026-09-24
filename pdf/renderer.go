package pdf

import (
	"fmt"

	"github.com/go-pdf/fpdf"
	"github.com/yuin/goldmark/ast"
)

// bulletGlyph marks an unordered list item -- U+2022, written as native
// UTF-8 like any other text (see pdf.go's fontFamily doc comment).
const bulletGlyph = "•"

// listIndent is how far a list item's text is offset from the page's
// left margin (mm).
const listIndent = 8.0

// renderer walks a goldmark AST and draws each node onto doc. source is
// the original markdown bytes -- goldmark's AST nodes store only byte
// offsets into it, not their own copy of the text. err is set the first
// time renderBlock reaches a block type outside this package's
// documented supported subset (see pdf.go's doc comment) -- rather than
// silently drop that block's content, which would produce an incomplete
// PDF with no indication anything was lost.
type renderer struct {
	doc    *fpdf.Fpdf
	source []byte
	err    error
}

// renderChildren renders every direct child block of n in order.
func (r *renderer) renderChildren(n ast.Node) {
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		r.renderBlock(child)
	}
}

// renderBlock renders a single block-level node and any spacing that
// follows it. Every block starts by snapping X back to the current left
// margin -- without this, a block immediately following a list starts
// from wherever the list's last line left the cursor, which is still the
// list's indented X even after renderList restores the margin itself
// (restoring the margin doesn't retroactively move the cursor).
func (r *renderer) renderBlock(n ast.Node) {
	left, top, _, _ := r.doc.GetMargins()
	r.doc.SetX(left)

	switch v := n.(type) {
	case *ast.Heading:
		size := headingFontSize(v.Level)
		// A heading belongs to what follows it, so it gets extra space
		// above and less below than a paragraph does -- except at the
		// top of a page, where space above would only push it down.
		if r.doc.GetY() > top {
			r.doc.Ln(headingSpaceBefore(size))
			r.doc.SetX(left)
		}
		r.doc.SetFont(fontFamily, "B", size)
		r.renderInlineChildren(n, "B", size)
		r.doc.Ln(lineHeight(size) * 1.15)
	case *ast.Paragraph, *ast.TextBlock:
		r.doc.SetFont(fontFamily, "", baseFontSize)
		r.renderInlineChildren(n, "", baseFontSize)
		r.doc.Ln(lineHeight(baseFontSize) * 1.5)
	case *ast.List:
		r.renderList(v)
	case *ast.ThematicBreak:
		left, _, right, _ := r.doc.GetMargins()
		pageW, pageH := r.doc.GetPageSize()
		_, breakMargin := r.doc.GetAutoPageBreak()
		h := lineHeight(baseFontSize) * 1.5
		// A rule introduces the block after it, so the two have to
		// break together. Reserving only the rule's own height let it
		// draw into space the following heading then couldn't fit in,
		// stranding the rule as the last mark on the page with a band
		// of white space beneath it and its heading orphaned onto the
		// next page.
		h += firstLineHeight(n.NextSibling())
		// Line() draws directly, unlike Write/Cell -- it doesn't
		// participate in fpdf's SetAutoPageBreak logic, so a break
		// landing near the bottom margin needs its own explicit check
		// or it (and the SetY below it) can land past the page's
		// printable area instead of starting a fresh page.
		if r.doc.GetY()+h > pageH-breakMargin {
			r.doc.AddPage()
		}
		y := r.doc.GetY() + lineHeight(baseFontSize)*0.5
		r.doc.Line(left, y, pageW-right, y)
		r.doc.SetY(y + lineHeight(baseFontSize))
	default:
		if r.err == nil {
			r.err = fmt.Errorf("pdf: unsupported block type %T", n)
		}
	}
}

// renderList renders a bullet or ordered list. Each item's text is
// indented from the page's left margin by listIndent -- changing the
// margin itself (not just the starting X) so a wrapped continuation line
// also lands under the bullet/ordinal rather than back at the page edge.
// Nested lists aren't supported -- swamp's drafted documents (see
// decisions.log, issue #45) never use them, and this package deliberately
// covers only the markdown subset those actually contain.
func (r *renderer) renderList(list *ast.List) {
	left, _, _, _ := r.doc.GetMargins()
	r.doc.SetLeftMargin(left + listIndent)
	defer r.doc.SetLeftMargin(left)

	// list.Start is always the actual literal number that opened an
	// ordered list (goldmark parses it via strconv.Atoi in
	// parser/list.go) -- including a genuine "0. item" -- so it needs
	// no unset-vs-zero fallback here. It's simply unused for a bullet
	// list (IsOrdered() false below), where it stays Go's zero value.
	ordinal := list.Start
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		prefix := bulletGlyph + "  "
		if list.IsOrdered() {
			prefix = fmt.Sprintf("%d.  ", ordinal)
			ordinal++
		}
		r.doc.SetFont(fontFamily, "", baseFontSize)
		r.doc.SetX(left + listIndent)
		r.doc.Write(lineHeight(baseFontSize), prefix)
		first := true
		for block := item.FirstChild(); block != nil; block = block.NextSibling() {
			// Only the item's first block continues on the prefix's
			// line -- a second block (a loose item's second paragraph)
			// needs its own line break and a fresh X under the item's
			// indent, or it runs together with whatever came before it.
			if !first {
				r.doc.Ln(lineHeight(baseFontSize))
				r.doc.SetX(left + listIndent)
			}
			r.renderInlineChildren(block, "", baseFontSize)
			first = false
		}
		r.doc.Ln(lineHeight(baseFontSize) * 1.2)
	}
	r.doc.Ln(lineHeight(baseFontSize) * 0.3)
}

// headingFontSize returns the point size for a heading of the given level
// (1-6), following resume conventions rather than a web page's: a
// prominent H1 (the name), H2 (section headings) a step above body text,
// and H3 onward (job/role headings) at body size, set apart by bold
// weight alone. The earlier 16/14/12pt steps made every job title
// shout.
func headingFontSize(level int) float64 {
	switch level {
	case 1:
		return 20
	case 2:
		return 13
	default:
		return baseFontSize
	}
}

// headingSpaceBefore is the extra space (mm) above a heading of fontSize
// (pt), on top of whatever the preceding block already left below itself.
func headingSpaceBefore(fontSize float64) float64 {
	return lineHeight(fontSize) * 0.5
}

// lineHeight returns a comfortable line height (mm) for fontSize (pt),
// following the common fpdf convention of roughly half the point size.
func lineHeight(fontSize float64) float64 {
	return fontSize * 0.5
}

// firstLineHeight is the height of the first line n will render as,
// including a heading's space above it, used to keep a thematic break on
// the same page as the block it introduces. A nil node is a rule with
// nothing after it, which needs no room reserved beyond its own.
func firstLineHeight(n ast.Node) float64 {
	switch v := n.(type) {
	case nil:
		return 0
	case *ast.Heading:
		size := headingFontSize(v.Level)
		return headingSpaceBefore(size) + lineHeight(size)
	default:
		return lineHeight(baseFontSize)
	}
}

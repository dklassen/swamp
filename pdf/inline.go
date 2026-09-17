package pdf

import (
	"github.com/yuin/goldmark/ast"
)

// linkText concatenates n's descendant Text nodes' raw content -- used
// for a Link's display text, which fpdf.WriteLinkString needs as a single
// plain string rather than a tree of styled runs (see renderInline).
func linkText(n ast.Node, source []byte) string {
	var out []byte
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if text, ok := child.(*ast.Text); ok {
			out = append(out, text.Segment.Value(source)...)
			continue
		}
		out = append(out, linkText(child, source)...)
	}
	return string(out)
}

// renderInlineChildren renders n's inline children (text, emphasis, links,
// ...) onto the current line, flowing as one wrapped paragraph. styleStr
// is the font style ("", "B", "I", "BI") in effect for this run --
// nested Emphasis nodes combine into it recursively. size is the point
// size in effect for this run, carried through from the enclosing block
// (see renderBlock) so a heading's larger size survives down to its
// actual text/link glyphs instead of being reset to baseFontSize.
func (r *renderer) renderInlineChildren(n ast.Node, styleStr string, size float64) {
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		r.renderInline(child, styleStr, size)
	}
}

func (r *renderer) renderInline(n ast.Node, styleStr string, size float64) {
	switch v := n.(type) {
	case *ast.Text:
		r.doc.SetFont(fontFamily, styleStr, size)
		r.doc.Write(lineHeight(size), string(v.Segment.Value(r.source)))
		switch {
		case v.HardLineBreak():
			r.doc.Ln(lineHeight(size))
		case v.SoftLineBreak():
			r.doc.Write(lineHeight(size), " ")
		}
	case *ast.Link:
		// Link text is flattened to its concatenated Text children --
		// nested emphasis inside link text isn't supported, since the
		// documents this package renders (see decisions.log, issue #45)
		// never format link text and WriteLinkString takes a single
		// styled run, not a mixed-style one.
		r.doc.SetFont(fontFamily, styleStr+"U", size)
		r.doc.WriteLinkString(lineHeight(size), linkText(n, r.source), string(v.Destination))
	case *ast.AutoLink:
		// A CommonMark autolink (<https://example.com>, <jane@doe.com>)
		// carries its text as a private value, not a child *ast.Text
		// node (see ast.AutoLink), so it falls through the default
		// case's child-node recursion as if it had no text at all.
		// goldmark's default parser never sets AutoLink.Protocol (see
		// parser/auto_link.go), so URL() returns the bare address
		// literal for an email autolink rather than a mailto: URI --
		// add the prefix ourselves, or a PDF viewer treats the address
		// as plain text instead of a clickable link.
		url := string(v.URL(r.source))
		if v.AutoLinkType == ast.AutoLinkEmail {
			url = "mailto:" + url
		}
		r.doc.SetFont(fontFamily, styleStr+"U", size)
		r.doc.WriteLinkString(lineHeight(size), string(v.Label(r.source)), url)
	case *ast.Emphasis:
		// Level 1 is markdown's single "*"/"_" (italic), level 2 is
		// "**"/"__" (bold) -- combine additively so "***x***" (both)
		// renders as bold-italic rather than one clobbering the other.
		style := styleStr
		if v.Level == 1 {
			style += "I"
		} else {
			style += "B"
		}
		r.renderInlineChildren(n, style, size)
	default:
		r.renderInlineChildren(n, styleStr, size)
	}
}

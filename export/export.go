// Package export converts an application's drafted markdown documents
// into PDF files at a caller-chosen destination. It exists to compose
// two packages that deliberately don't know about each other: documents,
// which resolves paths but never reads file content (see documents.go's
// doc comment), and pdf, which renders content but knows nothing about
// where a document lives. Both the `swamp export` CLI subcommand and the
// TUI's export screen drive the same functions here, so the two can't
// drift on how a document becomes a PDF.
package export

import (
	"fmt"
	"os"

	"strings"

	"github.com/dklassen/swamp/pdf"
	"github.com/dklassen/swamp/store"
)

// Document renders mdPath's markdown content to a PDF written at
// outPath. The destination is the caller's to choose -- unlike the
// original sibling-file-only behavior this replaced -- because the TUI
// lets the user export anywhere (e.g. ~/Desktop, to drag into a browser
// upload field).
func Document(mdPath, outPath string) error {
	content, err := os.ReadFile(mdPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", mdPath, err)
	}
	rendered, err := pdf.Render(content)
	if err != nil {
		return fmt.Errorf("render pdf: %w", err)
	}
	if err := os.WriteFile(outPath, rendered, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}
	return nil
}

// FileName builds the destination filename for one exported document:
// the company and posting title slugged and hyphen-joined, suffixed with
// the document type (e.g. "wealthsimple-delivery-platform-resume.pdf").
//
// The name is self-describing on purpose. The TUI lets the user export
// several applications into one folder (typically the desktop, to drag
// into a browser's upload field), where the CLI's plain
// cover_letter.pdf/resume.pdf names would collide and silently overwrite
// each other -- and wouldn't say which application they belonged to at
// the moment of the drag. Nothing is truncated: posting titles are short
// enough that a lossless name stays well inside any filesystem's limit,
// and an elided name would defeat the point.
func FileName(company, title string, documentType store.DocumentType) string {
	parts := make([]string, 0, 3)
	for _, raw := range []string{company, title} {
		// An empty component is dropped rather than joined, so a
		// missing company name can't produce a leading "-engineer".
		if slug := slugify(raw); slug != "" {
			parts = append(parts, slug)
		}
	}
	parts = append(parts, documentType.String())
	return strings.Join(parts, "-") + ".pdf"
}

// slugify reduces s to lowercase alphanumerics separated by single
// hyphens, with leading/trailing hyphens trimmed. Any run of other
// characters (spaces, punctuation, and non-ASCII alike) collapses to one
// hyphen -- this names a file, so the conservative ASCII-only result is
// the point, not a limitation to work around.
func slugify(s string) string {
	var b strings.Builder
	pendingSeparator := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			// The separator is only emitted once a following
			// alphanumeric actually arrives, which trims trailing runs
			// without a second pass.
			if pendingSeparator && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingSeparator = false
			b.WriteRune(r)
			continue
		}
		pendingSeparator = true
	}
	return b.String()
}

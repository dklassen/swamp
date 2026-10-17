package document

import "time"

// Export is one PDF exported from a drafted document. ContentSHA256 is the
// hash of exactly the markdown that was rendered, so IsCurrent tells
// whether the document has changed since.
type Export struct {
	ID            int64
	ApplicationID int64
	DocumentType  Type
	ContentSHA256 string
	Path          string
	ExportedAt    time.Time
}

// IsCurrent reports whether e was exported from content, hashed the same
// way as Review.IsCurrent. An export of an earlier draft is stale: the PDF
// no longer matches the document.
func (e Export) IsCurrent(content string) bool {
	return ContentSHA256(content) == e.ContentSHA256
}

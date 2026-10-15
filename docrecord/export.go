package docrecord

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

// Export is one PDF exported from a drafted document. ContentSHA256 is the
// hash of exactly the markdown that was rendered, so IsCurrent tells
// whether the document has changed since.
type Export struct {
	ID            int64
	ApplicationID int64
	DocumentType  documents.Type
	ContentSHA256 string
	Path          string
	ExportedAt    time.Time
}

// IsCurrent reports whether e was exported from content, hashed the same
// way as Review.IsCurrent. An export of an earlier draft is stale: the PDF
// no longer matches the document.
func (e Export) IsCurrent(content string) bool {
	return documents.ContentSHA256(content) == e.ContentSHA256
}

// RecordExport records that content, applicationID's documentType, was
// exported as a PDF to path. Callers record only after the PDF was
// written.
func RecordExport(ctx context.Context, r store.Runner, applicationID int64, documentType documents.Type, content, path string) error {
	err := store.Write(ctx, r, func(q *sqlcdb.Queries) error {
		return q.CreateDocumentExport(ctx, sqlcdb.CreateDocumentExportParams{
			ApplicationID: applicationID,
			DocumentType:  documentType.String(),
			ContentSha256: documents.ContentSHA256(content),
			Path:          path,
		})
	})
	if err != nil {
		return fmt.Errorf("docrecord: record export: %w", err)
	}
	return nil
}

// LatestExports returns applicationID's most recent export of each
// document type, omitting any type never exported.
func LatestExports(ctx context.Context, r store.Runner, applicationID int64) (map[documents.Type]Export, error) {
	exports := make(map[documents.Type]Export)
	for _, documentType := range documents.Types() {
		row, err := store.Read(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.DocumentExport, error) {
			return q.LatestDocumentExport(ctx, sqlcdb.LatestDocumentExportParams{
				ApplicationID: applicationID,
				DocumentType:  documentType.String(),
			})
		})
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("docrecord: latest export: %w", err)
		}
		exports[documentType] = Export{
			ID:            row.ID,
			ApplicationID: row.ApplicationID,
			DocumentType:  documentType,
			ContentSHA256: row.ContentSha256,
			Path:          row.Path,
			ExportedAt:    row.ExportedAt,
		}
	}
	return exports, nil
}

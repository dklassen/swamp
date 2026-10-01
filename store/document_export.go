package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dklassen/swamp/store/db"
)

// DocumentExport is one PDF exported from a drafted document (#188).
// ContentSHA256 is the hash of exactly the markdown that was rendered,
// so IsCurrent tells whether the document has changed since.
type DocumentExport struct {
	ID            int64
	ApplicationID int64
	DocumentType  DocumentType
	ContentSHA256 string
	Path          string
	ExportedAt    time.Time
}

// IsCurrent reports whether e was exported from content, hashed the same
// way as DocumentReview.IsCurrent. An export of an earlier draft is
// stale: the PDF no longer matches the document.
func (e DocumentExport) IsCurrent(content string) bool {
	return contentSHA256(content) == e.ContentSHA256
}

// RecordDocumentExport records that content, applicationID's
// documentType, was exported as a PDF to path. Callers record only after
// the PDF was written.
func (s *Store) RecordDocumentExport(ctx context.Context, applicationID int64, documentType DocumentType, content, path string) error {
	if err := s.queries.CreateDocumentExport(ctx, db.CreateDocumentExportParams{
		ApplicationID: applicationID,
		DocumentType:  documentType.String(),
		ContentSha256: contentSHA256(content),
		Path:          path,
	}); err != nil {
		return fmt.Errorf("store: record document export: %w", err)
	}
	return nil
}

// LatestDocumentExports returns applicationID's most recent export of
// each document type, omitting any type never exported.
func (s *Store) LatestDocumentExports(ctx context.Context, applicationID int64) (map[DocumentType]DocumentExport, error) {
	exports := make(map[DocumentType]DocumentExport)
	for _, documentType := range []DocumentType{DocumentTypeCoverLetter, DocumentTypeResume} {
		row, err := s.queries.LatestDocumentExport(ctx, db.LatestDocumentExportParams{
			ApplicationID: applicationID,
			DocumentType:  documentType.String(),
		})
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("store: latest document export: %w", err)
		}
		exports[documentType] = DocumentExport{
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

package repository

import (
	"context"
	"fmt"

	domain "github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

type DocumentExportRepository struct{}

// Insert records that content, applicationID's documentType, was exported
// as a PDF to path.
func (repository *DocumentExportRepository) Insert(ctx context.Context, r store.Runner, applicationID int64, documentType domain.Type, content, path string) error {
	err := store.Write(ctx, r, func(q *sqlcdb.Queries) error {
		return q.CreateDocumentExport(ctx, sqlcdb.CreateDocumentExportParams{
			ApplicationID: applicationID,
			DocumentType:  documentType.String(),
			ContentSha256: domain.ContentSHA256(content),
			Path:          path,
		})
	})
	if err != nil {
		return fmt.Errorf("document: record export: %w", err)
	}
	return nil
}

// Latest returns applicationID's most recent export of documentType, or
// store.ErrNotFound when it was never exported.
func (repository *DocumentExportRepository) Latest(ctx context.Context, r store.Runner, applicationID int64, documentType domain.Type) (*domain.Export, error) {
	row, err := store.Read(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.DocumentExport, error) {
		return q.LatestDocumentExport(ctx, sqlcdb.LatestDocumentExportParams{
			ApplicationID: applicationID,
			DocumentType:  documentType.String(),
		})
	})
	if err != nil {
		return nil, err
	}
	e, err := fromDocumentExportRow(row)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// GetMany returns the exports with the given ids that exist, in no order.
func (repository *DocumentExportRepository) GetMany(ctx context.Context, r store.Runner, ids []int64) ([]domain.Export, error) {
	rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.DocumentExport, error) {
		return q.ListDocumentExportsByID(ctx, ids)
	})
	if err != nil {
		return nil, fmt.Errorf("document: exports by id: %w", err)
	}
	exports := make([]domain.Export, len(rows))
	for i, row := range rows {
		if exports[i], err = fromDocumentExportRow(row); err != nil {
			return nil, err
		}
	}
	return exports, nil
}

func fromDocumentExportRow(row sqlcdb.DocumentExport) (domain.Export, error) {
	parsedType, err := domain.ParseType(row.DocumentType)
	if err != nil {
		return domain.Export{}, err
	}
	return domain.Export{
		ID:            row.ID,
		ApplicationID: row.ApplicationID,
		DocumentType:  parsedType,
		ContentSHA256: row.ContentSha256,
		Path:          row.Path,
		ExportedAt:    row.ExportedAt,
	}, nil
}

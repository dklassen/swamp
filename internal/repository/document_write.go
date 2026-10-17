package repository

import (
	"context"
	"fmt"

	domain "github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

type DocumentWriteRepository struct{}

// Insert records that source wrote content as applicationID's
// documentType.
func (repository *DocumentWriteRepository) Insert(ctx context.Context, r store.Runner, applicationID int64, documentType domain.Type, content string, source domain.Source) error {
	err := store.Write(ctx, r, func(q *sqlcdb.Queries) error {
		return q.CreateDocumentWrite(ctx, sqlcdb.CreateDocumentWriteParams{
			ApplicationID: applicationID,
			DocumentType:  documentType.String(),
			ContentSha256: domain.ContentSHA256(content),
			Source:        source.String(),
		})
	})
	if err != nil {
		return fmt.Errorf("document: record write: %w", err)
	}
	return nil
}

// Latest returns the newest recorded write of applicationID's
// documentType, or store.ErrNotFound when none was recorded.
func (repository *DocumentWriteRepository) Latest(ctx context.Context, r store.Runner, applicationID int64, documentType domain.Type) (domain.Write, error) {
	row, err := store.Read(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.DocumentWrite, error) {
		return q.LatestDocumentWrite(ctx, sqlcdb.LatestDocumentWriteParams{
			ApplicationID: applicationID,
			DocumentType:  documentType.String(),
		})
	})
	if err != nil {
		return domain.Write{}, err
	}
	parsedType, err := domain.ParseType(row.DocumentType)
	if err != nil {
		return domain.Write{}, fmt.Errorf("document: %w", err)
	}
	source, err := domain.ParseSource(row.Source)
	if err != nil {
		return domain.Write{}, err
	}
	return domain.Write{
		ID:            row.ID,
		ApplicationID: row.ApplicationID,
		DocumentType:  parsedType,
		ContentSHA256: row.ContentSha256,
		Source:        source,
		WrittenAt:     row.WrittenAt,
	}, nil
}

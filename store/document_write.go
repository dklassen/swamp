package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store/db"
)

// DocumentWriteSource is who wrote a document, as far as Swamp saw.
type DocumentWriteSource int

const (
	// DocumentWriteSourceWriteDocument is an agent, through MCP's
	// write_document.
	DocumentWriteSourceWriteDocument DocumentWriteSource = iota
	// DocumentWriteSourceEditor is the user, through the TUI's $EDITOR.
	DocumentWriteSourceEditor
)

// documentWriteSourceNames is the DB string form of each source, the one
// place the mapping is defined (as reviewOutcomeNames).
var documentWriteSourceNames = [...]string{
	DocumentWriteSourceWriteDocument: "write_document",
	DocumentWriteSourceEditor:        "editor",
}

// String is also the value stored in document_writes.source.
func (s DocumentWriteSource) String() string {
	if s < 0 || int(s) >= len(documentWriteSourceNames) {
		return fmt.Sprintf("DocumentWriteSource(%d)", int(s))
	}
	return documentWriteSourceNames[s]
}

// parseDocumentWriteSource fails loudly on a value it doesn't know, as
// ParseReviewOutcome does.
func parseDocumentWriteSource(s string) (DocumentWriteSource, error) {
	for i, name := range documentWriteSourceNames {
		if name == s {
			return DocumentWriteSource(i), nil
		}
	}
	return 0, fmt.Errorf("store: unknown document write source %q", s)
}

// DocumentWrite is one write of a document that Swamp saw (#258).
type DocumentWrite struct {
	ID            int64
	ApplicationID int64
	DocumentType  documents.Type
	ContentSHA256 string
	Source        DocumentWriteSource
	WrittenAt     time.Time
}

// RecordDocumentWrite records that source wrote content as
// applicationID's documentType. Recording commits, which is what lets a
// ChangeProbe see a document write (RFC 0007, step 8). Callers record
// only after the file was written.
func (s *Store) RecordDocumentWrite(ctx context.Context, applicationID int64, documentType documents.Type, content string, source DocumentWriteSource) error {
	if err := s.queries.CreateDocumentWrite(ctx, db.CreateDocumentWriteParams{
		ApplicationID: applicationID,
		DocumentType:  documentType.String(),
		ContentSha256: contentSHA256(content),
		Source:        source.String(),
	}); err != nil {
		return fmt.Errorf("store: record document write: %w", err)
	}
	return nil
}

// LatestDocumentWrite returns the newest recorded write of
// applicationID's documentType, and false if none was recorded.
func (s *Store) LatestDocumentWrite(ctx context.Context, applicationID int64, documentType documents.Type) (DocumentWrite, bool, error) {
	row, err := s.queries.LatestDocumentWrite(ctx, db.LatestDocumentWriteParams{
		ApplicationID: applicationID,
		DocumentType:  documentType.String(),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return DocumentWrite{}, false, nil
	}
	if err != nil {
		return DocumentWrite{}, false, fmt.Errorf("store: latest document write: %w", err)
	}
	parsedType, err := documents.ParseType(row.DocumentType)
	if err != nil {
		return DocumentWrite{}, false, fmt.Errorf("store: %w", err)
	}
	source, err := parseDocumentWriteSource(row.Source)
	if err != nil {
		return DocumentWrite{}, false, err
	}
	return DocumentWrite{
		ID:            row.ID,
		ApplicationID: row.ApplicationID,
		DocumentType:  parsedType,
		ContentSHA256: row.ContentSha256,
		Source:        source,
		WrittenAt:     row.WrittenAt,
	}, true, nil
}

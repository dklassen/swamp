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

// Source is who wrote a document, as far as Swamp saw.
type Source int

const (
	// SourceWriteDocument is an agent, through MCP's write_document.
	SourceWriteDocument Source = iota
	// SourceEditor is the user, through the TUI's $EDITOR.
	SourceEditor
)

// sourceNames is the DB string form of each source, the one place the
// mapping is defined (as outcomeNames).
var sourceNames = [...]string{
	SourceWriteDocument: "write_document",
	SourceEditor:        "editor",
}

// String is also the value stored in document_writes.source.
func (s Source) String() string {
	if s < 0 || int(s) >= len(sourceNames) {
		return fmt.Sprintf("Source(%d)", int(s))
	}
	return sourceNames[s]
}

// parseSource fails loudly on a value it doesn't know, as ParseOutcome
// does.
func parseSource(s string) (Source, error) {
	for i, name := range sourceNames {
		if name == s {
			return Source(i), nil
		}
	}
	return 0, fmt.Errorf("docrecord: unknown document write source %q", s)
}

// Write is one write of a document that Swamp saw.
type Write struct {
	ID            int64
	ApplicationID int64
	DocumentType  documents.Type
	ContentSHA256 string
	Source        Source
	WrittenAt     time.Time
}

// RecordWrite records that source wrote content as applicationID's
// documentType. Callers record only after the file was written.
func RecordWrite(ctx context.Context, r store.Runner, applicationID int64, documentType documents.Type, content string, source Source) error {
	err := store.Write(ctx, r, func(q *sqlcdb.Queries) error {
		return q.CreateDocumentWrite(ctx, sqlcdb.CreateDocumentWriteParams{
			ApplicationID: applicationID,
			DocumentType:  documentType.String(),
			ContentSha256: documents.ContentSHA256(content),
			Source:        source.String(),
		})
	})
	if err != nil {
		return fmt.Errorf("docrecord: record write: %w", err)
	}
	return nil
}

// LatestWrite returns the newest recorded write of applicationID's
// documentType, and false if none was recorded.
func LatestWrite(ctx context.Context, r store.Runner, applicationID int64, documentType documents.Type) (Write, bool, error) {
	row, err := store.Read(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.DocumentWrite, error) {
		return q.LatestDocumentWrite(ctx, sqlcdb.LatestDocumentWriteParams{
			ApplicationID: applicationID,
			DocumentType:  documentType.String(),
		})
	})
	if errors.Is(err, store.ErrNotFound) {
		return Write{}, false, nil
	}
	if err != nil {
		return Write{}, false, fmt.Errorf("docrecord: latest write: %w", err)
	}
	parsedType, err := documents.ParseType(row.DocumentType)
	if err != nil {
		return Write{}, false, fmt.Errorf("docrecord: %w", err)
	}
	source, err := parseSource(row.Source)
	if err != nil {
		return Write{}, false, err
	}
	return Write{
		ID:            row.ID,
		ApplicationID: row.ApplicationID,
		DocumentType:  parsedType,
		ContentSHA256: row.ContentSha256,
		Source:        source,
		WrittenAt:     row.WrittenAt,
	}, true, nil
}

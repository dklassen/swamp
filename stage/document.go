package stage

import (
	"context"
	"fmt"

	"github.com/dklassen/swamp/internal/domain/document"
)

// WriteDocument replaces applicationID's documentType document with
// content as the agent's write, and returns its path. replacesVersion is
// the version the agent read; see service.DocumentService.Write.
func (st *Stage) WriteDocument(ctx context.Context, applicationID int64, documentType document.Type, content, replacesVersion string) (string, error) {
	path, err := st.docs.Write(ctx, applicationID, documentType, content, replacesVersion, document.SourceWriteDocument)
	if err != nil {
		return "", fmt.Errorf("stage: %w", err)
	}
	return path, nil
}

// ReadDocument returns applicationID's documentType document and its
// path.
func (st *Stage) ReadDocument(ctx context.Context, applicationID int64, documentType document.Type) (path, content string, err error) {
	path, content, err = st.docs.Read(ctx, applicationID, documentType)
	if err != nil {
		return "", "", fmt.Errorf("stage: %w", err)
	}
	return path, content, nil
}

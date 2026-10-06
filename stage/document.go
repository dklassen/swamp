package stage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// ErrApplicationNotFound is WriteDocument and ReadDocument's error for an
// application ID that was never handed out.
var ErrApplicationNotFound = errors.New("stage: application not found")

// ErrDocumentNotWritten is ReadDocument's error for a live application
// whose document hasn't been written yet.
var ErrDocumentNotWritten = errors.New("stage: document not written yet")

// ApplicationDeletedError is WriteDocument and ReadDocument's error for an
// application the user deleted. PostingID is the posting it was for,
// which stage_prepare would start a fresh application on.
type ApplicationDeletedError struct {
	ApplicationID int64
	PostingID     int64
}

func (e *ApplicationDeletedError) Error() string {
	return fmt.Sprintf("stage: application %d (posting %d) was deleted", e.ApplicationID, e.PostingID)
}

// application returns applicationID's application, or an error saying why
// its documents can't be touched. Documents folders are keyed by ID alone,
// so writing for an ID with no application would leave a draft for
// whichever application gets that ID next (#244).
func (st *Stage) application(ctx context.Context, applicationID int64) (store.Application, error) {
	application, err := st.store.GetApplicationByIDIncludingDeleted(ctx, applicationID)
	if errors.Is(err, store.ErrNotFound) {
		return store.Application{}, fmt.Errorf("%w: %d", ErrApplicationNotFound, applicationID)
	}
	if err != nil {
		return store.Application{}, fmt.Errorf("stage: get application %d: %w", applicationID, err)
	}
	if !application.DeletedAt.IsZero() {
		return store.Application{}, &ApplicationDeletedError{ApplicationID: applicationID, PostingID: application.PostingID}
	}
	return application, nil
}

// WriteDocument replaces applicationID's documentType document with
// content and returns its path. It refuses an application that doesn't
// exist rather than creating a folder for it. With expectedSHA256, it
// writes only if the document is still that version (empty: still
// absent), and fails with documents.ErrChanged otherwise; nil writes
// unconditionally.
func (st *Stage) WriteDocument(ctx context.Context, applicationID int64, documentType documents.Type, content string, expectedSHA256 *string) (string, error) {
	if _, err := st.application(ctx, applicationID); err != nil {
		return "", err
	}
	var path string
	var err error
	if expectedSHA256 == nil {
		path, err = st.documents.Write(applicationID, documentType, content)
	} else {
		path, err = st.documents.WriteIfUnchanged(applicationID, documentType, content, *expectedSHA256)
	}
	if err != nil {
		return "", fmt.Errorf("stage: write %s: %w", documentType, err)
	}
	// Recorded so the TUI's change probe sees a file write (RFC 0007,
	// step 8), and so the review form can say the agent wrote it.
	if err := st.store.RecordDocumentWrite(ctx, applicationID, documentType, content, store.DocumentWriteSourceWriteDocument); err != nil {
		return "", fmt.Errorf("stage: %s was written to %s, but recording the write failed: %w", documentType, path, err)
	}
	return path, nil
}

// ReadDocument returns applicationID's documentType document and its
// path, with the same application check as WriteDocument. Unlike a write
// it never creates the application's folder.
func (st *Stage) ReadDocument(ctx context.Context, applicationID int64, documentType documents.Type) (path, content string, err error) {
	if _, err := st.application(ctx, applicationID); err != nil {
		return "", "", err
	}
	path, err = st.documents.Path(applicationID, documentType)
	if err != nil {
		return "", "", fmt.Errorf("stage: %w", err)
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", "", fmt.Errorf("%w: application %d %s", ErrDocumentNotWritten, applicationID, documentType)
	}
	if err != nil {
		return "", "", fmt.Errorf("stage: read %s: %w", path, err)
	}
	return path, string(b), nil
}

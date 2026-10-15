package stage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/dklassen/swamp/application"
	"github.com/dklassen/swamp/docrecord"
	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// ErrApplicationNotFound is WriteDocument and ReadDocument's error for an
// application ID that was never handed out.
var ErrApplicationNotFound = errors.New("stage: application not found")

// ErrDocumentNotWritten is ReadDocument's error for a live application
// whose document hasn't been written yet.
var ErrDocumentNotWritten = errors.New("stage: document not written yet")

// NoDocumentYet is the Version of a document that doesn't exist: Prepare
// reports it, and WriteDocument takes it back to expect none.
const NoDocumentYet = "no_document_yet"

// ErrEmptyReplacesVersion is WriteDocument's error for an empty
// replacesVersion, which reads too much like an omitted one.
var ErrEmptyReplacesVersion = errors.New("stage: replaces version is empty")

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
// whichever application gets that ID next.
func (st *Stage) application(ctx context.Context, applicationID int64) (application.Application, error) {
	app, err := application.GetByIDIncludingDeleted(ctx, st.store.DB(), applicationID)
	if errors.Is(err, store.ErrNotFound) {
		return application.Application{}, fmt.Errorf("%w: %d", ErrApplicationNotFound, applicationID)
	}
	if err != nil {
		return application.Application{}, fmt.Errorf("stage: get application %d: %w", applicationID, err)
	}
	if !app.DeletedAt.IsZero() {
		return application.Application{}, &ApplicationDeletedError{ApplicationID: applicationID, PostingID: app.PostingID}
	}
	return app, nil
}

// WriteDocument replaces applicationID's documentType document with
// content and returns its path. It refuses an application that doesn't
// exist rather than creating a folder for it. With replacesVersion, it
// writes only if the document is still that version (NoDocumentYet: still
// absent), and fails with documents.ErrChanged otherwise; nil writes
// unconditionally.
func (st *Stage) WriteDocument(ctx context.Context, applicationID int64, documentType documents.Type, content string, replacesVersion *string) (string, error) {
	if replacesVersion != nil && *replacesVersion == "" {
		return "", ErrEmptyReplacesVersion
	}
	if _, err := st.application(ctx, applicationID); err != nil {
		return "", err
	}
	var path string
	var err error
	switch {
	case replacesVersion == nil:
		path, err = st.documents.Write(applicationID, documentType, content)
	case *replacesVersion == NoDocumentYet:
		path, err = st.documents.WriteIfUnchanged(applicationID, documentType, content, "")
	default:
		path, err = st.documents.WriteIfUnchanged(applicationID, documentType, content, *replacesVersion)
	}
	if err != nil {
		return "", fmt.Errorf("stage: write %s: %w", documentType, err)
	}
	// A file write is invisible to the database; recording it is how other
	// processes learn of it, and that the agent made it.
	if err := docrecord.RecordWrite(ctx, st.store.DB(), applicationID, documentType, content, docrecord.SourceWriteDocument); err != nil {
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

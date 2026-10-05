package stage

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// ErrApplicationNotFound is WriteDocument's error for an application ID
// that was never handed out.
var ErrApplicationNotFound = errors.New("stage: application not found")

// ApplicationDeletedError is WriteDocument's error for an application the
// user deleted. PostingID is the posting it was for, which stage_prepare
// would start a fresh application on.
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
// exist rather than creating a folder for it.
func (st *Stage) WriteDocument(ctx context.Context, applicationID int64, documentType documents.Type, content string) (string, error) {
	if _, err := st.application(ctx, applicationID); err != nil {
		return "", err
	}
	path, err := st.documents.Path(applicationID, documentType)
	if err != nil {
		return "", fmt.Errorf("stage: %w", err)
	}
	if _, err := st.documents.EnsureDir(applicationID); err != nil {
		return "", fmt.Errorf("stage: ensure document directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("stage: write %s: %w", path, err)
	}
	return path, nil
}

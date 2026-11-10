package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/repository"
	"github.com/dklassen/swamp/store"
)

type reviewViewer interface {
	Count(ctx context.Context, r store.Runner, applicationID int64, documentType document.Type) (int64, error)
	List(ctx context.Context, r store.Runner, applicationID int64, documentType document.Type) ([]document.Review, error)
	Latest(ctx context.Context, r store.Runner, applicationID int64) (map[document.Type]document.Review, error)
	GetMany(ctx context.Context, r store.Runner, ids []int64) ([]document.Review, error)
}

type reviewRecorder interface {
	Insert(ctx context.Context, r store.Runner, applicationID int64, documentType document.Type, cycle int64, content string, outcome document.Outcome, notes string) (*document.Review, error)
}

type exportViewer interface {
	Latest(ctx context.Context, r store.Runner, applicationID int64, documentType document.Type) (*document.Export, error)
	GetMany(ctx context.Context, r store.Runner, ids []int64) ([]document.Export, error)
}

type exportRecorder interface {
	Insert(ctx context.Context, r store.Runner, applicationID int64, documentType document.Type, content, path string) error
}

type writeViewer interface {
	Latest(ctx context.Context, r store.Runner, applicationID int64, documentType document.Type) (*document.Write, error)
	GetMany(ctx context.Context, r store.Runner, ids []int64) ([]document.Write, error)
}

type writeRecorder interface {
	Insert(ctx context.Context, r store.Runner, applicationID int64, documentType document.Type, content string, source document.Source) error
}

type documentApplicationViewer interface {
	GetByIDIncludingDeleted(ctx context.Context, r store.Runner, id int64) (*application.Application, error)
}

type DocumentService struct {
	db             store.Store
	documents      *documents.Store
	reviewViewer   reviewViewer
	reviewRecorder reviewRecorder
	exportViewer   exportViewer
	exportRecorder exportRecorder
	writeViewer    writeViewer
	writeRecorder  writeRecorder
	applications   documentApplicationViewer
}

func NewDocumentService(db store.Store, docs *documents.Store) *DocumentService {
	reviews := &repository.DocumentReviewRepository{}
	exports := &repository.DocumentExportRepository{}
	writes := &repository.DocumentWriteRepository{}
	return &DocumentService{
		db:             db,
		documents:      docs,
		reviewViewer:   reviews,
		reviewRecorder: reviews,
		exportViewer:   exports,
		exportRecorder: exports,
		writeViewer:    writes,
		writeRecorder:  writes,
		applications:   &repository.ApplicationRepository{},
	}
}

// Review records a review of content (documentType's content as of right
// now -- the caller reads it off disk). Its cycle is the count of existing
// reviews for this application and document type, plus one. The count and
// the insert run in one transaction, so two reviews can't both read the
// same count and record the same cycle.
func (service *DocumentService) Review(ctx context.Context, applicationID int64, documentType document.Type, content string, outcome document.Outcome, notes string) (*document.Review, error) {
	var review *document.Review
	err := store.InTx(ctx, service.db, func(tx store.Tx) error {
		count, err := service.reviewViewer.Count(ctx, tx, applicationID, documentType)
		if err != nil {
			return err
		}
		review, err = service.reviewRecorder.Insert(ctx, tx, applicationID, documentType, count+1, content, outcome, notes)
		return err
	})
	if err != nil {
		return nil, err
	}
	return review, nil
}

// Reviews returns applicationID's reviews of documentType, most recent
// cycle first.
func (service *DocumentService) Reviews(ctx context.Context, applicationID int64, documentType document.Type) ([]document.Review, error) {
	return service.reviewViewer.List(ctx, service.db, applicationID, documentType)
}

// GetManyReviews returns the reviews with the given ids, in no order. An
// id with no review is left out.
func (service *DocumentService) GetManyReviews(ctx context.Context, ids []int64) ([]document.Review, error) {
	return service.reviewViewer.GetMany(ctx, service.db, ids)
}

// LatestReview returns applicationID's most recent review of
// documentType, or store.ErrNotFound when none has been recorded yet.
func (service *DocumentService) LatestReview(ctx context.Context, applicationID int64, documentType document.Type) (*document.Review, error) {
	reviews, err := service.reviewViewer.List(ctx, service.db, applicationID, documentType)
	if err != nil {
		return nil, err
	}
	if len(reviews) == 0 {
		return nil, store.ErrNotFound
	}
	return &reviews[0], nil
}

// LatestReviews returns applicationID's latest review of each document
// type that still describes the document on disk. A type with no review,
// a review of an earlier draft, or no document is left out: a missing
// entry is the only "no review" signal; see document.Review.
func (service *DocumentService) LatestReviews(ctx context.Context, applicationID int64) (map[document.Type]document.Review, error) {
	reviews, err := service.reviewViewer.Latest(ctx, service.db, applicationID)
	if err != nil {
		return nil, err
	}
	return documents.Current(service.documents.Status(applicationID), reviews, document.Review.IsCurrent)
}

// RecordExport records that content, applicationID's documentType, was
// exported as a PDF to path. Callers record only after the PDF was
// written.
func (service *DocumentService) RecordExport(ctx context.Context, applicationID int64, documentType document.Type, content, path string) error {
	return service.exportRecorder.Insert(ctx, service.db, applicationID, documentType, content, path)
}

// LatestExports returns applicationID's most recent export of each
// document type, omitting any type never exported.
func (service *DocumentService) LatestExports(ctx context.Context, applicationID int64) (map[document.Type]document.Export, error) {
	exports := make(map[document.Type]document.Export)
	for _, documentType := range document.Types() {
		export, err := service.exportViewer.Latest(ctx, service.db, applicationID, documentType)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("document: latest export: %w", err)
		}
		exports[documentType] = *export
	}
	return exports, nil
}

// GetManyExports returns the recorded exports with the given ids, in no
// order. An id with no export is left out.
func (service *DocumentService) GetManyExports(ctx context.Context, ids []int64) ([]document.Export, error) {
	return service.exportViewer.GetMany(ctx, service.db, ids)
}

// RecordWrite records that source wrote content as applicationID's
// documentType. Callers record only after the file was written.
func (service *DocumentService) RecordWrite(ctx context.Context, applicationID int64, documentType document.Type, content string, source document.Source) error {
	return service.writeRecorder.Insert(ctx, service.db, applicationID, documentType, content, source)
}

// LatestWrite returns the newest recorded write of applicationID's
// documentType, or store.ErrNotFound if none was recorded.
func (service *DocumentService) LatestWrite(ctx context.Context, applicationID int64, documentType document.Type) (*document.Write, error) {
	write, err := service.writeViewer.Latest(ctx, service.db, applicationID, documentType)
	if err != nil {
		return nil, fmt.Errorf("document: latest write: %w", err)
	}
	return write, nil
}

// GetManyWrites returns the recorded writes with the given ids, in no
// order. An id with no write is left out.
func (service *DocumentService) GetManyWrites(ctx context.Context, ids []int64) ([]document.Write, error) {
	return service.writeViewer.GetMany(ctx, service.db, ids)
}

// NoDocumentYet is the version of a document that doesn't exist: Write's
// version when the caller read none.
const NoDocumentYet = "no_document_yet"

// ErrApplicationNotFound is Write and Read's error for an application ID
// that was never handed out.
var ErrApplicationNotFound = errors.New("document: application not found")

// ErrDocumentNotWritten is Read's error for a live application whose
// document hasn't been written yet.
var ErrDocumentNotWritten = errors.New("document: not written yet")

// ErrNoVersion is Write's error for an empty version, which says nothing
// about what the caller read.
var ErrNoVersion = errors.New("document: no version given")

// ApplicationDeletedError is Write and Read's error for an application the
// user deleted. PostingID is the posting it was for.
type ApplicationDeletedError struct {
	ApplicationID int64
	PostingID     int64
}

func (e *ApplicationDeletedError) Error() string {
	return fmt.Sprintf("document: application %d (posting %d) was deleted", e.ApplicationID, e.PostingID)
}

// Write replaces applicationID's documentType document with content,
// records the write as source's, and returns its path. version is the
// version the caller read (NoDocumentYet for none); a document that has
// changed since is refused with documents.ErrChanged.
func (service *DocumentService) Write(ctx context.Context, applicationID int64, documentType document.Type, content, version string, source document.Source) (string, error) {
	if version == "" {
		return "", ErrNoVersion
	}
	if err := service.requireApplication(ctx, applicationID); err != nil {
		return "", err
	}
	read := version
	if version == NoDocumentYet {
		read = ""
	}
	path, err := service.documents.Write(applicationID, documentType, content, read)
	if err != nil {
		return "", fmt.Errorf("document: write %s: %w", documentType, err)
	}
	// A file write is invisible to the database; recording it is how other
	// processes learn of it.
	if err := service.writeRecorder.Insert(ctx, service.db, applicationID, documentType, content, source); err != nil {
		return "", fmt.Errorf("document: %s was written to %s, but recording the write failed: %w", documentType, path, err)
	}
	return path, nil
}

// requireApplication returns why applicationID's documents can't be
// touched, or nil. Documents are kept by application ID alone, so a write
// for an ID with no live application would leave a draft for whichever
// gets that ID next.
func (service *DocumentService) requireApplication(ctx context.Context, applicationID int64) error {
	app, err := service.applications.GetByIDIncludingDeleted(ctx, service.db, applicationID)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %d", ErrApplicationNotFound, applicationID)
	}
	if err != nil {
		return fmt.Errorf("document: get application %d: %w", applicationID, err)
	}
	if !app.DeletedAt.IsZero() {
		return &ApplicationDeletedError{ApplicationID: applicationID, PostingID: app.PostingID}
	}
	return nil
}

// Read returns applicationID's documentType document and its path.
func (service *DocumentService) Read(ctx context.Context, applicationID int64, documentType document.Type) (path, content string, err error) {
	if err := service.requireApplication(ctx, applicationID); err != nil {
		return "", "", err
	}
	path, err = service.documents.Path(applicationID, documentType)
	if err != nil {
		return "", "", fmt.Errorf("document: %w", err)
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", "", fmt.Errorf("%w: application %d %s", ErrDocumentNotWritten, applicationID, documentType)
	}
	if err != nil {
		return "", "", fmt.Errorf("document: read %s: %w", path, err)
	}
	return path, string(b), nil
}

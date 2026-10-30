package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/repository"
	"github.com/dklassen/swamp/store"
)

type reviewViewer interface {
	Count(ctx context.Context, r store.Runner, applicationID int64, documentType document.Type) (int64, error)
	List(ctx context.Context, r store.Runner, applicationID int64, documentType document.Type) ([]document.Review, error)
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

type DocumentService struct {
	db             store.Store
	reviewViewer   reviewViewer
	reviewRecorder reviewRecorder
	exportViewer   exportViewer
	exportRecorder exportRecorder
	writeViewer    writeViewer
	writeRecorder  writeRecorder
}

func NewDocumentService(db store.Store) *DocumentService {
	reviews := &repository.DocumentReviewRepository{}
	exports := &repository.DocumentExportRepository{}
	writes := &repository.DocumentWriteRepository{}
	return &DocumentService{
		db:             db,
		reviewViewer:   reviews,
		reviewRecorder: reviews,
		exportViewer:   exports,
		exportRecorder: exports,
		writeViewer:    writes,
		writeRecorder:  writes,
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

// LatestReviews resolves applicationID's latest review of each document
// type into a map, omitting any document type with no review yet. A
// missing entry is the only "no review" signal; see document.Review.
func (service *DocumentService) LatestReviews(ctx context.Context, applicationID int64) (map[document.Type]document.Review, error) {
	reviews := make(map[document.Type]document.Review)
	for _, documentType := range document.Types() {
		review, err := service.LatestReview(ctx, applicationID, documentType)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		reviews[documentType] = *review
	}
	return reviews, nil
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

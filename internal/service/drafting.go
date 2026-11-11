package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/repository"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

type draftingPostingViewer interface {
	GetWithCompany(ctx context.Context, r store.Runner, id int64) (*posting.Posting, *company.Company, error)
	ListInterested(ctx context.Context, r store.Runner, excluding []application.Status) ([]posting.Interested, error)
}

type draftingReviewViewer interface {
	Latest(ctx context.Context, r store.Runner, applicationID int64) (map[document.Type]document.Review, error)
	LatestMany(ctx context.Context, r store.Runner, applicationIDs []int64) (map[int64]map[document.Type]document.Review, error)
}

type draftingMarkupViewer interface {
	GetMany(ctx context.Context, r store.Runner, postingIDs []int64) ([]posting.Markup, error)
}

type draftingApplicationStore interface {
	GetByPostingID(ctx context.Context, r store.Runner, postingID int64) (*application.Application, error)
	Insert(ctx context.Context, r store.Runner, postingID int64, status application.Status) (*application.Application, error)
	ListActive(ctx context.Context, r store.Runner, excluding []application.Status) ([]posting.ActiveApplication, error)
}

// DraftingService is the process of drafting a posting's documents: what
// to draft, and starting or resuming one posting.
type DraftingService struct {
	db                       store.Store
	documents                *documents.Store
	postingViewer            draftingPostingViewer
	applications             draftingApplicationStore
	applicationHistoryWriter applicationHistoryWriter
	reviewViewer             draftingReviewViewer
	markupViewer             draftingMarkupViewer
	formViewer               formViewer
}

func NewDraftingService(db store.Store, docs *documents.Store) *DraftingService {
	return &DraftingService{
		db:                       db,
		documents:                docs,
		postingViewer:            &repository.PostingRepository{},
		applications:             &repository.ApplicationRepository{},
		applicationHistoryWriter: &repository.ApplicationHistoryRepository{},
		reviewViewer:             &repository.DocumentReviewRepository{},
		markupViewer:             &repository.PostingMarkupRepository{},
		formViewer:               &repository.PostingForms{},
	}
}

// DraftDocument is one of an application's documents as Prepare found it.
type DraftDocument struct {
	Path   string
	Exists bool
	// Version is the document's hash, or NoDocumentYet when it doesn't
	// exist: what a write of it names as the version it replaces.
	Version string
}

// Prepared is everything needed to draft a posting's documents.
type Prepared struct {
	Posting     posting.Posting
	Company     company.Company
	Application application.Application
	Documents   map[document.Type]DraftDocument
	// LatestReviews holds each type's latest review while it describes
	// the document on disk.
	LatestReviews map[document.Type]document.Review
	// Form is the posting's stored application form, nil when it has none.
	Form *jobboard.ApplicationForm
}

// Prepare starts drafting a posting, or resumes it: it returns the
// posting's application, starting one if there's none, with its documents'
// folder in place.
func (service *DraftingService) Prepare(ctx context.Context, postingID int64) (*Prepared, error) {
	var prepared Prepared
	err := store.InTx(ctx, service.db, func(tx store.Tx) error {
		p, c, err := service.postingViewer.GetWithCompany(ctx, tx, postingID)
		if err != nil {
			return err
		}
		if c.DeletedAt != nil {
			return &CompanyDeletedError{PostingID: p.ID, CompanyName: c.Name}
		}
		app, err := service.applications.GetByPostingID(ctx, tx, postingID)
		if errors.Is(err, store.ErrNotFound) {
			if p.ListingStatus == posting.ListingClosed {
				return ErrPostingClosed
			}
			if app, err = service.applications.Insert(ctx, tx, postingID, application.Started); err != nil {
				return err
			}
			err = service.applicationHistoryWriter.Insert(ctx, tx, app.ID, application.Started, application.ChangedByUser)
		}
		if err != nil {
			return err
		}
		prepared.Posting, prepared.Company, prepared.Application = *p, *c, *app
		form, err := service.storedForm(ctx, tx, postingID)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		prepared.Form = form
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("drafting: prepare posting %d: %w", postingID, err)
	}

	status, err := service.documents.EnsureDir(prepared.Application.ID)
	if err != nil {
		return nil, fmt.Errorf("drafting: document folder: %w", err)
	}
	prepared.Documents = make(map[document.Type]DraftDocument, len(document.Types()))
	for _, documentType := range document.Types() {
		doc, err := status.Doc(documentType)
		if err != nil {
			return nil, fmt.Errorf("drafting: %w", err)
		}
		sha, err := service.documents.SHA256(prepared.Application.ID, documentType)
		if err != nil {
			return nil, fmt.Errorf("drafting: hash %s: %w", documentType, err)
		}
		draft := DraftDocument{Path: doc.Path, Exists: sha != "", Version: sha}
		if !draft.Exists {
			draft.Version = NoDocumentYet
		}
		prepared.Documents[documentType] = draft
	}

	reviews, err := service.reviewViewer.Latest(ctx, service.db, prepared.Application.ID)
	if err != nil {
		return nil, fmt.Errorf("drafting: latest reviews: %w", err)
	}
	prepared.LatestReviews = make(map[document.Type]document.Review, len(reviews))
	for documentType, review := range reviews {
		// Matched against the version just read, so the review and the
		// version returned describe the same content.
		if draft := prepared.Documents[documentType]; draft.Exists && review.ContentSHA256 == draft.Version {
			prepared.LatestReviews[documentType] = review
		}
	}
	return &prepared, nil
}

func (service *DraftingService) storedForm(ctx context.Context, r store.Runner, postingID int64) (*jobboard.ApplicationForm, error) {
	stored, err := service.formViewer.Get(ctx, r, postingID)
	if err != nil {
		return nil, err
	}
	return decodeForm(stored)
}

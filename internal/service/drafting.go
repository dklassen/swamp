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
	"github.com/dklassen/swamp/store"
)

type draftingPostingViewer interface {
	GetWithCompany(ctx context.Context, r store.Runner, id int64) (*posting.Posting, *company.Company, error)
}

type draftingApplicationStore interface {
	GetByPostingID(ctx context.Context, r store.Runner, postingID int64) (*application.Application, error)
	Insert(ctx context.Context, r store.Runner, postingID int64, status application.Status) (*application.Application, error)
}

// DraftingService is the process of drafting a posting's documents: what
// to draft, and starting or resuming one posting.
type DraftingService struct {
	db                       store.Store
	documents                *documents.Store
	postingViewer            draftingPostingViewer
	applications             draftingApplicationStore
	applicationHistoryWriter applicationHistoryWriter
}

func NewDraftingService(db store.Store, docs *documents.Store) *DraftingService {
	return &DraftingService{
		db:                       db,
		documents:                docs,
		postingViewer:            &repository.PostingRepository{},
		applications:             &repository.ApplicationRepository{},
		applicationHistoryWriter: &repository.ApplicationHistoryRepository{},
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
		return nil
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
		prepared.Documents[documentType] = DraftDocument{Path: doc.Path, Exists: doc.Exists}
	}
	return &prepared, nil
}

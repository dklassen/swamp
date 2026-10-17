package service

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/repository"
	"github.com/dklassen/swamp/store"
)

// ErrPostingClosed is ApplyTo's error for a posting that's no longer
// listed: its form can't be submitted, and sync only ends an application
// when its posting closes, so one started now would sit at
// application_started for good.
var ErrPostingClosed = errors.New("application: posting is closed")

type postingViewer interface {
	Get(ctx context.Context, r store.Runner, id int64) (posting.Posting, error)
	GetBySource(ctx context.Context, r store.Runner, source, sourceID string) (posting.Posting, error)
	ListInterested(ctx context.Context, r store.Runner, excluding []application.Status) ([]posting.Interested, error)
	ListByCompany(ctx context.Context, r store.Runner, companyID int64) ([]posting.Posting, error)
	CountOpenByCompany(ctx context.Context, r store.Runner) (map[int64]int, error)
}

type postingSearcher interface {
	Search(ctx context.Context, r store.Runner, search posting.Search) (posting.SearchPage, error)
}

type postingWriter interface {
	Insert(ctx context.Context, r store.Runner, companyID int64, source, sourceID string, fields posting.IngestedFields) (posting.Posting, error)
	Update(ctx context.Context, r store.Runner, id int64, fields posting.IngestedFields) (posting.Posting, error)
	MarkSeen(ctx context.Context, r store.Runner, ids []int64) error
}

type postingStatusWriter interface {
	Close(ctx context.Context, r store.Runner, id int64) (bool, error)
	Reopen(ctx context.Context, r store.Runner, id int64) (bool, error)
}

type postingHistoryWriter interface {
	Insert(ctx context.Context, r store.Runner, p posting.Posting, changeType string) (posting.History, error)
}

type postingHistoryViewer interface {
	List(ctx context.Context, r store.Runner, postingID int64) ([]posting.History, error)
}

type markupViewer interface {
	Get(ctx context.Context, r store.Runner, postingID int64) (posting.Markup, error)
}

type markupWriter interface {
	Insert(ctx context.Context, r store.Runner, postingID int64) (posting.Markup, error)
	SetInterested(ctx context.Context, r store.Runner, postingID int64) (posting.Markup, error)
	UnmarkInterested(ctx context.Context, r store.Runner, postingID int64) (posting.Markup, error)
	SetArchived(ctx context.Context, r store.Runner, postingID int64) (posting.Markup, error)
	Unarchive(ctx context.Context, r store.Runner, postingID int64) (posting.Markup, error)
	SetNotes(ctx context.Context, r store.Runner, postingID int64, notes string) (posting.Markup, error)
}

type formViewer interface {
	Get(ctx context.Context, r store.Runner, postingID int64) (*posting.Form, error)
}

type formWriter interface {
	Save(ctx context.Context, r store.Runner, postingID int64, formJSON string) error
}

type postingApplicationViewer interface {
	GetByPostingID(ctx context.Context, r store.Runner, postingID int64) (application.Application, error)
}

type postingApplicationWriter interface {
	Insert(ctx context.Context, r store.Runner, postingID int64, status application.Status) (application.Application, error)
	SetStatus(ctx context.Context, r store.Runner, id int64, status application.Status) (application.Application, error)
}

type postingApplicationHistoryViewer interface {
	List(ctx context.Context, r store.Runner, applicationID int64) ([]application.StatusChange, error)
}

type postingApplicationHistoryWriter interface {
	Insert(ctx context.Context, r store.Runner, applicationID int64, status application.Status, changedBy application.ChangedBy) error
}

type PostingService struct {
	db                       store.Store
	postingViewer            postingViewer
	postingSearcher          postingSearcher
	postingWriter            postingWriter
	postingStatusWriter      postingStatusWriter
	postingHistoryWriter     postingHistoryWriter
	postingHistoryViewer     postingHistoryViewer
	markupViewer             markupViewer
	markupWriter             markupWriter
	formViewer               formViewer
	formWriter               formWriter
	applicationViewer        postingApplicationViewer
	applicationWriter        postingApplicationWriter
	applicationHistoryViewer postingApplicationHistoryViewer
	applicationHistoryWriter postingApplicationHistoryWriter
}

func NewPostingService(db store.Store) *PostingService {
	postings := &repository.PostingRepository{}
	postingHistory := &repository.PostingHistoryRepository{}
	markup := &repository.PostingMarkupRepository{}
	forms := &repository.PostingForms{}
	applications := &repository.ApplicationRepository{}
	applicationHistory := &repository.ApplicationHistoryRepository{}
	return &PostingService{
		db:                       db,
		postingViewer:            postings,
		postingSearcher:          &repository.PostingSearchRepository{},
		postingWriter:            postings,
		postingStatusWriter:      postings,
		postingHistoryWriter:     postingHistory,
		postingHistoryViewer:     postingHistory,
		markupViewer:             markup,
		markupWriter:             markup,
		formViewer:               forms,
		formWriter:               forms,
		applicationViewer:        applications,
		applicationWriter:        applications,
		applicationHistoryViewer: applicationHistory,
		applicationHistoryWriter: applicationHistory,
	}
}

// IngestResult reports what Ingest did.
type IngestResult struct {
	Posting posting.Posting
	// Created: the posting didn't exist and was inserted.
	Created bool
	// Updated: the posting existed with different content, which was
	// replaced and recorded as a "content_updated" history row.
	Updated bool
}

// Ingest saves a fetched posting, all in one transaction:
//   - not stored yet: inserts it, along with its markup row, so every
//     posting always has exactly one markup row;
//   - stored with different content: records the stored version as a
//     "content_updated" history row, then updates it;
//   - stored with the same content: changes nothing.
//
// Comparing against the row read inside the transaction, not one the
// caller read earlier, is what keeps an overlapping sync of the same
// posting from counting it as created twice or recording one change
// twice. An update keeps the row's listing_status untouched (see the
// UpdatePosting query comment for why).
func (service *PostingService) Ingest(ctx context.Context, companyID int64, source, sourceID string, fields posting.IngestedFields) (IngestResult, error) {
	var result IngestResult
	err := store.InTx(ctx, service.db, func(tx store.Tx) error {
		existing, err := service.postingViewer.GetBySource(ctx, tx, source, sourceID)
		switch {
		case errors.Is(err, store.ErrNotFound):
			created, err := service.postingWriter.Insert(ctx, tx, companyID, source, sourceID, fields)
			if err != nil {
				return fmt.Errorf("posting: create: %w", err)
			}
			if _, err := service.markupWriter.Insert(ctx, tx, created.ID); err != nil {
				return err
			}
			result = IngestResult{Posting: created, Created: true}
		case err != nil:
			return fmt.Errorf("posting: get by source: %w", err)
		case cmp.Equal(existing.IngestedFields, fields):
			result = IngestResult{Posting: existing}
		default:
			if _, err := service.postingHistoryWriter.Insert(ctx, tx, existing, posting.ChangeContentUpdated); err != nil {
				return err
			}
			updated, err := service.postingWriter.Update(ctx, tx, existing.ID, fields)
			if err != nil {
				return fmt.Errorf("posting: update: %w", err)
			}
			result = IngestResult{Posting: updated, Updated: true}
		}
		return nil
	})
	if err != nil {
		return IngestResult{}, err
	}
	return result, nil
}

// Search returns one page of the postings matching search.
func (service *PostingService) Search(ctx context.Context, search posting.Search) (posting.SearchPage, error) {
	return service.postingSearcher.Search(ctx, service.db, search)
}

// Get returns a posting by ID, or store.ErrNotFound.
func (service *PostingService) Get(ctx context.Context, postingID int64) (posting.Posting, error) {
	return service.postingViewer.Get(ctx, service.db, postingID)
}

// GetBySource returns the posting source knows as sourceID, or
// store.ErrNotFound.
func (service *PostingService) GetBySource(ctx context.Context, source, sourceID string) (posting.Posting, error) {
	return service.postingViewer.GetBySource(ctx, service.db, source, sourceID)
}

// History returns a posting's earlier versions, oldest first: a snapshot
// for each content change, close and reopen.
func (service *PostingService) History(ctx context.Context, postingID int64) ([]posting.History, error) {
	return service.postingHistoryViewer.List(ctx, service.db, postingID)
}

// ListByCompany returns every posting stored for a company, open or
// closed.
func (service *PostingService) ListByCompany(ctx context.Context, companyID int64) ([]posting.Posting, error) {
	return service.postingViewer.ListByCompany(ctx, service.db, companyID)
}

// CountOpenByCompany counts, per company, the postings its list shows by
// default: open, unarchived and let through by its filters. A company with
// none is missing from the map.
func (service *PostingService) CountOpenByCompany(ctx context.Context) (map[int64]int, error) {
	return service.postingViewer.CountOpenByCompany(ctx, service.db)
}

// MarkSeen records that a sync just saw these postings on their board. It
// isn't a content change, so updated_at and posting history are left
// alone. No IDs is a no-op.
func (service *PostingService) MarkSeen(ctx context.Context, ids []int64) error {
	return service.postingWriter.MarkSeen(ctx, service.db, ids)
}

// Interested returns the postings the user is interested in and hasn't
// archived, leaving out those whose application is at a dead end.
func (service *PostingService) Interested(ctx context.Context) ([]posting.Interested, error) {
	return service.postingViewer.ListInterested(ctx, service.db, application.Terminal())
}

// Application returns the posting's live application, or
// store.ErrNotFound when it has none.
func (service *PostingService) Application(ctx context.Context, postingID int64) (application.Application, error) {
	return service.applicationViewer.GetByPostingID(ctx, service.db, postingID)
}

// Markup returns the user's triage on the posting, or store.ErrNotFound.
func (service *PostingService) Markup(ctx context.Context, postingID int64) (posting.Markup, error) {
	return service.markupViewer.Get(ctx, service.db, postingID)
}

// SetInterested marks the posting interested, which also unarchives it.
func (service *PostingService) SetInterested(ctx context.Context, postingID int64) (posting.Markup, error) {
	return service.markupWriter.SetInterested(ctx, service.db, postingID)
}

func (service *PostingService) UnmarkInterested(ctx context.Context, postingID int64) (posting.Markup, error) {
	return service.markupWriter.UnmarkInterested(ctx, service.db, postingID)
}

// SetArchived archives the posting, which also unmarks it interested.
func (service *PostingService) SetArchived(ctx context.Context, postingID int64) (posting.Markup, error) {
	return service.markupWriter.SetArchived(ctx, service.db, postingID)
}

func (service *PostingService) Unarchive(ctx context.Context, postingID int64) (posting.Markup, error) {
	return service.markupWriter.Unarchive(ctx, service.db, postingID)
}

// SetNotes replaces the user's notes on the posting.
func (service *PostingService) SetNotes(ctx context.Context, postingID int64, notes string) (posting.Markup, error) {
	return service.markupWriter.SetNotes(ctx, service.db, postingID, notes)
}

// SaveForm stores what the posting's application form asks for, as JSON,
// replacing any earlier fetch.
func (service *PostingService) SaveForm(ctx context.Context, postingID int64, formJSON string) error {
	return service.formWriter.Save(ctx, service.db, postingID, formJSON)
}

// Form returns the posting's stored application form, or
// store.ErrNotFound when it was never fetched.
func (service *PostingService) Form(ctx context.Context, postingID int64) (*posting.Form, error) {
	return service.formViewer.Get(ctx, service.db, postingID)
}

// ApplyTo starts an application for a posting and records its
// application_started status as the user's change. It refuses a closed
// posting with ErrPostingClosed, checked in the same transaction so a sync
// closing the posting can't slip in between, and a missing one with
// store.ErrNotFound.
func (service *PostingService) ApplyTo(ctx context.Context, postingID int64) (application.Application, error) {
	var created application.Application
	err := store.InTx(ctx, service.db, func(tx store.Tx) error {
		p, err := service.postingViewer.Get(ctx, tx, postingID)
		if err != nil {
			return err
		}
		if p.ListingStatus == "closed" {
			return ErrPostingClosed
		}
		created, err = service.applicationWriter.Insert(ctx, tx, postingID, application.Started)
		if err != nil {
			return err
		}
		return service.applicationHistoryWriter.Insert(ctx, tx, created.ID, application.Started, application.ChangedByUser)
	})
	if err != nil {
		return application.Application{}, fmt.Errorf("application: create: %w", err)
	}
	return created, nil
}

// Close closes an open posting, records a "closed" snapshot of it as it
// was, and ends its application when that is at one of endFrom, all in one
// transaction: an interruption between them would leave the application
// open for good, since the next sync only looks at open postings. Nothing
// changes when the posting was already closed.
func (service *PostingService) Close(ctx context.Context, postingID int64, endFrom []application.Status) error {
	return store.InTx(ctx, service.db, func(tx store.Tx) error {
		before, err := service.postingViewer.Get(ctx, tx, postingID)
		if err != nil {
			return fmt.Errorf("posting: get posting to close: %w", err)
		}
		if closed, err := service.postingStatusWriter.Close(ctx, tx, postingID); err != nil || !closed {
			return err
		}
		if _, err := service.postingHistoryWriter.Insert(ctx, tx, before, posting.ChangeClosed); err != nil {
			return err
		}
		return service.endApplication(ctx, tx, postingID, endFrom)
	})
}

// Reopen marks a closed posting open again, records a "reopened" snapshot
// of it as it was just before, and undoes what Close did to its
// application, all in one transaction. Nothing changes when the posting
// was already open.
func (service *PostingService) Reopen(ctx context.Context, postingID int64) error {
	return store.InTx(ctx, service.db, func(tx store.Tx) error {
		before, err := service.postingViewer.Get(ctx, tx, postingID)
		if err != nil {
			return fmt.Errorf("posting: get posting to reopen: %w", err)
		}
		if reopened, err := service.postingStatusWriter.Reopen(ctx, tx, postingID); err != nil || !reopened {
			return err
		}
		if _, err := service.postingHistoryWriter.Insert(ctx, tx, before, posting.ChangeReopened); err != nil {
			return err
		}
		return service.restoreApplication(ctx, tx, postingID)
	})
}

// endApplication moves postingID's application, if it has one at a status
// in from, to posting_closed, recorded as sync's change.
func (service *PostingService) endApplication(ctx context.Context, tx store.Tx, postingID int64, from []application.Status) error {
	app, err := service.applicationViewer.GetByPostingID(ctx, tx, postingID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("application: end for closed posting %d: %w", postingID, err)
	}
	if !slices.Contains(from, app.Status) {
		return nil
	}
	if _, err := service.applicationWriter.SetStatus(ctx, tx, app.ID, application.PostingClosed); err != nil {
		return fmt.Errorf("application: end for closed posting %d: %w", postingID, err)
	}
	if err := service.applicationHistoryWriter.Insert(ctx, tx, app.ID, application.PostingClosed, application.ChangedBySync); err != nil {
		return fmt.Errorf("application: end for closed posting %d: %w", postingID, err)
	}
	return nil
}

// restoreApplication undoes endApplication when the posting reopens: an
// application sync moved to posting_closed goes back to the status it had
// before, recorded as sync's change. Only if sync's close is still the
// application's latest change -- a posting_closed the user set, or any
// status the user set since, is left alone, and so is a close recorded
// before history said who made it.
func (service *PostingService) restoreApplication(ctx context.Context, tx store.Tx, postingID int64) error {
	app, err := service.applicationViewer.GetByPostingID(ctx, tx, postingID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("application: restore for reopened posting %d: %w", postingID, err)
	}
	history, err := service.applicationHistoryViewer.List(ctx, tx, app.ID)
	if err != nil {
		return fmt.Errorf("application: restore for reopened posting %d: %w", postingID, err)
	}
	if len(history) < 2 {
		return nil
	}
	last := history[len(history)-1]
	if app.Status != application.PostingClosed ||
		last.Status != application.PostingClosed ||
		last.ChangedBy != application.ChangedBySync {
		return nil
	}
	previous := history[len(history)-2].Status
	if _, err := service.applicationWriter.SetStatus(ctx, tx, app.ID, previous); err != nil {
		return fmt.Errorf("application: restore for reopened posting %d: %w", postingID, err)
	}
	if err := service.applicationHistoryWriter.Insert(ctx, tx, app.ID, previous, application.ChangedBySync); err != nil {
		return fmt.Errorf("application: restore for reopened posting %d: %w", postingID, err)
	}
	return nil
}

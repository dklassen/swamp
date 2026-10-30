package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/change"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/repository"
	"github.com/dklassen/swamp/store"
)

type applicationViewer interface {
	Get(ctx context.Context, r store.Runner, id int64) (*application.Application, error)
	GetByIDIncludingDeleted(ctx context.Context, r store.Runner, id int64) (*application.Application, error)
	GetManyIncludingDeleted(ctx context.Context, r store.Runner, ids []int64) ([]application.Application, error)
	ListActive(ctx context.Context, r store.Runner, excluding []application.Status) ([]posting.ActiveApplication, error)
}

type applicationWriter interface {
	SetStatus(ctx context.Context, r store.Runner, id int64, status application.Status) (*application.Application, error)
	SetNotes(ctx context.Context, r store.Runner, id int64, notes string) (*application.Application, error)
	Delete(ctx context.Context, r store.Runner, id int64) error
}

type applicationHistoryViewer interface {
	List(ctx context.Context, r store.Runner, applicationID int64) ([]application.StatusChange, error)
	Latest(ctx context.Context, r store.Runner, applicationID int64) (*application.StatusChange, error)
}

type applicationHistoryWriter interface {
	Insert(ctx context.Context, r store.Runner, applicationID int64, status application.Status, changedBy application.ChangedBy) error
}

type applicationReviewViewer interface {
	List(ctx context.Context, r store.Runner, applicationID int64, documentType document.Type) ([]document.Review, error)
}

type ApplicationService struct {
	db                       store.Store
	applicationViewer        applicationViewer
	applicationWriter        applicationWriter
	applicationHistoryViewer applicationHistoryViewer
	applicationHistoryWriter applicationHistoryWriter
	reviewViewer             applicationReviewViewer
}

func NewApplicationService(db store.Store) *ApplicationService {
	applications := &repository.ApplicationRepository{}
	history := &repository.ApplicationHistoryRepository{}
	return &ApplicationService{
		db:                       db,
		applicationViewer:        applications,
		applicationWriter:        applications,
		applicationHistoryViewer: history,
		applicationHistoryWriter: history,
		reviewViewer:             &repository.DocumentReviewRepository{},
	}
}

// Active returns the applications not at a terminal status (see
// application.Terminal), most-recently-changed first, each with its
// posting, when it entered its status and its latest review per document
// type.
//
// Status-since and reviews take a few more reads per application rather
// than a windowed join: active applications are a personal-scale list (a
// handful to a few dozen), so the extra local round trips are negligible.
func (service *ApplicationService) Active(ctx context.Context) ([]posting.ActiveApplication, error) {
	apps, err := service.applicationViewer.ListActive(ctx, service.db, application.Terminal())
	if err != nil {
		return nil, fmt.Errorf("pursuit: list: %w", err)
	}
	for i := range apps {
		app := &apps[i]
		if app.StatusSince, err = service.StatusSince(ctx, app.Application); err != nil {
			return nil, fmt.Errorf("pursuit: %w", err)
		}
		reviews := make(map[document.Type]document.Review)
		for _, documentType := range document.Types() {
			list, err := service.reviewViewer.List(ctx, service.db, app.ID, documentType)
			if err != nil {
				return nil, fmt.Errorf("pursuit: reviews of application %d: %w", app.ID, err)
			}
			if len(list) > 0 {
				reviews[documentType] = list[0]
			}
		}
		app.LatestReviews = reviews
	}
	return apps, nil
}

// Get returns a live application by ID, or store.ErrNotFound.
func (service *ApplicationService) Get(ctx context.Context, applicationID int64) (*application.Application, error) {
	return service.applicationViewer.Get(ctx, service.db, applicationID)
}

// GetIncludingDeleted is Get, but also finds an application the user
// deleted (check DeletedAt).
func (service *ApplicationService) GetIncludingDeleted(ctx context.Context, applicationID int64) (*application.Application, error) {
	return service.applicationViewer.GetByIDIncludingDeleted(ctx, service.db, applicationID)
}

// GetManyIncludingDeleted returns the applications with the given ids,
// deleted or not (check DeletedAt), in no order. An id with no
// application is left out.
func (service *ApplicationService) GetManyIncludingDeleted(ctx context.Context, ids []int64) ([]application.Application, error) {
	return service.applicationViewer.GetManyIncludingDeleted(ctx, service.db, ids)
}

// SetNotes replaces a live application's notes, or returns
// store.ErrNotFound.
func (service *ApplicationService) SetNotes(ctx context.Context, applicationID int64, notes string) (*change.Of[application.Application], error) {
	var updated *change.Of[application.Application]
	err := store.InTx(ctx, service.db, func(tx store.Tx) error {
		before, err := service.applicationViewer.Get(ctx, tx, applicationID)
		if err != nil {
			return err
		}
		after, err := service.applicationWriter.SetNotes(ctx, tx, applicationID, notes)
		if err != nil {
			return err
		}
		updated = &change.Of[application.Application]{Before: *before, After: *after, At: after.UpdatedAt}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// Delete soft-deletes a live application, or returns store.ErrNotFound.
func (service *ApplicationService) Delete(ctx context.Context, applicationID int64) (*change.Of[application.Application], error) {
	var deleted *change.Of[application.Application]
	err := store.InTx(ctx, service.db, func(tx store.Tx) error {
		before, err := service.applicationViewer.Get(ctx, tx, applicationID)
		if err != nil {
			return err
		}
		if err := service.applicationWriter.Delete(ctx, tx, applicationID); err != nil {
			return err
		}
		after, err := service.applicationViewer.GetByIDIncludingDeleted(ctx, tx, applicationID)
		if err != nil {
			return fmt.Errorf("application: get deleted application: %w", err)
		}
		if after.DeletedAt.IsZero() {
			return fmt.Errorf("application: %d has no deleted_at after its delete", applicationID)
		}
		deleted = &change.Of[application.Application]{Before: *before, After: *after, At: after.DeletedAt}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return deleted, nil
}

// History returns an application's status changes, oldest first.
func (service *ApplicationService) History(ctx context.Context, applicationID int64) ([]application.StatusChange, error) {
	return service.applicationHistoryViewer.List(ctx, service.db, applicationID)
}

// SetStatus sets an application's status and records it as the user's
// change. Saving the status it already has records nothing: it isn't a
// change.
func (service *ApplicationService) SetStatus(ctx context.Context, applicationID int64, status application.Status) (*change.Of[application.Application], error) {
	var updated *change.Of[application.Application]
	err := store.InTx(ctx, service.db, func(tx store.Tx) error {
		before, err := service.applicationViewer.Get(ctx, tx, applicationID)
		if err != nil {
			return err
		}
		after, err := service.applicationWriter.SetStatus(ctx, tx, applicationID, status)
		if err != nil {
			return err
		}
		updated = &change.Of[application.Application]{Before: *before, After: *after, At: after.UpdatedAt}
		if before.Status == status {
			return nil
		}
		return service.applicationHistoryWriter.Insert(ctx, tx, after.ID, status, application.ChangedByUser)
	})
	if err != nil {
		return nil, fmt.Errorf("application: update status: %w", err)
	}
	return updated, nil
}

// StatusSince is when app entered its current status: its newest status
// history row, or CreatedAt for an application with no history (which
// only a write outside Swamp could leave).
func (service *ApplicationService) StatusSince(ctx context.Context, app application.Application) (time.Time, error) {
	latest, err := service.applicationHistoryViewer.Latest(ctx, service.db, app.ID)
	if errors.Is(err, store.ErrNotFound) {
		return app.CreatedAt, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return latest.ChangedAt, nil
}

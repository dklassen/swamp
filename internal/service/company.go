package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dklassen/swamp/internal/domain/change"
	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/domain/filter"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/repository"
	"github.com/dklassen/swamp/store"
)

type companyViewer interface {
	Get(ctx context.Context, r store.Runner, id int64) (*company.Company, error)
	GetIncludingDeleted(ctx context.Context, r store.Runner, id int64) (*company.Company, error)
	GetManyIncludingDeleted(ctx context.Context, r store.Runner, ids []int64) ([]company.Company, error)
	GetBySource(ctx context.Context, r store.Runner, source, sourceRef string) (*company.Company, error)
	ListActive(ctx context.Context, r store.Runner) ([]company.Company, error)
}

type companyWriter interface {
	Insert(ctx context.Context, r store.Runner, name, source, sourceRef string) (*company.Company, error)
	SetName(ctx context.Context, r store.Runner, id int64, name string) (*company.Company, error)
	SetDescription(ctx context.Context, r store.Runner, id int64, description string) (*company.Company, error)
	SoftDelete(ctx context.Context, r store.Runner, id int64) error
	Restore(ctx context.Context, r store.Runner, id int64) error
	RestoreWithName(ctx context.Context, r store.Runner, id int64, name string) (*company.Company, error)
	MarkFetched(ctx context.Context, r store.Runner, id int64) error
}

type syncLeaser interface {
	AcquireSyncLease(ctx context.Context, r store.Runner, id int64, token string, expiry time.Duration) (bool, error)
	ReleaseSyncLease(ctx context.Context, r store.Runner, id int64, token string) error
}

type filterViewer interface {
	List(ctx context.Context, r store.Runner, companyID int64) ([]filter.Filter, error)
}

type filterWriter interface {
	Insert(ctx context.Context, r store.Runner, companyID int64, f filter.Filter) error
	DeleteAll(ctx context.Context, r store.Runner, companyID int64) error
}

type companyPostingViewer interface {
	Get(ctx context.Context, r store.Runner, id int64) (*posting.Posting, error)
	ListOpenIDsByCompany(ctx context.Context, r store.Runner, companyID int64) ([]int64, error)
	Departments(ctx context.Context, r store.Runner, companyID int64) ([]string, error)
	Locations(ctx context.Context, r store.Runner, companyID int64) ([]string, error)
}

type companyPostingStatusWriter interface {
	Close(ctx context.Context, r store.Runner, id int64) (bool, error)
}

type companyPostingHistoryWriter interface {
	Insert(ctx context.Context, r store.Runner, p posting.Posting, changeType string) (*posting.History, error)
}

type CompanyService struct {
	db                   store.Store
	companyViewer        companyViewer
	companyWriter        companyWriter
	syncLeaser           syncLeaser
	filterViewer         filterViewer
	filterWriter         filterWriter
	postingViewer        companyPostingViewer
	postingStatusWriter  companyPostingStatusWriter
	postingHistoryWriter companyPostingHistoryWriter
}

func NewCompanyService(db store.Store) *CompanyService {
	companies := &repository.CompanyRepository{}
	filters := &repository.CompanyFilterRepository{}
	postings := &repository.PostingRepository{}
	return &CompanyService{
		db:                   db,
		companyViewer:        companies,
		companyWriter:        companies,
		syncLeaser:           companies,
		filterViewer:         filters,
		filterWriter:         filters,
		postingViewer:        postings,
		postingStatusWriter:  postings,
		postingHistoryWriter: &repository.PostingHistoryRepository{},
	}
}

// Add adds a company, or -- if one with this (source, source_ref) already
// exists, active or soft-deleted -- restores and renames that existing row
// instead. source+source_ref is unique regardless of deleted_at, so a
// plain insert would fail when re-adding a company the user had removed;
// restoring is what "re-add" means for a soft-deleted row.
func (service *CompanyService) Add(ctx context.Context, name, source, sourceRef string) (*company.Company, error) {
	var added *company.Company
	err := store.InTx(ctx, service.db, func(tx store.Tx) error {
		existing, err := service.companyViewer.GetBySource(ctx, tx, source, sourceRef)
		switch {
		case errors.Is(err, store.ErrNotFound):
			if added, err = service.companyWriter.Insert(ctx, tx, name, source, sourceRef); err != nil {
				return fmt.Errorf("company: create: %w", err)
			}
		case err != nil:
			return fmt.Errorf("company: get by source: %w", err)
		default:
			if added, err = service.companyWriter.RestoreWithName(ctx, tx, existing.ID, name); err != nil {
				return fmt.Errorf("company: restore: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return added, nil
}

// Delete marks a company deleted and closes its open postings, with a
// "closed" posting history row each, in one transaction. A deleted company
// is never synced, so nothing else would ever close them and they'd count
// as open for good. Applications are left alone: they're the user's, not
// the board's. Re-adding the company restores it (see Add), and its next
// sync reopens whatever is still listed. A company that's missing or
// already deleted is store.ErrNotFound.
func (service *CompanyService) Delete(ctx context.Context, id int64) (*change.Of[company.Company], error) {
	var deleted *change.Of[company.Company]
	err := store.InTx(ctx, service.db, func(tx store.Tx) error {
		before, err := service.companyViewer.Get(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := service.companyWriter.SoftDelete(ctx, tx, id); err != nil {
			return err
		}
		postingIDs, err := service.postingViewer.ListOpenIDsByCompany(ctx, tx, id)
		if err != nil {
			return fmt.Errorf("company: list open postings of deleted company: %w", err)
		}
		for _, postingID := range postingIDs {
			open, err := service.postingViewer.Get(ctx, tx, postingID)
			if err != nil {
				return fmt.Errorf("posting: get posting to close: %w", err)
			}
			closed, err := service.postingStatusWriter.Close(ctx, tx, postingID)
			if err != nil {
				return fmt.Errorf("posting: close: %w", err)
			}
			if !closed {
				continue
			}
			if _, err := service.postingHistoryWriter.Insert(ctx, tx, *open, posting.ChangeClosed); err != nil {
				return err
			}
		}
		after, err := service.companyViewer.GetIncludingDeleted(ctx, tx, id)
		if err != nil {
			return fmt.Errorf("company: get deleted company: %w", err)
		}
		if after.DeletedAt == nil {
			return fmt.Errorf("company: %d has no deleted_at after its delete", id)
		}
		deleted = &change.Of[company.Company]{Before: *before, After: *after, At: *after.DeletedAt}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return deleted, nil
}

// Filters returns the filters that decide which of the company's postings
// its list shows, in the order they were saved.
func (service *CompanyService) Filters(ctx context.Context, id int64) ([]filter.Filter, error) {
	return service.filterViewer.List(ctx, service.db, id)
}

// Departments returns the distinct, non-empty departments across the
// company's postings, the values a department filter can pick from.
func (service *CompanyService) Departments(ctx context.Context, id int64) ([]string, error) {
	return service.postingViewer.Departments(ctx, service.db, id)
}

// Locations is Departments for locations.
func (service *CompanyService) Locations(ctx context.Context, id int64) ([]string, error) {
	return service.postingViewer.Locations(ctx, service.db, id)
}

// SetFilters saves filters as the company's filters, in place of any it
// had, in one transaction so a failed insert never leaves the delete
// behind.
func (service *CompanyService) SetFilters(ctx context.Context, id int64, filters []filter.Filter) error {
	err := store.InTx(ctx, service.db, func(tx store.Tx) error {
		if err := service.filterWriter.DeleteAll(ctx, tx, id); err != nil {
			return err
		}
		for _, f := range filters {
			if err := service.filterWriter.Insert(ctx, tx, id, f); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("companyfilter: replace: %w", err)
	}
	return nil
}

// Get returns an active company, or store.ErrNotFound.
func (service *CompanyService) Get(ctx context.Context, id int64) (*company.Company, error) {
	return service.companyViewer.Get(ctx, service.db, id)
}

// GetIncludingDeleted is Get, but also finds a company the user deleted
// (check DeletedAt).
func (service *CompanyService) GetIncludingDeleted(ctx context.Context, id int64) (*company.Company, error) {
	return service.companyViewer.GetIncludingDeleted(ctx, service.db, id)
}

// GetManyIncludingDeleted returns the companies with the given ids,
// deleted or not (check DeletedAt), in no order. An id with no company is
// left out.
func (service *CompanyService) GetManyIncludingDeleted(ctx context.Context, ids []int64) ([]company.Company, error) {
	return service.companyViewer.GetManyIncludingDeleted(ctx, service.db, ids)
}

// GetBySource finds the company for a job board slug, including a
// soft-deleted one (check DeletedAt), or returns store.ErrNotFound.
func (service *CompanyService) GetBySource(ctx context.Context, source, sourceRef string) (*company.Company, error) {
	return service.companyViewer.GetBySource(ctx, service.db, source, sourceRef)
}

func (service *CompanyService) ListActive(ctx context.Context) ([]company.Company, error) {
	return service.companyViewer.ListActive(ctx, service.db)
}

// SetName renames an active company, or returns store.ErrNotFound.
func (service *CompanyService) SetName(ctx context.Context, id int64, name string) (*change.Of[company.Company], error) {
	var renamed *change.Of[company.Company]
	err := store.InTx(ctx, service.db, func(tx store.Tx) error {
		before, err := service.companyViewer.Get(ctx, tx, id)
		if err != nil {
			return err
		}
		after, err := service.companyWriter.SetName(ctx, tx, id, name)
		if err != nil {
			return err
		}
		renamed = &change.Of[company.Company]{Before: *before, After: *after, At: after.UpdatedAt}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return renamed, nil
}

func (service *CompanyService) SetDescription(ctx context.Context, id int64, description string) (*company.Company, error) {
	return service.companyWriter.SetDescription(ctx, service.db, id, description)
}

func (service *CompanyService) Restore(ctx context.Context, id int64) error {
	return service.companyWriter.Restore(ctx, service.db, id)
}

// MarkFetched records that id's postings were just fetched successfully.
func (service *CompanyService) MarkFetched(ctx context.Context, id int64) error {
	return service.companyWriter.MarkFetched(ctx, service.db, id)
}

// AcquireSyncLease takes id's sync lease for token if it's free or its
// holder took it more than expiry ago, and reports whether it did.
func (service *CompanyService) AcquireSyncLease(ctx context.Context, id int64, token string, expiry time.Duration) (bool, error) {
	return service.syncLeaser.AcquireSyncLease(ctx, service.db, id, token, expiry)
}

// ReleaseSyncLease frees id's sync lease if token still holds it.
func (service *CompanyService) ReleaseSyncLease(ctx context.Context, id int64, token string) error {
	return service.syncLeaser.ReleaseSyncLease(ctx, service.db, id, token)
}

package repository

import (
	"context"
	"database/sql"
	"fmt"

	domain "github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

type ApplicationRepository struct{}

// FromRow converts a generated applications row, parsing its status. A
// NULL status means something outside Swamp wrote the row: every write
// here supplies one.
func FromRow(row sqlcdb.Application) (domain.Application, error) {
	if !row.Status.Valid {
		return domain.Application{}, fmt.Errorf("application: application %d has NULL status", row.ID)
	}
	status, err := domain.ParseStatus(row.Status.String)
	if err != nil {
		return domain.Application{}, err
	}
	return domain.Application{
		ID:        row.ID,
		PostingID: row.PostingID,
		Status:    status,
		Notes:     row.Notes,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		DeletedAt: row.DeletedAt.Time,
	}, nil
}

func read(ctx context.Context, r store.Runner, fn func(*sqlcdb.Queries) (sqlcdb.Application, error)) (*domain.Application, error) {
	row, err := store.Read(ctx, r, fn)
	if err != nil {
		return nil, err
	}
	app, err := FromRow(row)
	if err != nil {
		return nil, err
	}
	return &app, nil
}

// Insert starts an application for postingID at status.
func (repository *ApplicationRepository) Insert(ctx context.Context, r store.Runner, postingID int64, status domain.Status) (*domain.Application, error) {
	return read(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.Application, error) {
		return q.CreateApplication(ctx, sqlcdb.CreateApplicationParams{
			PostingID: postingID,
			Status:    sql.NullString{String: status.String(), Valid: true},
		})
	})
}

// Get looks an application up by its own ID, unlike GetByPostingID.
// Returns store.ErrNotFound for an ID with no live row.
func (repository *ApplicationRepository) Get(ctx context.Context, r store.Runner, id int64) (*domain.Application, error) {
	return read(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.Application, error) {
		return q.GetApplicationByID(ctx, id)
	})
}

// GetByPostingID returns postingID's live application, or
// store.ErrNotFound.
func (repository *ApplicationRepository) GetByPostingID(ctx context.Context, r store.Runner, postingID int64) (*domain.Application, error) {
	return read(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.Application, error) {
		return q.GetApplication(ctx, postingID)
	})
}

// GetByIDIncludingDeleted is Get, but also finds an application the user
// deleted (check DeletedAt). Returns store.ErrNotFound for an ID that was
// never handed out.
func (repository *ApplicationRepository) GetByIDIncludingDeleted(ctx context.Context, r store.Runner, id int64) (*domain.Application, error) {
	return read(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.Application, error) {
		return q.GetApplicationByIDIncludingDeleted(ctx, id)
	})
}

// GetManyIncludingDeleted returns the applications with the given ids
// that exist, deleted or not, in no order.
func (repository *ApplicationRepository) GetManyIncludingDeleted(ctx context.Context, r store.Runner, ids []int64) ([]domain.Application, error) {
	rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.Application, error) {
		return q.ListApplicationsByIDIncludingDeleted(ctx, ids)
	})
	if err != nil {
		return nil, err
	}
	apps := make([]domain.Application, len(rows))
	for i, row := range rows {
		if apps[i], err = FromRow(row); err != nil {
			return nil, err
		}
	}
	return apps, nil
}

// ListActive returns the applications not at one of excluding, each with
// its posting and company name, most-recently-changed first. StatusSince
// and LatestReviews are left for the caller to fill.
func (repository *ApplicationRepository) ListActive(ctx context.Context, r store.Runner, excluding []domain.Status) ([]posting.ActiveApplication, error) {
	rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.ListActiveApplicationsRow, error) {
		return q.ListActiveApplications(ctx, statusParams(excluding))
	})
	if err != nil {
		return nil, err
	}
	apps := make([]posting.ActiveApplication, len(rows))
	for i, row := range rows {
		app, err := FromRow(row.Application)
		if err != nil {
			return nil, err
		}
		p, err := FromPostingRow(row.Posting)
		if err != nil {
			return nil, err
		}
		apps[i] = posting.ActiveApplication{
			Application: app,
			Posting:     p,
			CompanyName: row.CompanyName,
		}
	}
	return apps, nil
}

// Delete soft-deletes an application: it disappears from every lookup and
// list, and the posting can start a fresh one, but its status history,
// reviews, exports and interview stages stay, as do its documents on disk.
// It returns store.ErrNotFound for an ID with no live application.
func (repository *ApplicationRepository) Delete(ctx context.Context, r store.Runner, id int64) error {
	n, err := store.Read(ctx, r, func(q *sqlcdb.Queries) (int64, error) {
		return q.SoftDeleteApplication(ctx, id)
	})
	if err != nil {
		return fmt.Errorf("application: delete: %w", err)
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (repository *ApplicationRepository) SetStatus(ctx context.Context, r store.Runner, id int64, status domain.Status) (*domain.Application, error) {
	return read(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.Application, error) {
		return q.UpdateApplicationStatus(ctx, sqlcdb.UpdateApplicationStatusParams{
			ID:     id,
			Status: sql.NullString{String: status.String(), Valid: true},
		})
	})
}

func (repository *ApplicationRepository) SetNotes(ctx context.Context, r store.Runner, id int64, notes string) (*domain.Application, error) {
	return read(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.Application, error) {
		return q.UpdateApplicationNotes(ctx, sqlcdb.UpdateApplicationNotesParams{ID: id, Notes: notes})
	})
}

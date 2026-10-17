package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	domain "github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

// FromCompanyRow converts a generated companies row.
func FromCompanyRow(row sqlcdb.Company) domain.Company {
	c := domain.Company{
		ID:          row.ID,
		Name:        row.Name,
		Source:      row.Source,
		SourceRef:   row.SourceRef,
		Description: row.Description,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	if row.DeletedAt.Valid {
		c.DeletedAt = &row.DeletedAt.Time
	}
	if row.LastFetchedAt.Valid {
		c.LastFetchedAt = row.LastFetchedAt.Time
	}
	return c
}

func readCompany(ctx context.Context, r store.Runner, fn func(*sqlcdb.Queries) (sqlcdb.Company, error)) (*domain.Company, error) {
	row, err := store.Read(ctx, r, fn)
	if err != nil {
		return nil, err
	}
	c := FromCompanyRow(row)
	return &c, nil
}

type CompanyRepository struct{}

// Insert adds a company. (source, source_ref) is unique whether or not the
// company is deleted.
func (repository *CompanyRepository) Insert(ctx context.Context, r store.Runner, name, source, sourceRef string) (*domain.Company, error) {
	return readCompany(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.Company, error) {
		return q.CreateCompany(ctx, sqlcdb.CreateCompanyParams{Name: name, Source: source, SourceRef: sourceRef})
	})
}

// Get returns an active company, or store.ErrNotFound.
func (repository *CompanyRepository) Get(ctx context.Context, r store.Runner, id int64) (*domain.Company, error) {
	return readCompany(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.Company, error) {
		return q.GetCompany(ctx, id)
	})
}

// GetIncludingDeleted is Get, but also finds a company the user deleted
// (check DeletedAt).
func (repository *CompanyRepository) GetIncludingDeleted(ctx context.Context, r store.Runner, id int64) (*domain.Company, error) {
	return readCompany(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.Company, error) {
		return q.GetCompanyIncludingDeleted(ctx, id)
	})
}

// GetBySource finds the company for a job board slug, including a
// soft-deleted one (check DeletedAt), or returns store.ErrNotFound.
func (repository *CompanyRepository) GetBySource(ctx context.Context, r store.Runner, source, sourceRef string) (*domain.Company, error) {
	return readCompany(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.Company, error) {
		return q.GetCompanyBySourceAndSourceRef(ctx, sqlcdb.GetCompanyBySourceAndSourceRefParams{Source: source, SourceRef: sourceRef})
	})
}

func (repository *CompanyRepository) ListActive(ctx context.Context, r store.Runner) ([]domain.Company, error) {
	rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.Company, error) {
		return q.ListActiveCompanies(ctx)
	})
	if err != nil {
		return nil, err
	}
	companies := make([]domain.Company, len(rows))
	for i, row := range rows {
		companies[i] = FromCompanyRow(row)
	}
	return companies, nil
}

// SetName changes an active company's name only -- source and source_ref
// are immutable after creation: changing either really means "this is a
// different board," not "edit this company".
func (repository *CompanyRepository) SetName(ctx context.Context, r store.Runner, id int64, name string) (*domain.Company, error) {
	return readCompany(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.Company, error) {
		return q.UpdateCompanyName(ctx, sqlcdb.UpdateCompanyNameParams{ID: id, Name: name})
	})
}

// SetDescription sets who the company is (what it builds, stage, domain).
// Like SetName it excludes soft-deleted companies.
func (repository *CompanyRepository) SetDescription(ctx context.Context, r store.Runner, id int64, description string) (*domain.Company, error) {
	return readCompany(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.Company, error) {
		return q.UpdateCompanyDescription(ctx, sqlcdb.UpdateCompanyDescriptionParams{ID: id, Description: description})
	})
}

func (repository *CompanyRepository) SoftDelete(ctx context.Context, r store.Runner, id int64) error {
	return store.Write(ctx, r, func(q *sqlcdb.Queries) error {
		return q.SoftDeleteCompany(ctx, id)
	})
}

func (repository *CompanyRepository) Restore(ctx context.Context, r store.Runner, id int64) error {
	return store.Write(ctx, r, func(q *sqlcdb.Queries) error {
		return q.RestoreCompany(ctx, id)
	})
}

// RestoreWithName restores a company and renames it in one statement.
func (repository *CompanyRepository) RestoreWithName(ctx context.Context, r store.Runner, id int64, name string) (*domain.Company, error) {
	return readCompany(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.Company, error) {
		return q.RestoreCompanyWithName(ctx, sqlcdb.RestoreCompanyWithNameParams{ID: id, Name: name})
	})
}

// MarkFetched records that id's postings were just fetched successfully.
func (repository *CompanyRepository) MarkFetched(ctx context.Context, r store.Runner, id int64) error {
	return store.Write(ctx, r, func(q *sqlcdb.Queries) error {
		return q.MarkCompanyFetched(ctx, id)
	})
}

// AcquireSyncLease takes id's sync lease for token if it's free, or if its
// holder took it more than expiry ago (e.g. a process killed mid-sync), and
// reports whether it did. At most one holder at a time, across every
// process sharing the database.
func (repository *CompanyRepository) AcquireSyncLease(ctx context.Context, r store.Runner, id int64, token string, expiry time.Duration) (bool, error) {
	taken, err := store.Read(ctx, r, func(q *sqlcdb.Queries) (int64, error) {
		return q.AcquireCompanySyncLease(ctx, sqlcdb.AcquireCompanySyncLeaseParams{
			ID:            id,
			Token:         sql.NullString{String: token, Valid: true},
			ExpirySeconds: int64(expiry / time.Second),
		})
	})
	if err != nil {
		return false, fmt.Errorf("company: acquire sync lease: %w", err)
	}
	return taken == 1, nil
}

// ReleaseSyncLease frees id's sync lease if token still holds it.
// Releasing a lease token doesn't hold -- because it expired and was taken
// over -- changes nothing and isn't an error.
func (repository *CompanyRepository) ReleaseSyncLease(ctx context.Context, r store.Runner, id int64, token string) error {
	err := store.Write(ctx, r, func(q *sqlcdb.Queries) error {
		return q.ReleaseCompanySyncLease(ctx, sqlcdb.ReleaseCompanySyncLeaseParams{
			ID:    id,
			Token: sql.NullString{String: token, Valid: true},
		})
	})
	if err != nil {
		return fmt.Errorf("company: release sync lease: %w", err)
	}
	return nil
}

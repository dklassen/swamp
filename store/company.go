package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/dklassen/swamp/store/db"
)

type Company struct {
	ID        int64
	Name      string
	Source    string
	SourceRef string
	DeletedAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

func companyFromRow(row db.Company) Company {
	c := Company{
		ID:        row.ID,
		Name:      row.Name,
		Source:    row.Source,
		SourceRef: row.SourceRef,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
	if row.DeletedAt.Valid {
		c.DeletedAt = &row.DeletedAt.Time
	}
	return c
}

func (s *Store) CreateCompany(ctx context.Context, name, source, sourceRef string) (Company, error) {
	row, err := s.queries.CreateCompany(ctx, db.CreateCompanyParams{
		Name:      name,
		Source:    source,
		SourceRef: sourceRef,
	})
	if err != nil {
		return Company{}, err
	}
	return companyFromRow(row), nil
}

func (s *Store) SoftDeleteCompany(ctx context.Context, id int64) error {
	return s.queries.SoftDeleteCompany(ctx, id)
}

func (s *Store) RestoreCompany(ctx context.Context, id int64) error {
	return s.queries.RestoreCompany(ctx, id)
}

func (s *Store) ListActiveCompanies(ctx context.Context) ([]Company, error) {
	rows, err := s.queries.ListActiveCompanies(ctx)
	if err != nil {
		return nil, err
	}
	companies := make([]Company, len(rows))
	for i, row := range rows {
		companies[i] = companyFromRow(row)
	}
	return companies, nil
}

func (s *Store) GetCompany(ctx context.Context, id int64) (Company, error) {
	row, err := s.queries.GetCompany(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Company{}, ErrNotFound
		}
		return Company{}, err
	}
	return companyFromRow(row), nil
}

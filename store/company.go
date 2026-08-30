package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

// CreateCompany adds a company, or -- if one with this (source, source_ref)
// already exists, active or soft-deleted -- restores/renames that existing
// row instead. source+source_ref is UNIQUE regardless of deleted_at, so a
// plain insert would fail with a constraint violation when re-adding a
// company the user had previously removed; restoring is what "re-add"
// actually means for a soft-deleted row.
func (s *Store) CreateCompany(ctx context.Context, name, source, sourceRef string) (Company, error) {
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return Company{}, fmt.Errorf("store: begin create company tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := s.queries.WithTx(tx)

	existing, err := qtx.GetCompanyBySourceAndSourceRef(ctx, db.GetCompanyBySourceAndSourceRefParams{
		Source:    source,
		SourceRef: sourceRef,
	})

	var row db.Company
	switch {
	case errors.Is(err, sql.ErrNoRows):
		row, err = qtx.CreateCompany(ctx, db.CreateCompanyParams{
			Name:      name,
			Source:    source,
			SourceRef: sourceRef,
		})
		if err != nil {
			return Company{}, fmt.Errorf("store: create company: %w", err)
		}
	case err != nil:
		return Company{}, fmt.Errorf("store: get company by source: %w", err)
	default:
		row, err = qtx.RestoreCompanyWithName(ctx, db.RestoreCompanyWithNameParams{
			Name: name,
			ID:   existing.ID,
		})
		if err != nil {
			return Company{}, fmt.Errorf("store: restore company: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return Company{}, fmt.Errorf("store: commit create company tx: %w", err)
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

// UpdateCompanyName changes an active company's name only -- source and
// source_ref are immutable after creation (see decisions.log: changing
// either really means "this is a different board," not "edit this
// company").
func (s *Store) UpdateCompanyName(ctx context.Context, id int64, name string) (Company, error) {
	row, err := s.queries.UpdateCompanyName(ctx, db.UpdateCompanyNameParams{ID: id, Name: name})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Company{}, ErrNotFound
		}
		return Company{}, err
	}
	return companyFromRow(row), nil
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

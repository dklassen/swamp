package store

import (
	"context"
	"time"

	"github.com/dklassen/swamp/store/db"
)

// CompanyFilter is one field/value constraint on a company's postings.
// Multiple rows for the same (company, field) OR together; distinct
// fields AND together. See db/migrations/00001_initial_schema.sql for the
// full matching semantics.
type CompanyFilter struct {
	ID        int64
	CompanyID int64
	Field     string
	Value     string
	CreatedAt time.Time
}

func companyFilterFromRow(row db.CompanyFilter) CompanyFilter {
	return CompanyFilter{
		ID:        row.ID,
		CompanyID: row.CompanyID,
		Field:     row.Field,
		Value:     row.Value,
		CreatedAt: row.CreatedAt,
	}
}

func (s *Store) CreateCompanyFilter(ctx context.Context, companyID int64, field, value string) (CompanyFilter, error) {
	row, err := s.queries.CreateCompanyFilter(ctx, db.CreateCompanyFilterParams{
		CompanyID: companyID,
		Field:     field,
		Value:     value,
	})
	if err != nil {
		return CompanyFilter{}, err
	}
	return companyFilterFromRow(row), nil
}

func (s *Store) ListCompanyFilters(ctx context.Context, companyID int64) ([]CompanyFilter, error) {
	rows, err := s.queries.ListCompanyFilters(ctx, companyID)
	if err != nil {
		return nil, err
	}
	filters := make([]CompanyFilter, len(rows))
	for i, row := range rows {
		filters[i] = companyFilterFromRow(row)
	}
	return filters, nil
}

// DeleteCompanyFilters removes every filter row for a company. Used by
// ReplaceCompanyFilters (delete all existing filters, then create the new
// set, rather than diffing individual rows) -- exported separately since
// it's also useful on its own.
func (s *Store) DeleteCompanyFilters(ctx context.Context, companyID int64) error {
	return s.queries.DeleteCompanyFilters(ctx, companyID)
}

// ReplaceCompanyFilters replaces companyID's saved filters with filters
// wholesale -- only each element's Field/Value is read, so callers can
// pass freshly-built CompanyFilter values with no ID/CompanyID/CreatedAt
// set. Deliberately field-name-agnostic (unlike sync, this package has
// no dependency on the filter package's FieldDepartment/FieldLocation
// constants -- see filter's own doc comment on why store/sync/filter's
// dependency direction is one-way); the caller decides what Field values
// mean (see decisions.log, #56).
func (s *Store) ReplaceCompanyFilters(ctx context.Context, companyID int64, filters []CompanyFilter) error {
	if err := s.DeleteCompanyFilters(ctx, companyID); err != nil {
		return err
	}
	for _, f := range filters {
		if _, err := s.CreateCompanyFilter(ctx, companyID, f.Field, f.Value); err != nil {
			return err
		}
	}
	return nil
}

package repository

import (
	"context"
	"fmt"

	"github.com/dklassen/swamp/internal/domain/filter"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

type CompanyFilterRepository struct{}

// List returns companyID's saved filters, in the order they were saved.
func (repository *CompanyFilterRepository) List(ctx context.Context, r store.Runner, companyID int64) ([]filter.Filter, error) {
	rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.CompanyFilter, error) {
		return q.ListCompanyFilters(ctx, companyID)
	})
	if err != nil {
		return nil, fmt.Errorf("companyfilter: list: %w", err)
	}
	filters := make([]filter.Filter, len(rows))
	for i, row := range rows {
		filters[i] = filter.Filter{Field: row.Field, Value: row.Value}
	}
	return filters, nil
}

// Insert adds one filter to companyID's filters.
func (repository *CompanyFilterRepository) Insert(ctx context.Context, r store.Runner, companyID int64, f filter.Filter) error {
	return store.Write(ctx, r, func(q *sqlcdb.Queries) error {
		_, err := q.CreateCompanyFilter(ctx, sqlcdb.CreateCompanyFilterParams{CompanyID: companyID, Field: f.Field, Value: f.Value})
		return err
	})
}

// DeleteAll removes every filter companyID has.
func (repository *CompanyFilterRepository) DeleteAll(ctx context.Context, r store.Runner, companyID int64) error {
	return store.Write(ctx, r, func(q *sqlcdb.Queries) error {
		return q.DeleteCompanyFilters(ctx, companyID)
	})
}

// Package companyfilter reads and saves the filters that decide which of a
// company's postings its list shows.
package companyfilter

import (
	"context"
	"fmt"

	"github.com/dklassen/swamp/filter"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

// List returns companyID's saved filters, in the order they were saved.
func List(ctx context.Context, r store.Runner, companyID int64) ([]filter.Filter, error) {
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

// Replace saves filters as companyID's filters, in place of any it had.
func Replace(ctx context.Context, tx store.Tx, companyID int64, filters []filter.Filter) error {
	// A savepoint, so a failed insert never leaves the delete behind, even
	// when the caller ignores the error and commits.
	err := store.InTx(ctx, tx, func(tx store.Tx) error {
		return store.Write(ctx, tx, func(q *sqlcdb.Queries) error {
			if err := q.DeleteCompanyFilters(ctx, companyID); err != nil {
				return err
			}
			for _, f := range filters {
				if _, err := q.CreateCompanyFilter(ctx, sqlcdb.CreateCompanyFilterParams{CompanyID: companyID, Field: f.Field, Value: f.Value}); err != nil {
					return err
				}
			}
			return nil
		})
	})
	if err != nil {
		return fmt.Errorf("companyfilter: replace: %w", err)
	}
	return nil
}

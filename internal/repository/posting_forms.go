package repository

import (
	"context"
	"fmt"

	domain "github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

// PostingForms stores the application form each posting asks for.
type PostingForms struct{}

// Save stores what postingID's application form asks for, as JSON
// (a marshalled jobboard.ApplicationForm), replacing any earlier fetch
// and stamping it fetched now.
func (PostingForms) Save(ctx context.Context, r store.Runner, postingID int64, formJSON string) error {
	err := store.Write(ctx, r, func(q *sqlcdb.Queries) error {
		return q.SavePostingApplicationForm(ctx, sqlcdb.SavePostingApplicationFormParams{PostingID: postingID, Form: formJSON})
	})
	if err != nil {
		return fmt.Errorf("application: save form: %w", err)
	}
	return nil
}

// Get returns postingID's stored application form, or store.ErrNotFound
// when it was never fetched.
func (PostingForms) Get(ctx context.Context, r store.Runner, postingID int64) (*domain.Form, error) {
	row, err := store.Read(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.PostingApplicationForm, error) {
		return q.GetPostingApplicationForm(ctx, postingID)
	})
	if err != nil {
		return nil, fmt.Errorf("application: get form: %w", err)
	}
	return &domain.Form{PostingID: row.PostingID, JSON: row.Form, FetchedAt: row.FetchedAt}, nil
}

// ListOpenWithout returns companyID's open postings that have no stored
// form, by ID.
func (PostingForms) ListOpenWithout(ctx context.Context, r store.Runner, companyID int64) ([]domain.Posting, error) {
	rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.Posting, error) {
		return q.ListOpenPostingsWithoutForm(ctx, companyID)
	})
	if err != nil {
		return nil, fmt.Errorf("application: list postings without a form: %w", err)
	}
	postings := make([]domain.Posting, len(rows))
	for i, row := range rows {
		if postings[i], err = FromPostingRow(row); err != nil {
			return nil, err
		}
	}
	return postings, nil
}

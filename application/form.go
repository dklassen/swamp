package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

// SaveForm stores what postingID's application form asks for, as JSON
// (stage marshals jobboard.ApplicationForm), replacing any earlier fetch
// and stamping it fetched now.
func SaveForm(ctx context.Context, r store.Runner, postingID int64, formJSON string) error {
	err := store.Write(ctx, r, func(q *sqlcdb.Queries) error {
		return q.SavePostingApplicationForm(ctx, sqlcdb.SavePostingApplicationFormParams{PostingID: postingID, Form: formJSON})
	})
	if err != nil {
		return fmt.Errorf("application: save form: %w", err)
	}
	return nil
}

// GetForm returns postingID's stored application form JSON and when it
// was fetched. ok is false when it was never fetched.
func GetForm(ctx context.Context, r store.Runner, postingID int64) (formJSON string, fetchedAt time.Time, ok bool, err error) {
	row, err := store.Read(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.PostingApplicationForm, error) {
		return q.GetPostingApplicationForm(ctx, postingID)
	})
	if errors.Is(err, store.ErrNotFound) {
		return "", time.Time{}, false, nil
	}
	if err != nil {
		return "", time.Time{}, false, fmt.Errorf("application: get form: %w", err)
	}
	return row.Form, row.FetchedAt, true, nil
}

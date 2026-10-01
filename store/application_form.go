package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dklassen/swamp/store/db"
)

// SaveApplicationForm stores what postingID's application form asks for
// (#168), as JSON (stage marshals jobboard.ApplicationForm), replacing
// any earlier fetch and stamping it fetched now.
func (s *Store) SaveApplicationForm(ctx context.Context, postingID int64, formJSON string) error {
	if err := s.queries.SavePostingApplicationForm(ctx, db.SavePostingApplicationFormParams{
		PostingID: postingID,
		Form:      formJSON,
	}); err != nil {
		return fmt.Errorf("store: save application form: %w", err)
	}
	return nil
}

// GetApplicationForm returns postingID's stored application form JSON and
// when it was fetched. ok is false when it was never fetched.
func (s *Store) GetApplicationForm(ctx context.Context, postingID int64) (formJSON string, fetchedAt time.Time, ok bool, err error) {
	row, err := s.queries.GetPostingApplicationForm(ctx, postingID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, false, nil
	}
	if err != nil {
		return "", time.Time{}, false, fmt.Errorf("store: get application form: %w", err)
	}
	return row.Form, row.FetchedAt, true, nil
}

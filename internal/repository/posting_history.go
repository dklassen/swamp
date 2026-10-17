package repository

import (
	"context"
	"encoding/json"
	"fmt"

	domain "github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

// FromPostingHistoryRow converts a generated posting_history row.
func FromPostingHistoryRow(row sqlcdb.PostingHistory) domain.History {
	return domain.History{
		ID:         row.ID,
		PostingID:  row.PostingID,
		ChangeType: row.ChangeType,
		Snapshot:   row.Snapshot,
		RecordedAt: row.RecordedAt,
	}
}

type PostingHistoryRepository struct{}

// Insert records posting as it was before a changeType change, as a JSON
// snapshot.
func (repository *PostingHistoryRepository) Insert(ctx context.Context, r store.Runner, posting domain.Posting, changeType string) (domain.History, error) {
	snapshot, err := json.Marshal(posting)
	if err != nil {
		return domain.History{}, fmt.Errorf("posting: marshal snapshot: %w", err)
	}
	row, err := store.Read(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.PostingHistory, error) {
		return q.CreatePostingHistory(ctx, sqlcdb.CreatePostingHistoryParams{
			PostingID:  posting.ID,
			ChangeType: changeType,
			Snapshot:   string(snapshot),
		})
	})
	if err != nil {
		return domain.History{}, fmt.Errorf("posting: record %s: %w", changeType, err)
	}
	return FromPostingHistoryRow(row), nil
}

// List returns a posting's history, oldest first.
func (repository *PostingHistoryRepository) List(ctx context.Context, r store.Runner, postingID int64) ([]domain.History, error) {
	rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.PostingHistory, error) {
		return q.ListPostingHistoryByPosting(ctx, postingID)
	})
	if err != nil {
		return nil, err
	}
	history := make([]domain.History, len(rows))
	for i, row := range rows {
		history[i] = FromPostingHistoryRow(row)
	}
	return history, nil
}

package store

import (
	"context"
	"time"

	"github.com/dklassen/swamp/store/db"
)

// PostingHistory is a snapshot of a posting's prior ingested state,
// written whenever a re-fetch detects a diff in an ingested field, or the
// posting's listing_status transitions.
type PostingHistory struct {
	ID         int64
	PostingID  int64
	ChangeType string
	Snapshot   string
	RecordedAt time.Time
}

func postingHistoryFromRow(row db.PostingHistory) PostingHistory {
	return PostingHistory{
		ID:         row.ID,
		PostingID:  row.PostingID,
		ChangeType: row.ChangeType,
		Snapshot:   row.Snapshot,
		RecordedAt: row.RecordedAt,
	}
}

func (s *Store) CreatePostingHistory(ctx context.Context, postingID int64, changeType, snapshot string) (PostingHistory, error) {
	row, err := s.queries.CreatePostingHistory(ctx, db.CreatePostingHistoryParams{
		PostingID:  postingID,
		ChangeType: changeType,
		Snapshot:   snapshot,
	})
	if err != nil {
		return PostingHistory{}, err
	}
	return postingHistoryFromRow(row), nil
}

func (s *Store) ListPostingHistory(ctx context.Context, postingID int64) ([]PostingHistory, error) {
	rows, err := s.queries.ListPostingHistoryByPosting(ctx, postingID)
	if err != nil {
		return nil, err
	}
	history := make([]PostingHistory, len(rows))
	for i, row := range rows {
		history[i] = postingHistoryFromRow(row)
	}
	return history, nil
}

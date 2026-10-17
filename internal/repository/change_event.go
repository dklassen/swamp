package repository

import (
	"context"
	"fmt"

	domain "github.com/dklassen/swamp/internal/domain/change"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

type ChangeEventRepository struct{}

// Latest is the newest event's ID, 0 for an empty log.
func (repository *ChangeEventRepository) Latest(ctx context.Context, r store.Runner) (int64, error) {
	latest, err := store.Read(ctx, r, func(q *sqlcdb.Queries) (int64, error) {
		return q.LatestChangeEventID(ctx)
	})
	if err != nil {
		return 0, fmt.Errorf("change: latest event: %w", err)
	}
	return latest, nil
}

// After returns the events after id, oldest first.
func (repository *ChangeEventRepository) After(ctx context.Context, r store.Runner, id int64) ([]domain.Event, error) {
	rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.ChangeEvent, error) {
		return q.ChangeEventsAfter(ctx, id)
	})
	if err != nil {
		return nil, fmt.Errorf("change: events: %w", err)
	}
	events := make([]domain.Event, len(rows))
	for i, row := range rows {
		events[i] = domain.Event{
			ID:     row.ID,
			Table:  row.TableName,
			RowID:  row.RowID,
			Op:     row.Op,
			Old:    row.Old.String,
			New:    row.New.String,
			Origin: row.Origin.String,
			At:     row.At,
		}
	}
	return events, nil
}

package store

import (
	"context"
	"fmt"
	"time"
)

// ChangeEvent is one change to a logged table, as change_events records
// it. Old and New are JSON of the logged columns ("" for an insert's Old
// and a delete's New). Origin is the writing process's "kind:pid", ""
// for a writer outside Swamp.
type ChangeEvent struct {
	ID     int64
	Table  string
	RowID  int64
	Op     string
	Old    string
	New    string
	Origin string
	At     time.Time
}

// ChangeFeed reads change events in order, each once.
type ChangeFeed struct {
	store  *Store
	cursor int64
}

// NewChangeFeed starts after the newest event: a process has already
// loaded what changed before it started.
func (s *Store) NewChangeFeed(ctx context.Context) (*ChangeFeed, error) {
	latest, err := s.LatestChangeEventID(ctx)
	if err != nil {
		return nil, err
	}
	return &ChangeFeed{store: s, cursor: latest}, nil
}

// Next returns the events since the last call, oldest first, or none.
// Nothing committed meanwhile is missed, however long since that was.
func (f *ChangeFeed) Next(ctx context.Context) ([]ChangeEvent, error) {
	events, err := f.store.ChangeEventsAfter(ctx, f.cursor)
	if err != nil {
		return nil, err
	}
	if len(events) > 0 {
		f.cursor = events[len(events)-1].ID
	}
	return events, nil
}

// LatestChangeEventID is the newest event's ID, 0 for an empty log.
func (s *Store) LatestChangeEventID(ctx context.Context) (int64, error) {
	latest, err := s.queries.LatestChangeEventID(ctx)
	if err != nil {
		return 0, fmt.Errorf("store: latest change event: %w", err)
	}
	return latest, nil
}

// ChangeEventsAfter returns the events after id, oldest first.
func (s *Store) ChangeEventsAfter(ctx context.Context, id int64) ([]ChangeEvent, error) {
	rows, err := s.queries.ChangeEventsAfter(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("store: change events: %w", err)
	}
	events := make([]ChangeEvent, len(rows))
	for i, r := range rows {
		events[i] = ChangeEvent{
			ID:     r.ID,
			Table:  r.TableName,
			RowID:  r.RowID,
			Op:     r.Op,
			Old:    r.Old.String,
			New:    r.New.String,
			Origin: r.Origin.String,
			At:     r.At,
		}
	}
	return events, nil
}

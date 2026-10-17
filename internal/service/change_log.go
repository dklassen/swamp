package service

import (
	"context"

	"github.com/dklassen/swamp/internal/domain/change"
	"github.com/dklassen/swamp/internal/repository"
	"github.com/dklassen/swamp/store"
)

type changeEventViewer interface {
	Latest(ctx context.Context, r store.Runner) (int64, error)
	After(ctx context.Context, r store.Runner, id int64) ([]change.Event, error)
}

// ChangeLog reads what Swamp's processes changed, as change_events
// records it. It keeps no place in the log: each reader keeps its own.
type ChangeLog struct {
	db     store.Store
	events changeEventViewer
}

func NewChangeLog(db store.Store) *ChangeLog {
	return &ChangeLog{db: db, events: &repository.ChangeEventRepository{}}
}

// Latest is the newest event's ID, 0 for an empty log.
func (service *ChangeLog) Latest(ctx context.Context) (int64, error) {
	return service.events.Latest(ctx, service.db)
}

// After returns the events after id, oldest first. Nothing committed
// meanwhile is missed, however long since that was.
func (service *ChangeLog) After(ctx context.Context, id int64) ([]change.Event, error) {
	return service.events.After(ctx, service.db, id)
}

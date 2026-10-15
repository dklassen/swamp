package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dklassen/swamp/applicationstatus"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/store/sqlcdb"
)

// StatusChange is one status an application entered, and when.
// Append-only: every write path that sets applications.status records one
// in the same transaction, so an application's history is complete from
// the migration that introduced it (00014) onward. Rows before that are a
// backfill of created_at/updated_at only.
type StatusChange struct {
	ID            int64
	ApplicationID int64
	Status        applicationstatus.Status
	ChangedBy     ChangedBy
	ChangedAt     time.Time
}

// ChangedBy says who made an application status change. Like
// applicationstatus.Status, Go is the source of truth for the values; the
// changed_by column is plain TEXT.
type ChangedBy int

const (
	// ChangedByUnknown: recorded before changed_by existed (00018).
	ChangedByUnknown ChangedBy = iota
	// ChangedByUser: the TUI or an MCP tool, on the user's behalf.
	ChangedByUser
	// ChangedBySync: a sync closing or reopening the posting.
	ChangedBySync
)

// changedByNames is the single Go<->DB string mapping for ChangedBy,
// indexed by its int value.
var changedByNames = [...]string{
	ChangedByUnknown: "unknown",
	ChangedByUser:    "user",
	ChangedBySync:    "sync",
}

func (c ChangedBy) String() string {
	if c < 0 || int(c) >= len(changedByNames) {
		return fmt.Sprintf("ChangedBy(%d)", int(c))
	}
	return changedByNames[c]
}

// parseChangedBy converts a changed_by column value, failing loudly on one
// it doesn't know.
func parseChangedBy(s string) (ChangedBy, error) {
	for i, name := range changedByNames {
		if name == s {
			return ChangedBy(i), nil
		}
	}
	return 0, fmt.Errorf("application: unknown status changed_by %q", s)
}

// recordStatus appends a status history row on tx, so it commits or rolls
// back with the status change it records; every path that writes
// applications.status goes through here so they can't drift apart.
func recordStatus(ctx context.Context, tx store.Tx, applicationID int64, status applicationstatus.Status, changedBy ChangedBy) error {
	err := store.Write(ctx, tx, func(q *sqlcdb.Queries) error {
		return q.CreateApplicationStatusHistory(ctx, sqlcdb.CreateApplicationStatusHistoryParams{
			ApplicationID: applicationID,
			Status:        status.String(),
			ChangedBy:     changedBy.String(),
		})
	})
	if err != nil {
		return fmt.Errorf("record application %d status %s: %w", applicationID, status, err)
	}
	return nil
}

// ListStatusHistory returns an application's status changes, oldest
// first.
func ListStatusHistory(ctx context.Context, r store.Runner, applicationID int64) ([]StatusChange, error) {
	rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.ApplicationStatusHistory, error) {
		return q.ListApplicationStatusHistory(ctx, applicationID)
	})
	if err != nil {
		return nil, fmt.Errorf("application: status history: %w", err)
	}
	history := make([]StatusChange, len(rows))
	for i, row := range rows {
		if history[i], err = parseStatusChange(row); err != nil {
			return nil, err
		}
	}
	return history, nil
}

// StatusSince is when app entered its current status: its newest status
// history row, or CreatedAt for an application with no history (which
// only a write outside Swamp could leave).
func StatusSince(ctx context.Context, r store.Runner, app Application) (time.Time, error) {
	row, err := store.Read(ctx, r, func(q *sqlcdb.Queries) (sqlcdb.ApplicationStatusHistory, error) {
		return q.LatestApplicationStatusChange(ctx, app.ID)
	})
	if errors.Is(err, store.ErrNotFound) {
		return app.CreatedAt, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("application: latest status change for application %d: %w", app.ID, err)
	}
	return row.ChangedAt, nil
}

// parseStatusChange converts a status history row, failing on a status or
// changed_by value it doesn't know.
func parseStatusChange(row sqlcdb.ApplicationStatusHistory) (StatusChange, error) {
	status, err := applicationstatus.Parse(row.Status)
	if err != nil {
		return StatusChange{}, err
	}
	changedBy, err := parseChangedBy(row.ChangedBy)
	if err != nil {
		return StatusChange{}, err
	}
	return StatusChange{
		ID:            row.ID,
		ApplicationID: row.ApplicationID,
		Status:        status,
		ChangedBy:     changedBy,
		ChangedAt:     row.ChangedAt,
	}, nil
}

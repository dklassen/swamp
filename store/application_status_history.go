package store

import (
	"context"
	"fmt"
	"time"

	"github.com/dklassen/swamp/store/db"
)

// ApplicationStatusChange is one status an application entered, and when
// (#162). Append-only: every write path that sets applications.status
// records one in the same transaction, so an application's history is
// complete from the migration that introduced it (00014) onward. Rows
// before that are a backfill of created_at/updated_at only.
type ApplicationStatusChange struct {
	ID            int64
	ApplicationID int64
	Status        ApplicationStatus
	ChangedBy     StatusChangedBy
	ChangedAt     time.Time
}

// StatusChangedBy says who made an application status change (#174). Like
// ApplicationStatus, Go is the source of truth for the values; the
// changed_by column is plain TEXT.
type StatusChangedBy int

const (
	// StatusChangedByUnknown: recorded before changed_by existed (00018).
	StatusChangedByUnknown StatusChangedBy = iota
	// StatusChangedByUser: the TUI or an MCP tool, on the user's behalf.
	StatusChangedByUser
	// StatusChangedBySync: a sync closing or reopening the posting.
	StatusChangedBySync
)

// statusChangedByNames is the single Go<->DB string mapping for
// StatusChangedBy, indexed by its int value.
var statusChangedByNames = [...]string{
	StatusChangedByUnknown: "unknown",
	StatusChangedByUser:    "user",
	StatusChangedBySync:    "sync",
}

func (c StatusChangedBy) String() string {
	if c < 0 || int(c) >= len(statusChangedByNames) {
		return fmt.Sprintf("StatusChangedBy(%d)", int(c))
	}
	return statusChangedByNames[c]
}

// parseStatusChangedBy converts a changed_by column value, failing loudly
// on one it doesn't know.
func parseStatusChangedBy(s string) (StatusChangedBy, error) {
	for i, name := range statusChangedByNames {
		if name == s {
			return StatusChangedBy(i), nil
		}
	}
	return 0, fmt.Errorf("store: unknown status changed_by %q", s)
}

// recordApplicationStatus appends a status history row. It takes the
// caller's transaction-bound queries because the row must commit or roll
// back with the status change it records; every path that writes
// applications.status goes through here so they can't drift apart.
func recordApplicationStatus(ctx context.Context, qtx *db.Queries, applicationID int64, status ApplicationStatus, changedBy StatusChangedBy) error {
	if err := qtx.CreateApplicationStatusHistory(ctx, db.CreateApplicationStatusHistoryParams{
		ApplicationID: applicationID,
		Status:        status.String(),
		ChangedBy:     changedBy.String(),
	}); err != nil {
		return fmt.Errorf("store: record application %d status %s: %w", applicationID, status, err)
	}
	return nil
}

// ListApplicationStatusHistory returns an application's status changes,
// oldest first.
func (s *Store) ListApplicationStatusHistory(ctx context.Context, applicationID int64) ([]ApplicationStatusChange, error) {
	rows, err := s.queries.ListApplicationStatusHistory(ctx, applicationID)
	if err != nil {
		return nil, err
	}
	history := make([]ApplicationStatusChange, len(rows))
	for i, row := range rows {
		status, err := ParseApplicationStatus(row.Status)
		if err != nil {
			return nil, err
		}
		changedBy, err := parseStatusChangedBy(row.ChangedBy)
		if err != nil {
			return nil, err
		}
		history[i] = ApplicationStatusChange{
			ID:            row.ID,
			ApplicationID: row.ApplicationID,
			Status:        status,
			ChangedBy:     changedBy,
			ChangedAt:     row.ChangedAt,
		}
	}
	return history, nil
}

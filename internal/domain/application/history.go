package application

import (
	"fmt"
	"time"
)

// StatusChange is one status an application entered, and when.
// Append-only: every write path that sets applications.status records one
// in the same transaction, so an application's history is complete from
// the migration that introduced it (00014) onward. Rows before that are a
// backfill of created_at/updated_at only.
type StatusChange struct {
	ID            int64
	ApplicationID int64
	Status        Status
	ChangedBy     ChangedBy
	ChangedAt     time.Time
}

// ChangedBy says who made an application status change. Like
// Status, Go is the source of truth for the values; the
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

// ParseChangedBy converts a changed_by column value, failing loudly on one
// it doesn't know.
func ParseChangedBy(s string) (ChangedBy, error) {
	for i, name := range changedByNames {
		if name == s {
			return ChangedBy(i), nil
		}
	}
	return 0, fmt.Errorf("application: unknown status changed_by %q", s)
}

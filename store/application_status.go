package store

import (
	"database/sql"

	"github.com/dklassen/swamp/applicationstatus"
)

// terminalStatusParams converts applicationstatus.Terminal into the
// sqlc.slice('terminal_statuses') argument the queries that exclude
// dead-end applications take.
func terminalStatusParams() []sql.NullString {
	terminal := applicationstatus.Terminal()
	params := make([]sql.NullString, len(terminal))
	for i, status := range terminal {
		params[i] = sql.NullString{String: status.String(), Valid: true}
	}
	return params
}

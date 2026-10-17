// Package change is the log of changes to Swamp's logged tables, which
// other Swamp processes read to learn what changed.
package change

import "time"

// Event is one change to a logged table, as change_events records it. Old
// and New are JSON of the logged columns ("" for an insert's Old and a
// delete's New). Origin is the writing process's "kind:pid", "" for a
// writer outside Swamp.
type Event struct {
	ID     int64
	Table  string
	RowID  int64
	Op     string
	Old    string
	New    string
	Origin string
	At     time.Time
}

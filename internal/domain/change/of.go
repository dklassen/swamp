package change

import "time"

// Of is one write to an entity: the row just before and just after it, read
// in the write's transaction, and when it happened.
type Of[T any] struct {
	Before T
	After  T
	At     time.Time
}

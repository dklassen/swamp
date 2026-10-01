package store

import "errors"

// ErrNotFound is returned when a lookup finds no matching row.
var ErrNotFound = errors.New("store: not found")

// ErrPostingClosed is CreateApplication's error for a posting that's no
// longer listed (#175): its form can't be submitted, and sync only ends an
// application when its posting closes, so one started now would sit at
// application_started for good.
var ErrPostingClosed = errors.New("store: posting is closed")

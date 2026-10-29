package model

import (
	"errors"

	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/sync"
	"github.com/dklassen/swamp/tui/message"
)

// ErrorOf is err as an event carries it. The code is kept apart from the
// text, since text alone can't be matched with errors.Is.
func ErrorOf(err error) message.Error {
	code := message.ErrOther
	switch {
	case errors.Is(err, store.ErrNotFound):
		code = message.ErrNotFound
	case errors.Is(err, sync.ErrSyncInProgress):
		code = message.ErrSyncInProgress
	case errors.Is(err, service.ErrPostingClosed):
		code = message.ErrPostingClosed
	}
	return message.Error{Code: code, Text: err.Error()}
}

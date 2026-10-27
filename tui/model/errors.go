package model

import (
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

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
	}
	return message.Error{Code: code, Text: err.Error()}
}

// ReportErr sends err from src for whoever has a route for it, the banner
// among them.
func ReportErr(src message.Source, err error) tea.Cmd {
	return func() tea.Msg {
		return message.New(src, message.Subject{}, message.OpNone, time.Now().UTC(), ErrorOf(err))
	}
}

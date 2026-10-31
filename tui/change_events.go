package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// changePollInterval is how often the TUI reads the change log. Nothing is
// lost between reads, so this only decides how soon a change appears.
const changePollInterval = 500 * time.Millisecond

// WithChangeLog makes the App show changes other processes make, read
// from the change log after since. origin is this process's own, whose
// rows its commands already sent as events.
func (a *App) WithChangeLog(since int64, origin string) *App {
	a.origin = origin
	a.changes = newChangeReader(a.companyService, a.postingService, a.applicationService, a.documentService, a.scoringService, origin)
	a.after = since
	a.changePoll = changePollInterval
	return a
}

// pollChanges asks for the next tick; nil without a change log.
func (a *App) pollChanges() tea.Cmd {
	if a.changes == nil {
		return nil
	}
	src := message.Source{Instance: a.instance}
	return tea.Tick(a.changePoll, func(at time.Time) tea.Msg {
		return message.New(src, message.Subject{}, message.OpNone, at.UTC(), message.Signal[message.ChangeTick]{})
	})
}

// changeTick reads the log, unless a read is still in flight, and asks
// for the next tick either way.
func (a *App) changeTick(message.Event[message.Signal[message.ChangeTick]]) tea.Cmd {
	if a.readingChanges {
		return a.pollChanges()
	}
	a.readingChanges = true
	return tea.Batch(a.readChanges(), a.pollChanges())
}

// readChanges reads the change log's rows after App's place in it as the
// events they record.
func (a *App) readChanges() tea.Cmd {
	log, changes, src, after := a.changeLogService, a.changes, message.Source{Instance: a.instance}, a.after
	return func() tea.Msg {
		ctx := context.Background()
		failed := func(err error) tea.Msg {
			return message.New(src, message.Subject{}, message.OpNone, time.Now().UTC(), model.ErrorOf(err))
		}
		rows, err := log.After(ctx, after)
		if err != nil {
			return failed(err)
		}
		events, err := changes.changed(ctx, rows)
		if err != nil {
			return failed(err)
		}
		page := message.ChangePage{Events: events, After: after}
		if n := len(rows); n > 0 {
			page.After = rows[n-1].ID
		}
		return message.New(src, message.Subject{}, message.OpNone, time.Now().UTC(), message.Read[message.ChangePage]{Value: page})
	}
}

// changesRead sends each event read to every model with a route for it,
// in the log's order, and moves App's place past them.
func (a *App) changesRead(e message.Event[message.Read[message.ChangePage]]) tea.Cmd {
	a.readingChanges = false
	a.after = e.Payload.Value.After
	var cmds []tea.Cmd
	for _, event := range e.Payload.Value.Events {
		cmds = append(cmds, a.router.Dispatch(event))
	}
	return tea.Batch(cmds...)
}

// changesFailed lets the next tick read from the same place. The banner
// shows the error.
func (a *App) changesFailed(message.Event[message.Error]) tea.Cmd {
	a.readingChanges = false
	return nil
}
